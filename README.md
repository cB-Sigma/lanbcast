# lanbcast

Sends a LAN discovery probe to the limited broadcast address, to the directed broadcast address of every LAN interface and, if asked, to the IPv6 all-nodes group on every LAN interface, and collects the answers, in Go.

[![Go Reference](https://pkg.go.dev/badge/github.com/cB-Sigma/lanbcast.svg)](https://pkg.go.dev/github.com/cB-Sigma/lanbcast) [![test](https://github.com/cB-Sigma/lanbcast/actions/workflows/test.yml/badge.svg)](https://github.com/cB-Sigma/lanbcast/actions/workflows/test.yml) [![Licence: Apache-2.0](https://img.shields.io/badge/licence-Apache--2.0-blue.svg)](LICENSE) [![Go](https://img.shields.io/badge/go-1.22-00ADD8.svg)](go.mod)

A host with a VPN or several interfaces may route 255.255.255.255
somewhere it cannot be sent (macOS answers "can't assign requested
address" while a VPN tunnel holds that route), or through one interface
only. The directed broadcast addresses (for example 192.168.0.255) still
reach each LAN, and a socket bound to one interface sends the limited
broadcast out of that interface. Standard library only, no cgo.

## Features

| Behaviour | Status |
|---|---|
| Send to 255.255.255.255 and to the directed broadcast address of each interface that is up and can broadcast (`WriteBroadcast`, `Send`) | Implemented |
| Skip loopback and point-to-point (VPN) interfaces, and /31 and /32 networks | Implemented |
| Skip VPN, tunnel and peer-to-peer radio interfaces by name (`TunnelNames`: utun, tun, tap, wg, ppp, ipsec, gif, stf, zt, tailscale, awdl, llw) | Implemented in `Send`, `Probe` and `ProbeConn`, overridable; `WriteBroadcast` keeps its v0.1 choice |
| Choose interfaces by name pattern, exclusion pattern or predicate (`Options`) | Implemented |
| IPv6: send to ff02::1 (or any ff02::/16 group) once per interface, zone set to the interface name | Implemented, opt-in: `Send` with an IPv6 socket, or `ProbeOptions.IPv6` |
| Send the probe and read the answers on the same socket until a context ends (`Probe`, `ProbeConn`, `Collect`) | Implemented |
| Drop the duplicate answer of a device that heard both the limited and a directed broadcast | Implemented, on by default (`KeepDuplicates` turns it off) |
| Resend the probe while waiting | Implemented (`Resends`, `ResendInterval`) |
| Send the limited broadcast out of every LAN interface, not only the one the routing table picks | Implemented, opt-in (`ProbeOptions.PerInterface`): tested on macOS; Linux from kernel source; Windows not tested |
| Name the interface an answer came in on | Implemented: exact for per-interface sockets and IPv6 link-local senders, inferred from the subnet for other IPv4 senders, empty for a sender outside every local subnet |
| Send any other destination alone | Implemented |
| Succeed when at least one send succeeded; otherwise return the first error | Implemented |

`WriteBroadcast` is unchanged from v0.1: same signature, same interfaces,
same sends. A device that hears both the limited and a directed broadcast
answers twice; with `WriteBroadcast` the caller keys answers by device,
`Probe` and `ProbeConn` drop identical copies.

IPv6 is opt-in because most devices that answer vendor broadcast probes
(WiZ, Xiaomi miIO, TP-Link Kasa, EtherNet/IP adapters) speak IPv4 only.
ff02::1 reaches every IPv6 node on the link, so a device that listens on
the probe's port over IPv6 hears it the way an IPv4 device hears a
broadcast.

## Choosing how to send a discovery broadcast

The plain-`net` and x/net rows of this comparison were read from source in
October 2026 (Go 1.27.1, golang.org/x/net v0.61.0); the Go socket options
below were also checked in Go 1.26.2. Check the projects themselves before
relying on a row.

| | this module | `net.ListenPacket` and `WriteTo` 255.255.255.255 | golang.org/x/net/ipv4 `PacketConn` | schollz/peerdiscovery v1.7.6 | lowbit-dev/beacon (2026-05-11 commit) |
|---|---|---|---|---|---|
| What it is | send functions over any `net.PacketConn`, and a probe helper that also reads the answers | the standard library (Go sets SO_BROADCAST on IPv4 UDP sockets) | per-packet control messages: choose the outgoing interface (Linux, macOS), read the arrival interface | discovery between peers running the same library, by multicast | send one probe to one address, return the first answer |
| Reaches the LAN while a VPN holds the 255.255.255.255 route | yes, through each LAN's directed broadcast, and with `PerInterface` through a socket bound to each LAN | no | only through code you write: it does not find interfaces or addresses | not applicable (multicast group 239.255.255.250) | no |
| Finds each interface's directed broadcast address | yes; skips loopback, point-to-point, tunnel names, /31 and /32 | no | no | no | no |
| IPv6 | ff02::/16 per interface, opt-in | by hand, one zone at a time | through golang.org/x/net/ipv6 | yes (ff02::c by default) | no |
| Collects answers | all of them until a context ends, duplicates dropped | by hand | by hand | yes, from peers | the first only |
| Arrival interface | exact for per-interface sockets and IPv6 link-local senders; inferred from the subnet otherwise | no | exact, from control messages (Linux, macOS) | no | no |
| Works with devices that answer a vendor broadcast probe (WiZ, miIO, TP-Link, EtherNet/IP) | yes | yes, on a host without that VPN route | yes | no, both ends must run it | yes, one answer only; it sends from a dual-stack "udp" socket, which on Windows gets no SO_BROADCAST from Go |
| Dependencies | none | none | golang.org/x/net (with x/sys) | golang.org/x/net | none |

Pick this module when your program sends a vendor's discovery probe and
must work on laptops and gateways with VPNs or several interfaces. Use
plain `net` when the host has one LAN and no VPN. Use x/net/ipv4 when you
need the exact arrival interface of every IPv4 answer on one shared
socket, or other per-packet control (TTL, source address), and
peerdiscovery when every node runs your own Go program.

## Operating system notes

- **Socket options (all platforms).** Go turns on SO_BROADCAST for every
  UDP socket on Linux, macOS and the BSDs, AIX and Solaris
  (`net/sockopt_*.go`). On Windows it does so only when the socket is not
  IPv6 (`net/sockopt_windows.go`), and `net.ListenPacket("udp", ...)` is a
  dual-stack IPv6 socket there. Use `"udp4"` for broadcasts; `Probe` does.
- **Limited broadcast, one interface.** Without `PerInterface`,
  255.255.255.255 leaves through the one interface the routing table
  picks, on every system. The directed broadcasts cover the other LANs,
  but a device with no address in the LAN's subnet (factory default,
  setup mode, a static address from another site) hears only the limited
  broadcast.
- **`PerInterface` on Linux.** Each socket is bound to the interface's
  IPv4 address. Linux sends a limited broadcast through the interface that
  owns the source address (`net/ipv4/route.c`,
  `ip_route_output_key_hash_rcu`), so SO_BINDTODEVICE and its privilege
  are not needed. This is checked against the kernel source only; it has
  not been run on a Linux host with several LANs. The GitHub test job runs
  the host test on one Ubuntu runner, which logs rather than fails when
  the runner does not loop broadcasts back.
- **`PerInterface` on macOS.** The socket is bound to the address and to
  the interface with IP_BOUND_IF. Tested on macOS 26 (en0, with Apple's
  peer-to-peer and VPN interfaces present).
- **`PerInterface` on Windows and the BSDs.** Only the address binding is
  done. Windows is widely reported to send a limited broadcast through the
  interface of the bound address; this module has not been tested on
  Windows or the BSDs.
- **Answers sent by broadcast.** A socket bound to a unicast address does
  not receive broadcasts. A device that answers by broadcast rather than
  to the sender is heard by the default single socket, not by
  `PerInterface` sockets.
- **Interface flags on Windows.** Go derives them from the adapter type:
  Ethernet, Wi-Fi, token ring and IEEE 1394 can broadcast; PPP and tunnel
  adapters are point-to-point; other types (many VPN drivers present as
  "proprietary virtual") have neither flag, so both the IPv4 and IPv6
  paths skip them. OpenVPN's TAP-Windows adapter presents as Ethernet, and
  Windows names are the user's friendly names ("Ethernet 2"), so the
  tunnel name list rarely matches there: use `Options.Exclude` or
  `Options.Filter`. Name patterns are compared without regard to case.
- **IPv6 zones.** The zone of each ff02::/16 destination is the interface
  name, which Go resolves to an index on every platform, Windows friendly
  names included. An interface needs an IPv6 link-local address and the
  multicast flag to be used.
- **Apple interfaces.** awdl0 and llw0 (peer-to-peer Wi-Fi) are up, can
  broadcast and have IPv6 link-local addresses, so without the name list
  they would receive the IPv6 probe; they are on the tunnel list.

## Install

```
go get github.com/cB-Sigma/lanbcast@latest
```

Requires Go 1.22 or later.

## Quick start

Probe and collect, from `ExampleProbe`:

```go
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()
opts := &lanbcast.ProbeOptions{PerInterface: true, IPv6: true, Resends: 1}
err := lanbcast.Probe(ctx, []byte(`{"method":"getPilot","params":{}}`), 38899, opts, func(a lanbcast.Answer) {
	fmt.Println(a.From, a.Interface, string(a.Data))
})
```

On your own socket, from `ExampleWriteBroadcast`:

```go
conn, err := net.ListenPacket("udp4", ":0")
if err != nil {
	return err
}
defer conn.Close()
probe := []byte(`{"method":"getPilot","params":{}}`)
err = lanbcast.WriteBroadcast(conn, probe, &net.UDPAddr{IP: net.IPv4bcast, Port: 38899})
```

Read the answers from `conn` as usual, or use `ProbeConn` to send and read
in one call.

## Documentation

Reference: [pkg.go.dev/github.com/cB-Sigma/lanbcast](https://pkg.go.dev/github.com/cB-Sigma/lanbcast).

| Name | Contents |
|---|---|
| `WriteBroadcast` | v0.1 send: limited broadcast plus each LAN's directed broadcast |
| `Send` | the same with `Options`, plus per-interface ff02::/16 multicast |
| `Destinations` | the addresses `Send` would use |
| `Options` | interface choice: `Interfaces`, `Exclude`, `AllowTunnels`, `Filter` |
| `TunnelNames` | the default excluded name patterns |
| `Probe` | opens sockets, sends, passes answers to a callback until the context ends |
| `ProbeConn` | the same on the caller's socket |
| `Collect` | `Probe` returning a slice |
| `ProbeOptions` | `KeepDuplicates`, `Resends`, `ResendInterval`, `BufferSize`, `LocalPort`, `IPv6`, `PerInterface` |
| `Answer` | sender, bytes, interface |

## Testing

```
go test -race ./...
```

The tests use made-up interface tables, a fake `net.PacketConn`, loopback
sockets, and this host's real interfaces (`TestProbeOnHost`, skipped with
`-short`; each part logs instead of failing when the host does not deliver
its own broadcasts or multicasts back to itself). `// Covers: SIG-...`
comments in tests are requirement IDs of the SigmaM2M project's compliance
report and have no meaning outside it.

### Interop tested against

Not applicable: the module only chooses addresses, sends and reads. The
libraries that use it are tested against their devices or simulators.

## Related repositories

- [github.com/cB-Sigma/wiz](https://github.com/cB-Sigma/wiz) uses it for
  WiZ light discovery.
- SigmaM2M's EtherNet/IP, Xiaomi miIO and TP-Link libraries use it for
  their discovery broadcasts.

This module has no simulator.

## Status

v0.x. The API may change before v1. See [CHANGELOG.md](CHANGELOG.md).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## Security

See [SECURITY.md](SECURITY.md) for how to report a vulnerability.

## Licence

Apache-2.0 (see [LICENSE](LICENSE) and [NOTICE](NOTICE)). Copyright 2026
Abhinav Gautam.
