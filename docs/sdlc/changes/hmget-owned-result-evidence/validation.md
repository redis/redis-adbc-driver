# Validation and scope — November 13, 2026

Measured fixture commit `12bb757d`, based on merged main `ba855f79`.
Pinned go-redis v9.23.0 and Go 1.27.3; actual parser sources and fixture hashes
are retained in the [complete evidence](../../../../benchmarks/upstream-allocation/owned-2026-11-13/results.json).

## Actual checks

- One `run.py --suite redis-owned --mode bench --count 3 --benchtime 200ms
  --gomaxprocs 1` invocation completed: focused parser/ownership race checks,
  then all 36 benchmark samples across 12 cases. No failures/restarts/discards.
- Complete byte/null equality before and after each timed case. Every field is
  consumed during timing. New result vectors allocate inside timing; no borrowed
  slot reuse or fixed-size payload slab is excluded from this consumer's count.
- Separate `--mode check --gomaxprocs 4` race/lifetime invocation passed. Covers
  retained original results after input/parser/command reuse, independent result
  mutation, concurrent read-only retention, large binary values, NULL/empty,
  exact integer bytes, Unicode/NUL/CRLF and error/incomplete-array replies.
- `summarize.py` audited all 36 samples, twelve case identities and three samples
  per case. The new summarizer also reproduced all 84 historical samples and 28
  distributions exactly from their original raw logs, without rewriting them.
- Default suite remains unchanged; no Arrow test rerun was needed for this
  opt-in server-free parser fixture. The module cache was not modified.

## Interpretation

At 4096×105 / 32-KiB reader size, fresh owned typed results observed 28.2% less
parser/consumption time, 24.7% fewer Go bytes and 47.2% fewer allocations than
actual SliceCmd in this small local sample. This is a proposed typed-result
contract experiment, not a supported go-redis command API, generic RESP decoder
replacement, borrowed buffer implementation, driver patch or end-to-end speedup.
Hook/pipeline/async/routing/cancellation/retry API integration remains future
upstream design and validation. No RSS claim is made.

No Redis server, ClickBench, external endpoint or additional infrastructure was
used. Raw logs and private dependency copies stay private; all public sample
metadata was checked for private paths/endpoints/credentials. No source modules
or production libraries were changed by the tests.

See [intent](intent.md), [contract](spec.md), [plan](plan.md) and
[report](../../../../benchmarks/upstream-allocation/owned-2026-11-13/report.md).

## Independent light review

The fetch-budget subagent reviewed the fixture, runner, summarizer and claims
without edits or heavy work and found no blocking issue. Review confirmed fresh
vector allocation inside timing and retained value ownership. Each array is
published only after complete decoding; a page can already contain prior
completed arrays when a later array fails. Malformed width/framing does not
promise parser/socket reuse. Those lifecycle/recovery semantics remain public
command-integration work, rather than a capability claimed by this fixture.

Final result/change-record links, private-path/endpoint/credential scans and
`git diff --check` passed. Historical sample/distribution equivalence passed.
Human maintainer review of any eventual public API remains separate.
