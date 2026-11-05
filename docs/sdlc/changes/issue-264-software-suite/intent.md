# Intent: verified Software suite (#264)

Date: 2026-11-04. Requestor: Albert Wong; implementation and validation: Codex.
Base: `f1e1f033d8e90b3d07de64d63d36b185944228f6`.
Issue: [#264](https://github.com/redis/redis-adbc-driver/issues/264).

Developers need one documented command that verifies the owned Software fixture
before supplying a retained-control persistence declaration. Current main
already contains `test-software`, `software-setup.sh --exec`, QPF verification
and the updated CLAUDE.md guidance from the Arrow interoperability work.
Reconcile and validate those changes rather than add a second runner.

The remaining work repairs the runner regression test after the TLS migration,
runs it in CI, repairs the README command formatting, and records focused and
full-suite evidence. Driver policy behavior and the separate transport EOF
investigation remain outside this change.

See [spec](spec.md) and [plan and evidence](plan.md).
