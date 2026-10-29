# Plan

Date: 2026-10-29. Source: `95d6583`. Author: Codex. Status: completed locally.

1. Build the native library and start an isolated Redis Software instance: done.
2. Add focused direct pandas SQL read tests in `python/tests/test_pandas.py`: done.
3. Execute all new cases with a real Redis connection and check values/types: done.
4. Update the integration guide and comparison table with evidence: done.

The existing Python analytics CI job collects `python/tests/` and already
installs pandas through the test extra; no workflow change is needed. Its Redis
OSS environment differs from this local Software run; CI was not executed here.

See [intent](intent.md), [specification](spec.md) and [validation](validation.md).
