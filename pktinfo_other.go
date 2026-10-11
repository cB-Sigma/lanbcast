// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

//go:build !linux && !darwin && !windows

package lanbcast

import "net"

// Elsewhere (the BSDs, Solaris, ...) the arrival interface of an IPv4
// answer is inferred from its subnet, as in v0.2.

func enablePktinfo(*net.UDPConn, bool) bool { return false }

func pktinfoIndex([]byte, bool) int { return 0 }

const oobSize = 0
