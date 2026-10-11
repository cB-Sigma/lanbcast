// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package lanbcast_test

import (
	"fmt"
	"net"

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
