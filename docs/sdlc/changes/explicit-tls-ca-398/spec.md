# Contract

Date: 2026-10-21. Status: implemented; review pending.
See [intent](intent.md), [plan](plan.md), and issue #398.

`adbc.redis.tls_ca_file` is a database string option. Empty preserves default
system trust. Nonempty identifies a PEM bundle used as the exclusive roots for
new rediss connections, including all OSS Cluster node clients and single/proxy
connections. Read the file when building each new connection configuration;
existing connections are not reloaded. Each worker sets its own local path.
Never serialize paths, certificates, or credentials into partition descriptors.

Reject nonempty files with plaintext URIs or skip_verify=true, unreadable files,
and bundles without valid certificates with InvalidArgument. Preserve TLS 1.2
minimum, URI hostname checking, connection credentials and timeout behavior.
Return no file contents in errors. No global trust-store or environment mutation.

Share private fixture generation across Docker and native Redis. A TLS-only native
fixture uses restricted files, an owned subprocess and verified CLI health checks;
stop still checks the PID before shutdown. A separate CA-signed wrong-host leaf
allows real native worker hostname-rejection tests. Only credential-free result
JSON is uploaded; remove generated private material during CI cleanup.

Both default and minimum supported ADBC clients must pass installed macOS wheel
TLS validation. Linux TLS gates continue using the explicit roots and same
15 checks. This is not Cloud or Redis Software proxy TLS compatibility evidence.
