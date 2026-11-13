# Validation status

November 13, 2026. Implementation is based on `3213aa54`; production baseline
matches the fully measured `5f759300`. Native performance acceptance remains
pending. No elapsed-time or RSS improvement is claimed from these checks.

## Regression and focused race checks

A private Go overlay inserted the baseline-equivalent residual-refusal guard
at the production projection factory. The new test compiled and then failed
`TestUnmanagedFilteredProjectionParity` because no filtered projection was
selected, as expected. This is a selection regression with the same candidate
test/types, not a claim of executing the entire old source tree. Raw logs and
the overlay are retained privately.

The candidate then passed this focused command with Redis endpoints unset:

```sh
go test -race -count=1 -p 1 -timeout=5m \
  -run '^Test(Unmanaged(Filtered|ScanProjection|Projection|WideProjection)|ScanProjection)' -json .
```

There were 15 test pass events, six expected endpoint-gated integration skips,
zero failures/race reports. Parent/nested events are counted. Cases compare
the generic evaluator's results, status/SQLSTATE and error ordering against
compiled integer/boolean predicates, typed parameters and NULLs. They retain
short circuit, malformed required fields on rejected rows, unsupported fallback,
exact integers above 2^53, binary keys, duplicate outputs, OFFSET/LIMIT,
processed/matched diagnostics and independently retained Arrow buffers.

## Verified Software live race checks

The parent explicitly loaned its dedicated owned Redis Software database.
Its `make -C go redis-up` fixture was reverified immediately before testing:
active/effective 4x QPF, six Search workers, mnp/32 connections, one 4-GiB
noeviction RAM shard and strict certificate/hostname-verified data TLS/RESP3.
No server was provisioned or alternate endpoint used by this child.

The live `go test -race -count=1 -p 1 -timeout=5m` selection passed 24 test
events, zero skips/failures/races. A 9001×105 fixture checks every selected
value/type/NULL and membership under an indexed integer range; retained records
survive Next, complete or early Release and cancellation. The live mutation test
installs the new projection before changing/deleting/retyping a discovered
source HASH and confirms stale/non-HASH rows remain excluded. Existing numeric
range, exact large-integer/index-change, filtered foreign-membership and
unfiltered ownership controls also passed. Source DROP/read cleanup checks retain
application HASHes, arbitrary application __rowid fields and the application index.

After the checks every client/process exited. Removed all test indexes/data only
from the authorized loaned database and asserted DBSIZE=0 plus FT._LIST empty.
The parent retains its container/network/TLS ownership; this child did not tear
them down. Generated environment/credentials and all raw operational artifacts
remain private. No 100M ClickBench or unsupported server/provider was used.

All outcomes, changed-source hashes and private raw-log digests are in
[checks.json](checks.json). No source changed between these candidate checks.
Matched native baseline/candidate timing and broader final checks are pending
the parent's separate exclusive slots.

See [intent](intent.md), [contract](spec.md) and [plan](plan.md).
