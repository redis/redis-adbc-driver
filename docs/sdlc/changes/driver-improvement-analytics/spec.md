# Contract

The traffic selector scopes usage and retention. Explicit fixed client classifications override CI environment detection; legacy runner inference is a separate uncertain category. Unknown is never assumed to be customer traffic.

Cumulative version/statement/traffic comparisons preserve delta deduplication. Historical versions stay unknown. Recent-sample size comparisons use measured input work only and explicitly disclose sample bias and distinct percentile semantics.

New diagnostic categories are allowlisted at both client and collector boundaries. Raw errors and plans never enter wire types. Supported aggregate attempts, group-limit retries and row fallback counts reuse atomic metrics; uninstrumented paths remain unobserved. Old schema-1 clients remain accepted; collector rollout precedes new drivers.

See [intent](intent.md) and [plan](plan.md).
