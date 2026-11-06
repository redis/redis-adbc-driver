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
