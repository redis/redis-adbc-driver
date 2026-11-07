# Investigation and delivery plan

Date: 2026-11-06. Requestor: repository owner (user). Author: Codex. Source: `bbc0c0a099dba585a41dadf355a6b8324e3ead1c`. Issue: [#345](https://github.com/redis/redis-adbc-driver/issues/345). Status: investigation in progress.

1. Read retained evidence and reproduce on unchanged current source with the existing proof observer.
2. Trace metadata ownership/reference transitions, client routing and lease lifetime; add bounded test-only observation where evidence is missing.
3. Reproduce an actual cause and preserve the regression before changing production behavior.
4. Validate source-specific recovery, reference/value safety, authorization and native behavior; review and deliver a fully checked PR closing #345.
5. Verify #345 closure, then start the reachable utility quota audit for #341 on current main.

See [intent](intent.md), [spec](spec.md) and [evidence](validation.md).
