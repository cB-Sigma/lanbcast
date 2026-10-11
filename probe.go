// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package lanbcast

import (
	"bytes"
	"context"
	"errors"
	"net"
	"strconv"
	"sync"
	"time"
)

// Answer is one datagram received after a probe.
type Answer struct {
	// From is the sender. An IPv6 link-local sender carries its zone.
	From *net.UDPAddr
	// Data is the datagram.
	Data []byte
	// Interface names the interface the answer came in on: the one the
	// socket was bound to (ProbeOptions.PerInterface), else the zone of an
	// IPv6 link-local sender, else the interface whose IPv4 subnet holds
	// the sender's address (an inference: overlapping subnets pick the
	// first). It is empty when none applies, for example for a sender
	// outside every local subnet.
	Interface string
}

// ProbeOptions configures [Probe], [ProbeConn] and [Collect]. The zero
// value sends once, over IPv4, through one socket, and drops duplicates.
type ProbeOptions struct {
	// Options chooses the interfaces.
	Options

	// KeepDuplicates passes every datagram to the callback. By default a
	// datagram with the same sender and the same bytes as an earlier one
	// is dropped: a device that hears both the limited and a directed
	// broadcast usually answers twice.
	KeepDuplicates bool

	// Resends is how many more times the probe is sent while waiting for
	// answers, ResendInterval apart (default one second). Broadcasts are
	// lost more easily than unicasts.
	Resends        int
	ResendInterval time.Duration

	// BufferSize is the largest answer read, in bytes (default 65535).
	BufferSize int

	// The fields below apply to Probe and Collect only; ProbeConn uses the
	// caller's socket.

	// LocalPort is the local UDP port to send from and receive on
	// (default: one chosen by the system). Some devices answer to a fixed
	// port rather than to the sender's.
	LocalPort int

	// IPv6 also sends the probe from an IPv6 socket to the all-nodes
	// link-local multicast group ff02::1 on every chosen interface. Many
	// devices that answer vendor broadcast probes speak IPv4 only.
	IPv6 bool

	// PerInterface sends the limited broadcast from one socket per chosen
	// interface, bound to that interface's IPv4 address, instead of once
	// through the routing table. It reaches devices that only hear
	// 255.255.255.255, such as devices without an address in the LAN's
	// subnet, on every interface instead of only the one the routing table
	// picks. The directed broadcast of each interface goes from the same
	// socket. See the README for how each operating system honours it.
	PerInterface bool
}

// Probe sends payload to UDP port port on the LAN and passes each answer
// to handle until ctx ends; it then returns nil. By default it sends over
// IPv4 to 255.255.255.255 and to every chosen interface's directed
// broadcast address from one socket; o can add IPv6 and per-interface
// sockets. handle is called from Probe's goroutine, one answer at a time.
// A ctx without a deadline must be cancelled to stop Probe.
//
// Probe fails when no socket could be opened, when no send succeeded, or
// when a socket read fails for a reason other than ctx ending.
func Probe(ctx context.Context, payload []byte, port int, o *ProbeOptions, handle func(Answer)) error {
	if o == nil {
		o = &ProbeOptions{}
	}
	if err := o.check(); err != nil {
		return err
	}
	socks, err := openSockets(ctx, port, o)
	defer func() {
		for _, s := range socks {
			s.c.Close()
		}
	}()
	if len(socks) == 0 {
		if err == nil {
			err = errors.New("lanbcast: no interface to probe")
		}
		return err
	}
	return run(ctx, socks, payload, o, handle, false)
}

// ProbeConn is [Probe] over the caller's socket c: it sends payload to dst
// with [Send] and passes each answer read from c to handle until ctx ends.
// The IPv6, PerInterface and LocalPort fields of o are not used; for IPv6,
// pass an IPv6 socket and an ff02::/16 destination. ProbeConn sets c's
// read deadline while it runs and clears it on return; it does not close
// c.
func ProbeConn(ctx context.Context, c net.PacketConn, payload []byte, dst *net.UDPAddr, o *ProbeOptions, handle func(Answer)) error {
	if o == nil {
		o = &ProbeOptions{}
	}
	targets, err := Destinations(dst, &o.Options)
	if err != nil {
		return err
	}
	return run(ctx, []sock{{c: c, targets: targets}}, payload, o, handle, true)
}

// Collect runs [Probe] and returns the answers in the order they arrived.
func Collect(ctx context.Context, payload []byte, port int, o *ProbeOptions) ([]Answer, error) {
	var out []Answer
	err := Probe(ctx, payload, port, o, func(a Answer) { out = append(out, a) })
	return out, err
}

// sock is one socket of a probe and where it sends.
type sock struct {
	c       net.PacketConn
	targets []*net.UDPAddr
	ifname  string // set when the socket is bound to one interface
}

// openSockets opens the sockets o asks for. It returns those it could
// open and the first error.
func openSockets(ctx context.Context, port int, o *ProbeOptions) ([]sock, error) {
	var socks []sock
	var first error
	keep := func(err error) {
		if first == nil {
			first = err
		}
	}
	lport := strconv.Itoa(o.LocalPort)
	limited := &net.UDPAddr{IP: net.IPv4bcast, Port: port}
	ifs := o.choose(hostInterfaces())
	if o.PerInterface {
		for _, ifc := range ifs {
			nets := ifc.nets4()
			if ifc.Flags&net.FlagBroadcast == 0 || len(nets) == 0 {
				continue
			}
			lc := net.ListenConfig{Control: bindControl(ifc.Index)}
			c, err := lc.ListenPacket(ctx, "udp4", net.JoinHostPort(nets[0].IP.String(), lport))
			if err != nil {
				keep(err)
				continue
			}
			targets := []*net.UDPAddr{limited}
			for _, b := range ifc.broadcasts() {
				targets = append(targets, &net.UDPAddr{IP: b, Port: port})
			}
			socks = append(socks, sock{c: c, targets: targets, ifname: ifc.Name})
		}
	} else {
		var lc net.ListenConfig
		c, err := lc.ListenPacket(ctx, "udp4", ":"+lport)
		if err != nil {
			keep(err)
		} else {
			socks = append(socks, sock{c: c, targets: expand(limited, ifs)})
		}
	}
	if o.IPv6 {
		targets := expand(&net.UDPAddr{IP: net.IPv6linklocalallnodes, Port: port}, ifs)
		if len(targets) > 0 {
			var lc net.ListenConfig
			c, err := lc.ListenPacket(ctx, "udp6", "[::]:"+lport)
			if err != nil {
				keep(err)
			} else {
				socks = append(socks, sock{c: c, targets: targets})
			}
		}
	}
	return socks, first
}

// run sends the probe from every socket, reads answers until ctx ends and
// passes them to handle. With restore it clears the read deadlines it set
// before returning.
func run(ctx context.Context, socks []sock, payload []byte, o *ProbeOptions, handle func(Answer), restore bool) error {
	if err := sendAll(socks, payload); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	if restore {
		defer func() {
			for _, s := range socks {
				_ = s.c.SetReadDeadline(time.Time{})
			}
		}()
	}
	deadline, _ := ctx.Deadline() // zero when there is none
	for _, s := range socks {
		if err := s.c.SetReadDeadline(deadline); err != nil {
			return err
		}
	}
	// Ending ctx wakes every blocked read.
	stopped := make(chan struct{})
	go func() {
		<-ctx.Done()
		for _, s := range socks {
			_ = s.c.SetReadDeadline(time.Unix(1, 0))
		}
		close(stopped)
	}()
	defer func() { cancel(); <-stopped }()

	if o.Resends > 0 {
		go resend(ctx, socks, payload, o.Resends, o.ResendInterval)
	}

	answers := make(chan Answer, 64)
	errs := make(chan error, len(socks))
	size := o.BufferSize
	if size <= 0 {
		size = 65535
	}
	var wg sync.WaitGroup
	for _, s := range socks {
		wg.Add(1)
		go func(s sock) {
			defer wg.Done()
			if err := read(ctx, s, size, answers); err != nil {
				errs <- err
			}
		}(s)
	}
	go func() { wg.Wait(); close(answers) }()

	host := hostInterfaces()
	local := localIPs(host)
	seen := map[string]bool{}
	for a := range answers {
		if bytes.Equal(a.Data, payload) && local[a.From.IP.String()] {
			continue // our own probe, looped back
		}
		if a.Interface == "" {
			a.Interface = subnetInterface(a.From.IP, host)
		}
		if !o.KeepDuplicates {
			key := a.From.String() + "\x00" + string(a.Data)
			if seen[key] {
				continue
			}
			seen[key] = true
		}
		handle(a)
	}
	select {
	case err := <-errs:
		return err
	default:
		return nil
	}
}

// sendAll sends the probe from every socket. It succeeds when one socket
// sent it.
func sendAll(socks []sock, payload []byte) error {
	var first error
	sent := false
	for _, s := range socks {
		if err := writeAll(s.c, payload, s.targets); err != nil {
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
		first = errors.New("lanbcast: no address to probe")
	}
	return first
}

// resend sends the probe again n times, every interval, until ctx ends.
func resend(ctx context.Context, socks []sock, payload []byte, n int, interval time.Duration) {
	if interval <= 0 {
		interval = time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for i := 0; i < n; i++ {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			for _, s := range socks {
				_ = writeAll(s.c, payload, s.targets)
			}
		}
	}
}

// read passes the datagrams arriving on s to answers until ctx ends. It
// returns an error only for a failure that is not ctx ending.
func read(ctx context.Context, s sock, size int, answers chan<- Answer) error {
	buf := make([]byte, size)
	for {
		n, from, err := s.c.ReadFrom(buf)
		if err != nil {
			var ne net.Error
			if ctx.Err() != nil || (errors.As(err, &ne) && ne.Timeout()) {
				return nil
			}
			return err
		}
		ua, ok := from.(*net.UDPAddr)
		if !ok {
			continue
		}
		a := Answer{From: ua, Data: append([]byte(nil), buf[:n]...), Interface: s.ifname}
		if a.Interface == "" {
			a.Interface = ua.Zone
		}
		select {
		case answers <- a:
		case <-ctx.Done():
			return nil
		}
	}
}

// localIPs returns the host's addresses, to recognise a probe that looped
// back.
func localIPs(host []iface) map[string]bool {
	out := map[string]bool{}
	for _, ifc := range host {
		for _, a := range ifc.addrs {
			if n, ok := a.(*net.IPNet); ok {
				out[n.IP.String()] = true
			}
		}
	}
	return out
}

// subnetInterface names the first interface that is up and not a loopback
// whose IPv4 subnet holds ip, or returns "".
func subnetInterface(ip net.IP, host []iface) string {
	ip4 := ip.To4()
	if ip4 == nil {
		return ""
	}
	for _, ifc := range host {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		for _, a := range ifc.addrs {
			if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil && n.Contains(ip4) {
				return ifc.Name
			}
		}
	}
	return ""
}
