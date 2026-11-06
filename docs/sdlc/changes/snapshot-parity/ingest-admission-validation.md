# Same-target Arrow admission before input consumption

Date: 2026-11-04. Parent 153ac81. Production remains protocol 15.
Full [feature and performance parity](plan.md) remains incomplete.

## Reproduction and behavior

The unchanged TestParallelBulkIngest starts four append writers with 2001 rows
per batch and requires all 8004 rows and unique physical identities. Before this
change, three writers return explicit SQLSTATE 40001 and only one batch commits;
the failing root package takes 1.717 seconds. After admission, the original test
passes without caller retry or weakened assertions in 3.012 seconds. This is a
correctness result, not a throughput comparison between equivalent successes.

Append/create-append writers claim their logical target before workspace creation
and Arrow Next. Another participating writer for that target waits, releases its
provisional pin and reacquires a current root. Independent targets can consume
input concurrently. Concurrent first create-append also coordinates before a
table descriptor exists. SQL/DDL and nonparticipating clients retain the existing
publication conflicts; neither SQL nor consumed Arrow input is replayed.

A bounded SHA-256 target hint lives on the existing leased writer pin. Its root,
writing flag, reference retention, expiry and reclamation are unchanged. Existing
renewals preserve JSON fields; old collectors already protect the same pin root.
Successful publication clears writing, admitting the next writer. This adds no
ownership state, object, reference edge, key or ACL grant, so no format revision
is required. It does not activate the separately proposed reader ACL redesign.

Only a known refusal is polled. Cancellation, deadline and connection close stop
waiting before input consumption. An uncertain admission reply returns its error
and releases the provisional pin; it never retries that admission or reads input.
Expired owners cannot retain admission. Queue fairness is not guaranteed.

## Regression and verification

Owned strict-TLS Redis Software, Redis 8.6.2, three primary shards, Search,
noeviction and effective active 4x QPF verified before the database selections.
Mutating suites run sequentially on macOS ARM64. Logs and operational artifacts
remain private. No ClickBench run or reload.

The focused Go admission tests pass eight events, zero skips/failures, root
package 4.995 seconds. They verify unread waiting input, independent target
progress, an intervening SQL publication, retained old rows, exact current rows
and unique IDs, cancellation/deadline/Close statuses, no leaked pins, one lost
reply with zero input reads, renewal preserving the hint, owner expiry and four
concurrent first create-appends consuming each input once. A final map-reset
correction prevents a renewal assertion from inheriting a previous JSON field.
The broader race selection passes 15 events with zero skips/failures, root
package 8.587 seconds, including the original parallel ingest, temporary ingest,
default/CHECK/length/time-zone conversion and managed ownership/refresh cases.
Unit packages pass 946 events with 843 expected integration skip events and no
failures. These skips do not establish database compatibility. The versioned
native library builds with make -C go build.

The complete snapshot race selection (`^TestSnapshot`, `-race -count=1 -p 1`)
passes 252 events with 21 explicit skips, zero failures and root package time
304.280 seconds. The skips are nine Cluster API cases, six permission cases and
six separately gated reader-model cases; they do not prove compatibility.
The rebuilt-native C/ADBC snapshot suite passes all 15 tests in 13.679 seconds,
zero skips/failures. Its new four-writer test checks both append and first-create
append with no caller retry, exact int64/NULL values, full fetch, one consumption
per batch and eight unique physical IDs. The independent-target stream barrier
test also passes, preserving simultaneous consumption for distinct targets.
Full ordinary, hosted platform, Cluster API/recovery, process resume, sustained
workload and comparative performance gates remain open.

## Review

Author review inspected the pre-input ordering, target isolation, pin/root CAS,
lease expiry/renewal, cleanup, cancellation and uncertain-reply behavior. Tests
observe real control replies through an idle no-worker maintenance delegate;
they do not fabricate Search metadata or install a production test hook. Native
regressions synchronize callers before admission and independently fetch exact
integers above 2^53, NULLs and unique physical identities. This is not maintainer
approval or evidence of full parity.
