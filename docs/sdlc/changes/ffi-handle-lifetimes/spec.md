# Specification

Date/base/requestor/status: see [intent](intent.md). See [plan](plan.md).

Go's runtime/cgo Handle documentation explicitly prohibits coercing the integer
to unsafe.Pointer. The upstream driverbase template still uses that conversion
(inspected template blob 8ea2d3885d8fcf89af7627455a9b8fe86f0d6ff0).
Use the established Arrow Go cdata pattern: a C-allocated uintptr holder, with
only the integer registry ID stored inside. Never persist an unpinned Go pointer.

Each database, connection, statement and stream creates exactly one holder and
registry entry. Its normal/error release clears public ownership before child
callbacks, takes the Go owner, deletes the registry entry and frees the holder
exactly once. Retained Arrow batches and foreign callbacks keep their existing
owners and timing. Headers and public handle struct sizes remain unchanged;
private_data is opaque and cannot be transferred between driver instances.

All four forced collections remain. No GC policy or performance acceptance is
implied by fixing the safety gate. Preserve errors/SQLSTATE and existing Close,
cancellation and input-reference behavior. Run normal, race/checkptr and CGO
checks, then appropriate sanitizer/native lifetime qualification before selecting
any subsequent policy experiment. Live tests use owned verified Software 4× QPF.


Error messages are data: use a constant format string at all setErr callers that
forward an error message. Preserve literal percent markers, status and SQLSTATE;
retain vet and add actual C-boundary coverage for all three forwarding branches.
