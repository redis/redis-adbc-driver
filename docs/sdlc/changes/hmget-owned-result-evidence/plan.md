# Plan

1. Add an isolated owned string/null consumer and lifetime/error/large-value
   tests beside the existing private-module fixture.
2. Add an opt-in `redis-owned` suite to the runner and backward-compatible
   summarizer support, leaving historical samples and default suite unchanged.
3. During a coordinated exclusive host slot, run focused race checks, then all
   twelve cases with three samples per case. Run independent concurrent-retained
   lifetime checks with multiple Go scheduler workers outside timing.
4. Review source hashes, all sample identities and complete value validation;
   publish sanitized evidence and an explicit owned-result API contract gap.
5. Update the existing upstream decoder issue with the concrete new evidence
   if successful; do not claim a production API or general application speedup.

See [intent](intent.md) and [contract](spec.md).
