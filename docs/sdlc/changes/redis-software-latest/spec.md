# Specification

Date: 2026-11-04. Base: `efb9cbf`. [Intent](intent.md), [plan](plan.md).

Both Software Compose services pull `redislabs/redis:latest` on startup.
New RAM and Flex databases request Redis 8.8; RAM accepts a
`SOFTWARE_REDIS_VERSION` override. Flex defaults to 256 MB RAM out of
1,024 MB total. Reused databases keep their database version.

The QPF helper explicitly requests `mnp` routing and 32 connections along
with active scaling factor 4. It still verifies six Search workers and all
required effective settings before running tests. This is necessary because
8.2.6-25 / Redis 8.8 returned active 4x QPF with `cmp` and five connections.
An already compliant database is not reconfigured.

The rolling tag can change independently of this PR. Record exact versions
for every measurement; latest does not mean Flex is supported. Historical
Flex probes used driver revision `95d6583` and are labeled accordingly.

The [default-versus-fixture comparison](../../../compatibility.md#redis-88-defaults-versus-our-test-fixture)
defines the observed scope, scheduling policies, and concurrency limits.

The managed-proxy TLS/RBAC check accepts either a filtered empty CONFIG GET
reply or NoPermissionError. Both prevent configuration discovery; readable
configuration and unrelated errors remain failures. Worker mutation/write
denials remain separately verified.
