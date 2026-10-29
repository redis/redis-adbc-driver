# Plan

Date: 2026-10-29. Source: public main `da1e7c1`. Author: Codex. Status: completed locally.

1. Build the native library and start an isolated Redis Software instance: done.
2. Add focused direct pandas SQL read tests in `python/tests/test_pandas_direct_reads.py`: done.
3. Execute all new cases with a real Redis connection and check values/types: done.
4. Update the integration guide and comparison table with evidence: done.

The existing Python analytics CI job collects `python/tests/` and already
installs pandas through the test extra; no workflow change is needed. CI was not executed here.

PR preparation moved the changes onto public main `da1e7c1`, which has unrelated
history to the original checkout. Existing `test_pandas.py` is preserved; the
additional cases live in `test_pandas_direct_reads.py`.

See [intent](intent.md), [specification](spec.md) and [validation](validation.md).
