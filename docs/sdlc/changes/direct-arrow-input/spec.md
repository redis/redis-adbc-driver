# Contract and acceptance

Keep complete required-input decoding and timezone fitting before output
coercion/append. Fuse already compatible primitive coercion and Arrow append
using pointers into the existing one-row scratch. Preserve fallback for all
other conversions, aliases, mismatched physical layouts, and byte-budget mode.
No added row staging or borrowed result buffers; Arrow owns appended data.
Keep width eligibility unchanged at 64 columns and do not tune after timing.

Before any timing: acceptance requires complete correctness/race/Software
checks, consistent native elapsed benefit in both managed/unmanaged wide read
matrices, and no clear memory regression. Retain every attempt and all samples.
A clear RSS range increase, material timing regression or unproven correctness
keeps candidate evidence-only. Micro timings alone cannot justify adoption.
See [intent](intent.md) and [plan](plan.md).
