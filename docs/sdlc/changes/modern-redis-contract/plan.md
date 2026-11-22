# Modern Redis contract implementation and validation

2026-11-22 · Original source: 1d0d0b3b · PR base: 616a54d2 · Status: locally validated

1. Audit driver command construction, authentication and metadata enumeration.
2. Replace script envelopes and script-cache executors with versioned Functions;
   inventory entry points with `go generate .` and verify with
   `go run ./internal/cmd/genfunctions -check`.
3. Require explicit ACL usernames and function initialization on every socket.
4. Replace metadata SMEMBERS calls with deduplicated SSCAN enumeration; update
   generated SQL RBAC rules, documented ACLs and affected test observers.
5. Validate unit, live Software and snapshot/compaction behavior on disposable
   Redis Software with active verified 4x QPF; review lifecycle/ownership risks.

## Evidence

All live runs used disposable Redis Software 8.8.0, three RAM shards, verified
active/effective 4x QPF, noeviction and verified data TLS. The client was macOS
through a Docker-published port. These are correctness checks, not performance
claims; operational endpoints, principals, trust material and raw logs are private.

- `env -u REDIS_URI -u REDIS_SNAPSHOT_URI go test -count=1 -p 1 ./...`:
  all packages passed on the final implementation; integration skips are expected.
- Full default `make test-software`: passed, 1,336 top-level pass events,
  414 top-level skips, zero failures; root package 965.180 seconds.
- Separate `Test(Snapshot|Unmanaged)` run: 466 top-level passes, 70 skips and two
  test-observer failures. Corrected command-refusal classification and initialized
  the selective-compaction fault-injection function. Focused final-source rerun
  of both cases plus atomic incomplete-copy recovery passed: 3/3, zero skips.
  The broad snapshot run itself is not represented as a green run.
- Actual Software restricted-principal gates passed under `-race`: FCALL-denied
  (EVAL-capable) operator rejection; point leases without HGETALL; GC, row-batch,
  replacement and import-journal permission preflights; cleanup recovery; all six
  reader-lease model cases; SQL RBAC including peer isolation and payload privacy.
  Updated fixture grants to FCALL and the actual read commands needed to reach
  the deliberately denied HDEL/ZREM checks. Kept their negative assertions.
- The operator gate exposed go-redis unwrapping initialization errors. Preserve
  the explicit Functions-requirement diagnostic without changing the refusal.
- Function installation/refusal, no-fallback, canceled/deduplicated cursor-page,
  explicit-credential and global pipeline-reply-loss unit regressions passed.
- Live controls, Arrow row/index batches and compaction remained functional
  through SCRIPT FLUSH using FCALL without script-body fallback.
- An initial live run exposed an omitted startup-assembled compaction function.
  Fixed inventory discovery, made dispatch selections statically visible and
  checked critical entry points against the installed library inventory.
- `go run ./internal/cmd/genfunctions -check`, `git diff --check`, changed-document
  local link checks and `make -C go build` passed. No protocol/storage format bump.

## Review

Reviewed lifecycle/no-replay behavior, writer and Search fences, ACL key/command
separation, immutable function naming and Cluster option propagation. No unresolved
change-specific finding remains. This is an agent review, not human merge approval.
Real Cluster API, failover/RDB recovery and Flex-specific fixtures were not run.
Historical measurements remain unchanged. Initial failed/interrupted runs are
retained privately as debugging evidence, not acceptance passes.

No ClickBench, Cloud/AWS provisioning, release or historical benchmark rewrite.
Operational endpoints, credentials and logs remain outside checked-in records.

See [intent](intent.md) and [specification](spec.md).

## PR preparation

Applied the scoped change cleanly to current main (`616a54d2`) in an isolated
checkout. Unrelated telemetry, workflow and extension changes remain outside this
PR. The live evidence above describes the original implementation source; fresh
combined-source checks are recorded below rather than extending that evidence
to untested upstream changes.

Combined-source checks passed on 2026-11-22:

- All unit packages with both Redis endpoint variables unset (`-count=1 -p 1`);
  integration skips expected. Root package 33.273 seconds.
- Focused Functions/credentials/command-contract, indexed-append, atomic-copy and
  selective incomplete-tombstone recovery tests against the same disposable
  Software fixture after fresh management-policy/QPF verification: passed, root
  package 15.412 seconds. The separately provisioned denied-FCALL operator was
  not recreated for this focused rerun; its earlier gate remains source-scoped
  evidence above.
- Shared-library build, generated-function inventory check and whitespace checks.

No full combined-source Software, Cluster, failover/RDB or Flex rerun is claimed.

## PR #575 restricted-reader correction

The first PR CI run passed all Go and snapshot shards, but the independent
retained-worker fixture and the macOS minimal-reader test still provisioned
PING/TIME/GETRANGE without FCALL. The required Functions bootstrap therefore
refused those users before retained replay. Add only FCALL to the public-driver
reader grants, preserve the namespace read-only key pattern and denial checks,
and document the preinstalled-library requirement. The lower-level raw storage
reader permissions are unchanged. No fallback or namespace write grant was added.

Correction validation: all unit packages passed (root 29.313 seconds); four
Software fixture management safety tests passed. The full native partition TLS
check passed on owned Redis Software 8.8.0 with freshly verified effective 4x
QPF and strict TLS: 19 checks, 6 metadata checks, and 5 retained subchecks,
including independent minimal-ACL workers, namespace denial, replay after source
DROP, cancellation/early release, revocation and verified owned-key cleanup.
The standalone Go ACL test could not be used as live evidence locally because
its initial attempt overlapped TLS fixture reconfiguration; no pass is claimed
for that attempt. CI reruns exercise it on their isolated fixtures.

The setup guide now explains bundled automatic installation and the trusted
setup-user path using the existing online doctor, without requiring manual Lua
provisioning or granting FUNCTION LOAD to restricted runtime users.
