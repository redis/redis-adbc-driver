# Validation and review

Date: 2026-11-20. Requestor: Albert Wong. Author: Codex. Base: `e11653eb`.
Status: local verification and self-review complete; maintainer review pending.

See [intent](intent.md), [spec](spec.md), [plan](plan.md), and
[sanitized native controls](native-results.json).

## Native Search investigation (#559)

Redis Software **8.2.6-58**, Redis **8.8.0**, Search module **80808**.
RAM: three primaries. Actual Flex: one primary, 1 GiB total/256 MiB RAM,
container directory for flash. Both use authenticated trusted TLS/RESP3,
noeviction and management-verified active 4x QPF, six effective Search workers,
mnp routing and 32 proxy connections. RAM was created with `make -C go redis-up`;
Flex with `go/flex-setup.sh`, including its QPF gate. Client: macOS arm64 over
Docker published ports. These are correctness fixtures, not SSD/performance tests.
The existing user's Flex workload was left running throughout.

Each product completed 24 native cases, with six keys per case and a separate
expiry phase. Full enumeration and singleton INKEYS always agreed; HASH images
and absolute expiry times remained unchanged during the fixed-source controls.
ASCII, printable escape and non-UTF8 controls followed their expected membership.
One prefix versus overlapping prefixes produced the same NUL result.

| Native source condition | RAM, filtered | Flex, filtered | Unfiltered (both) |
|---|---|---|---|
| Initial scan, no TTL | omitted | included | included |
| Initial scan, TTL before index creation | omitted | included | included |
| Initial scan, TTL assigned after index creation | omitted | omitted | included |
| HSET after index creation, any tested TTL timing | omitted | omitted | included |

The NUL key is `app:` plus bytes `61 00 62`. The FILTER is `@active==1`;
its HASH has `active=1`. Its full-index absence rules out an INKEYS-only parsing
explanation in these fixtures. The matrix isolates a filtered native membership
problem and, on Flex, a difference between initial scan and notifications.
It does not identify the internal server implementation defect. The TTL
notification control does not mean the HASH expired: its absolute TTL remains
about an hour in the future and EXISTS/DUMP still succeed.

Redis documents [binary-safe keyspace keys](https://redis.io/docs/latest/develop/using-commands/keyspace/).
The reviewed [FT.CREATE reference](https://redis.io/docs/latest/commands/ft.create/)
does not establish an intentional NUL-key exclusion from FILTER indexing.
Therefore this is documented as an observed native limitation, not an intentional
supported-key restriction or a driver repair. Arbitrary NUL-bearing membership
is not guaranteed by filtered application indexes on the tested builds. Use
NUL-free application key names when complete filtered-index inclusion is needed.
Existing historical evidence remains labeled by its actual settings.

Partition tests retain the native membership oracle, add full enumeration
agreement and positive unfiltered NUL inclusion, and check DUMP/PEXPIRETIME after
all partitions are fetched. The native matrix was repeated after removing an
unnecessary placeholder-key setup: the same 24-case outcome pattern recurred.
No production membership/fallback behavior changed.

Reproduce on a disposable, exclusively owned endpoint after management/QPF gates:

```sh
uv sync --script scripts/search_binary_key_membership.py
# Load the private REDIS_URI / SSL_CERT_FILE exports after dependency setup.
uv run --offline --script scripts/search_binary_key_membership.py
```

## DEFAULT verification (#481/#484)

Before-fix unit regressions reproduced both syntax acceptance and `22P02` when
retaining numeric DEFAULT `'1.50'` into integer. The fixed live lifecycle returns
`2`; negative halves round away from zero, and old numeric scale is not reapplied.
Prepared SQL installed before ALTER and Arrow omitted-column appends use the
retained default, while explicit supplied/NULL values bypass it. Metadata survives
serialization and repeated ALTER; incompatible defaults leave rows/schema intact.

Independent comparisons use the native shared library built by `make -C go build`
and the differential harness's pinned PostgreSQL 18.6 amd64 image, UTF8/C collation.
Expected SQLSTATEs are explicit; no exclusions or allowlist changes are added.
An initial comparison caught quantified ANY being interpreted as a function after
DEFAULT restriction; a syntax refusal and ANY/SOME/ALL regressions were added.

```sh
python3 go/pgdiff/default_repairs.py /private/default-repairs-corpus
# With the owned Software endpoint verified and its private exports loaded:
make -C go pgdiff PGDIFF_ARGS='--corpus /private/default-repairs-corpus --report build/default-repairs.json'
```

## Final checks

- Unit-only `env -u REDIS_URI go test -count=1 -p 1 ./...`: all packages pass.
  Endpoint-dependent integrations and optional external data skip in this run.
- Software race selection, run sequentially on its fixture:
  `go test -race -count=1 -p 1 -timeout=15m -v -run
  'TestSQL(Default|AlterType|CreationZone)|TestDefault|TestAlterType|TestApplyAlterType|TestFoldLocalTimes' .`:
  **32 top-level tests pass, zero failures/skips**.
- `go test -race -count=1 -p 1 -timeout=10m -v -run '^TestUnmanagedPartition' .`,
  with the dedicated `REDIS_SNAPSHOT_URI` and appropriate Flex option:
  **16 top-level passes on RAM and 16 on Flex**. Both runs explicitly skip the
  standalone subprocess entry point (exercised by its passing parent test) and
  the opt-in performance profile. Native permission, warning, transport and
  composed-error regressions are included; no capability skips were introduced.
- Independent comparison: **97/97 scenarios, 408 SQL steps, zero infrastructure
  errors/exclusions**, including 45 new grammar/retained-type scenarios and all
  52 unchanged #479 timing scenarios. See [comparison counts](default-results.json).
- Native membership: **24/24 cases completed per product**, with the NUL indexing
  limitations in the table above retained as observations, not passing inclusion
  claims. The final source uses direct HSET setup without placeholder mutations.
- `make -C go pgdiff-test`: **42 tests pass**. `make -C go build`, Python compile,
  changed-file formatting, local documentation-link checks and `git diff --check`
  pass. No full Software suite, full curated SQL corpus or benchmark is claimed.

During verification, a new test initially enabled foreign discovery in random
map iteration order; setting unmanaged mode first fixed that fixture setup.
An overlapping differential run also left unrelated zero-reference objects
visible to an existing global cleanup assertion. The affected ALTER test passed
alone, then the complete 32-test selection passed sequentially. Neither result
was addressed by weakening assertions, skipping a capability or changing GC.

## Review

Codex self-review covered the three REVIEW.md passes: expression scope/error
precedence, typed input versus assignment typmods, deferred evaluation,
metadata persistence, prepared/Arrow lifecycle and atomic refusal; source/index
ownership, exact binary bytes, warning/error refusal and TTL preservation; and
acceptance/evidence/provenance. No remaining actionable finding was identified.
This is not a human maintainer approval. The native server limitation remains;
#559's deliverable is its isolated reproducer, affected-configuration evidence,
regressions and explicit documented coverage limit.

Raw logs, endpoint/infrastructure identifiers, credentials and trust material
remain private.

## PR #563 CI follow-up

The first complete CI run found two test-fixture issues:

- `TestTimeZonesIssue88` still used an unparenthesized DEFAULT `AT TIME ZONE`
  expression. Its expression is now parenthesized, retaining its existing
  time-zone/value assertions and PostgreSQL's restricted DEFAULT grammar.
- The directory aggregate cancellation barrier counted cancellation only after
  its initial readiness wait. A sibling could observe cancellation in that
  first wait after all four workers had entered, return correctly, and leave
  the observation counter at two instead of three. Count that exit as well;
  the strict three-sibling count, join, failure-cause and budget assertions stay.

A deterministic unit regression reproduced the missing early-cancellation count
before the fix. It passed 20 repetitions with the race detector afterward.
The time-zone lifecycle passed on newly created Software with management-verified
active/effective 4x QPF. The snapshot failure/join lifecycle passed ten repetitions with the race
detector (40 failure-mode executions). All Go unit packages also pass.
These changes affect tests only; production behavior and historical native
membership/default comparison evidence are unchanged. Live ordinary and snapshot
checks run sequentially on the disposable fixture.
