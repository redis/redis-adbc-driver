# Evidence contract

[Intent](intent.md), [plan](plan.md), [validation](validation.md),
[source API analysis](source-contract.md).

The archived experiment boxes immutable field strings once into the first HMGET
command’s owned args, then copies that field tail into independent later command
args. A single-key probe allocates no separate template. Commands never share a
mutable argument slice. Keep the existing per-command
NoRetry wrapper, whole-pipeline validation, request budgets, ACL fallbacks,
WRONGTYPE policy and complete later HGETALL all-NULL image unchanged. Do not cache
field permissions, source values or existence across scans.

The unit microbenchmark exercises production batch construction and decoding with
transport-hook replies for 1/1000 keys at 2/16/128 selected fields. It excludes the
network, server, Search, SQL and Arrow. Compare identical harnesses on both sources.

The opt-in Software sparse profile compares native HGETALL with the production
projected reader over 256 existing HASHes, one wrong-type key and one missing key.
Cases: all 256 selected field pairs present; 26 present/230 absent; all 256 absent;
and 26 present/230 absent with an unselected 16 KiB value on every HASH. The selected
BIGINT string is `9007199254740993` and the selected text contains an actual NUL.
Every returned source image is checked independently outside individual latency
timing. All-NULL projected reads must return the complete fallback image. Check
native source-command counts. Use one/four clients, one warmup and three measured
trials, eight operations per record, reverse mode order on alternating trials.

Final selected scope contains no production reader change. The boxing experiment
is archived after repeated four-client SQL median loss; unit ownership coverage
for it stays with the archived source. No adaptive reader, Lua materialization or
speculative Search alias/reducer path is introduced. A measured sparse penalty is a limitation to publish, not grounds
for silently changing live-read semantics.

Review: [PR #440](https://github.com/redis/redis-adbc-driver/pull/440).
