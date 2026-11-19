# Plan

Date: 2026-11-19. Author: Codex. Status: preparation. PR: [#534](https://github.com/redis/redis-adbc-driver/pull/534).
See [intent](intent.md), [specification](spec.md), and [validation](validation.md).

1. Inspect latest release, main, merged checks and current publication workflows.
2. Update release availability documentation; validate docs, open/merge preparation
   PR after checks, then resolve main's exact release commit.
3. Publish v0.0.30 with reviewable release notes; follow automatic Test, binaries
   and PyPI workflows. Diagnose failures without moving the release tag.
4. Download/verify final assets and wheels; smoke-test installed host artifacts
   and update GitHub release notes with actual evidence and limitations.
