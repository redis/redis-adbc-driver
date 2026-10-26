# Implementation and verification

Date: 2026-10-22. Status: verified locally; PR review pending.

1. Add one shared shell QPF gate to RAM and Flex fixture startup.
2. Preserve and verify QPF through Software TLS configuration.
3. Document default development/test settings, ClickBench requirements, and excluded providers in README and session-start instructions.
4. Add API-drift regressions and CI coverage selection for fixture changes.
5. Run shell syntax, focused Python checks, and an isolated real Software fixture when available. Review the scoped diff and open a PR containing reusable source and documentation only.

See [intent](intent.md) and [specification](spec.md). Actual check outcomes are recorded in validation.md.
