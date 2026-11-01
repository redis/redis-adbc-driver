# Close the remaining Arrow interchange issues

Date: 2026-10-31. Requestor: repository user in the active Codex chat.
Base: `006f09950d72d9bea4b4b7295c07f334c5386f61` (merged PR #269).

Complete [#198](https://github.com/redis/redis-adbc-driver/issues/198),
[#200–207](https://github.com/redis/redis-adbc-driver/issues/200) and
[#209](https://github.com/redis/redis-adbc-driver/issues/209) without treating the
already merged implementation as sufficient acceptance proof. #199 and #208
are already closed and remain predecessors.

Deliver safe explicit policies for blocking fallback, dictionary output memory
and extension storage normalization; fill scalar/nested schema/value/lifecycle
coverage through Go, native C, IPC and independent partition readers. Preserve
exact numeric/temporal semantics and reject malformed or incompatible input.

Physical input-layout identity is deliberately normalized under the public
contract; no native unsigned SQL type, arbitrary extension plugins, Map/Union
storage, universal memory guarantee, CDC, transactions or consistency features
are added. Intermittent EOF investigation remains separate in #265/upstream
Docker #776. See [specification](spec.md) and [verification](plan.md).
