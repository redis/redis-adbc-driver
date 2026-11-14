# Plan

**NOT SELECTED.** Frozen experiment evidence is retained; selected code restores the original metadata path. See the [final validation](../performance-followup-20261114/validation.md).

Date: November 14, 2026. Author: Codex. Status: source prototype ready.
[Intent](intent.md), [specification](spec.md), [validation](validation.md).

1. Add bounded root helper and a queued acquisition read; consume it in existing
   resolver initialization without changing its dependency/authorization logic.
2. Exercise native protocol arguments, per-pin scope, absence/corruption,
   permission/size decline, all command/pipeline unknowns and original precedence.
   Retain no-repair assertions, including known lease NOSCRIPT followed by lost
   or malformed root replies. Cross-review before live validation.
3. In the root-controlled Software slot, independently verify effective 4x QPF,
   strict TLS/RESP3; run focused tests and matched native point measurements with
   no competing test/build/timing process. Compare against frozen PR #439 source.
   Retain all samples and negative results. Remove the fast path if extra server
   work does not justify its exchange reduction on measured workloads.
4. Record actual sources, commands, passes/skips and ownership cleanup. Link a PR
   only once created; agent review does not constitute maintainer approval.
