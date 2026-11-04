# Specification: bounded Substrait execution

Date: 2026-11-04. Baseline: `f4d5b78`. Author: Codex.
[Intent](intent.md), [plan](plan.md), [validation](validation.md).

The [runtime contract](../../../substrait.md) defines the grammar, types,
functions, binding, statuses and ceilings. Version 0.108.0 replaces the unshipped
0.61.0 proposal. Review of the current upstream schema found URN-based extension
references, a required execution-behavior declaration, new detached subtrees,
parameters, type aliases and lambdas. Only the per-plan behavior and two offline
URNs are accepted; newly added expressions/relations/declarations reject.
The latest release's BETWEEN and mask changes do not affect this subset:
BETWEEN and masks are rejected. Emit ordering follows the pinned Project rules.

Wire validation precedes unmarshalling and semantic translation, with duplicate
singular/oneof and unknown-field checks at every nested message. Typed ASTs
retain ordinal identity, literal precision and output nullability, independent
of SQL inference. Execution reuses exact HASH scans/residual evaluation and
existing Arrow conversion, with a dedicated owned reader and per-execution
scan/byte/deadline budgets. SQL execution keeps its existing limits.

C admission checks precede pointer dereference, size narrowing and copying. The
regeneration patch script restores these checks after external wrapper generation.
No ABI change. The generated schemas stay internal and carry source hashes and
licensing. A runtime support claim requires actual driver-manager evidence;
[validation](validation.md) records what is available, not maintainer approval.
