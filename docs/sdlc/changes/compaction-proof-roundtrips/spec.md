# Contract

Preserve all production behavior at the recorded base. Add only supplemental
pipeline-shape counts to the existing opt-in compaction diagnostic. Shapes contain
ordered fixed command categories with repeated adjacent categories compressed;
they contain no keys, fields, arguments, values, endpoints or credentials.
Keep the existing round-trip categories, timing fields, workload, pacing,
checkpoint bounds and complete final value validation unchanged.

Each measurement uses fresh per-step pins and unchanged committed manifest,
root-map, bucket-directory/reference, object existence/reference, descriptor,
schema/OID/lineage, index readiness and source-row proofs. No cross-step checked
metadata is retained. Unknown writes remain resolved from durable state without
replay; no control operation, Lua script, ACL requirement or protocol changes.

Counts explain existing calls; they do not demonstrate a speedup. Do not time an
identical production binary twice and label it baseline/candidate. Keep all
attempts and distinguish correctness, profiling and native evidence.

See [intent](intent.md) and [plan](plan.md).
