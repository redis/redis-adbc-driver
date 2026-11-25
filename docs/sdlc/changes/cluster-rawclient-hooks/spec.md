# Contract

Date: 2026-11-24. Status: implemented; human approval pending.
See [intent](intent.md) and [plan](plan.md).

`sqlHarness.rawClient(hooks...)` runs its test hooks (the pin-origin hook and
the given hooks) on every command exactly once, before the snapshot hook, on a
single endpoint and on an OSS Cluster API endpoint alike:

- On a cluster, each node client gets the test hooks through `OnNewNode`,
  registered before `addSnapshotHooks` registers its own, so the hook order on
  a node matches the cluster client's.
- The cluster client's first hook, `clusterHooked`, marks the context of each
  command and pipeline that goes through the cluster client's hooks. The node
  copies (`nodeTestHook`) pass marked commands straight on, so a command the
  cluster client sends on to a node is not seen twice.
- Unmarked commands, which reach a node's client directly (Search commands via
  `store.search`, WATCH … EXEC via `ClusterClient.Watch`), run the test hooks
  on the node.
- A node copy keeps the hook's `DialHook`; a cluster client never dials.

Single-endpoint behaviour is unchanged. No driver code, assertion or skip
changes. `TestRawClientHooksSeeEachCommandOnce` pins the contract: a counting
hook must see SET, a pipelined GET and EXISTS, `FT._LIST` through
`store.searchDo`, and a WATCH transaction's GET and SET each exactly once.
