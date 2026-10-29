# Polars regression rerun specification

Date: 2026-10-29. Author: Codex. Status: human review pending.

Run the unchanged `python/tests/test_polars.py` and `test_driver.py` against a
fresh native source build and a disposable Redis instance. The Software run
must verify active 4x QPF and effective Search routing before testing. Require
eight passing cases, zero skips and warnings treated as errors. Record exact
source and dependency versions and distinguish each server distribution.
Update only the Polars support claim and evidence; preserve the untested
Flight SQL claim and other tools' support claims.

See [intent](intent.md), [plan](plan.md), and [validation](validation.md).
