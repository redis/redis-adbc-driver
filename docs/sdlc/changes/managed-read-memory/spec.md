# Contract

See [intent](intent.md) and [plan](plan.md).

The raw HASH page's outer slice is driver-owned. Once a row has been handed to
the synchronous projection callback, its page slot can be cleared; the callback
retains its local row slice and any decoded immutable strings. Never clear or
reuse the command's own reply slots. Clear the remaining outer page references
on completion, callback errors or cancellation.

Arrow output must outlive raw-page release, subsequent reader advancement,
reader Release and statement cancellation. Preserve exact integers above 2^53,
NUL/Unicode strings, empty strings and NULLs, plus complete values/multiplicities.
Keep the current 4096-row candidate/fetch bounds and one-page lookahead.

Additive command construction may prebox immutable field-name strings once per
fetch batch. Every command must still own fresh argument slots, its key/header
and result. Preserve HMGET command metadata/first-key routing, pipeline hooks
and errors, blank-key alignment, binary/duplicate/empty fields and field order.
Do not share mutable argument slices or bypass the existing pipeline.

Quantify peak worker RSS and full-result elapsed distributions with native C-ABI
1/4-reader controls, not only Go allocation counts. No speedup or memory claim
without matched measurements and complete independent output validation.
