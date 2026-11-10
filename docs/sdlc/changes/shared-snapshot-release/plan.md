# Shared snapshot release readiness: plan

Date: 2026-11-10. Source baseline: `018e0368`. Status: locally validated; hosted release/publication pending.
Issue: [#365](https://github.com/redis/redis-adbc-driver/issues/365).
Related: [intent](intent.md), [specification](spec.md), [acceptance](acceptance.md),
[verification](verification.md), [proposed release notes](release-notes.md).

1. Add the existing shared-view validator to the normal native suite using its
   selected native/wheel library and database options.
2. Build a version-stamped local v0.0.24 candidate, package/install its macOS wheel,
   verify packaged library identity, and run complete native ADBC validation on
   authenticated TLS Redis Software with setup-verified 4x QPF/noeviction.
3. Record current merged PR checks and actual validation outcomes, including
   skips, without treating pending platform jobs as completed evidence.
4. Document actionable native connector changes and prepare release notes.
5. Open a follow-up PR and prepare a reviewable release candidate. Publish only
   within explicit release authorization, verify binary and PyPI workflows and
   installation/checksums, then identify the released version and close #365
   only after all its acceptance evidence is linked.
