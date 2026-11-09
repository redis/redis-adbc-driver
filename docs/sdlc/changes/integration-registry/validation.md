# Verification evidence

Date: 2026-11-09. Base: `cc3dad27c14901bacaef5c4a6b3cca55f43537a9`.
Issue: [#343](https://github.com/redis/redis-adbc-driver/issues/343).
Status: local checks complete; final CI and delivery recorded on the PR.

The original guide/profile memberships, Python/extras/candidate settings and
pytest selectors were extracted before editing and compared with the new registry.
All mappings match. Both original execution orders are retained. TLS delegation
now matches only the exact standalone `ray-tls` token; malformed tokens containing
that substring are rejected by registry validation before fixture creation. The
pre-refactor runner was checked in the same safe mocked checkout: `ray-tls `
entered the fixture runner and exited 0; the new regression expects validation
exit 2 with no side effects and passes. Duplicate lists
and selection cases were removed from the entry points rather than left alongside
the registry. The result summarizer's default inventory also uses the registry.

The offline suite exercises actual Bash entry points in a temporary checkout with
mocked external commands. It covers all profiles, selectors, environment choices,
quoted paths, setup-before-pytest ordering, dbt/Kestra/R hooks, separate Ray TLS
delegation, invalid complete requests, stale reports and failed installs/tests.
Guide-runner mocks cover complete guide selection, per-guide evidence flags,
summary replacement, timeouts and missing interpreters. Structural checks compare
registered guides with documentation, verify aliases/selector definitions and
resolve dependency extras against pyproject.toml. Separate existing TLS lifecycle
tests cover owned fixture cleanup, preexisting fixture refusal, private-CA
isolation and failure/skip/xfail accounting. CI helpers run both suites.

No local Redis instance, package installation, runtime download or Cloud resource
was used for these offline contracts. They do not establish all-tool integration
compatibility. Existing CI performs actual Software setup and backend validation;
its exact-head results will be recorded on the PR. No benchmark or performance
claim is made. Operational artifacts remain private.

See [intent](intent.md), [spec](spec.md), [plan](plan.md).

## Local outcomes

- Registry and real-entry-point offline contracts: 10 tests passed, zero skips.
- Existing candidate TLS lifecycle/accounting tests: 6 passed, zero skips.
- Existing installer tests: 17 passed, zero skips.
- CI matrix selection tests: 9 passed, zero skips.
- Exact pre-refactor mapping and execution/help order comparison: passed.
- Bash syntax, Python AST parsing, local documentation links and diff whitespace:
  passed.

The old local pytest environments were unavailable (one lacked pytest; another
had a missing interpreter standard library). No dependency repair or installation
was needed: the new standard-library suite covers the existing guide checker's
success/marker contract, and CI runs the original pytest guide tests against its
fresh analytics environment. No all-integration local runtime success is claimed.

Reviewed source changes and evidence against REVIEW.md. No actionable finding
remains; this is an agent review, not human approval. The explicit runtime hooks,
server setup and TLS ownership implementation remain unchanged.
