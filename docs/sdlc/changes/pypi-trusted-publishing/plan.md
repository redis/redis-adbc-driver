# Plan and evidence

Date: 2026-10-20. Status: implementation prepared.
See [intent](intent.md) and [spec](spec.md).

1. Verify 0.0.15 release CI, wheel checksums, metadata, and installation: done.
2. Publish the three existing wheels to PyPI: done.
3. Reuse the `publish-pypi.yml` workflow added to main during setup: done.
4. Add safe retries and update installation/runbook docs: done.
5. Verify publisher registration, merge the focused change, and verify OIDC.

Initial publication evidence: all three release checksums match PyPI JSON.
A fresh Python 3.12 environment installed `adbc-driver-redis[dagster]==0.0.15`
from PyPI, loaded `AdbcDriverInit`, and constructed `RedisADBCIOManager`.
Twine metadata checks passed with warnings about the missing long description.
No Redis integration suite was rerun for this publication-only change.

Workflow validation: actionlint 1.7.12 passed with shellcheck disabled (not
installed). The duplicate release-job implementation was removed after detecting
the existing manual workflow. PyPI visibly lists `publish-pypi.yml` for this
repository and environment `pypi`. `git diff --check` passed. The GitHub `pypi`
environment has no required reviewers or branch restrictions; the workflow
limits execution to main. Its existing stable release, platform, checksum, and
metadata validation is unchanged. Add `skip-existing: true` for safe retries.
