# Specification

Date: 2026-11-05. Status: experimental. See [intent](intent.md) and [plan](plan.md).

The public option, positive capability checks, schema, bounded range protocol,
consistency contract, ownership checks, feature restrictions and measured scope
are specified in [the stopgap guide](../../../redis-flex-stopgap.md).

Correctness requires complete fetches on unchanged data, exact HASH decoding,
no range output before overflow is resolved, and terminal errors on ambiguous
replies. Full feature parity and performance acceptance for #322 remain open.
