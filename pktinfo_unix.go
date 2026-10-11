// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

//go:build linux || darwin

package lanbcast

import (
	"encoding/binary"
	"net"
	"runtime"
	"syscall"
	"unsafe"
)

// The arrival interface of each datagram, from the packet information the
// kernel attaches when asked: IP_PKTINFO on Linux, IP_RECVPKTINFO on
// macOS (both deliver struct in_pktinfo, whose first field is the
// interface index), and IPV6_RECVPKTINFO (struct in6_pktinfo, the index
// after the 16-byte address). The option values are those of the
// systems' headers.

const (
	recvPktinfo4 = 0x8  // IP_PKTINFO, Linux
	pktinfo4     = 0x8  // the control message type, Linux
	recvPktinfo6 = 0x31 // IPV6_RECVPKTINFO, Linux
	pktinfo6     = 0x32 // IPV6_PKTINFO, Linux

	darwinRecvPktinfo4 = 0x1a // IP_RECVPKTINFO; IP_PKTINFO is the same value
	darwinRecvPktinfo6 = 0x3d // IPV6_RECVPKTINFO
	darwinPktinfo6     = 0x2e // IPV6_PKTINFO
)

func pktinfoOptions(v6 bool) (level, opt, typ int) {
	switch {
	case runtime.GOOS == "darwin" && v6:
		return syscall.IPPROTO_IPV6, darwinRecvPktinfo6, darwinPktinfo6
	case runtime.GOOS == "darwin":
		return syscall.IPPROTO_IP, darwinRecvPktinfo4, darwinRecvPktinfo4
	case v6:
		return syscall.IPPROTO_IPV6, recvPktinfo6, pktinfo6
	}
	return syscall.IPPROTO_IP, recvPktinfo4, pktinfo4
}

// enablePktinfo asks the kernel for each datagram's arrival interface. It
// reports whether that worked.
func enablePktinfo(c *net.UDPConn, v6 bool) bool {
	rc, err := c.SyscallConn()
	if err != nil {
		return false
	}
	level, opt, _ := pktinfoOptions(v6)
	var serr error
	if err := rc.Control(func(fd uintptr) {
		serr = syscall.SetsockoptInt(int(fd), level, opt, 1)
	}); err != nil {
		return false
	}
	return serr == nil
}

// pktinfoIndex returns the arrival interface index from a datagram's
// control messages, or 0.
func pktinfoIndex(oob []byte, v6 bool) int {
	msgs, err := syscall.ParseSocketControlMessage(oob)
	if err != nil {
		return 0
	}
	level, _, typ := pktinfoOptions(v6)
	for _, m := range msgs {
		if int(m.Header.Level) != level || int(m.Header.Type) != typ {
			continue
		}
		off := 0
		if v6 {
			off = 16
		}
		if len(m.Data) < off+4 {
			return 0
		}
		return int(nativeEndian.Uint32(m.Data[off:]))
	}
	return 0
}

var nativeEndian binary.ByteOrder = func() binary.ByteOrder {
	x := uint16(1)
	if *(*byte)(unsafe.Pointer(&x)) == 1 {
		return binary.LittleEndian
	}
	return binary.BigEndian
}()

// oobSize is enough for one packet information message.
const oobSize = 128
