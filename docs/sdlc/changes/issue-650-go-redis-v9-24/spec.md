# go-redis v9.24.0 requalification

See [intent](intent.md) and [plan and evidence](plan.md). Behaviour changes are
from the [v9.24.0 release notes](https://github.com/redis/go-redis/releases/tag/v9.24.0).

## Contract

- Require `github.com/redis/go-redis/v9 v9.24.0`; `go mod tidy` changes no
  other module.
- A later borrower of the shared pool never inherits WATCH or MULTI after a
  callback error with a refused UNWATCH, or after a refused EXEC inside MULTI,
  on single-endpoint and cluster clients. The execution pool keeps its own
  discard after a failed transaction or cleanup.
- The compact-integer planner's RTT PINGs (`aggregate_compact_integer.go`)
  stay outside statement command diagnostics and execution accounting.
  v9.24.0 `WithTimeout` clones keep their parent's hooks, so the probe context
  hides the statement's diagnostics and execution transport. A timed-out probe
  keeps the established source and does not discard the execution pool.
- `clusterOptions` copies every setting that `Options` and `ClusterOptions`
  share, including v9.24.0's `AuthEmptyPassword`,
  `ConnMaxLifetimeReapInterval`, `PipelineMinIdleConns`, the client-side cache
  settings, `CommandMetadata` and `OTelRecorder`. The URI parameters
  `auth_empty_password`, `conn_max_lifetime_reap_interval` and
  `pipeline_min_idle_conns` reach both clients. The driver still overrides
  `context_timeout_enabled` and `disable_identity`.
- Documented Cluster API ACL profiles add `+cluster|shards`, and the tests and
  validation scripts that hold users to them (`TestACLDriverUser`, the retained
  partition ACL tests, `check_partition_tls.py`, `retained_partition_tls.py`)
  grant it. Those tests require an empty ACL log. A user with only
  `+cluster|slots` still works through the fallback, but every topology load
  is refused once.

## Behaviour changes reviewed

| v9.24.0 change | Driver use | Disposition |
|---|---|---|
| `Tx.Close` requires `OK` from UNWATCH; a refused EXEC or MULTI drops the socket (#4316, #4192); EXEC reply count checked (#4186) | All `Watch`/`TxPipelined` paths | Adopted; regression tests on a modeled peer |
| `WithTimeout` keeps hooks (#4095) | RTT probe only | Probe context masks diagnostics and execution transport |
| `MaxActiveConns` counts excess, dedicated and dialing connections (#4174) | Only from the URI's `max_active_conns`; no `Conn`/`NewConn` use | Documented; default 0 (no cap) unchanged |
| Topology prefers `CLUSTER SHARDS`, falls back to `CLUSTER SLOTS` | Every cluster client; no `ClusterSlots` override outside tests | `+cluster\|shards` in Cluster API ACL profiles; checked live (see plan) |
| Context-deadline socket timeouts match `context.DeadlineExceeded` and are not retried (#4263, #4306) | `ContextTimeoutEnabled` on every client | `transportContextError`/`explainTimeout` already classify both forms |
| No replayed dial sequence on retries (#4304) | Default dialer on store clients; execution pools use a custom dialer | No change; faster failure on refused dials |
| `FT.CURSOR READ` not retried after a lost reply | Driver already sends cursor reads once | Consistent with `TestTimeoutNotRetried` |
| New `ParseURL` parameters (#4063 and others) | URI passed through | Documented the overridden ones; `dialer_retries` and `dialer_retry_timeout` stay cluster-only in the driver |
| `Cmd.Int64`/`Text` accept RESP3 doubles; `Cmd.Uint64` rejects negatives | No `Cmd.Uint64` use | Value-preserving; no change |
| Struct `Scan`, CSC, Pub/Sub, OTel, MultiDB, full-duplex, `Script.Load` SHA, search builders | Not used (`Script.Run` only; raw `FT.*` commands) | Not applicable |
| New `Cmdable` methods | No custom implementation | Builds |

## Acceptance

Unit-only `go test -count=1 -p 1 ./...` and the live Software suite
(`make -C go test-software`) pass on a verified 4x QPF fixture. The Watch-based
transaction, lease, cancellation and refusal tests pass. The new regressions
fail with v9.23.0 (shared pool) or without the probe change (probe), and pass
with both changes.
