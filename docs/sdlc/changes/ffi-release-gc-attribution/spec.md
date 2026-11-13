# Contract and scope

The normal driver, module dependencies, exported ABI and release behavior remain
unchanged. Experimental libraries add a build-gated server-free ownership fixture
and optionally omit exactly four `runtime.GC()` calls through a private overlay.
The supplied patch is an attribution experiment, not an adopted optimization.

Exercise the actual release wrappers, cgo handles, Arrow export/import ownership,
foreign array and stream callbacks, retained output buffers, errors and SQLSTATEs.
Double-release database/connection/statement calls must return invalid-state;
Arrow consumers must skip NULL release pointers on repeated cleanup. The fixture
repeats that consumer helper, rather than calling a saved callback after release.
Do not treat this as the handle-release contract.

Measure each C release entrypoint separately with preparation and runtime counter
reads outside timing. Check explicit owner closure once, cleared handles, checked
Arrow allocation balance and forced-GC counts. Retained output arrays must remain
readable after stream and statement teardown; foreign producer callbacks must run
at their last explicit owner release. A passing non-sanitized probe does not prove
ASAN behavior or production driver cleanup, Redis pools, workers or cancellations.

A later native read comparison uses unchanged complete-output validation and a
verified owned Redis Software TLS/RESP3 fixture with active/effective 4x QPF. Keep
all samples, warmups, failed attempts and order/host/RSS caveats. No ClickBench.

See [intent](intent.md) and [plan](plan.md).
