# Managed writer handoff: proposed contract

Date/source/status: [intent](intent.md). This is a scheduling optimization, not an admission proof.

- Default `adbc.redis.writer_admission_queue=false` retains legacy native admission without fingerprinting or registry work. When explicitly true, one bounded process-local registry coordinates opaque endpoint/principal/database/target identities across independently created database handles. Dynamic credential providers and custom routing may opt out to existing admission. There are no explicit endpoint, credential, table-name, store/client/transport or catalog fields in the registry. An active cancellation registration can indirectly retain its parent execution context until normal turn release or cancellation; its stop closure is detached/nilled on release. Request/target cardinality bounds do not bound arbitrary caller-owned context memory.
- Up to 256 target queues and 1,024 requests are tracked. Overflow retains the existing native server-controlled path; it does not introduce an error or bypass any check.
- The first ordinary fresh pin and metadata lookup identifies a managed target. A non-head releases its pin before waiting; the head repeats fresh pin/metadata/native admission after handoff. A local turn never authorizes a write.
- The head uses the existing bounded backoff when the authoritative server refuses admission. Other queued local writers do not repeatedly acquire pins. Other processes and uncoordinated clients still compete through unchanged server admission. FIFO applies only to tracked local turns, with cancellation and bounded-overflow exclusions.
- The confirmed writer keeps the turn through its snapshot pin lifetime. Ordinary bounded pin cleanup hands it off. Cancellation/Close can release the local turn early even if user input ignores cancellation; unchanged native lease/admission rules protect subsequent writers. No Redis command, input or publication is replayed.
- Missing/unmanaged/changed targets release or rekey the local hint and retain the existing fresh catalog behavior. Different logical tables retain independent turns.
- All metadata/ACL/lease/row-directory/publication checks and exact Arrow values remain unchanged. Protocol version 17 and durable formats remain unchanged.

The identity fingerprint and all lifecycle race cases require independent root review before execution. See [plan](plan.md). PR: integration pending.

## Explicit opt-in gate added after measurement

The public database option `adbc.redis.writer_admission_queue` accepts exact
`true`/`false` strings and defaults to `false`. It affects newly opened
connections; existing stores retain their selection. Default false avoids
identity fingerprinting, the extra trust-bundle read and local registry work.
True opts eligible managed writers into the bounded process-local tradeoff; it
is not another driver/table mode, a server-admission bypass or a global FIFO
guarantee. Live unmanaged writes are unaffected.

The always-on `03e04002` prototype supplied the recorded Go pilot/long timings.
Adding this public option is a functional post-measurement change. Its actual
default/enabled option, lifecycle and safety checks are recorded separately, and
a new frozen native candidate must enable/verify the public option before any
final selection claim. The preceding Go measurements must not be relabeled as
timings of the final gated build.
