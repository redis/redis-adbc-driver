# Plan and evidence

Date: 2026-10-20. Status: implementation prepared.
See [intent](intent.md) and [spec](spec.md).

1. Verify 0.0.15 release CI, wheel checksums, metadata, and installation: done.
2. Publish the three existing wheels to PyPI: done.
3. Add the isolated publishing job and update installation/runbook docs: done.
4. Validate the workflow and artifact selection; record results below.
5. Create the GitHub environment, register the PyPI publisher, and verify OIDC.

Initial publication evidence: all three release checksums match PyPI JSON.
A fresh Python 3.12 environment installed `adbc-driver-redis[dagster]==0.0.15`
from PyPI, loaded `AdbcDriverInit`, and constructed `RedisADBCIOManager`.
Twine metadata checks passed with warnings about the missing long description.
No Redis integration suite was rerun for this publication-only change.

Workflow validation: actionlint 1.7.12 passed with shellcheck disabled (not
installed). Executed the wheel-selection shell step against empty/historical
artifacts and three wheels mixed with tarballs/checksums; it skipped the former
and selected only the three wheels in the latter. Assertions verified the PR
gate, dependency on upload, environment, and isolated OIDC permission.
`git diff --check` passed. The GitHub `pypi` environment exists with no required
reviewers or branch restrictions; successful release checks gate publication.
