# Implementation and evidence

See [intent](intent.md) and [spec](spec.md).

Implemented exact orphaned-scan identification and adoption, an incident-specific locked controller, explicit finisher recovery entry point, and non-budget failure checks. Native driver and timed worker are unchanged. Thirteen focused adoption/resume/priority tests pass; changed Python sources compile and git diff whitespace checks pass.

Live recovery supervisor started at 06:17:55Z and verified the original statement PID and job without restarting it. Guarded finisher PID 56983 entered waiting_for_recovered_api_preservation at 06:20:02Z. Remaining scan correctness, final preservation and cleanup are pending. The maintenance overlap is retained as a timing limitation, separate from correctness acceptance.

Review: agent inspected identity checks, original-trial isolation, lock lifecycle, immutable deadlines, preservation and cleanup gates. Human review is pending.
