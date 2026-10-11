// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package lanbcast

import (
	"encoding/binary"
	"net"
	"syscall"
	"unsafe"
)

// The arrival interface on Windows: IP_PKTINFO and IPV6_PKTINFO (both 19
// in ws2ipdef.h) make WSARecvMsg, which Go's ReadMsgUDP uses, return an
// IN_PKTINFO (address, then interface index) or IN6_PKTINFO (16-byte
// address, then index). Control messages are WSACMSGHDR (a SIZE_T length,
// an INT level, an INT type), with header and data aligned to the
// pointer size.

const (
	winPktinfo    = 19 // IP_PKTINFO and IPV6_PKTINFO
	ipprotoIP     = 0
	ipprotoIPv6   = 41
	ptrSize       = int(unsafe.Sizeof(uintptr(0)))
	cmsgHeaderLen = ptrSize + 8
)

func enablePktinfo(c *net.UDPConn, v6 bool) bool {
	rc, err := c.SyscallConn()
	if err != nil {
		return false
	}
	level := ipprotoIP
	if v6 {
		level = ipprotoIPv6
	}
	var serr error
	if err := rc.Control(func(fd uintptr) {
		serr = syscall.SetsockoptInt(syscall.Handle(fd), level, winPktinfo, 1)
	}); err != nil {
		return false
	}
	return serr == nil
}

func align(n int) int { return (n + ptrSize - 1) &^ (ptrSize - 1) }

func pktinfoIndex(oob []byte, v6 bool) int {
	level, off := ipprotoIP, 4
	if v6 {
		level, off = ipprotoIPv6, 16
	}
	for len(oob) >= cmsgHeaderLen {
		var l int
		if ptrSize == 8 {
			l = int(binary.LittleEndian.Uint64(oob))
		} else {
			l = int(binary.LittleEndian.Uint32(oob))
		}
		lv := int(int32(binary.LittleEndian.Uint32(oob[ptrSize:])))
		typ := int(int32(binary.LittleEndian.Uint32(oob[ptrSize+4:])))
		if l < cmsgHeaderLen || l > len(oob) {
			return 0
		}
		data := oob[align(cmsgHeaderLen):l]
		if lv == level && typ == winPktinfo && len(data) >= off+4 {
			return int(binary.LittleEndian.Uint32(data[off:]))
		}
		next := align(l)
		if next > len(oob) {
			return 0
		}
		oob = oob[next:]
	}
	return 0
}

const oobSize = 128
