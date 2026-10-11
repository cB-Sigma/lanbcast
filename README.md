# lanbcast

Sends a LAN discovery datagram to the limited broadcast address and to the directed broadcast address of every IPv4 LAN interface, in Go.

[![Go Reference](https://pkg.go.dev/badge/github.com/cB-Sigma/lanbcast.svg)](https://pkg.go.dev/github.com/cB-Sigma/lanbcast) [![test](https://github.com/cB-Sigma/lanbcast/actions/workflows/test.yml/badge.svg)](https://github.com/cB-Sigma/lanbcast/actions/workflows/test.yml) [![Licence: Apache-2.0](https://img.shields.io/badge/licence-Apache--2.0-blue.svg)](LICENSE) [![Go](https://img.shields.io/badge/go-1.22-00ADD8.svg)](go.mod)

A host with a VPN or several interfaces may route 255.255.255.255
somewhere it cannot be sent (macOS answers "can't assign requested
address" while a VPN tunnel holds that route). The directed broadcast
addresses (for example 192.168.0.255) still reach the LAN. Standard
library only, no cgo.

## Features

| Behaviour | Status |
|---|---|
| Send to 255.255.255.255 and to the directed broadcast address of each interface that is up and can broadcast | Implemented |
| Skip loopback and point-to-point (VPN) interfaces, and /31 and /32 networks | Implemented |
| Send any other destination alone | Implemented |
| Succeed when at least one send succeeded; otherwise return the first error | Implemented |
| IPv6 (multicast discovery) | Not in scope |

A device that hears both the limited and a directed broadcast answers
twice; callers key answers by device.

## Choosing how to send a discovery broadcast

This module is under 100 lines over the standard library; the choice is
mostly whether to write those lines yourself. Compared from source in
October 2026 (Go 1.27.1, golang.org/x/net v0.61.0). Check the projects
themselves before relying on a row.

| | this module | `net.ListenPacket` and `WriteTo` 255.255.255.255 | golang.org/x/net/ipv4 `PacketConn` | schollz/peerdiscovery v1.7.6 | lowbit-dev/beacon (2026-05-11 commit) |
|---|---|---|---|---|---|
| What it is | one function over any `net.PacketConn` | the standard library (Go sets SO_BROADCAST on IPv4 UDP sockets) | per-packet control messages: choose the outgoing interface (Linux, macOS), read the arrival interface | discovery between peers running the same library, by multicast | send one probe to one address, return the first answer |
| Reaches the LAN while a VPN holds the 255.255.255.255 route | yes, through the directed broadcast of each LAN | no | only through code you write: it does not find interfaces or addresses | not applicable (multicast group 239.255.255.250) | no |
| Finds each interface's directed broadcast address | yes; skips loopback, point-to-point, /31 and /32 | no | no | no | no |
| Works with devices that answer a vendor broadcast probe (WiZ, miIO, TP-Link, EtherNet/IP) | yes | yes, on a host without that VPN route | yes | no, both ends must run it | yes, one answer only |
| Dependencies | none | none | golang.org/x/net (with x/sys) | golang.org/x/net | none |

Pick this module when your program sends a vendor's discovery probe and
must work on laptops and gateways with VPNs or several interfaces. Use
plain `net` when the host has one LAN and no VPN. Use x/net/ipv4 when you
also need to know which interface an answer came in on, and
peerdiscovery when every node runs your own Go program.

## Install

```
go get github.com/cB-Sigma/lanbcast@latest
```

Requires Go 1.22 or later.

## Quick start

From `ExampleWriteBroadcast`:

```go
import (
	"net"

	"github.com/cB-Sigma/lanbcast"
)

conn, err := net.ListenPacket("udp4", ":0")
if err != nil {
	return err
}
defer conn.Close()
probe := []byte(`{"method":"getPilot","params":{}}`)
err = lanbcast.WriteBroadcast(conn, probe, &net.UDPAddr{IP: net.IPv4bcast, Port: 38899})
```

Read the answers from `conn` as usual.

## Documentation

Reference: [pkg.go.dev/github.com/cB-Sigma/lanbcast](https://pkg.go.dev/github.com/cB-Sigma/lanbcast).

| Package | Contents |
|---|---|
| `lanbcast` | `WriteBroadcast` |

## Testing

```
go test -race ./...
```

The tests use a fake `net.PacketConn` and this host's real interfaces.
`// Covers: SIG-...` comments in tests are requirement IDs of the
SigmaM2M project's compliance report and have no meaning outside it.

### Interop tested against

Not applicable: the module only chooses addresses and sends. The
libraries that use it are tested against their devices or simulators.

## Related repositories

- [github.com/cB-Sigma/wiz](https://github.com/cB-Sigma/wiz) uses it for
  WiZ light discovery.
- SigmaM2M's EtherNet/IP, Xiaomi miIO and TP-Link libraries use it for
  their discovery broadcasts.

This module has no simulator.

## Status

v0.x. The API may change before v1.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## Security

See [SECURITY.md](SECURITY.md) for how to report a vulnerability.

## Licence

Apache-2.0 (see [LICENSE](LICENSE) and [NOTICE](NOTICE)). Copyright 2026
Abhinav Gautam.
