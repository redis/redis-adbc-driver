# Evaluated contract

The unapplied candidate retained the entire Value conversion matrix and all
full-slice coercion/default/length/NOT NULL/CHECK validation. It preserved
explicit source keys, managed allocation/publication, metadata facts, external
text encodings and generic Arrow fallbacks. Arguments retained owned slots and
owned input string payloads; no borrowed Arrow lifetime was introduced.

It pre-sized managed field and unmanaged command vectors using actual non-NULL
counts, boxed stable unmanaged fields once per conversion slice, and used direct
string length in the existing byte estimator with the same formatter fallback.
Physical field ordering, provenance, added missing fields, NULL omission,
empty/binary strings, exact numbers, row/pipeline limits, unknown outcomes,
cancellation and acknowledged counts were required to remain identical.

The candidate is rejected: correctness success and reduced isolated allocations
do not override native four-writer regressions or the one-row sparse cost.
This evidence-only branch applies no production or benchmark Go change. Its
[candidate.patch](candidate.patch) reproduces the exact source/test experiment
when applied separately to baseline `3213aa54`; it is not a supported option.

See [intent](intent.md), [plan](plan.md) and [validation](validation.md).
