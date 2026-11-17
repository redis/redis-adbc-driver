# Validation

See [intent](intent.md), [spec](spec.md), [plan](plan.md).

Baseline: the previously retained C-ABI race probe aborts in createHandle before
any ownership scenarios. The unchanged Go 1.27.3 runtime/cgo source explicitly
states that coercing an integer Handle to unsafe.Pointer is unsafe. Arrow Go
v18.8.0 cdata uses a C uintptr holder with explicit free on release.

Current implementation preserves all forced GC calls. Gate results are pending;
passing a focused holder test is not full Arrow lifetime or sanitizer acceptance.

The focused driverlib test initially exposed three pre-existing vet failures:
nonconstant text passed as setErr's format argument. Those calls now pass `%s`
with the message as data, preserving literal percent markers. Vet stays enabled.
`go test -count=1 -race -gcflags=all=-d=checkptr=2 -tags driverlib ./pkg`
passes, including GC survival, registry deletion and 2,048 independent concurrent
holder lifetimes. Native/sanitizer gates remain to be run.
