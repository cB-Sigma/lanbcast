// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package lanbcast

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// device listens on a UDP socket and answers every datagram it receives
// with the given replies, counting what it heard.
type device struct {
	c     net.PacketConn
	heard atomic.Int32
}

func newDevice(t *testing.T, network, addr string, replies ...string) *device {
	t.Helper()
	c, err := net.ListenPacket(network, addr)
	if err != nil {
		t.Skipf("cannot listen on %s %s: %v", network, addr, err)
	}
	d := &device{c: c}
	t.Cleanup(func() { c.Close() })
	go func() {
		buf := make([]byte, 2048)
		for {
			_, from, err := c.ReadFrom(buf)
			if err != nil {
				return
			}
			d.heard.Add(1)
			for _, r := range replies {
				_, _ = c.WriteTo([]byte(r), from)
			}
		}
	}()
	return d
}

func (d *device) port() int { return d.c.LocalAddr().(*net.UDPAddr).Port }

func loopbackConn(t *testing.T) net.PacketConn {
	t.Helper()
	c, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("no IPv4 loopback: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// ProbeConn sends, reads answers until the context ends, drops identical
// copies unless asked to keep them, and leaves the socket open without a
// read deadline.
func TestProbeConnAnswersAndDuplicates(t *testing.T) {
	d := newDevice(t, "udp4", "127.0.0.1:0", "on", "on", "off")
	dst := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: d.port()}

	for _, keep := range []bool{false, true} {
		c := loopbackConn(t)
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		var got []string
		err := ProbeConn(ctx, c, []byte("probe"), dst, &ProbeOptions{KeepDuplicates: keep}, func(a Answer) {
			if a.From.Port != d.port() {
				t.Errorf("answer from %v", a.From)
			}
			got = append(got, string(a.Data))
		})
		cancel()
		if err != nil {
			t.Fatalf("ProbeConn: %v", err)
		}
		want := "on,off"
		if keep {
			want = "on,on,off"
		}
		if strings.Join(got, ",") != want {
			t.Errorf("keep=%v: answers %v, want %s", keep, got, want)
		}
		// Still open, and no deadline left behind.
		if _, err := c.WriteTo([]byte("probe"), dst); err != nil {
			t.Errorf("socket closed: %v", err)
		}
		_ = c.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		if _, _, err := c.ReadFrom(make([]byte, 16)); err != nil {
			t.Errorf("read after ProbeConn: %v", err)
		}
	}
}

// Resends send the probe again while waiting.
func TestProbeConnResends(t *testing.T) {
	d := newDevice(t, "udp4", "127.0.0.1:0", "on")
	c := loopbackConn(t)
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	n := 0
	err := ProbeConn(ctx, c, []byte("probe"), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: d.port()},
		&ProbeOptions{Resends: 2, ResendInterval: 50 * time.Millisecond}, func(Answer) { n++ })
	if err != nil {
		t.Fatal(err)
	}
	if h := d.heard.Load(); h != 3 {
		t.Errorf("device heard %d probes, want 3", h)
	}
	if n != 1 {
		t.Errorf("%d answers passed, want 1 (the rest are duplicates)", n)
	}
}

// A probe sent to our own socket comes back from a local address with the
// same bytes and is not an answer.
func TestProbeConnDropsOwnProbe(t *testing.T) {
	c := loopbackConn(t)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	n := 0
	if err := ProbeConn(ctx, c, []byte("probe"), c.LocalAddr().(*net.UDPAddr), nil, func(Answer) { n++ }); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("own probe passed as %d answers", n)
	}
}

// Cancelling the context stops a probe that has no deadline.
func TestProbeConnCancel(t *testing.T) {
	c := loopbackConn(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- ProbeConn(ctx, c, []byte("probe"), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9}, nil, func(Answer) {})
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("cancelled probe: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ProbeConn did not stop on cancel")
	}
}

// failConn refuses every send.
type failConn struct{ net.PacketConn }

func (failConn) WriteTo([]byte, net.Addr) (int, error) {
	return 0, errors.New("sendto: network is unreachable")
}

// A probe that could not be sent fails at once.
func TestProbeConnSendFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	err := ProbeConn(ctx, failConn{}, []byte("probe"), &net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 1}, nil, func(Answer) {})
	if err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Errorf("err = %v", err)
	}
	if err := ProbeConn(ctx, failConn{}, nil, &net.UDPAddr{IP: net.IPv4bcast, Port: 1}, &ProbeOptions{Options: Options{Exclude: []string{"["}}}, nil); err == nil {
		t.Error("bad pattern accepted")
	}
}

// On this host: Probe reaches a device listening on all addresses through
// the LAN broadcast, the per-interface sockets and IPv6 all-nodes
// multicast. Each part skips when this host has no such interface or does
// not deliver its own broadcasts or multicasts back to itself.
func TestProbeOnHost(t *testing.T) {
	if testing.Short() {
		t.Skip("uses the host's network")
	}
	var def Options
	ifs := def.choose(hostInterfaces())
	if len(expand(&net.UDPAddr{IP: net.IPv4bcast}, ifs)) < 2 {
		t.Skip("no IPv4 LAN interface")
	}
	d := newDevice(t, "udp4", ":0", "here")

	for _, per := range []bool{false, true} {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		answers, err := Collect(ctx, []byte("probe"), d.port(), &ProbeOptions{PerInterface: per})
		cancel()
		if err != nil {
			t.Errorf("PerInterface=%v: %v", per, err)
			continue
		}
		if len(answers) == 0 {
			t.Logf("PerInterface=%v: no answer; this host does not loop its broadcasts back", per)
			continue
		}
		for _, a := range answers {
			if string(a.Data) != "here" || a.From.Port != d.port() {
				t.Errorf("PerInterface=%v: answer %+v", per, a)
			}
			if per && a.Interface == "" {
				t.Errorf("per-interface answer without interface: %+v", a)
			}
		}
		t.Logf("PerInterface=%v: %d answers, device heard %d", per, len(answers), d.heard.Load())
	}

	d6 := newDevice(t, "udp6", "[::]:0", "here6")
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	answers, err := Collect(ctx, []byte("probe6"), d6.port(), &ProbeOptions{IPv6: true})
	if err != nil {
		t.Fatalf("IPv6 probe: %v", err)
	}
	got6 := false
	for _, a := range answers {
		if string(a.Data) == "here6" && a.From.IP.To4() == nil {
			got6 = true
			if a.From.IP.IsLinkLocalUnicast() && a.Interface == "" {
				t.Errorf("link-local answer without interface: %+v", a)
			}
		}
	}
	t.Logf("IPv6: answer received %v (device heard %d)", got6, d6.heard.Load())
}
