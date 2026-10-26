# Plan

Date: 2026-10-21. Source: merged #414. Author: Codex; requestor: Albert Wong (keep going). Refs [#170](https://github.com/redis/redis-adbc-driver/issues/170). Status: in progress.

1. Add an owned TLS-only three-primary Docker fixture and all-primary ACL helper.
2. Reuse the shared checker with strict three-primary routing requirements and all-node denials/caps.
3. Run live native and installed-wheel checks, lifecycle fault tests, existing standalone regression and workflow checks.
4. Clean up fixtures, preserve evidence and open a PR.

See [intent](intent.md) and [spec](spec.md).

Completed: three-primary TLS-only fixture with clientAuth cluster-bus leaf, strict local primary discovery/caps/health, all-primary ACL lifecycle, direct physical-row presence verification, native and installed-wheel runs (68 checks), standalone regression (15), management fault checks (25), Linux CI gates and owned resource cleanup. See [evidence](evidence.md).
