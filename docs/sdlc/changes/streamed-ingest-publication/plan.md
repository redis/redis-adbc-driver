# Plan

1. Pull main and freeze a clean detached baseline at 24f654b7; record library/CLI
   versions, source/build/input/harness hashes and actual environment privately.
2. Review the existing Arrow worker and full-result validator; implement a
   separate helper without editing established workload definitions.
3. Acquire an exclusive timing slot. Verify the owned Software fixture and run
   all three input contracts with one/four callers, one warmup plus three
   measurements each (24 trials), reversing case order between repetitions.
4. Rerun the full same-driver 156-trial matrix (84 reads, 40 writes, 32 creation)
   using unchanged workers as a current reference, not a causal historical
   revision comparison.
5. Independently check output types/values/counts/ordinals, call counts and input
   batch ceilings. Record every attempt and do not tune after timing.
6. Publish sanitized samples, an arithmetic/distribution audit, timing and
   publication-semantics caveats, reproduction instructions and final cleanup.
   Root owns the shared fixture; subagents receive serialized loans only.

See [intent](intent.md) and [contract](spec.md).

Review: [PR #427](https://github.com/redis/redis-adbc-driver/pull/427) is open; no merge or human approval is claimed.
