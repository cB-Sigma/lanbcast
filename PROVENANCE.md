# Provenance

Written for SigmaM2M, a oneM2M platform, as part of its LAN device
discovery. When the LAN protocol libraries were split into modules on
2026-10-06, `enip`, `miio`, `tplink` and `wiz` each took a byte-identical
private copy of the helper. On the same day the four copies were replaced
by this module (the files moved here unchanged), so each library depends
on one shared helper instead.

No code was taken from another project and no specification text is
reproduced. The behaviour (directed broadcast address = address OR NOT
mask, no broadcast on /31 and /32, loopback and point-to-point links
skipped) is ordinary IPv4 practice; RFC 919 and RFC 922 describe
broadcast addressing, and RFC 3021 the /31 case.

v0.2.0 (2026-10-11) added interface options, the tunnel name list,
per-interface IPv6 link-local multicast, the probe helpers and
per-interface sockets, written for this module. Platform behaviour was
checked by reading, not copied: Go's `net/sockopt_*.go` and
`net/interface_windows.go` (Go 1.26.2 and 1.27.1) for SO_BROADCAST and
Windows interface flags, the Linux kernel's `net/ipv4/route.c`
(`ip_route_output_key_hash_rcu`) for how a limited broadcast picks its
interface from the source address, and the `IP_BOUND_IF` constant of
Go's `syscall` package for macOS. All-nodes multicast (ff02::1) and
link-local scope are ordinary IPv6 practice (RFC 4291). The tunnel name
list comes from interface names seen on macOS, Linux and BSD hosts.

The tests and the Examples use made-up addresses, made-up interface
tables, a fake PacketConn and loopback sockets; there is no test data.

v0.3.0 (2026-10-12) added the arrival interface from packet information
and the responder, written for this module. The socket option and control
message values were taken as facts from the systems' headers, cross-checked
with golang.org/x/sys v0.48.0's constants (not imported): IP_PKTINFO,
IPV6_RECVPKTINFO and IPV6_PKTINFO on Linux; IP_RECVPKTINFO,
IPV6_RECVPKTINFO and IPV6_PKTINFO on macOS; IP_PKTINFO, IPV6_PKTINFO and
the WSACMSGHDR, IN_PKTINFO and IN6_PKTINFO layouts on Windows. The
namespace test (netns_linux_test.go and testdata/netns/Dockerfile) and the
GitHub workflow are ours.
