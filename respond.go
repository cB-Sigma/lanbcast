// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package lanbcast

import (
	"context"
	"errors"
	"net"
	"strconv"
	"sync"
	"time"
)

// Request is a datagram a [Responder] received.
type Request struct {
	// From is the sender; a reply goes there.
	From *net.UDPAddr
	// Data is the datagram.
	Data []byte
	// Interface names the interface the datagram came in on, as
	// [Answer.Interface] does, with InterfaceExact saying whether it was
	// reported by the kernel (Linux, macOS, Windows) or inferred.
	Interface      string
	InterfaceExact bool
}

// RespondOptions configures a [Responder]. The zero value listens over
// IPv4 on every interface.
type RespondOptions struct {
	// Options, when any of its fields is set, chooses the interfaces
	// requests are taken from (as for sending: tunnels are skipped unless
	// AllowTunnels); a request that arrives on another interface is
	// dropped. The zero value takes requests from every interface but
	// loopback.
	Options

	// Loopback also takes requests that arrive on a loopback interface
	// (a probe sent on the same host, as tests do).
	Loopback bool

	// IPv6 also listens on an IPv6 socket on the same port, for probes
	// sent to ff02::1 or another link-local group. The socket joins no
	// group: ff02::1 reaches every node without joining.
	IPv6 bool

	// Address is the IPv4 address to listen on (default: all). Binding to
	// a unicast address stops broadcasts from arriving on most systems.
	Address net.IP

	// BufferSize is the largest request read, in bytes (default 65535).
	BufferSize int
}

// Responder answers discovery probes: the device side of [Probe]. It is
// what a simulator, an emulated device or a test peer needs: listen on a
// UDP port on every LAN interface, see which interface each probe came in
// on, and answer to the sender from the same socket.
type Responder struct {
	o     RespondOptions
	socks []sock
	host  []iface
	once  sync.Once
}

// NewResponder opens the sockets on UDP port port. Close releases them.
func NewResponder(port int, o *RespondOptions) (*Responder, error) {
	if o == nil {
		o = &RespondOptions{}
	}
	if err := o.check(); err != nil {
		return nil, err
	}
	r := &Responder{o: *o, host: hostInterfaces()}
	addr := ""
	if o.Address != nil {
		addr = o.Address.String()
	}
	c, err := net.ListenPacket("udp4", net.JoinHostPort(addr, strconv.Itoa(port)))
	if err != nil {
		return nil, err
	}
	r.socks = append(r.socks, newSock(c, nil, ""))
	if o.IPv6 {
		c6, err := net.ListenPacket("udp6", net.JoinHostPort("::", strconv.Itoa(port)))
		if err != nil {
			c.Close()
			return nil, err
		}
		r.socks = append(r.socks, newSock(c6, nil, ""))
	}
	return r, nil
}

// LocalAddr is the IPv4 socket's address (with the port chosen by the
// system when port was 0).
func (r *Responder) LocalAddr() *net.UDPAddr { return r.socks[0].c.LocalAddr().(*net.UDPAddr) }

// Close closes the sockets; Serve then returns.
func (r *Responder) Close() error {
	var first error
	r.once.Do(func() {
		for _, s := range r.socks {
			if err := s.c.Close(); err != nil && first == nil {
				first = err
			}
		}
	})
	return first
}

// Serve reads requests until ctx ends or the Responder is closed, and
// calls handle for each, one at a time; a non-empty reply is sent to the
// sender from the socket the request came in on. Serve returns nil when
// ctx ends or Close was called, and the first read error otherwise.
func (r *Responder) Serve(ctx context.Context, handle func(Request) []byte) error {
	size := r.o.BufferSize
	if size <= 0 {
		size = 65535
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stopped := make(chan struct{})
	go func() {
		<-ctx.Done()
		for _, s := range r.socks {
			_ = s.c.SetReadDeadline(time.Unix(1, 0))
		}
		close(stopped)
	}()
	defer func() { cancel(); <-stopped }()

	type got struct {
		a Answer
		s sock
	}
	reqs := make(chan got, 64)
	errs := make(chan error, len(r.socks))
	var wg sync.WaitGroup
	for _, s := range r.socks {
		wg.Add(1)
		go func(s sock) {
			defer wg.Done()
			answers := make(chan Answer, 16)
			done := make(chan struct{})
			go func() {
				defer close(done)
				for a := range answers {
					select {
					case reqs <- got{a, s}:
					case <-ctx.Done():
					}
				}
			}()
			err := read(ctx, s, size, answers)
			close(answers)
			<-done
			if err != nil && !errors.Is(err, net.ErrClosed) {
				errs <- err
			}
		}(s)
	}
	go func() { wg.Wait(); close(reqs) }()

	allowed := r.o.choose(r.host)
	if len(r.o.Interfaces) == 0 && len(r.o.Exclude) == 0 && r.o.Filter == nil && !r.o.AllowTunnels {
		allowed = nil
		for _, ifc := range r.host {
			if ifc.Flags&net.FlagLoopback == 0 {
				allowed = append(allowed, ifc)
			}
		}
	}
	ok := map[string]bool{}
	for _, ifc := range allowed {
		ok[ifc.Name] = true
	}
	for g := range reqs {
		a := g.a
		if a.Interface == "" {
			a.Interface = subnetInterface(a.From.IP, r.host)
		}
		if !r.takes(a, ok) {
			continue
		}
		reply := handle(Request{From: a.From, Data: a.Data, Interface: a.Interface, InterfaceExact: a.InterfaceExact})
		if len(reply) > 0 {
			_, _ = g.s.c.WriteTo(reply, a.From)
		}
	}
	select {
	case err := <-errs:
		return err
	default:
		return nil
	}
}

// takes reports whether a request's arrival interface is one the options
// allow. A request whose interface is unknown is taken.
func (r *Responder) takes(a Answer, ok map[string]bool) bool {
	if a.Interface == "" || ok[a.Interface] {
		return true
	}
	for _, ifc := range r.host {
		if ifc.Name == a.Interface && ifc.Flags&net.FlagLoopback != 0 {
			return r.o.Loopback
		}
	}
	return false
}
