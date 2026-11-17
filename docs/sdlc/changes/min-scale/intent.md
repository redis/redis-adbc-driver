# MIN_SCALE intent

Date: 2026-11-17. Requestor: Albert Wong. Author: Codex.
Source baseline: `19ebcb46` (merged #476). Status: implementation and local verification.
Issue: [#110](https://github.com/redis/redis-adbc-driver/issues/110).

The open-issue review found `min_scale(numeric)` explicitly requested in #110
and absent from the function registry. Add this bounded scalar operation with
exact finite values and PostgreSQL signature/error behavior. This advances #110;
it does not complete its full function/formatting surface or numeric special
value design (#362). GCD/LCM overload work (#475) is excluded.

See [spec](spec.md), [plan](plan.md) and [validation](validation.md).
