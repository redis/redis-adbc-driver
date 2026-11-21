# Two-participant immutable-generation reference model

Issue [#568](https://github.com/redis/redis-adbc-driver/issues/568), 2026-11-20.
Source base: `bc6780e9`. This is executable design exploration, **not a driver
implementation, Redis compatibility proof, distributed transaction product, or
completion of #568**. It uses only the Python standard library and no servers.

```sh
python3 scripts/multishard_model/model.py
python3 -m unittest discover -s scripts/multishard_model -p 'test_*.py' -v
```

The CLI exhausts the repaired finite model and prints three negative examples.
A state limit raises `INCOMPLETE`; truncated exploration cannot report success.

## Encoded contract

Two planned participants P0/P1 and their local grants are registered/issued
before the initial state, before local output is prepared.
Each has one immutable generation/prefix and an independently expiring writer
lease. Preparation and sealing are atomic abstractions for complete local work;
this model has no rows, Arrow batches, Search, index readiness or native commands.
Seal receipts become central knowledge; a lost seal reply is resolved by a read,
without replaying preparation/input. Sealed output remains retained after lease
expiry. The central authority atomically decides OPEN → COMMIT or OPEN → ABORT.
Only two known seal receipts permit commit; the decision never reverses.

One representative reader pin models the zero/nonzero retention boundary. A
reader pins only a live committed generation; retirement permanently rejects
new pins and commit. Existing pins remain protected. Central cleanup tickets are
issued only after terminal abort, or after irreversible retirement with no pins.
Local collection accepts only an issued exact generation/participant/prefix
ticket, fences the object as DELETING, then completes deletion. New writes cannot
cross that fence. Prepared objects have no TTL or independent lease-expiry GC.
An issued ticket's safety cannot be revoked under the no-rollback assumptions.
The model's `authorize_delete` checks capability issuance as message provenance
(a ghost/authenticity check), not a runtime cross-shard read. The separate
`local_delete_fence` receives only local participant state and the delivered
capability; it cannot inspect the central decision or ticket registry. Capability
authentication is assumed, not implemented or proven cryptographically.

The breadth-first explorer enumerates all event reorderings admitted by this
finite state machine, including expiry before/after sealing, seal/decision reply
loss and read resolution, cancellation, commit, retirement, pin/release, ticket
delay and cleanup. Duplicate no-op deliveries and arbitrary message delay/loss
are abstracted by idempotent fences and leaving enabled events unexecuted.
There is no fairness/liveness claim: perpetual message loss may retain output
forever. Physical prefixes are unique and never reused. A separate two-incarnation
example checks the aliasing hazard and exact-ticket/reuse defenses.

Local prepare/seal delivery checks only the local unexpired grant and phase;
there is **no atomic check of the remote central decision**. Previously issued
requests may arrive after central abort/retirement, and their seal receipts may
arrive late. Terminal decisions cannot republish them. A GC ticket delivered
before initialization fences even an absent object, permanently preventing late
initialization/recreation. Ticket delivery after initialization fences that
existing output before deletion. Both race orders are encoded and tested.

## Results and counterexamples

The repaired explorer reaches **3,096 states / 14,340 edges**, with no encoded
visibility/pinned-root violation. This is finite exploration, not a mathematical
proof for arbitrary participants, pins, generations or implementation behavior.
Every visible/pinned committed generation must retain both sealed participants;
reads either return both or raise `Unavailable`, never silently return a subset.

- **Naive lease-expiry GC:** prepare/seal P0, expire its lease, delete P0 locally,
  prepare/seal P1, then commit using the retained seal receipts. The committed
  root references missing P0. Local expiry cannot authorize destruction.
- **Delayed ticket plus reused prefix:** abort an old generation and delay its
  ticket, reuse that prefix for a new committed/pinned generation, then deliver
  a prefix-only delete. The new participant disappears. The repaired allocation
  refuses prefix reuse, and exact generation/participant/prefix matching also
  rejects the old ticket even if a caller deliberately bypasses allocation.
  Its registry is a sequential in-memory toy, not a concurrent/distributed
  allocator implementation; successful allocation records the used prefix.
- **Durability rollback — UNSOLVED:** commit both participants, retire with no
  pins, issue a valid ticket and delete P0. Restore central history from before
  retirement while retaining the remote deletion. The restored committed root
  now references missing P0. The example detects that state and makes a read
  unavailable; detection does not restore data or establish the promised
  durability boundary. This event is deliberately outside the positive model.

## Assumptions and limits

Central decisions, retirement, participant identities and ticket issuance are
durable and monotonic; local seals/deletion fences are durable. There is no
central or participant history rollback, restore, identity reuse or Byzantine
ticket forgery in the positive explorer. Partial persistence, failover and restore
need a separately specified common durability boundary before any runtime
guarantee. The negative rollback example prevents treating that assumption as
an established Redis feature.

The model assumes one central publication/reference/pin authority and retained
immutable outputs, rather than independently timed live shard read views. It
does not require a cross-shard live read-view API for immutable manifest reads.
Whether supported Redis primitives can implement every encoded atomic/durability
boundary remains unverified. Native views can be useful for live-source capture,
but do not replace this retention/decision contract.

Excluded: multiple writers/generations in the positive BFS, pin lease expiry,
unbounded reader counts, resource budgets, topology migration, DML overlays,
compaction, schema races, Search, actual local script failures, backup compatibility
and performance. There is no native test, AWS deployment, ingestion or benchmark
in this change. Human design review and full issue acceptance remain pending.
