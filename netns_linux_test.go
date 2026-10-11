// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package lanbcast

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The Linux multi-LAN test builds, with network namespaces and veth pairs,
// a host with three LANs and a VPN-like route, and runs Probe against
// Responders in the other namespaces:
//
//	lanA 10.71.1.1/24 <-> devA 10.71.1.2/24      (namespace lbA)
//	lanB 10.71.2.1/24 <-> devB 10.71.2.2/24      (namespace lbB)
//	lanC 10.71.3.1/24 <-> devC 192.168.250.9/24  (namespace lbC: a device
//	                                              with no address in the
//	                                              LAN's subnet)
//	vpn0 (dummy) holds the route to 255.255.255.255
//
// It needs root and the ip command, so it runs only with SIGMA_NETNS=1,
// in a privileged container:
//
//	docker build -t sigma-lanbcast-netns testdata/netns
//	docker run --rm --privileged -v "$PWD":/src sigma-lanbcast-netns
//
// TestNetnsHelperResponder is the responder each namespace runs (the test
// binary started again inside the namespace).

const netnsPort = 47011

func TestNetnsHelperResponder(t *testing.T) {
	spec := os.Getenv("LANBCAST_RESPONDER")
	if spec == "" {
		t.Skip("helper: run by TestNetnsMultiLAN only")
	}
	name, _, _ := strings.Cut(spec, ",")
	r, err := NewResponder(netnsPort, &RespondOptions{IPv6: true})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	fmt.Println("ready")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = r.Serve(ctx, func(q Request) []byte {
		if string(q.Data) != "probe" {
			return nil
		}
		return []byte(fmt.Sprintf("%s|%s|%v", name, q.Interface, q.InterfaceExact))
	})
}

func ipCmd(t *testing.T, args ...string) {
	t.Helper()
	out, err := exec.Command("ip", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("ip %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// Covers: SIG-IPE-2751
func TestNetnsMultiLAN(t *testing.T) {
	if os.Getenv("SIGMA_NETNS") != "1" || os.Geteuid() != 0 {
		t.Skip("SIGMA_NETNS=1 and root needed (see the comment above)")
	}
	if out, err := exec.Command("sysctl", "-w", "net.ipv6.conf.default.accept_dad=0").CombinedOutput(); err != nil {
		t.Logf("sysctl: %v %s", err, out)
	}
	type lan struct{ ns, host, dev, hostAddr, devAddr, devRoute string }
	lans := []lan{
		{"lbA", "lanA", "devA", "10.71.1.1/24", "10.71.1.2/24", ""},
		{"lbB", "lanB", "devB", "10.71.2.1/24", "10.71.2.2/24", ""},
		{"lbC", "lanC", "devC", "10.71.3.1/24", "192.168.250.9/24", "devC"},
	}
	for _, l := range lans {
		ipCmd(t, "netns", "add", l.ns)
		ns := l.ns
		t.Cleanup(func() { _ = exec.Command("ip", "netns", "del", ns).Run() })
		ipCmd(t, "link", "add", l.host, "type", "veth", "peer", "name", l.dev)
		ipCmd(t, "link", "set", l.dev, "netns", l.ns)
		ipCmd(t, "addr", "add", l.hostAddr, "dev", l.host)
		ipCmd(t, "link", "set", l.host, "up")
		ipCmd(t, "-n", l.ns, "addr", "add", l.devAddr, "dev", l.dev)
		ipCmd(t, "-n", l.ns, "link", "set", l.dev, "up")
		ipCmd(t, "-n", l.ns, "link", "set", "lo", "up")
		if l.devRoute != "" {
			ipCmd(t, "-n", l.ns, "route", "add", "default", "dev", l.devRoute)
		}
	}
	ipCmd(t, "link", "add", "vpn0", "type", "dummy")
	t.Cleanup(func() { _ = exec.Command("ip", "link", "del", "vpn0").Run() })
	ipCmd(t, "link", "set", "vpn0", "up")
	ipCmd(t, "route", "add", "255.255.255.255/32", "dev", "vpn0")

	for _, l := range lans {
		cmd := exec.Command("ip", "netns", "exec", l.ns, os.Args[0], "-test.run", "^TestNetnsHelperResponder$", "-test.v")
		cmd.Env = append(os.Environ(), "LANBCAST_RESPONDER="+l.ns)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
		ready := make(chan bool, 1)
		go func() {
			sc := bufio.NewScanner(stdout)
			for sc.Scan() {
				if sc.Text() == "ready" {
					ready <- true
				}
			}
		}()
		select {
		case <-ready:
		case <-time.After(10 * time.Second):
			t.Fatalf("%s: responder not ready", l.ns)
		}
	}
	time.Sleep(1500 * time.Millisecond) // IPv6 link-local addresses settle

	probe := func(o *ProbeOptions) map[string]Answer {
		ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
		defer cancel()
		got := map[string]Answer{}
		err := Probe(ctx, []byte("probe"), netnsPort, o, func(a Answer) {
			key := string(a.Data)
			if a.From.IP.To4() == nil {
				key = "v6:" + key
			}
			got[key] = a
		})
		if err != nil {
			t.Fatalf("Probe: %v", err)
		}
		var keys []string
		for k, a := range got {
			keys = append(keys, k+" from "+a.From.String()+" on "+a.Interface+" exact="+strconv.FormatBool(a.InterfaceExact))
		}
		sort.Strings(keys)
		t.Logf("%+v:\n  %s", *o, strings.Join(keys, "\n  "))
		return got
	}
	has := func(got map[string]Answer, key, iface string) bool {
		a, ok := got[key]
		return ok && a.Interface == iface && a.InterfaceExact
	}

	// One socket: 255.255.255.255 goes to vpn0, so only the directed
	// broadcasts arrive; the device without a subnet address is missed.
	one := probe(&ProbeOptions{})
	if !has(one, "lbA|devA|true", "lanA") || !has(one, "lbB|devB|true", "lanB") {
		t.Error("directed broadcasts did not reach both LANs, with exact interfaces at both ends")
	}
	for k := range one {
		if strings.HasPrefix(k, "lbC|") {
			t.Error("the VPN route did not take the limited broadcast (test setup)")
		}
	}

	// PerInterface: the limited broadcast leaves through each LAN, so
	// the device without a subnet address answers too.
	per := probe(&ProbeOptions{PerInterface: true})
	for _, w := range []struct{ key, iface string }{{"lbA|devA|true", "lanA"}, {"lbB|devB|true", "lanB"}, {"lbC|devC|true", "lanC"}} {
		if !has(per, w.key, w.iface) {
			t.Errorf("PerInterface: no exact answer %s on %s", w.key, w.iface)
		}
	}

	// IPv6: ff02::1 on every interface, answers named by zone and kernel.
	v6 := probe(&ProbeOptions{IPv6: true, Options: Options{Interfaces: []string{"lan*"}}})
	for _, w := range []struct{ key, iface string }{{"v6:lbA|devA|true", "lanA"}, {"v6:lbB|devB|true", "lanB"}, {"v6:lbC|devC|true", "lanC"}} {
		if !has(v6, w.key, w.iface) {
			t.Errorf("IPv6: no exact answer %s on %s", w.key, w.iface)
		}
	}
}
