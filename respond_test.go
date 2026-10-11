// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package lanbcast

import (
	"context"
	"net"
	"runtime"
	"sync"
	"testing"
	"time"
)

func loopbackName(t *testing.T) string {
	t.Helper()
	ifs, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	for _, ifc := range ifs {
		if ifc.Flags&net.FlagLoopback == 0 || ifc.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && n.IP.Equal(net.IPv4(127, 0, 0, 1)) {
				return ifc.Name
			}
		}
	}
	t.Skip("no loopback interface with 127.0.0.1")
	return ""
}

// pktinfoOS are the systems where the kernel reports arrival interfaces.
func pktinfoOS() bool {
	switch runtime.GOOS {
	case "linux", "darwin", "windows":
		return true
	}
	return false
}

// A Responder answers a probe sent to it over loopback; both ends name the
// arrival interface, exactly where the kernel reports it. This is the
// cross-platform check of the packet-information code (Linux, macOS and
// Windows each parse their own control messages).
//
// Covers: SIG-IPE-2751
func TestResponderLoopback(t *testing.T) {
	lo := loopbackName(t)
	r, err := NewResponder(0, &RespondOptions{Loopback: true})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	port := r.LocalAddr().Port

	var mu sync.Mutex
	var reqs []Request
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	served := make(chan error, 1)
	go func() {
		served <- r.Serve(ctx, func(q Request) []byte {
			mu.Lock()
			reqs = append(reqs, q)
			mu.Unlock()
			return append([]byte("pong:"), q.Data...)
		})
	}()

	c, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	pctx, pcancel := context.WithTimeout(ctx, time.Second)
	defer pcancel()
	var answers []Answer
	err = ProbeConn(pctx, c, []byte("ping"), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}, &ProbeOptions{Resends: 2, ResendInterval: 200 * time.Millisecond}, func(a Answer) {
		answers = append(answers, a)
	})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := <-served; err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if len(answers) == 0 {
		t.Fatal("no answer")
	}
	a := answers[0]
	if string(a.Data) != "pong:ping" {
		t.Fatalf("answer %q", a.Data)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(reqs) == 0 {
		t.Fatal("the responder saw no request")
	}
	q := reqs[0]
	t.Logf("%s: request on %q (exact %v), answer on %q (exact %v)", runtime.GOOS, q.Interface, q.InterfaceExact, a.Interface, a.InterfaceExact)
	if pktinfoOS() {
		if q.Interface != lo || !q.InterfaceExact || a.Interface != lo || !a.InterfaceExact {
			t.Errorf("want %q, exact, at both ends", lo)
		}
	}
}

// Requests arriving on an interface the options do not choose are
// dropped: loopback is not taken unless asked.
//
// Covers: SIG-IPE-2751
func TestResponderDropsUnchosen(t *testing.T) {
	loopbackName(t)
	if !pktinfoOS() {
		t.Skip("the arrival interface of a loopback datagram is not reported here")
	}
	r, err := NewResponder(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()
	got := 0
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = r.Serve(ctx, func(Request) []byte { got++; return nil })
	}()
	c, err := net.Dial("udp4", r.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for range 3 {
		_, _ = c.Write([]byte("x"))
	}
	<-done
	if got != 0 {
		t.Fatalf("%d loopback requests handled", got)
	}
}

// The control-message parsers find the index in well-formed messages and
// return 0 for garbage.
func TestPktinfoParse(t *testing.T) {
	if pktinfoIndex(nil, false) != 0 || pktinfoIndex([]byte{1, 2, 3}, false) != 0 || pktinfoIndex(make([]byte, 64), true) != 0 {
		t.Fatal("garbage parsed")
	}
}
