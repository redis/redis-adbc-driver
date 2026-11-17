# Specification

2026-11-16. Base: `225e0056`. Status: implemented; maintainer review pending.
See [intent](intent.md), [plan](plan.md).

Every field built by `arrowField` carries `ARROW:FLIGHT:SQL:TYPE_NAME` equal
to its `ColType.SQLName()`. This preserves retained VARCHAR/CHAR lengths,
NUMERIC precision/scale and temporal fractional-second precision without
inferring them from Arrow units. This is the canonical declaration used by
GetObjects and information_schema.columns.driver_data_type, not original SQL
source text: aliases normalize, implicit precision becomes explicit, TEXT is
reported as VARCHAR, and computed expressions describe their inferred types.

Nested Arrow child fields and inferred parameter fields use the same metadata
contract; no new SQL parser or type inference rules are introduced.

GetTableSchema covers empty/nonempty permanent and temporary tables and views,
with exact case-sensitive names. Query readers and ExecuteSchema describe their
result types through the same builder. GetObjects continues reporting the same
canonical declarations in xdbc_type_name. Column remarks coexist with TYPE_NAME
in GetTableSchema; query results do not gain comment propagation.

Ordinary fields receive no Arrow extension keys. JSON/JSONB keep arrow.json;
UUID and TIME WITH TIME ZONE keep their existing string mappings. Ordinary
TYPE_NAME is descriptive on ingestion: it does not impose VARCHAR length or
replace the established physical-type mapping. Existing semantic exceptions
continue to apply. A client dropping optional field metadata loses declaration
fidelity, not SQL execution or value correctness. No client capability promises
or new unsupported-type fallback are introduced.

Acceptance: reproduce the old missing declarations; test physical type and
IPC preservation, ingestion acceptance, native table/view/temp/query schemas,
comments and GetObjects; validate the unchanged public Flight service with a
standard Flight SQL client against verified Redis Software 4x QPF.
