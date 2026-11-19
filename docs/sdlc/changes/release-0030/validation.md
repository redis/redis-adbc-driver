# Validation and release tracking

Date: 2026-11-19. Author: Codex. Status: preparation. PR: pending creation.
See [intent](intent.md), [specification](spec.md), and [plan](plan.md).

User explicitly requested a GitHub release. Latest published release inspected:
v0.0.29. Main includes merged PRs #532 and #533; the former's full amd64/arm64
checks passed, and the latter's documentation checks passed. Source version
metadata is overridden by the release tag in the existing packaging pipeline.

Local historical tags differ from the origin for some early releases. The
all-tags fetch refused to overwrite them; no existing tag was changed. The new
v0.0.30 tag will be created by GitHub at the exact verified release commit.

No runtime change is included in this preparation. Documentation and release
checks are recorded on the preparation PR and final GitHub release. Release
completion requires binary/PyPI success and independent artifact verification.
