# Python environment selection contract

See [intent](intent.md) and [plan and evidence](plan.md).

The validation project's `.python-version` selects 3.14, matching its declared
minimum. Both Linux and macOS release wheel steps extract `[build-system].requires`
from the checked-out source's `python/pyproject.toml` into a temporary requirements
file and pass it to `uv run --with-requirements`. This keeps no-isolation builds
consistent with their backend requirements even with an older uv tool cache,
and honors the actual source ref when building old release tags.

Acceptance: the stale 3.13 pin and wheel 0.47.0 failures reproduce before the
fix; the corrected validation environment resolves and collects tests; the
manifest-derived wheel command produces a native wheel. Workflow syntax checks
pass. Full Redis validation and Linux release execution require hosted CI.
