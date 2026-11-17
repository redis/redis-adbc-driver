# Spec

Date: 2026-11-17. Requestor: Albert Wong. Author: Codex.
Base: `42f6e0b1`. Status: implemented; local verification complete; PR review pending.
Issue: [#110](https://github.com/redis/redis-adbc-driver/issues/110).

Support NORMALIZE(text [, NFC|NFD|NFKC|NFKD]) with NFC default and text output. The unqualified SQL special form accepts a bare normalization keyword; qualified/quoted ordinary calls accept text form expressions. Support UNISTR(text) with four/six/eight hex-digit escape forms, escaped backslashes and adjacent UTF-16 surrogate pairs. Reject malformed escapes/pairs (42601), zero/out-of-range code points (22023) and incompatible typed arguments (42883), including typed NULLs and empty relations. Value-dependent errors follow PostgreSQL constant planning and lazy CASE behavior. Preserve character-to-text trailing-space coercion, Arrow string results and parameter schema/reuse. Match PostgreSQL 18 Unicode 16 normalization, including long nonstarter sequences without stream-safe CGJ insertion. Use existing x/text tables with explicit Unicode 17 normalization-property exclusions and Unicode conformance evidence; no toolchain/dependency version upgrades.

General DEFAULT value-error timing remains a documented pre-existing deviation ([#479](https://github.com/redis/redis-adbc-driver/issues/479)); valid and lazy defaults and invalid signature rejection are covered here.

See [intent](intent.md), [spec](spec.md), [plan](plan.md).
