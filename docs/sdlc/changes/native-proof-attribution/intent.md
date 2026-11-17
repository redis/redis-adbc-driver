# Native point-read proof attribution

Date: 2026-11-17. Status: diagnostic implementation; performance acceptance open.
Requestor: repository user, continuing issues #399/#444–#447. Author: Codex.
Source baseline: `55da2521`; tracking [#444](https://github.com/redis/redis-adbc-driver/issues/444).

The existing Go-only profile cannot establish which metadata/proof calls occur
inside a native Python/C-driver operation, or decompose independently pooled
native timings. Collect observations on the real native path before selecting a
new optimization. Preserve all shipping proof, ACL, lease, replay and GC behavior.

Deliver a private-build observer, exact provenance, bounded observations and
independent typed-data/state checks. It must not become a public diagnostic API
or a latency acceptance benchmark. No optimization, issue closure or measured
server-CPU/packet-RTT claim is part of this change.

See [specification](spec.md) and [plan/validation](plan.md).
