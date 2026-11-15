# Performance work — November 14, 2026

Three subagents investigated read response waits, managed writer coordination
and exact unmanaged source projection. Live work used an exclusively scheduled
owned Redis Software fixture with active/effective 4× QPF. The
[environment](environment.json) distinguishes diagnostic and native client
platforms, routes and resource limits. No ClickBench was run.

## Opt-in managed writer coordination

`adbc.redis.writer_admission_queue` defaults to **false**. Eligible newly opened
connections can opt into bounded process-local coordination. Every writer still
requires fresh catalog, target and native server admission checks. Waiting
writers release their pins; cancellation and Close release local scheduling
state. Other processes, unrepresentable identities and bounded overflow retain
server-controlled admission. This is an option within managed mode, without a
storage-format change or global FIFO guarantee.

The [normal Linux Python/ADBC confirmation](../../managed-writer-handoff/native-linux/README.md)
retains all four frozen A1/B1/B2/A2 windows: 192 validated records, 6,144 operations
including warmups, and 4,608 measured operations. At four clients, the opt-in
configuration observed 9.5–19.2% higher closed-loop workload throughput across
INSERT/UPDATE/DELETE, and pooled p95 fell from approximately 195–284 ms to
70–76 ms. **Medians rose from approximately 7 ms to 47 ms and means increased.**
One-client results were essentially unchanged. Raw controls drifted in both
directions; all pooled and per-pair results remain visible. These finite samples
are not capacity or causal confidence estimates.

The [earlier Go pilot/long comparison](../../managed-writer-handoff/report.md)
retains 6,912 long-run acknowledgements and full typed final-state oracles. It
measured the preceding always-on prototype on the macOS published-port route;
it is not relabeled as final gated/Linux evidence. The public default-false gate
then passed actual default/enabled safety checks and the native confirmation.

Client/cursor **object** creation and teardown are outside native operation
timing; lazy first-operation transport work remains included. Each trial creates
new clients, so warmup does not prewarm later clients' sockets. Existing raw
retry labels are preserved with separate policy/scope annotations. SQL and raw
Redis operations provide different guarantees.

## Read delay: a topology-dependent transport finding

[Standalone Go and redis-py controls](../../point-valid-isolation/README.md)
reproduced a roughly 15 ms next-response delay after large TLS replies on the
macOS Docker published-port route, without ADBC, SQL, Search or Lua. Identical Go
source against the unchanged Software endpoint from its Linux network namespace
had roughly 0.017 ms medians and no cliff. Linux redis-py independently had no
cliff. OS, route and some client runtime/resources changed together, so the
precise faulty component remains unassigned.

The fixed two-index proof grouping workaround was rejected: it improved narrow
cases but worsened a wide/deep helper case about fourfold. No proof-skipping,
reply-size heuristic or read-runtime workaround is selected. The tested
[Linux native client environment](../../linux-client/README.md) avoids applying
the host-route artifact to this round's final write comparison. Historical
measurements retain their original environment labels; transport timings are
not SQL latency improvements.

## Upstream exact-source foundation

The [raw HASH projection prototype](../../unmanaged-source-projection/README.md)
includes a pinned upstream rlookup patch, exact core/native adapter source,
seven passing Rust tests and a real native-API type-check. Its Linux driver/raw
repro passed seven typed SQL queries, live changes, NULL/type/existence,
ownership and cleanup checks. Aliased numeric LOAD lost precision in three
adversarial cases. LOAD* preserved selected bytes but returned 131,230 value
bytes versus 126 selected bytes in the synthetic wide-field fixture.

The prototype is **not installed or exposed through a Search command**. Parser,
coordinator, cursor, RBAC and exact reducer integration remain prerequisites.
Its byte budget admits output after materialization; it is not a hard transient
memory bound. No current driver fanout reduction is claimed, and no upstream
issue/comment/PR was submitted during this work.

## Final checks and cleanup

- [Broad unit checks](unit-checks.json): 1,639 pass events, 1,347 expected skips
  with Redis endpoints empty; zero failures.
- [Default-focused race checks](final-race-default.json): 82 pass events, zero
  failures/skips, including explicit opt-in safety cases.
- [Enabled admission race checks](final-race-enabled.json): 22 pass events,
  zero failures/skips on verified Software.
- [Documentation checks](docs-checks.json): strict build and verification of
  45 selected public pages passed. Supplemental links, source/hash manifests,
  complete-grid/corruption audits and generated-credential/owner/path privacy
  scans also passed.
- [Cleanup](cleanup-proof.json): owned fixture/client containers and network,
  client image tag and generated credentials removed; preexisting containers,
  custom networks and TLS material preserved. The strict network-ID check found
  that Docker's unused built-in bridge had a different ID; its configuration
  was unchanged and no containers were attached. The recorded bridge creation
  predates the first timed read; no attempt was made to recreate/change it.
  Base images/build caches and private evidence remain.

Test-event counts include parent/subtests. The records identify their actual
source revisions; subsequent integration updates to evidence do not relabel
frozen measurements. Root and independent subagent source review found no
remaining runtime blocker. Human maintainer approval is not inferred.
