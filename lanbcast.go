// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

// Package lanbcast sends LAN discovery datagrams to the limited broadcast
// address and to the directed broadcast address of every LAN interface,
// and optionally to the IPv6 all-nodes link-local multicast group on every
// LAN interface.
//
// [WriteBroadcast] is the original function and keeps its behaviour.
// [Send] does the same with [Options]: interfaces chosen by name pattern or
// predicate, VPN and tunnel interfaces skipped by default, and link-local
// IPv6 multicast (ff02::1 and the rest of ff02::/16) sent once per
// interface. [Destinations] lists the addresses either would send to.
//
// [Probe] and [ProbeConn] send a probe and pass the answers that arrive on
// the same socket to a callback until a context ends, dropping the
// duplicate answers a device sends when it hears more than one broadcast.
// [Probe] can also send the limited broadcast once per interface, from a
// socket bound to that interface, and over IPv6.
package lanbcast

import (
	"errors"
	"fmt"
	"net"
	"path"
	"strings"
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
//
// WriteBroadcast skips only loopback, point-to-point and non-broadcast
// interfaces, as it did in v0.1. [Send] also skips interfaces named like
// VPN and tunnel interfaces and takes [Options].
func WriteBroadcast(c net.PacketConn, payload []byte, dst *net.UDPAddr) error {
	targets := []*net.UDPAddr{dst}
	if ip4 := dst.IP.To4(); ip4 != nil && ip4.Equal(limitedBroadcast) {
		for _, b := range lanBroadcasts() {
			targets = append(targets, &net.UDPAddr{IP: b, Port: dst.Port})
		}
	}
	return writeAll(c, payload, targets)
}

// Options chooses the interfaces [Send], [Destinations], [Probe] and
// [ProbeConn] use. The zero value uses every interface that is up and not
// a loopback, point-to-point or tunnel interface (see [TunnelNames]).
type Options struct {
	// Interfaces, when not empty, limits sends to interfaces whose name
	// matches one of these patterns (path.Match syntax, compared without
	// regard to case). An interface named here is used even when its name
	// is also on the tunnel list; point-to-point links still need
	// AllowTunnels.
	Interfaces []string

	// Exclude skips interfaces whose name matches one of these patterns
	// (path.Match syntax, compared without regard to case). It wins over
	// Interfaces.
	Exclude []string

	// AllowTunnels turns off the default exclusion of point-to-point
	// interfaces and of the names in [TunnelNames].
	AllowTunnels bool

	// Filter, when set, is asked about every interface that passed the
	// other rules; returning false skips it.
	Filter func(net.Interface) bool
}

// tunnelNames are interface name patterns of VPNs, tunnels and
// peer-to-peer radio links, which do not lead to a LAN that devices sit on.
var tunnelNames = []string{
	"utun*",      // macOS VPN and system tunnels
	"tun*",       // Linux and BSD layer-3 tunnels (OpenVPN, many VPN apps)
	"tap*",       // layer-2 VPN taps
	"wg*",        // WireGuard
	"ppp*",       // PPP and PPTP/L2TP dial-ups
	"ipsec*",     // IPsec virtual interfaces
	"gif*",       // BSD and macOS generic IP tunnels
	"stf*",       // 6to4
	"zt*",        // ZeroTier
	"tailscale*", // Tailscale
	"awdl*",      // Apple Wireless Direct Link (peer-to-peer Wi-Fi)
	"llw*",       // Apple low-latency WLAN companion of awdl
}

// TunnelNames returns the interface name patterns that [Send], [Probe] and
// [ProbeConn] skip unless [Options.AllowTunnels] is set or the interface
// is named in [Options.Interfaces]. The slice is a copy.
func TunnelNames() []string {
	return append([]string(nil), tunnelNames...)
}

// Send sends payload to dst like [WriteBroadcast], choosing interfaces by
// o (nil means the zero Options). dst is expanded as follows:
//
//   - 255.255.255.255: sent there, then to the directed broadcast address
//     of every chosen interface that can broadcast.
//   - an IPv6 link-local multicast address (ff02::/16, for example
//     net.IPv6linklocalallnodes) without a zone: sent once per chosen
//     interface that is multicast-capable and has an IPv6 link-local
//     address, with the interface name as zone. The connection must be an
//     IPv6 socket, such as net.ListenPacket("udp6", "[::]:0").
//   - anything else: sent to dst alone.
//
// It fails only when no send succeeded, with the first error, or when an
// interface pattern in o is malformed.
func Send(c net.PacketConn, payload []byte, dst *net.UDPAddr, o *Options) error {
	targets, err := Destinations(dst, o)
	if err != nil {
		return err
	}
	return writeAll(c, payload, targets)
}

// Destinations returns the addresses [Send] would send to for dst and o,
// in sending order.
func Destinations(dst *net.UDPAddr, o *Options) ([]*net.UDPAddr, error) {
	if dst == nil {
		return nil, errors.New("lanbcast: no destination")
	}
	if o == nil {
		o = &Options{}
	}
	if err := o.check(); err != nil {
		return nil, err
	}
	if !expands(dst) {
		return []*net.UDPAddr{dst}, nil
	}
	return expand(dst, o.choose(hostInterfaces())), nil
}

// expands reports whether dst is a destination Send expands per interface.
func expands(dst *net.UDPAddr) bool {
	if ip4 := dst.IP.To4(); ip4 != nil {
		return ip4.Equal(limitedBroadcast)
	}
	return dst.IP.IsLinkLocalMulticast() && dst.Zone == ""
}

// expand lists the destinations for dst over the chosen interfaces.
func expand(dst *net.UDPAddr, ifs []iface) []*net.UDPAddr {
	if ip4 := dst.IP.To4(); ip4 != nil {
		out := []*net.UDPAddr{dst}
		seen := map[string]bool{}
		for _, ifc := range ifs {
			if ifc.Flags&net.FlagBroadcast == 0 {
				continue
			}
			for _, b := range ifc.broadcasts() {
				if !seen[b.String()] {
					seen[b.String()] = true
					out = append(out, &net.UDPAddr{IP: b, Port: dst.Port})
				}
			}
		}
		return out
	}
	var out []*net.UDPAddr
	for _, ifc := range ifs {
		if ifc.Flags&net.FlagMulticast == 0 || !ifc.hasLinkLocal6() {
			continue
		}
		out = append(out, &net.UDPAddr{IP: dst.IP, Port: dst.Port, Zone: ifc.Name})
	}
	return out
}

// writeAll sends payload to every target. It succeeds when one send did.
func writeAll(c net.PacketConn, payload []byte, targets []*net.UDPAddr) error {
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

// iface is an interface with its addresses, so selection can be tested
// with made-up interfaces.
type iface struct {
	net.Interface
	addrs []net.Addr
}

// hostInterfaces lists this host's interfaces and their addresses. An
// interface whose addresses cannot be read is listed without addresses.
func hostInterfaces() []iface {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil
	}
	out := make([]iface, 0, len(ifs))
	for _, ifc := range ifs {
		addrs, _ := ifc.Addrs()
		out = append(out, iface{Interface: ifc, addrs: addrs})
	}
	return out
}

// broadcasts returns the directed broadcast address of each IPv4 network
// on the interface; /31 and /32 networks have none.
func (ifc iface) broadcasts() []net.IP {
	var out []net.IP
	for _, n := range ifc.nets4() {
		b := make(net.IP, net.IPv4len)
		for i := range b {
			b[i] = n.IP[i] | ^n.Mask[i]
		}
		out = append(out, b)
	}
	return out
}

// nets4 returns the IPv4 networks on the interface that have a broadcast
// address, with 4-byte IP and mask.
func (ifc iface) nets4() []net.IPNet {
	var out []net.IPNet
	for _, a := range ifc.addrs {
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
		out = append(out, net.IPNet{IP: ip4, Mask: mask})
	}
	return out
}

// hasLinkLocal6 reports whether the interface has an IPv6 link-local
// unicast address, without which it cannot send link-local multicast.
func (ifc iface) hasLinkLocal6() bool {
	for _, a := range ifc.addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.To4() == nil && n.IP.IsLinkLocalUnicast() {
			return true
		}
	}
	return false
}

// lanBroadcasts lists the directed broadcast address of each IPv4 network
// on an interface that is up, can broadcast, and is not a loopback or
// point-to-point (VPN) link. It is WriteBroadcast's v0.1 selection.
func lanBroadcasts() []net.IP {
	return legacyBroadcasts(hostInterfaces())
}

func legacyBroadcasts(ifs []iface) []net.IP {
	var out []net.IP
	seen := map[string]bool{}
	for _, ifc := range ifs {
		f := ifc.Flags
		if f&net.FlagUp == 0 || f&net.FlagBroadcast == 0 || f&net.FlagLoopback != 0 || f&net.FlagPointToPoint != 0 {
			continue
		}
		for _, b := range ifc.broadcasts() {
			if !seen[b.String()] {
				seen[b.String()] = true
				out = append(out, b)
			}
		}
	}
	return out
}

// check rejects malformed name patterns.
func (o *Options) check() error {
	for _, p := range append(append([]string(nil), o.Interfaces...), o.Exclude...) {
		if _, err := path.Match(strings.ToLower(p), ""); err != nil {
			return fmt.Errorf("lanbcast: interface pattern %q: %w", p, err)
		}
	}
	return nil
}

// choose returns the interfaces o allows, in the order given. Patterns
// must have passed check.
func (o *Options) choose(ifs []iface) []iface {
	var out []iface
	for _, ifc := range ifs {
		if o.allows(ifc.Interface) {
			out = append(out, ifc)
		}
	}
	return out
}

// allows applies the rules of Options to one interface.
func (o *Options) allows(ifc net.Interface) bool {
	f := ifc.Flags
	if f&net.FlagUp == 0 || f&net.FlagLoopback != 0 {
		return false
	}
	if matchAny(o.Exclude, ifc.Name) {
		return false
	}
	named := len(o.Interfaces) > 0 && matchAny(o.Interfaces, ifc.Name)
	if len(o.Interfaces) > 0 && !named {
		return false
	}
	if !o.AllowTunnels {
		if f&net.FlagPointToPoint != 0 {
			return false
		}
		if !named && matchAny(tunnelNames, ifc.Name) {
			return false
		}
	}
	return o.Filter == nil || o.Filter(ifc)
}

// matchAny reports whether name matches one of the patterns, ignoring
// case (Windows interface names are mixed case).
func matchAny(patterns []string, name string) bool {
	name = strings.ToLower(name)
	for _, p := range patterns {
		if ok, _ := path.Match(strings.ToLower(p), name); ok {
			return true
		}
	}
	return false
}
