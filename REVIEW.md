# Review policy for the Redis ADBC driver

Status: proposed repository guidance, extracted 2026-10-20. No branch protection,
review bot, hook or named approval authority is installed by this file.

## Inputs and output

Read the change's intent, spec and plan, the diff, relevant tests, and the linked
evidence. The project baseline is in [docs/sdlc](docs/sdlc/README.md); it does not
substitute for a feature's acceptance criteria. If change records are absent,
state that limitation and review the requested behavior and diff directly.

For every actionable finding give severity, pass, file/line, concrete trigger,
observable impact and the smallest useful correction. Separate reproduced issues
from hypotheses. Close with checks inspected/run and remaining uncertainty.

## Review passes

1. **Correctness and lifecycle.** Check SQL NULL/NaN/overflow/decimal/time-zone
   semantics, physical Arrow types, error SQLSTATEs, identifier handling and
   duplicate/order behavior. Exercise pushdown and driver fallback where the
   change affects both. Check partial writes, prefix isolation, recovery,
   cancellation, cursor deletion, stream errors, Arrow buffer ownership and
   worker-budget admission/release races.
2. **Security and data ownership.** Check SQL-to-Search escaping, namespace and
   metadata key construction, ACL assumptions, TLS/URI handling and diagnostic
   output. Adopted HASHes remain application-owned; scrutinize drop/truncate,
   sweep, rename and rekey paths for unintended deletion. Diagnostics can contain
   SQL literals; do not treat them as safe public telemetry.
3. **Intent, design and evidence.** Compare behavior and changed files with the
   accepted change records. Check compatibility claims, public options and
   documentation. Treat a missing run, skipped capability or incomplete benchmark
   as an evidence gap. Require complete fetch and independent result validation
   for performance claims; record hardware, server, source revision and settings.

## Severity and human decisions

- Critical: credible data loss, security exposure or broadly unusable driver.
- Important: reproducible wrong rows/types/errors, resource leaks, hangs,
  recovery failures or a material supported-environment regression.
- Minor: localized maintainability or documentation issues without those impacts.
- Nit: optional style; report at most three and omit formatter-only findings.

The proposed policy is human maintainer review before merging material behavior
changes. Storage/ownership, ACL/TLS, recovery, compatibility scope, workflow
permissions and releases deserve explicit risk review. Record actual reviewer
identity and decision on the PR; an agent finding is not approval. Repository
settings must be inspected separately before claiming this is enforced.

## Avoid noise and retain evidence

Do not repeat deterministic CI formatting/build failures as speculative bugs.
Review generated corpus provenance and the importer when derived data changes;
do not hand-edit vendored upstream SQL to make a comparison pass. Keep known
PostgreSQL mismatches visible rather than silently enlarging an allowlist.
When the same mistake recurs, add a concise, verified rule to `CLAUDE.md` and link
the regression case. See [validation](docs/sdlc/validation.md).
