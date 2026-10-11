// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

// Package lanbcast sends LAN discovery datagrams to the limited broadcast
// address and to the directed broadcast address of every LAN interface.
package lanbcast

import (
	"errors"
	"net"
)

// limitedBroadcast is 255.255.255.255, the address every LAN discovery
// defaults to.
var limitedBroadcast = net.IPv4bcast.To4()

// WriteBroadcast sends one discovery datagram to dst. When dst is the
// limited broadcast address it also goes to the directed broadcast address
// (for example 192.168.0.255) of every LAN interface that is up: a host with
// a VPN or several interfaces may route 255.255.255.255 somewhere it cannot
// be sent (macOS answers "can't assign requested address" while a VPN
// tunnel holds that route), and the directed address still reaches the LAN.
// A device that hears both answers twice; callers already key answers by
// device. It fails only when no send succeeded, with the first error.
func WriteBroadcast(c net.PacketConn, payload []byte, dst *net.UDPAddr) error {
	targets := []*net.UDPAddr{dst}
	if ip4 := dst.IP.To4(); ip4 != nil && ip4.Equal(limitedBroadcast) {
		for _, b := range lanBroadcasts() {
			targets = append(targets, &net.UDPAddr{IP: b, Port: dst.Port})
		}
	}
	var first error
	sent := false
	for _, t := range targets {
		if _, err := c.WriteTo(payload, t); err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		sent = true
	}
	if sent {
		return nil
	}
	if first == nil {
		first = errors.New("no broadcast address to send to")
	}
	return first
}

// lanBroadcasts lists the directed broadcast address of each IPv4 network
// on an interface that is up, can broadcast, and is not a loopback or
// point-to-point (VPN) link.
func lanBroadcasts() []net.IP {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []net.IP
	seen := map[string]bool{}
	for _, ifc := range ifs {
		f := ifc.Flags
		if f&net.FlagUp == 0 || f&net.FlagBroadcast == 0 || f&net.FlagLoopback != 0 || f&net.FlagPointToPoint != 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip4, mask := n.IP.To4(), n.Mask
			if ip4 == nil || len(mask) != net.IPv4len {
				continue
			}
			if ones, _ := mask.Size(); ones >= 31 {
				continue // /31 and /32 have no broadcast address
			}
			b := make(net.IP, net.IPv4len)
			for i := range b {
				b[i] = ip4[i] | ^mask[i]
			}
			if !seen[b.String()] {
				seen[b.String()] = true
				out = append(out, b)
			}
		}
	}
	return out
}
