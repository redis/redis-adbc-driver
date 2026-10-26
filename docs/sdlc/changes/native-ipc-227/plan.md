# Implementation plan: native IPC

Date: 2026-10-22. Author: Codex. Status: verification.
Source: `9493c647aee33916cfc32840b9ba517388e51b50`.
Linked [intent](intent.md), [spec](spec.md), and [#78](https://github.com/redis/redis-adbc-driver/issues/78).

1. Create CDC-only #457 and narrow #227. Completed.
2. Add native statement input/output/format options and sequential stream/file
   adapters in `go/ipc.go`; integrate standard execution and partition guards.
3. Expose option names in Python; provide an executable example using only ADBC
   manager calls. Serialization stays inside the driver.
4. Exercise Go ownership/cancellation/late-error tests and independent PyArrow
   round trips through the C library on local Redis Software with unique tables.
5. Publish capability matrix and operator/memory assessment; link existing dated
   memory evidence without presenting it as fresh validation.
6. Review according to REVIEW.md and record actual checks in [evidence](evidence.md).

No client-only serializer or custom C ABI extension is needed. Broader executor
changes remain tracked in #227 and require separate operator-specific evidence.
