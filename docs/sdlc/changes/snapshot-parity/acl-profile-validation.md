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
