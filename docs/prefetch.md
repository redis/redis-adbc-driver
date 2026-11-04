# Wide-read prefetch

The default-enabled candidate overlaps HASH fetching with Arrow conversion for
eligible wide managed-table streams. Ordinary reads and public partition reads
share the same path. Eligibility requires at least 64 needed columns, a positional
HASH projection and more than one discovery page. Known single-page discoveries,
narrow projections, GPU execution and unsupported plans retain their existing
paths. Batch sizes, returned types and row ownership are unchanged.

Each reader can retain its current reply page plus **one additional owned page**.
The lookahead follows the existing 4,096-row discovery-page request; it is not a
fixed byte quota. Variable-width values determine memory cost, and concurrent
readers each have their own page budget. Delivered Arrow batches have separate
row/byte settings and can contain several discovery pages.

The producer owns discovery and HASH fetches; the consumer owns typed decoding
and Arrow construction. Cursor READ/DEL are serialized, cancellation joins the
producer and its cursor finalizer before pools close, and consumer EOF rechecks
readiness after final-page conversion. Concurrent updates/deletes are observed
at HASH fetch time. This provides no snapshot or new consistency guarantee.

The implementation uses the existing Search/HMGET commands and current
RESP3/TLS/ACL/routing requirements. It needs no server export command or module.
A typed server export remains a separate [server proposal](typed-export-design.md).

Normal Go builds and `make -C go build` include the default candidate. The
build-only `scanprefetchoff` tag selects the synchronous baseline for measurements
or troubleshooting. Include it alongside other build tags, for example
`driverlib,scanprefetchoff` for a C shared library, retaining the Makefile's version
linker flag. The legacy `scanprefetch` tag remains accepted and is unnecessary
when default prefetch is enabled; explicit `scanprefetchoff` takes precedence.

See the [qualification record](sdlc/changes/prefetch-default/validation.md) for
completed checks and the remaining final gate. No universal speedup or strict
per-query byte cap is promised.
