# Three-primary TLS partition evidence

2026-10-21. Source: 5232e30 (merged #414); native runtime libraries built from
053e638, whose Go runtime is unchanged by #414. Requestor: Albert Wong.
Agent: Codex. Human review and new hosted checks pending.
See [intent](intent.md), [spec](spec.md), [plan](plan.md),
[runner](../../../partition-cluster-tls.md), and [summary/hashes](evidence/summary.json).

## Fixture and scope

One newly owned Redis 8.6.2/Search 8.6.0 OSS Cluster container with three
primary processes and independently reachable TLS client endpoints on loopback
ports 16441-16443. TLS also protects the cluster bus; no plaintext client port,
persistence or replicas. Every primary's real Search aggregate cap is 137.
The checker validates each live node ID against discovery, ready state and
that real cap before writing ACLs. No shared database or paid Cloud deployment
was used. RESP3 remains the default for native and node-management clients.

The standalone fixture's serverAuth-only certificate could not authenticate
cluster-bus peers. Initial startup had one known node and only 5,461 assigned
slots. A separate clientAuth leaf signed by the same generated private CA
resolved this; the server leaf, hostname validation and trust verification
remain intact. The failed owned container and private material were removed
before retry. [Setup correction](evidence/setup-correction.json).

Fresh planner/worker users are installed on every primary after checking their
names are absent everywhere. Names are registered before the first write so a
partial creation is cleaned up. Permission mutations affect all primaries;
worker CONFIG and write denials are tested on each node. Cleanup continues
after node failures and cannot report success without verified user absence
on every primary. Workers receive their own identity/CA/descriptor through
stdin and a restricted environment; no administrative URI is inherited.

## Actual routing proof

Each final run required three distinct advertised owners, complete nonoverlapping
coverage of all 16,384 slots, and 1,099 unique live fixture row keys. Every owner
had rows. Direct authenticated TLS connections to each expected primary then
verified physical key presence with nontransactional single-key EXISTS pipelines.
These clients do not follow MOVED responses, so a wrong owner or missing key
fails. This is stronger than counting backend shards behind Cloud's one external
proxy owner. The JSON records include per-owner row counts and
`physical_row_presence_verified: true`.

Complete Arrow schema/values were independently checked through native workers
after planner closure, including Search and direct HASH reads, duplicate/deleted
IDs, malformed/stale descriptors, FT.INFO/HMGET denials, native wrong-CA and
wrong-host rejection, cancellation, early release, live updates/deletes and
later-insert exclusion. No assertions were weakened or skips added.

| Client | Routing | Result | All-primary ACL cleanup |
|---|---|---|---|
| macOS ARM64 native library, Python 3.13.16, ADBC 1.12.0 | auto | [17 passed](evidence/macos-native.json) | [passed](evidence/macos-native-cleanup.json) |
| macOS ARM64 installed wheel, Python 3.14.8, ADBC 1.8.0 | auto | [17 passed](evidence/macos-minimum-wheel.json) | [passed](evidence/macos-minimum-wheel-cleanup.json) |
| Linux ARM64 installed wheel, Python 3.14.8, ADBC 1.12.0 | auto | [17 passed](evidence/linux-wheel-py3.14-adbc1.12.0.json) | [passed](evidence/linux-wheel-py3.14-adbc1.12.0-cleanup.json) |
| Linux ARM64 installed wheel, Python 3.13.16, ADBC 1.8.0 | explicit true | [17 passed](evidence/linux-wheel-py3.13-adbc1.8.0.json) | [passed](evidence/linux-wheel-py3.13-adbc1.8.0-cleanup.json) |

**68 Cluster TLS checks passed** across four runs. Linux clients used the native
ARM64 AlmaLinux 8 Docker client image on the local ARM64 VM, sharing the owned
server's network namespace. macOS clients reached the three published loopback
ports from the host. This is not an x86_64 execution claim.

## Regression and lifecycle verification

- Existing standalone native TLS expectations: **15 checks passed** against
  owned Redis 8.10.2/Search 8.10.1. [Result](evidence/standalone-regression.json).
- Management/fault checks: **25 passed** (17 prior Cloud/Software checks plus
  eight cluster checks). Added coverage rejects foreign/missing endpoints,
  unready nodes before ACL mutation, preexisting users and false physical-row
  presence; verifies partial-creation cleanup, all-node permission changes,
  continued cleanup after failure, honest failed status and invalid-port rejection
  before filesystem/container mutation.
  [Output](evidence/unit-tests.log).
- Python compilation, Bash syntax, actionlint on both modified workflows,
  whitespace checks and evidence JSON/link/hash checks passed.

Equivalent native command: set `REDIS_TLS_ADMIN_URI` and `SSL_CERT_FILE` from
this private fixture, choose the built native library, then run
`scripts/check_partition_tls.py --oss-cluster-fixture <fixture>
--minimum-cluster-primaries 3 --output <fresh-report>` with the validation Python.
Wheel runs add `--require-wheel` with the native override unset. The Linux
minimum-client run additionally passed `--cluster-mode true`. Exact versions,
native hashes, routing mode, topology, caps and check names are in the JSON.

No production Go runtime code changed. Full Go/upstream Python suites were not
rerun locally; existing Test/Release jobs retain them. The new three-primary
checks are wired into both Linux Test architecture jobs and both installed-wheel
Release build jobs, with failure cleanup and narrowly selected JSON artifacts.

## Cleanup and remaining work

Every run verified both owned users absent on every primary. The exact Linux
client container and label-verified cluster container were removed; the owned
standalone native PID was checked and stopped. All private fixture directories
and the temporary macOS client environment were removed. No paid resources
were created.

PR #414's hosted Release run 39687224667 was cancelled before its new client
matrix ran. It supplies no hosted matrix result. The next PR runs that matrix
as well as these added Cluster TLS checks; native x86_64 coverage remains pending.
Issue #363 stays open for the remaining Cloud/client matrix, failure/resharding,
older Python/full-suite combinations and representative performance evidence.
This is a fixed correctness fixture, not a benchmark or snapshot guarantee.

Agent review followed REVIEW.md for lifecycle, ownership/TLS, workflow permissions
and evidence scope. No actionable findings remain from that review. Human review
and new hosted checks remain pending.

The final entry-point port guard was added after the four data/lifecycle runs.
A new real three-primary TLS startup passed with valid ports and was cleaned up;
invalid ports failed before creating a directory/container. Core TLS configuration
was unchanged. Summary metadata retains the data-run and final script hashes
separately. [Final startup check](evidence/final-startup-check.json).


## Integration with current main

Integrated main `4b0fdb5`, including the scoped CI matrix from #415 and Search
reply parser from #416. The workflow resolution retains `matrix.integrations`
for management fault checks and the narrowed Release PR triggers.

Rebuilt the macOS ARM64 native library with that parser: the three-primary TLS
run passed **17 checks**, including physical presence of all 1,099 rows across
three owners; standalone TLS passed **15 checks**. All-primary ACL cleanup
passed. Management fault tests passed **25**, CI matrix selector tests passed
**5**, and six focused Go reply/parser tests passed (including RESP2/RESP3
subcases). Actionlint passed for all three workflows changed by the merge.

[Validation metadata](evidence/post-merge-validation.json),
[cluster result](evidence/post-merge-cluster.json),
[standalone result](evidence/post-merge-standalone.json),
[ACL cleanup](evidence/post-merge-cluster-cleanup.json), and
[fault test output](evidence/post-merge-fault-tests.log) retain this new scope.
Historical wheel results above were not repeated with the new parser.
The owned cluster container was removed, the native Redis PID was stopped,
and both private fixture directories were removed. No paid resources were created.
