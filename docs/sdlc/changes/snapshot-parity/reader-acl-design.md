# Isolated reader leases: staged design and validation

Date: 2026-11-03. Production parent: 75ad5d5; storage protocol remains 15.
Status: model validated; production integration incomplete. See [plan](plan.md).

## Current failure and security boundary

The ordinary suite still fails at TestACLAppIndexReader. Its AppIndex assertion
predates managed copies, and its zero-write reader role cannot acquire snapshot
pins. Protocol 15 stores reader pins in the catalog HASH containing the root
pointer. Granting HSET there also permits direct root mutation. Changing only
the assertion or granting that permission would not restore reader isolation.

Private probes on Redis Software confirmed that an EVAL-only ACL selector can
permit the conservative read/write key access required by EVAL while separate
command selectors still reject actual HSET against the catalog. Both direct and
scripted catalog mutation must remain denied. Every accessed key remains an
explicit KEYS argument in the metadata slot; no ACL bypass through hidden keys
is proposed. Redis documents [ACL selectors and key permissions](https://redis.io/docs/latest/operate/oss_and_stack/management/security/acl/)
and [script ACL checks](https://redis.io/docs/latest/develop/interact/programmability/lua-api/).

## Proposed production integration

Separate reader lease fields into one HASH per authenticated principal, keyed by
the SHA-256 of its username. Readers may HSET/HDEL only that HASH and SADD their
namespace to a shared directory; they cannot remove directory entries or change
catalog definitions or rows. Existing writer pins remain catalog fields. This
retains control writes for SQL readers, rather than claiming zero backend writes.
ACL permissions still assume protocol-compliant clients; they do not make a
principal's own arbitrary lease contents trustworthy.

Acquisition and collection receive an explicit directory snapshot and all lease
HASH keys. In the atomic script, cardinality, membership and uniqueness checks
must reject a stale directory before any mutation. The collector protects every
live reader and writer root. Acquisition, renewal and validation check committed
root state; reuse of an existing invalid token fails instead of selecting a new
generation. A lost acquisition reply resolves through a read-only token lookup,
without replaying acquisition or SQL. Only a proven directory refusal before
mutation can permit directory refresh and retry.

This requires a new storage protocol: old collectors do not know these reader
references. Protocol 15 cannot safely coexist with an activated version of this
layout. The agreed no-migration policy remains. The model's `16-model` marker
is only an isolated test guard, not an activated production format.

Integration must cover existing-catalog Open without initialization writes,
capability detection without a mutating probe, restricted-role maintenance
suppression, heartbeat/validation/release, every reclamation and root-reference
path, partition frame namespaces and cross-principal worker acquisition, and
sequence CAS checks. Writer-owned partition capabilities and reader-owned ones
must both protect already acquired workers after producer release. Strict reader
recipes need actual managed-copy SQL tests, source/private index permissions,
and direct/scripted denial assertions. These changes are not implemented here.

## Model and actual verification

The Go model lives entirely in snapshot_reader_model_test.go, excluded from the
driver binary. Its Lua checks a unique isolated reader-model key prefix, every
declared key's case prefix, and the fake catalog's `16-model` marker before
mutation. It neither opens the driver nor invokes its collector. The Python
harness verifies the owned Software fixture, creates temporary principals,
checks the actual production catalog DUMP before/after, and removes model keys
and API principals even on failure. Operational artifacts remain private.

Verified Redis Software, Redis 8.6.2, strict TLS, three primaries, noeviction,
active effective 4x QPF; macOS ARM64. `make -C go redis-up` completed before
the sequential model run. Command (with the owned fixture's private environment):

```
python scripts/snapshot_reader_acl_model.py --software-api-port <owned-api-port> \
  --software-data-port <owned-data-port> --owned-software-fixture
```

The harness runs `go test -race -json -count=1 -p 1 -run
'^TestSnapshotReaderLeaseModel' .` in go/. Final result: six top-level passes,
zero skips/failures, root package 2.053 seconds. Harness exited zero, confirming
unchanged production catalog and removal of owned resources.

Unit-only packages with all Redis/model endpoints removed also exited zero:
923 pass events and 821 expected integration skips (including the six model
cases). These skips are not server compatibility evidence. Python AST parsing,
the harness help command, Go formatting and staged diff whitespace checks pass.

- Isolation and retention: direct/scripted catalog writes, row writes, another
  principal's lease writes and directory removal are denied; an old root survives
  publication and renews, then becomes collectible after release.
- Membership and partitions: stale enumeration refuses before mutation;
  cross-principal workers retain their root after producer/group release.
- Quota and expiry: 128 live leases across principals enforce the model quota;
  expired leases cease protecting roots and own expired fields can be pruned.
- Writer capability and preflight: a catalog-held producer capability transfers
  protection to a reader; denied permissions cannot partially register a new
  namespace before acquisition fails.
- Stale/escaped state: wrong-root renewal/validation and invalid-token reuse
  fail without changing lease bytes; an escaped declared key is rejected.
- Lost reply: the hook returns an IO error after successful acquisition; exactly
  one acquisition runs. HGET resolves the original pinned root after publication,
  and protection lasts until explicit release.

Earlier runs are not folded into success: the initial ACL-denial helper rejected
Redis's Lua ACL error spelling, and an expanded run failed compilation on an
unused import. Both were corrected before the final six-case run.

## Remaining risks and gates

The model does not prove production SQL, full reference-graph collection,
sequence behavior, compaction, Cluster API routing, failover or persistence.
No throughput or performance-parity claim follows from its runtime. Namespace
enumeration/cache behavior, churn, inactive-principal cleanup and resource bounds
still need implementation and measurement; the test helper uses trusted admin
enumeration. The model does not test hostile fabrication of own lease records.

Automatic approval review rejected the earlier combined production patch because
it changed reader isolation, reclamation, partitions, sequences and compatibility
without staged validation. That patch was not applied. This checkpoint provides
reviewable evidence without changing the live protocol. Production integration
and its complete lifecycle/security regression gates remain required before
claiming ACL parity or full snapshot completion.

Author review covered declared-key isolation, real command ACL denials, admission
before mutation, directory membership, reader/writer protection, partition
handoff, late-reply classification, cleanup and accidental format activation.
This is not independent maintainer approval.
