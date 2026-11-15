# Managed writer handoff: proposed contract

Date/source/status: [intent](intent.md). This is a scheduling optimization, not an admission proof.

- One bounded process-local registry coordinates opaque endpoint/principal/database/target identities across independently created database handles. Dynamic credential providers and custom routing may opt out to existing admission. No endpoint, credential, table name, store, client, transport or catalog object is retained in the registry.
- Up to 256 target queues and 1,024 requests are tracked. Overflow retains the existing native server-controlled path; it does not introduce an error or bypass any check.
- The first ordinary fresh pin and metadata lookup identifies a managed target. A non-head releases its pin before waiting; the head repeats fresh pin/metadata/native admission after handoff. A local turn never authorizes a write.
- The head uses the existing bounded backoff when the authoritative server refuses admission. Other queued local writers do not repeatedly acquire pins. Other processes and uncoordinated clients still compete through unchanged server admission. FIFO applies only to tracked local turns, with cancellation and bounded-overflow exclusions.
- The confirmed writer keeps the turn through its snapshot pin lifetime. Ordinary bounded pin cleanup hands it off. Cancellation/Close can release the local turn early even if user input ignores cancellation; unchanged native lease/admission rules protect subsequent writers. No Redis command, input or publication is replayed.
- Missing/unmanaged/changed targets release or rekey the local hint and retain the existing fresh catalog behavior. Different logical tables retain independent turns.
- All metadata/ACL/lease/row-directory/publication checks and exact Arrow values remain unchanged. Protocol version 17 and durable formats remain unchanged.

The identity fingerprint and all lifecycle race cases require independent root review before execution. See [plan](plan.md). PR: integration pending.
