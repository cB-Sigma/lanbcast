# Changelog

## v0.2.0 (2026-10-11)

Added, all backwards compatible: `WriteBroadcast` keeps its signature,
its interface choice and its sends.

- `Send`, `Destinations` and `Options`: choose interfaces by name pattern
  (`Interfaces`, `Exclude`) or predicate (`Filter`). VPN, tunnel and
  Apple peer-to-peer interfaces are skipped by name (`TunnelNames`) as
  well as by the point-to-point flag; `AllowTunnels` or naming an
  interface overrides that.
- IPv6, opt-in: an ff02::/16 destination without a zone, such as ff02::1,
  is sent once per multicast-capable interface with an IPv6 link-local
  address, zone set to the interface name.
- `Probe`, `ProbeConn`, `Collect`, `ProbeOptions` and `Answer`: send a
  probe and pass the answers that arrive on the same socket to a callback
  until a context ends. Identical copies from one sender are dropped
  unless `KeepDuplicates` is set; `Resends` repeats the probe; the
  host's own looped-back probe is ignored.
- `ProbeOptions.PerInterface`: one socket per interface, bound to its
  address (and on macOS to the interface with IP_BOUND_IF), so
  255.255.255.255 leaves through every LAN rather than the one the
  routing table picks.
- `Answer.Interface`: the interface an answer came in on, exact for
  per-interface sockets and IPv6 link-local senders, inferred from the
  subnet for other IPv4 senders.
- README: operating system notes (Go's SO_BROADCAST handling on Windows,
  Windows interface flags, how Linux, macOS and Windows choose the
  interface for a limited broadcast) and an updated comparison.

## v0.1.1 (2026-10-11)

README only: comparison with plain `net`, x/net/ipv4 and other discovery
helpers.

## v0.1.0 (2026-10-11)

`WriteBroadcast`, moved unchanged from the private copies in SigmaM2M's
LAN libraries (see PROVENANCE.md).
