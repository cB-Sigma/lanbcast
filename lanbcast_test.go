// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package lanbcast

import (
	"errors"
	"net"
	"testing"
)

// sendLog is a PacketConn that records sends and refuses some addresses.
type sendLog struct {
	net.PacketConn
	refuse func(*net.UDPAddr) bool
	sent   []string
}

func (s *sendLog) WriteTo(b []byte, a net.Addr) (int, error) {
	ua := a.(*net.UDPAddr)
	if s.refuse(ua) {
		return 0, errors.New("sendto: can't assign requested address")
	}
	s.sent = append(s.sent, ua.String())
	return len(b), nil
}

// A limited broadcast that the host cannot send still reaches the LAN
// through each interface's directed broadcast address; it fails only when
// nothing went out, and an explicit address is sent to alone.
//
// Covers: SIG-ONB-031
func TestWriteBroadcastFallsBackToLANs(t *testing.T) {
	lans := lanBroadcasts()
	for _, b := range lans {
		if b.Equal(limitedBroadcast) || b.IsLoopback() {
			t.Errorf("lanBroadcasts offered %v", b)
		}
	}
	limited := &net.UDPAddr{IP: net.IPv4bcast, Port: 38899}
	blocked := &sendLog{refuse: func(a *net.UDPAddr) bool { return a.IP.Equal(net.IPv4bcast) }}
	err := WriteBroadcast(blocked, []byte("x"), limited)
	if len(lans) == 0 {
		if err == nil {
			t.Fatal("no LAN and a refused broadcast reported success")
		}
	} else if err != nil || len(blocked.sent) != len(lans) {
		t.Fatalf("refused limited broadcast: err %v, sent %v, LANs %v", err, blocked.sent, lans)
	}

	all := &sendLog{refuse: func(*net.UDPAddr) bool { return true }}
	if err := WriteBroadcast(all, []byte("x"), limited); err == nil || err.Error() != "sendto: can't assign requested address" {
		t.Errorf("every send refused = %v", err)
	}

	open := &sendLog{refuse: func(*net.UDPAddr) bool { return false }}
	if err := WriteBroadcast(open, []byte("x"), limited); err != nil || len(open.sent) != 1+len(lans) || open.sent[0] != "255.255.255.255:38899" {
		t.Errorf("limited broadcast sends = %v %v", open.sent, err)
	}

	given := &sendLog{refuse: func(*net.UDPAddr) bool { return false }}
	if err := WriteBroadcast(given, []byte("x"), &net.UDPAddr{IP: net.IPv4(192, 168, 7, 255), Port: 9999}); err != nil || len(given.sent) != 1 {
		t.Errorf("explicit address sends = %v %v", given.sent, err)
	}
}
