# Reused Software fixture request bounds

Date: 2026-11-04. Parent: e670a71. Status: verified on the owned fixture.
PR: https://github.com/redis/redis-adbc-driver/pull/284.
See [intent](intent.md), [spec](spec.md), [plan](plan.md).

A final-index regression invocation stalled before Go tests began. The live
management request was confirmed to be an idempotent proxy-certificate PUT,
not a create POST. Independent bounded management reads responded. The stored
public proxy certificate already matched the requested certificate exactly.
Only that confirmed stalled curl was interrupted; its setup process exited
with failure, and no Go result was claimed for that invocation. The private
certificate-state response was removed and no certificate/key data is published.

Setup now compares the installed public certificate before reinstalling it.
Matching certificates avoid a needless proxy reload. Missing/different state
retains the update path. Certificate responses flow directly to a boolean jq
check and are not printed or persisted; they can contain private keys.
GET/PUT attempts now have a five-second connection bound and ten-second request
bound, retaining their existing retry policy/window. The unauthenticated
bootstrap read is also bounded. POST retry behavior is unchanged.

All six mocked setup/exec tests pass, including a new control that refuses an
identical-certificate PUT. Unsafe/unknown eviction policies and invalid topology,
TLS or effective QPF still prevent command execution. Shell syntax passes.
Subsequent actual `make -C go test-software` invocations completed setup and
verified strict data TLS, noeviction and effective 4x QPF on the owned Software
fixture (Redis 8.8.0, three primary shards behind its proxy). These are not
Cluster API, failover or persistence-recovery results. No grants or deployment
policy were broadened.
