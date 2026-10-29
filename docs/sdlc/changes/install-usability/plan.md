# Plan: installation usability

Date: 2026-10-29. Source: `95d6583` plus this change. Status: local work complete.
Inputs: [intent](intent.md), [spec](spec.md). Evidence: [validation](validation.md).

## Files and implementation

1. Extend the Python module CLI with doctor while retaining install-manifest.
   Implement fixed diagnostic hints, optional manifest-name queries, explicit
   offline behavior, result validation and handle cleanup.
2. Add focused tests for credential suppression, failed native loading, manifest
   failures, offline behavior, argument validation, query cleanup and live checks.
3. Add a standalone wheel-header validator and regression tests; invoke it in
   PyPI publication and add its unit tests/path triggers to PR CI.
4. Add docs/setup.md and links from the main README, package README and integration
   guides. Update integration guide discovery without replacing newer support evidence.
5. Run focused tests, an existing macOS binary smoke check, link/example checks
   and diff review. Record unavailable live verification without substituting
   historical results or broadening test skips.

## Review and release

Reviewed locally against REVIEW.md: native loading, credential handling, fetched
result validation, cleanup, manifest semantics, platform claims and workflow
permissions. No human approval, CI success or publication is asserted. A future
release must include the changed Python modules and pass the existing release
pipeline; live Search/TLS checks still require a configured test endpoint.
