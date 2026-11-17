# Plan

Date/base/requestor/status: see [intent](intent.md) and [spec](spec.md).

1. Preserve the existing failing baseline C-ABI race/checkptr outcome; inspect
   the current upstream template and official Go/Arrow ownership implementations.
2. Replace the invalid pointer encoding with C-owned integer holders; pair all
   four release paths and add direct GC/concurrent registry-lifetime regressions.
3. Build/run the actual native stub C/PyArrow ownership probe with checkptr/race
   enabled; extend coverage for imported/rebound/retained/error/concurrent paths.
4. Run sanitizer/native real-driver checks and record every gate/failure in
   [validation](validation.md). Do not remove GC or relabel old benchmark results.
