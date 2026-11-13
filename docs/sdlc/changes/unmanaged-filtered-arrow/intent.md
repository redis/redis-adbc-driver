# Intent

Date: November 13, 2026. Requested by the repository user; implemented by Codex.
Base `3213aa54`, production equivalent to measured `5f759300`.

Reduce decoded-map and intermediate-row allocation for filtered explicit
unmanaged Arrow reads. The previous direct projection supports unfiltered reads;
filtered four-reader controls still use live HGETALL and decoded Value maps.
Keep application HASH/index ownership and live predicate semantics unchanged.

See [contract](spec.md), [plan](plan.md) and [validation](validation.md).
