# Owned typed HMGET result evidence — November 13, 2026

All **36 samples across twelve cases** passed, with complete byte/null validation
before and after each case. The [complete results](results.json) retain every
sample, hashes, pinned versions, hardware and command outcomes. Measured fixture
source is `12bb757d0b0ad40c37ce3bc27d8fd89baa36acdb`, based on merged main
`ba855f79`. Go 1.27.3, darwin/arm64, Apple M5 Max, GOMAXPROCS=1,
three observations per case with a 200ms minimum. Other task builds/tests/timing
were paused; light source/docs work continued on the shared workstation.

## Owned result versus generic result

Median per 4096-row × 105-field page with a 32-KiB parser buffer:

| Consumer | ms/page | Go bytes/page | Go allocations/page |
|---|---:|---:|---:|
| Actual SliceCmd | 16.677 | 22,323,130 | 782,468 |
| Fresh owned string/null vectors | 11.972 | 16,804,441 | 412,974 |

Observed parser/consumption time was 28.2% lower, Go allocation bytes
24.7% lower and allocations 47.2% lower in this isolated comparison.
All 256/1024/4096-row, 32-KiB/1-MiB cases are preserved. These short repetitions
are workload observations, not confidence intervals or a promised driver gain.

The new consumer allocates **fresh row vectors inside timing**, including string
slots and a separate validity vector. String payloads use pinned go-redis's
ReadString primitive and are independently owned. Unlike the earlier
string_slice_reuse prototype, later replies do not replace slots retained by an
older result. Its byte count therefore includes vector allocation that was
excluded from that earlier reused-slot control. Do not combine the two numbers
into one allocation or RSS claim.

Both cases prepare the common outer page, wire bytes and parser outside timing;
every field is consumed. Generic decoding uses the actual SliceCmd readReply.
Command construction, network/TLS, server work, pooling, hooks, Arrow, IPC and
Python are excluded. No Redis server was used and no RSS claim is made.

## Ownership and public API boundary

Focused race checks passed during the benchmark invocation and again separately
with GOMAXPROCS=4. Tests retain all original results while overwriting parser
input, decode different bytes/null masks into reused command slots, mutate an
independent result, and read retained results concurrently. They check NULL versus
empty, exact integers above 2^53, NUL/Unicode/CRLF, a 16-KiB binary value, following
reply alignment, and incomplete/server-error/NULL-array failure without a partial
owned result.

This is an **HMGET-shaped owned-result contract experiment**, not a public
command implementation or a replacement for generic SliceCmd across arbitrary
RESP types/commands. A supported typed HMGET API still needs command method/type
design and integration tests for result-inspecting hooks, pipeline/async faces,
routing, cancellation and retry behavior. Owned strings avoid requiring a new
borrowed-byte lifetime; the validity vector must preserve NULL/empty distinction.
No production driver or dependency change is introduced here. Array decoding
is individually atomic; whole-page rollback and parser/socket reuse after malformed
replies are not provided or claimed by this fixture.

See the [runner and reproduction instructions](../README.md#independently-owned-typed-result-control)
and [change validation](../../../docs/sdlc/changes/hmget-owned-result-evidence/validation.md).
Raw logs and private source copies remain private. Historical measurements and
the original default benchmark suite remain unchanged.
