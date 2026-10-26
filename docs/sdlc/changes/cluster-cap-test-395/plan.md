# Implementation and validation

Date: 2026-10-21. Status: local verification complete; human review pending.
See [intent](intent.md) and [spec](spec.md).

1. Capture the first full-suite failure and leftover node caps (3/137).
2. Save, set and restore CONFIG on every primary; check cleanup errors.
3. Run focused cap and attribute tests: passed, 4.297 s on three-primary Redis
   8.6.2 / macOS arm64 Docker host / Go 1.26.6. Independent CONFIG GET confirmed
   all three values restored to 2147483648.
4. Require the cap test on OSS single/cluster CI, both architectures. actionlint,
   gofmt and diff checks passed.
5. Final full Go suite with runtime cap enabled: all three packages passed
   (151.418 s, 1.384 s, 0.179 s), 973 passing test/subtest events and 11 existing
   opt-in/environment skips. See [machine-readable evidence](evidence/cluster-go.json)
   for every skip and its reason. No failures. All three primary caps independently
   verified as 2147483648 after the suite. Includes merged #394 and this fix.

Review inspected concurrent callback capture, per-primary restoration, bounded
cleanup, unchanged exact rows and configured-cap handling. Agent review is not
maintainer approval. Hosted results remain pending.
