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

The tests and the Example use made-up addresses and a fake PacketConn;
there is no test data.
