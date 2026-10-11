// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package lanbcast_test

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/cB-Sigma/lanbcast"
)

// recorder is a net.PacketConn that records where datagrams were sent.
type recorder struct {
	net.PacketConn
	sent []string
}

func (r *recorder) WriteTo(b []byte, a net.Addr) (int, error) {
	r.sent = append(r.sent, a.String())
	return len(b), nil
}

// Send a discovery probe to the limited broadcast address. It goes there
// first, then to the directed broadcast address of each LAN interface of
// this host (how many depends on the host). Any other destination is sent
// to alone. In real use the connection is a UDP socket from
// net.ListenPacket("udp4", ":0").
func ExampleWriteBroadcast() {
	conn := &recorder{}
	probe := []byte(`{"method":"getPilot","params":{}}`)

	err := lanbcast.WriteBroadcast(conn, probe, &net.UDPAddr{IP: net.IPv4bcast, Port: 38899})
	fmt.Println("first send:", conn.sent[0], err)

	conn.sent = nil
	err = lanbcast.WriteBroadcast(conn, probe, &net.UDPAddr{IP: net.IPv4(192, 168, 7, 255), Port: 38899})
	fmt.Println("explicit address:", conn.sent, err)
	// Output:
	// first send: 255.255.255.255:38899 <nil>
	// explicit address: [192.168.7.255:38899] <nil>
}

// Send with Options limits the interfaces by name. Here no interface
// matches, so only the limited broadcast itself goes out. Tunnel and VPN
// interfaces are skipped unless named or AllowTunnels is set.
func ExampleSend() {
	conn := &recorder{}
	probe := []byte(`{"method":"getPilot","params":{}}`)
	opts := &lanbcast.Options{Interfaces: []string{"no-such-interface"}}

	err := lanbcast.Send(conn, probe, &net.UDPAddr{IP: net.IPv4bcast, Port: 38899}, opts)
	fmt.Println(conn.sent, err)
	// Output:
	// [255.255.255.255:38899] <nil>
}

// Destinations shows where Send would send. An IPv6 link-local multicast
// address without a zone becomes one address per interface; with a zone it
// is used as given.
func ExampleDestinations() {
	zoned := &net.UDPAddr{IP: net.IPv6linklocalallnodes, Port: 5683, Zone: "en0"}
	d, err := lanbcast.Destinations(zoned, nil)
	fmt.Println(d, err)

	// Without a zone: one address per interface, the zone holding the
	// interface name (for example "en0" or "eth0"). How many depends on
	// the host.
	_, err = lanbcast.Destinations(&net.UDPAddr{IP: net.IPv6linklocalallnodes, Port: 5683}, nil)
	fmt.Println(err)
	// Output:
	// [[ff02::1%en0]:5683] <nil>
	// <nil>
}

// ProbeConn sends a probe on the caller's socket and passes each answer to
// a callback until the context ends. Here a device on the loopback
// interface answers twice with the same bytes; the copy is dropped.
func ExampleProbeConn() {
	device, _ := net.ListenPacket("udp4", "127.0.0.1:0")
	defer device.Close()
	go func() {
		buf := make([]byte, 64)
		_, from, err := device.ReadFrom(buf)
		if err == nil {
			device.WriteTo([]byte(`{"mac":"a8bb50000001"}`), from)
			device.WriteTo([]byte(`{"mac":"a8bb50000001"}`), from)
		}
	}()

	conn, _ := net.ListenPacket("udp4", "127.0.0.1:0")
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	err := lanbcast.ProbeConn(ctx, conn, []byte(`{"method":"getPilot"}`), device.LocalAddr().(*net.UDPAddr), nil,
		func(a lanbcast.Answer) { fmt.Println(string(a.Data)) })
	fmt.Println(err)
	// Output:
	// {"mac":"a8bb50000001"}
	// <nil>
}

// Probe opens its own sockets: here one per LAN interface, each bound to
// that interface so the limited broadcast leaves through every LAN, plus
// an IPv6 socket for ff02::1. It needs a network, so it has no output.
func ExampleProbe() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	opts := &lanbcast.ProbeOptions{PerInterface: true, IPv6: true, Resends: 1}
	err := lanbcast.Probe(ctx, []byte(`{"method":"getPilot","params":{}}`), 38899, opts, func(a lanbcast.Answer) {
		fmt.Println(a.From, a.Interface, string(a.Data))
	})
	if err != nil {
		fmt.Println(err)
	}
}

// Collect returns the answers of a probe as a slice.
func ExampleCollect() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	answers, err := lanbcast.Collect(ctx, []byte{0x21, 0x31, 0x00, 0x20}, 54321, nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, a := range answers {
		fmt.Println(a.From.IP)
	}
}
