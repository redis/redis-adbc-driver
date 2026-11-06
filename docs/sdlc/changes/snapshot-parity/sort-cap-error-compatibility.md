# Native aggregate-cap refusal compatibility

At public source `431dcb2`, both installed-wheel client jobs fail in retained
ordered results even though the parent Software wheel jobs pass. The Redis
8.6.2 TLS fixture returns the native argument refusal
`LIMIT exceeds maximum of 137`, without the newer `SEARCH_LIMIT_OVER ` tag.
The driver only recognizes the tagged form and stops capability discovery.
[The completed amd64 job](https://github.com/redis/redis-adbc-driver/actions/runs/42152094727/job/127679836967)
records that failure in `retained_partition_tls.py` before retained production.

Accept the exact bare form as well as the tagged form, only from a direct native
Redis error. The number must remain canonical, nonnegative, smaller than the
probe limit and within int64. Plain, wrapped and joined errors, suffixes, other
reducers, malformed/overflow values and non-lower limits remain refusals to
infer capacity. Cancellation is checked before inference. Generated probes keep
their NoRetry transport behavior; user SQL is not replayed.

A new strict native-message matrix fails on the preceding code for the bare
form (root 0.794 seconds), then passes after the narrow parser change (0.825
seconds). All ordinary Go unit packages pass afterward (root 27.119 seconds).

On an owned strict-TLS Redis Software fixture with effective 4x QPF, noeviction
and the ordinary aggregate cap, the focused race selection passes **43 test
events**, no skips/failures/races, root **4.885 seconds**. It retains ordered
visibility, limits, residuals, rounded ties and stream refusal assertions. An
injected direct native bare refusal returns exact rows and evaluates the 299
visible rows' volatile predicate once. Tagged and bare plain/wrapped/joined and
canceled cases stop before SQL with one NoRetry probe and no data query.

The older server's real response is evidenced by hosted CI; development does
not provision a non-Software server. Replacement installed-wheel jobs on both
architectures remain the final compatibility gate for this follow-up. Broader
reader/native/race evidence in the existing reports identifies its own source
and predates this narrow response-format change.
