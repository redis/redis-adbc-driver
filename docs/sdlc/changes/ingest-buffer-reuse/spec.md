# Contract and acceptance

Keep default evaluation, column conversion and full-slice checks in their current
order before insertRows. Keep the existing 4096-row conversion boundaries,
managed whole-operation publication and unmanaged acknowledged partial effects.
Each row view has capacity equal to its width. Clear every active Value and row
view on success and every conversion/default/check/insert error, before the input
reader advances. Reuse is local to one executeIngest invocation; no global pool,
concurrent sharing or mutation of earlier Arrow input. Unsliced legacy records
larger than 4096 rows use temporary storage that is not retained for reuse.

Acceptance fixed before measurement: correctness and focused race/Software checks
must pass without weakened assertions; complete-conversion allocation comparisons
must reduce allocated bytes across repeated wide slices without a material time
regression. Parent-coordinated native managed/unmanaged comparisons must validate
all values/counts and show no clear elapsed/RSS regression. Otherwise preserve the
candidate as evidence only. All attempts/samples and limitations remain recorded;
no selective reruns or post-hoc change to this gate.

See [intent](intent.md), [plan](plan.md) and [validation](validation.md).

Outcome: evidence only. The original frozen contract remains above; measured
source `55396058` is preserved unapplied. Native memory gains accompany mixed
elapsed outcomes, so the original adoption gate is not established. Production
Go remains identical to `88774f10`. See [validation](validation.md).
