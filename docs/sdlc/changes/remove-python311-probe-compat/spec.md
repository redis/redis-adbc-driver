# Contract

Date: 2026-11-17. See [intent](intent.md) and [plan](plan.md).

Both private native probe builders require Python 3.14 or newer and use
`tar.extractall(..., filter='data')`. Remove the custom extraction path used for
Python 3.11. Keep exact source selection, Go toolchain selection, private build
locations and shipping runtime behavior unchanged.

Document Python 3.14 for new builder/runner invocations. Historical reports keep
their actual Python versions and hashes. The historical native-proof auditor
verifies its frozen harness commit rather than requiring the current builder to
have its old hash. This is provenance checking, not an older-runtime code path.
