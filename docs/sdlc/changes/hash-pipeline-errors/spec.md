# Specification

See [intent](intent.md) and [plan](plan.md).

`fetchHashCommands` tolerates only a nil error or direct `redis.Nil` from
pipeline execution. Wrapped Nil, joined Nil/transport errors and ordinary
transport errors must propagate with their diagnostics. Both `fetchRows` and
`fetchRowSlices` must return no rows on failure. Individual command errors must
still propagate, including when the pipeline returns nil or direct Nil.

Use a nondelegating go-redis hook that populates valid HMGET values separately
from pipeline and command errors. Positive controls assert unchanged values
for both readers. Existing parsed scalar-cohort coverage verifies source failure
and absence of successful cached lanes. These are synthetic fault tests, not
evidence of a live Redis failure or a Redis Software compatibility run.
