# MIN_SCALE specification

Date: 2026-11-17. Source: `19ebcb46`. Status: implemented subset.
See [intent](intent.md), [plan](plan.md) and [validation](validation.md).

- Expose `min_scale(numeric) -> integer` with Arrow int32 result and strict NULLs.
- Accept finite decimals and implicit integer arguments. Resolve unknown string
  literals as numeric with the existing PostgreSQL numeric input parser. Parsing
  uses its existing bounds and retains scale without exporting an intermediate
  wide numeric through Arrow. This adds no new stored/Arrow numeric representation.
- Reject typed text, floats, booleans, arrays and other absent signatures with
  42883 during planning, including typed NULLs and empty input relations.
- Validate invalid unknown literals during planning, retaining 22P02/22003.
- Count necessary fractional digits exactly, without floating point or input
  mutation. Zero, integers and negative numeric scales return zero.
- Preserve existing numeric nonfinite rejection (#362); PostgreSQL's NULL result
  for NaN/infinities is an explicit unimplemented exclusion.
- Preserve result type with no rows and with decimal Arrow parameter batches.
