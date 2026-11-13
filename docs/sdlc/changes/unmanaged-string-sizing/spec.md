# Evaluated contract

The unapplied candidate changed only unmanaged INSERT command byte accounting:
a concrete string contributes its length plus the same 32-byte argument margin;
non-strings retain fmt.Sprint, with the same command-level margin. It required
exact equivalence for empty/binary/invalid-UTF8/NUL/Unicode strings, aliases,
controls and future non-string arguments.

Managed writes, the entire Value matrix and original command-vector growth/
per-cell field boxing stayed baseline. No NULL-count pass, cache allocation,
public option or borrowed input lifetime was introduced. Arrow/SQL preflight,
text encoding, source ownership, pipeline/row limits, duplicate-key outcomes,
acknowledged-count/error and non-replay contracts remained unchanged.

The candidate passed correctness and isolated controls but did not establish
native throughput/RSS gain. This evidence-only branch applies none of its
production, test or selected-case harness source. The complete
[candidate.patch](candidate.patch) can be applied to a separate clean baseline
`3213aa54` for reproduction; it is not a supported runtime option.

See [intent](intent.md), [plan](plan.md) and [validation](validation.md).
