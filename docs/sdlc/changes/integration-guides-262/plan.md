# Plan: integration guide coverage

Date: 2026-10-30 (America/Los_Angeles). Source base: `3fd6bf36eef8f6ab069a6ee06929e972a40c11f3`.
Requestor: repository maintainer in the Codex chat. Author: Codex.
PR: [#262](https://github.com/redis/redis-adbc-driver/pull/262). Status: local implementation and verification complete; human review pending; no human approval inferred.


1. Map the 38 README entries to 18 existing guides and 20 missing guides.
2. Author the missing guides from existing APIs and workflow source; add shared
   setup and uniquely owned sample-data support.
3. Update every README row and the guide index; provide a repeatable checker.
4. Build the native driver, verify the Software fixture, and execute all 38
   guides in dependency-compatible environments. Fix guide or checker failures.
5. Check Markdown links, anchors and Python syntax; record actual outcomes,
   limitations and self-review; update PR #262.

See [intent](intent.md), [specification](spec.md) and [validation](validation.md).
