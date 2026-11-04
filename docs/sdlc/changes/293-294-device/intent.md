# Intent: Arrow device layouts and manager integration

Date: 2026-11-04. Requestor: repository user. Author: Codex.
Base: `efb9cbfb1c280f7ba0c11667d5a3a2e6309b167d`. Status: implementation/validation.
Issues: [293](https://github.com/redis/redis-adbc-driver/issues/293),
[294](https://github.com/redis/redis-adbc-driver/issues/294).

Extend explicit physical interchange to nullable variable-width UTF-8/binary,
including large offsets, and expose a versioned Python staging adapter that
uses public manager APIs. Preserve borrowed input ownership and independent
output lifetime. CPU clients remain independent of CUDA packages/libraries.

No expansion of CUDA SQL execution, dictionaries, decimal/time or nested types.
See [spec](spec.md), [plan](plan.md) and [validation](validation.md).
