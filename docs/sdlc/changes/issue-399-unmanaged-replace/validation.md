# Verification

2026-11-17. Source `b63a010d` plus this working change. Requestor: Albert Wong.
See [intent](intent.md), [spec](spec.md) and [plan](plan.md).

Implementation branch: `codex/issue-399-unmanaged-replace`. These checks precede
PR creation and do not imply a merge or release. Operational logs, endpoint
identifiers, credentials and certificates stay private.

## Environment and checks

macOS arm64 client, Go 1.27.3, disposable Redis Software Redis 8.8.0 RAM database,
one physical primary, TLS through a Docker published port. Setup used
`make -C go redis-up` with a dedicated Compose project/ports, followed by
`software-setup.sh --exec` for live checks. The management readiness gate verified
active scaling factor 4, six effective Search workers, mnp routing, 32 connections,
active primary and noeviction before running tests. This is correctness evidence,
not a benchmark or distributed/Cluster API validation.
The disposable container/network and generated endpoint declaration were removed
after verification; the built shared library remains available locally.

| Check | Actual outcome |
|---|---|
| `cd go && env -u REDIS_URI -u REDIS_SNAPSHOT_URI go test -count=1 -p 1 ./...` | Passed all packages; integration prerequisites absent, expected integration skips |
| `go test -race -count=1 -p 1 -timeout=15m -run 'TestUnmanaged\|Test.*Replace\|Test.*BindStream\|TestSnapshotArrowIngest' -v .` through verified Software setup | Passed; includes managed import replacement, managed Arrow atomicity/modes, unmanaged append/create, source ownership and new replacement paths |
| Subsequent `go test -race -count=1 -p 1 -run 'TestUnmanagedArrowReplaceInputFailures/null_key\|TestUnmanagedReplaceInventoryReplyBudget' -v .` | Passed NULL-key failure and server-side remaining inventory reply-budget refusal; source inspection confirmed expected state |
| `scripts/unmanaged_replace_acl.py` through verified Software setup, with temporary actual management-created principals | DEL, source HSET, FT.DROPINDEX and FT.CREATE denials passed before source mutation/input; temporary users/roles/ACL mappings removed |
| `make -C go build` | Passed native shared-library build with repository version linker flags |
| `uv run --offline --with redis python -m unittest discover -s scripts -p 'test_software_partition_tls.py'` | Four management-helper tests passed |
| `gofmt`, `git diff --check`, local Markdown target checks | Passed |

The live selection sets both REDIS_SNAPSHOT_URI and REDIS_URI to the same isolated
disposable endpoint because several existing unmanaged regression helpers read
REDIS_URI directly. It runs sequentially with `-p 1`, without cached results.
The separate replacement-only race pass also passed with REDIS_URI unset.

Ordinary live runs explicitly skip the opt-in aggregate/concurrency/performance/
sparse/retrieval profiles, the unrelated managed replacement-batch ACL fixture,
and the new four specialized ACL cases. The separate ACL run supplies all four
new principals and runs those cases without skips. No performance profile,
ClickBench, full default Software suite or external-service test was run.

## Coverage and corrected failures

- Inspect source HASHes independently of SQL after schema/data replacement:
  old keys/unmodeled fields removed, a reused key becomes persistent, outside
  values/TTLs preserved, actual NUL key accepted, dedicated membership index reset,
  new SQL schema reads correctly and DROP/sweep preserves application resources.
- Reject missing/wrong consent, mismatched physical identity, nested/non-HASH
  keys, overlapping/filtered/multi-prefix/unmodeled indexes, another registration,
  missing __key, managed/missing/foreign targets before reading input or changing
  source data. Existing retained/shared/staged tests remain in the regression
  selection. Missing-key coverage observes the actual rebound reader.
- Mutate a source HASH image/type/expiry or delete it between inventory and
  guarded deletion; preserve observed concurrent changes. Preserve a newly
  inserted key and stop without expanding deletion scope. Detect registration
  removal and changed index definitions, including alteration of the newly
  created membership index before registration.
- Lose delete/index-drop/index-create/write replies after actual application;
  verify partial final state, separate acknowledgment counts and no mutation or
  input replay. Reader/conversion errors and duplicate input after an earlier
  conversion slice retain acknowledged new rows and removed old data.
- Empty input succeeds for both populated and already empty sources. Statement
  cancellation and connection Close stop after one acknowledged deletion without
  consuming input or advancing index DDL. The first Close regression exposed an
  ordinary-cancellation status; replacement now uses executionCloseError to keep
  the connection-close cause, original diagnostics and progress.
- The first duplicate fixture stopped in existing conversion-slice validation
  before writes. Moving the duplicate beyond that slice verifies partial progress
  without weakening the refusal. Index lost-reply hooks use the direct replacement
  entry to observe the actual DDL client rather than an unrelated execution pool.
- A global HSET-denied test principal initially could not acquire catalog reader
  leases. Its ACL selector now permits only metadata HSET while still denying
  source HSET, exercising the intended real source permission refusal.

## Review and limits

Agent review covered correctness/lifecycle, source ownership/ACLs, and intent,
public contract and retained evidence under REVIEW.md. No remaining actionable
finding was identified in this scope. Human review has not been performed.

Quiescence and exclusive use are administrator attestations, not a source lock.
Raw concurrent writers can violate the contract without detection. Initial and
final DUMP inventories and input-key tracking have explicit 100,000-key / 64 MiB
bounds; no full-scale capacity/performance claim is made. Source replacement is
destructive and partially visible, including failed index/catalog transitions.
No rollback or automatic resume is offered. Multi-shard/Cluster API, Flex and
failover behavior were not exercised by these runs. Historical results were not
modified or relabeled.
