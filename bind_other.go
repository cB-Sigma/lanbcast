// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

//go:build !darwin

package lanbcast

import "syscall"

// bindControl adds nothing outside macOS: binding the socket to the
// interface's address is what selects the interface. Linux sends a limited
// broadcast through the interface that owns the socket's source address
// (net/ipv4/route.c, ip_route_output_key_hash_rcu), without the privilege
// SO_BINDTODEVICE may need.
func bindControl(int) func(network, address string, c syscall.RawConn) error {
	return nil
}
