# Plan

2026-11-25; see [intent](intent.md), [spec](spec.md), [validation](validation.md).

1. Profile subset Q1 in Flex mode (Linux route, 4-CPU container), and read the SF1 diagnostics for bytes and allocation per row.
2. Combine Flex partition partial groups in memory, and reuse tuple encodings for metrics. Add the combiner unit test.
3. Reprofile the subset before and after.
4. Assess server-side aggregation on Flex; it can't be made exact.
5. In a confirmed read-only SF1 window: run Q1 on the committed build (warm, then cold after reading other tables). Run an uncommitted build that allows more than 32 workers at 64 and 92 workers, with server connection counts and peak RSS.
6. Run the subset in Flex and RAM modes, the unit suite and the targeted snapshot tests, then open the PR.
