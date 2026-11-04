# Specification: host-staged GPU experiment

Date: 2026-11-03. Source: `95d6583`. Status: experimental.
See [intent](intent.md), [plan](plan.md) and
[detailed feasibility assessment](../../../gpu-query-feasibility.md).

Use existing host Arrow output with optional consumer-side cuDF execution.
Preserve the shipping CPU default. The experiment supports nullable int64
group keys/values, a nonnegative-value predicate and count/min/max/sum.
Reject out-of-domain types and overflowing sums before GPU execution. Compare
all results and output physical types with an independent Python oracle.

Time upload, synchronized compute, download and host-to-host execution. Redis
mode reads an existing stable table, fully fetches both scan and direct SQL,
and clearly labels modeled pipeline costs. Synthetic trials cannot establish
Redis query speedup. CUDA failures cannot silently run on CPU.

The expanded native contract is in [gpu-execution.md](../../../gpu-execution.md).
CUDA evaluates one integer column/literal comparison in bounded batches; CPU
fetching, grouping and exact aggregation remain. Explicit CUDA failures cannot
silently substitute CPU execution. Restricted query shapes return unsupported
errors. The ordinary CPU path retains existing pushdown/stream behavior.

Add standalone host/device staging, native BindDevice and ExecuteDevice stream
extensions without changing standard ADBC slots. Accept flat numeric/boolean
CPU/CUDA arrays with explicit device IDs, event synchronization and independent
output ownership. Reject unsupported device/types and invalid sizes before data
access. Cancellation is observed around bounded CUDA batches. Test real CUDA
producer/consumer lifetime, NULLs, integer boundaries and early stream release.
