// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package lanbcast

import "syscall"

// bindControl ties a socket to one interface with IP_BOUND_IF, so that
// 255.255.255.255 leaves through that interface rather than the one the
// routing table picks (which may be a VPN tunnel).
func bindControl(index int) func(network, address string, c syscall.RawConn) error {
	return func(network, address string, c syscall.RawConn) error {
		var serr error
		err := c.Control(func(fd uintptr) {
			serr = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IP, syscall.IP_BOUND_IF, index)
		})
		if err != nil {
			return err
		}
		return serr
	}
}
