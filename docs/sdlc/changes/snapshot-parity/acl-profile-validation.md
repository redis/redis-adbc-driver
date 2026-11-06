# ACL fixture profiles for snapshot storage

The completed hosted run at `09b4e78` fails the macOS ACL families during Open:
its legacy recipes omit EVAL and lease rights. Schema patterns such as
`public:*` also do not match format-16 private row keys. These failures are
separate from the independent Software TLS worker refusal repaired by reader
isolation.

The fixture adapter registers each restricted backend domain and uses the
production policy's positive metadata grants and lease selectors. Requested
query commands and explicit denials remain controlled by each fixture. It
preserves external HASH patterns and adds the physical equivalent of intentional
SQL schema patterns. Trusted writers receive the current snapshot maintenance
commands and versioned index namespace; restricted users receive no catalog or
row writes. Cleanup removes only each fixture's owned domain/leases.

Read-only SQL write assertions now require the exact Unauthorized writer-lease
refusal and an unchanged published root. Their source rows/views still must
remain unchanged. The raw EXECABORT test remains and targets resolved physical
metadata. Cross-schema SQL denial is retained alongside explicit raw FT.SEARCH
and FT.AGGREGATE permission assertions. Snapshot rekey uses the ordinary writer
profile; its former INCR/DUMP/RESTORE requirement is obsolete. Existing ACL test
families remain enabled.

All Go unit packages and documentation/profile checks pass (root 27.339 seconds
at the first checkpoint and 27.127 seconds after the complete adaptation). Exact profiles from the compiled test adapter
are exported privately and applied to real temporary Software roles. On an owned
Software 8.8 deployment with strict TLS, effective 4x QPF, noeviction and aggregate
cap 2,147,483,648, five end-to-end checks pass:

- The writer creates/inserts/updates, rewrites a column type, rekeys and truncates.
- A reader fetches full values and schema; SQL UPDATE receives the exact lease
  denial and leaves the published root unchanged.
- Direct catalog/row/peer-lease HASH writes and active-directory removal fail
  with typed backend permission errors.
- A finite direct partition returns exact values with FT.INFO, FT.SEARCH,
  FT.AGGREGATE, FT.CURSOR and FT.PROFILE explicitly denied. Broad reads remain
  denied by the FT.INFO permission boundary.
- Removing HMGET denies direct partition values.

The real writer check first reproduces a missing index-key permission: metadata
`adbc:*` does not cover `adbc_snapshot16_{meta}_*`. Adding only that versioned
index namespace repairs CREATE and subsequent writer operations. Private probe
corrections handle actual post-TRUNCATE row IDs, RESP3 maps and typed Redis
permission exceptions; they do not relax profile assertions.

Owned users, roles, ACL mappings, producer groups and domain leases are cleaned;
ordinary cap/QPF/noeviction are reverified. No non-Software server is started for
this development validation. Existing hosted macOS/Cluster ACL families remain
the compatibility gate; their replacement runs are still pending.

## Exact managed-copy grants and authorized foreign capture

A follow-up keeps source-scoped import fixtures scoped to one logical copy's
stable access identity, rather than granting every managed row in `public`.
The app-index and adopt readers/updater retain their separate external source
patterns. Foreign-index inspection first verifies principal support, captures
its owned foreign copy through an administrator, and then grants that exact
copy to the reader. Restricted Open never performs the capture. Its UPDATE
assertion uses the exact writer-lease denial and preserves source values;
cleanup removes only the owned foreign copy.

Actual Software roles reproduce the missing-copy failure before administrator
capture. After capture and the exact copy grant, full foreign values/schema are
readable, UPDATE receives the exact lease denial with an unchanged published
root, unrelated public rows remain denied, and the source HASH is unchanged.
Owned principal, index, copy and domain cleanup completes. All Go unit packages
pass after the fixture follow-up (root 26.924 seconds). This is actual Software
role evidence; the existing hosted macOS ACL fixtures remain required.

## macOS fixture isolation and managed partition/sweep contracts

At PR head `0c969bd`, [Test run 42156026576](https://github.com/redis/redis-adbc-driver/actions/runs/42156026576)
passes completely, including both Software platform jobs. All ten Linux distro
jobs and both installed-wheel client jobs pass in the corresponding
[Release run](https://github.com/redis/redis-adbc-driver/actions/runs/42156026631).
That Release run still fails macOS, so this is not a green-PR claim.

Follow-up source `66e027f` addresses four fixture assumptions:

- The destructive read-only-key `FT.DROPINDEX ... DD` proof uses an independent
  application HASH/index, retaining its three-row deletion assertion. It no
  longer intentionally removes an index referenced by the managed catalog. The
  managed victim must still contain all three rows afterward; cross-schema
  SQL/raw Search denials remain required.
- HMGET revocation must produce a concrete IO/NOPERM/HMGET failure with no values,
  whether immutable metadata admission or subsequent row iteration needs it first.
- A broad partition (row-ID range 1..1025, two actual rows) must still return
  exactly 1 and 2 with no Search cap and with FT.AGGREGATE revoked. A raw command
  proves that revocation is enforced. FT.INFO revocation must still fail with
  Unauthorized; exact directory replay does not remove broad index validation.
- Sweep uses genuinely registered rows retained by an old-root pin through DROP,
  rather than unregistered raw rows inserted after DROP. Its restricted operator
  can read but cannot delete the other schema. A concrete NOPERM partial result
  must account for every authorized deletion and preserve the unauthorized row.
  An administrator then resumes and verifies rows, index, directory and ownership
  graph reclamation. GC ordering may encounter the forbidden segment first.

All Go unit packages pass (root 26.755 seconds). The exact broad and pruned Go
assertion bodies also pass under `-race` against temporary Software API-managed
roles (8.425 and 4.409 seconds); only principal setup/revocation is adapted because
Software blocks protocol ACL SETUSER. A real registered-data sweep probe deletes
20 authorized rows, preserves the other schema's one row with a key permission
error, and permits administrator recovery. Temporary roles/domains are removed
and the owned Software fixture is stopped. These checks use Software 8.8, strict
TLS, effective QPF 4x, noeviction, and the ordinary 2,147,483,648 aggregate cap.
The hosted macOS run remains required for its backend-specific ACL-log/DD proof.

### Explicit backend ACL audit probe

The next hosted macOS run at `076c838` passes all earlier failing permission and
inspection families. Its only Go failure is the sweep helper reporting an empty ACL log. Native
`redis.acl_check_cmd` preflight can return NOPERM without a command execution
audit entry; the RESP3 decoder issue described below can also hide entries. Partial-report conservation, unauthorized-row
preservation and administrator reclamation checks already pass.

The follow-up retains those assertions and explicitly issues DEL for the single
forbidden fixture row using the same restricted principal. DEL must return
NOPERM, the row must remain, and ACL LOG must identify that exact key. This
separates a preflight refusal from an executed backend command's audit record.
All unit packages pass (root 26.966 seconds). Five race-enabled registered-data
sweep repetitions on the owned Software role pass direct DEL denial, unchanged
forbidden data, partial-report conservation and strong administrator reclamation.
Software's blocked protocol ACL LOG remains the hosted macOS audit gate; it is
not claimed as locally validated. The current hosted Software jobs are allowed
to finish before publishing this test-only follow-up.


### Preserve RESP3 audit entries

The helper's raw ACL LOG parser accepted only flat arrays and silently discarded
RESP3 maps. [Redis 8.6's ACL LOG implementation](https://github.com/redis/redis/blob/8.6/src/acl.c#L2886)
emits map entries. The helper now uses go-redis's typed ACLLog decoder and keeps
its exact principal filter, reason/object values, ordering and deduplication.
This also makes the existing no-denials assertions observe real RESP3 events.

An in-memory wire regression negotiates RESP3 and returns target-principal,
other-principal and duplicate map entries. The old helper loses all events
(focused red, 0.927 seconds); the typed helper returns the exact target denial
(focused race green, 1.778 seconds). No additional Redis distribution is started.
The explicit forbidden DEL still supplies a real backend refusal for the hosted
macOS audit assertion; NOPERM, row preservation and exact-key auditing remain
required.
All Go unit packages pass after typed decoding (root 27.072 seconds). Both
Software jobs in Test run 42160980011 are terminal green at `076c838`; its Release
workflow remains failed only by the old macOS audit assertion. The follow-ups
still require a new hosted macOS result and are not claimed as green CI.

### Writer cache permission and raw probe handshake

With RESP3 audit decoding repaired, macOS at `c79084a` exposes the previously
hidden EVALSHA command refusals in trusted-writer fixtures. The control executor
accepts EVAL-only writers through a proven top-level command-refusal fallback,
so SQL succeeds but that compatibility probe creates an ACL event. The documented
writer recipe and its fixtures now include EVALSHA for successful cached control
execution without refusals. Existing EVAL-only compatibility remains unchanged;
row grants and denied operations are not broadened.

The sweep's raw DEL probe now explicitly uses RESP3 and disables optional client
identity metadata, matching the ordinary driver handshake. Its extra CLIENT
SETINFO refusal is removed without granting that unrelated command. All existing
no-denials and exact forbidden-key assertions remain enabled.

All Go unit packages pass (root 27.555 seconds). Five real Software sweep races
with the final EVALSHA profile and handshake pass (5.306 seconds), including
NOPERM, exact forbidden-row preservation, partial-result conservation and strong
administrator reclamation. The hosted macOS audit gate is still pending. The
production runtime is identical between the fully Software-green `076c838` and
`c79084a`; their differences are limited to audit test fixtures and evidence.
