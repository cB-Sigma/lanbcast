// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package lanbcast

import (
	"net"
	"reflect"
	"strings"
	"testing"
)

const (
	up    = net.FlagUp | net.FlagRunning
	lan   = up | net.FlagBroadcast | net.FlagMulticast
	p2p   = up | net.FlagPointToPoint | net.FlagMulticast
	plain = up | net.FlagMulticast
)

func cidr(s string) *net.IPNet {
	ip, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	n.IP = ip
	return n
}

func fakeIf(index int, name string, flags net.Flags, addrs ...string) iface {
	ifc := iface{Interface: net.Interface{Index: index, Name: name, Flags: flags}}
	for _, a := range addrs {
		ifc.addrs = append(ifc.addrs, cidr(a))
	}
	return ifc
}

// A host with a LAN, a container bridge, VPNs of several kinds, Apple's
// peer-to-peer Wi-Fi, a loopback, a down interface and a /31 link.
func fakeHost() []iface {
	return []iface{
		fakeIf(1, "lo0", up|net.FlagLoopback|net.FlagMulticast, "127.0.0.1/8", "::1/128", "fe80::1/64"),
		fakeIf(2, "en0", lan, "192.168.1.10/24", "fe80::10/64", "2001:db8::10/64"),
		fakeIf(3, "docker0", lan, "172.17.0.1/16"),
		fakeIf(4, "utun3", p2p, "fe80::3/64"),
		fakeIf(5, "tap0", lan, "10.8.0.2/24", "fe80::5/64"),
		fakeIf(6, "wg0", plain, "10.9.0.1/24", "fe80::6/64"),
		fakeIf(7, "awdl0", lan, "fe80::7/64"),
		fakeIf(8, "Tailscale", lan, "100.64.0.1/10", "fe80::8/64"),
		fakeIf(9, "eth1", net.FlagBroadcast|net.FlagMulticast, "192.168.50.1/24"),
		fakeIf(10, "en1", lan, "198.51.100.0/31"),
	}
}

func names(ifs []iface) []string {
	var out []string
	for _, i := range ifs {
		out = append(out, i.Name)
	}
	return out
}

func addrs(as []*net.UDPAddr) []string {
	var out []string
	for _, a := range as {
		out = append(out, a.String())
	}
	return out
}

// The default choice skips loopback, down, point-to-point and tunnel-named
// interfaces; Interfaces, Exclude, AllowTunnels and Filter change it.
func TestOptionsChooseInterfaces(t *testing.T) {
	host := fakeHost()
	cases := []struct {
		name string
		o    Options
		want []string
	}{
		{"default", Options{}, []string{"en0", "docker0", "en1"}},
		{"named tunnel is used", Options{Interfaces: []string{"TAP*", "en0"}}, []string{"en0", "tap0"}},
		{"named point-to-point still needs AllowTunnels", Options{Interfaces: []string{"utun*"}}, nil},
		{"exclude wins", Options{Interfaces: []string{"en*"}, Exclude: []string{"en1"}}, []string{"en0"}},
		{"exclude docker", Options{Exclude: []string{"docker*"}}, []string{"en0", "en1"}},
		{"allow tunnels", Options{AllowTunnels: true}, []string{"en0", "docker0", "utun3", "tap0", "wg0", "awdl0", "Tailscale", "en1"}},
		{"filter", Options{Filter: func(i net.Interface) bool { return i.Index != 3 }}, []string{"en0", "en1"}},
	}
	for _, c := range cases {
		if got := names(c.o.choose(host)); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: chose %v, want %v", c.name, got, c.want)
		}
	}
}

// 255.255.255.255 expands to the directed broadcast of every chosen
// interface that can broadcast; ff02::/16 expands to one zoned address per
// chosen multicast interface with an IPv6 link-local address.
func TestExpand(t *testing.T) {
	host := fakeHost()
	var def Options
	limited := &net.UDPAddr{IP: net.IPv4bcast, Port: 38899}
	got := addrs(expand(limited, def.choose(host)))
	want := []string{"255.255.255.255:38899", "192.168.1.255:38899", "172.17.255.255:38899"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("limited: %v, want %v", got, want)
	}

	all := &net.UDPAddr{IP: net.IPv6linklocalallnodes, Port: 5000}
	if got := addrs(expand(all, def.choose(host))); !reflect.DeepEqual(got, []string{"[ff02::1%en0]:5000"}) {
		t.Errorf("ff02::1: %v", got)
	}
	tunnels := Options{AllowTunnels: true}
	got = addrs(expand(&net.UDPAddr{IP: net.ParseIP("ff02::fb"), Port: 5353}, tunnels.choose(host)))
	want = []string{"[ff02::fb%en0]:5353", "[ff02::fb%utun3]:5353", "[ff02::fb%tap0]:5353", "[ff02::fb%wg0]:5353", "[ff02::fb%awdl0]:5353", "[ff02::fb%Tailscale]:5353"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ff02::fb with tunnels: %v, want %v", got, want)
	}
}

// WriteBroadcast keeps its v0.1 choice: tunnel names are not skipped, only
// loopback, point-to-point and non-broadcast interfaces.
func TestWriteBroadcastSelectionUnchanged(t *testing.T) {
	var got []string
	for _, b := range legacyBroadcasts(fakeHost()) {
		got = append(got, b.String())
	}
	want := []string{"192.168.1.255", "172.17.255.255", "10.8.0.255", "100.127.255.255"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("legacy broadcasts %v, want %v", got, want)
	}
}

// Addresses that are not expanded are sent to alone, a zoned multicast
// included; a malformed pattern and a missing destination are errors.
func TestDestinations(t *testing.T) {
	for _, dst := range []*net.UDPAddr{
		{IP: net.IPv4(192, 168, 7, 255), Port: 1},
		{IP: net.IPv4(239, 255, 255, 250), Port: 1900},
		{IP: net.IPv6linklocalallnodes, Port: 1, Zone: "en0"},
		{IP: net.ParseIP("ff05::1"), Port: 1},
		{IP: net.ParseIP("fe80::1"), Port: 1, Zone: "en0"},
	} {
		got, err := Destinations(dst, nil)
		if err != nil || len(got) != 1 || got[0] != dst {
			t.Errorf("Destinations(%v) = %v, %v", dst, got, err)
		}
	}
	if _, err := Destinations(nil, nil); err == nil {
		t.Error("nil destination accepted")
	}
	for _, o := range []*Options{{Interfaces: []string{"["}}, {Exclude: []string{"en["}}} {
		_, err := Destinations(&net.UDPAddr{IP: net.IPv4bcast, Port: 1}, o)
		if err == nil || !strings.Contains(err.Error(), "interface pattern") {
			t.Errorf("bad pattern %+v: %v", o, err)
		}
		if err := Send(&sendLog{refuse: func(*net.UDPAddr) bool { return false }}, nil, &net.UDPAddr{IP: net.IPv4bcast, Port: 1}, o); err == nil {
			t.Errorf("Send accepted bad pattern %+v", o)
		}
	}

	// On this host: the limited broadcast goes first, and nothing chosen
	// is a loopback or tunnel interface.
	got, err := Destinations(&net.UDPAddr{IP: net.IPv4bcast, Port: 9}, nil)
	if err != nil || len(got) == 0 || got[0].String() != "255.255.255.255:9" {
		t.Fatalf("host destinations %v, %v", got, err)
	}
	var def Options
	for _, ifc := range def.choose(hostInterfaces()) {
		if ifc.Flags&(net.FlagLoopback|net.FlagPointToPoint) != 0 || matchAny(tunnelNames, ifc.Name) {
			t.Errorf("chose %s (%v)", ifc.Name, ifc.Flags)
		}
	}
}

// Send succeeds when one send went out and otherwise returns the first
// error, like WriteBroadcast.
func TestSendErrors(t *testing.T) {
	refused := &sendLog{refuse: func(*net.UDPAddr) bool { return true }}
	if err := Send(refused, []byte("x"), &net.UDPAddr{IP: net.IPv4(10, 0, 0, 255), Port: 1}, nil); err == nil {
		t.Error("refused send reported success")
	}
	open := &sendLog{refuse: func(*net.UDPAddr) bool { return false }}
	if err := Send(open, []byte("x"), &net.UDPAddr{IP: net.IPv4(10, 0, 0, 255), Port: 1}, &Options{}); err != nil || len(open.sent) != 1 {
		t.Errorf("send = %v, %v", open.sent, err)
	}
}

// An IPv4 sender's interface is the one whose subnet holds its address;
// loopback, down interfaces, IPv6 senders and strangers get none.
func TestSubnetInterface(t *testing.T) {
	host := fakeHost()
	for ip, want := range map[string]string{
		"192.168.1.77":   "en0",
		"172.17.3.4":     "docker0",
		"10.8.0.9":       "tap0",
		"192.168.50.9":   "", // eth1 is down
		"127.0.0.1":      "",
		"203.0.113.5":    "",
		"fe80::1234":     "",
		"198.51.100.1":   "en1",
		"100.100.100.10": "Tailscale",
	} {
		if got := subnetInterface(net.ParseIP(ip), host); got != want {
			t.Errorf("subnetInterface(%s) = %q, want %q", ip, got, want)
		}
	}
}

// TunnelNames hands out a copy.
func TestTunnelNamesCopy(t *testing.T) {
	n := TunnelNames()
	n[0] = "en*"
	if tunnelNames[0] == "en*" {
		t.Error("TunnelNames exposed the package list")
	}
}
