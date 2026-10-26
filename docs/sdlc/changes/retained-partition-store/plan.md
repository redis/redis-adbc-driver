# Plan

2026-10-21. Base `1282e4a`. Author: Codex; requestor: Albert Wong (runtime features required). Status: foundation implemented and locally verified; PR review pending. Refs #359/#360/#362/#363.

1. Completed: implement internal namespace/quota/upload/atomic-commit/revoke/read state machines with exact scope and TTLs.
2. Completed: test quota races, aborted publication, orphan expiry, corruption, missing chunks, independent ACLs and cluster routing on owned fixtures.
3. Evidence preserved and owned fixtures removed. Open a reviewable foundation PR.
4. Integrate bounded global execution and the public opt-in API separately, with independent-process exact Arrow replay and layout/large-ID coverage before closing the issues.

See [intent](intent.md), [spec](spec.md), [plan](plan.md).
