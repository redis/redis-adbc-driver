# Modern Redis contract specification

2026-11-22 · Source: 1d0d0b3b · Status: implemented and locally validated

- Reject connections without an explicit ACL username and password before Redis network access.
  Explicitly naming the ACL user `default` remains valid; password-only and
  anonymous authentication do not. Username/password options override the URI.
- Require RESP3 and Redis Functions on every new socket, including maintenance,
  execution and Cluster API node connections. Connection initialization probes
  the exact version's library and installs it only after a direct native
  function-not-found reply. Initializers require FUNCTION LOAD; preinitialized
  operators require FCALL and their operation's underlying command/key grants.
- Generate an inventory of all statically named entry points, keeping
  dispatch selections visible at their call sites. Derive function names from the
  entire inventory version and body; derive the library name from its code.
  Never replace another driver's library. Preserve old versions for active
  clients; automatic library retirement is outside scope.
- Driver operations use FCALL, including server-side writer/Search fences and
  queued snapshot validation. Missing functions, ACL denials and unknown replies
  do not select EVAL or replay operations. Retain durable outcome resolution.
- Use SSCAN for metadata sets, deduplicate repeated members, honor cancellation,
  discard partial results on failure and keep cursors local to one enumeration.
  Existing pins/WATCH retain their consistency responsibility. The authorization
  directory keeps its atomic cardinality/memory bound and enumerates with SSCAN
  inside that bounded function.
- Do not issue KEYS, SMEMBERS, EVAL/EVALSHA or SCRIPT from driver runtime paths.
  Do not add queue APIs or substitute Redis JSON for authoritative HASH storage.

Redis documents [Functions](https://redis.io/docs/latest/develop/programmability/functions-intro/)
and [Software command support](https://redis.io/docs/latest/operate/rs/references/compatibility/commands/scripting/).
The [ACL reference](../../../driver-reference.md#acl-users) describes
initialization permissions and the breaking connection contract.

See [intent](intent.md) and [validation](plan.md).
