# Validation execution contract

Date: 2026-10-20. Source: `95d6583`. Status: implemented; CI pending.
PR: see the containing PR. See [intent](intent.md) and [plan/evidence](plan.md).

Compatibility jobs have a 20-minute limit. Redis startup, image pull, validation,
and CLI smoke checks have 5-, 3-, 8- and 3-minute limits respectively. Failure
inspection and unconditional validation-container removal each have one minute.
Each matrix entry uses its own runner, so the container name is runner-local.

Image download is a separate step. The validation shell prints its platform and
system-install, fixture, Python-resolution and test phases. apt/dnf and uv network
requests use explicit timeout/retry settings; the step deadline bounds all phases.
uv resolves dependencies before running the unchanged test directory with verbose,
unbuffered pytest output. Pytest emits a traceback after a test runs for 60 seconds;
that diagnostic does not itself terminate the test. The step deadline does.

On failure, capture container processes/logs and Redis logs before removing any
remaining validation container. No test skips or assertions are relaxed. A timeout
is a failed check, not evidence of compatibility. Published-release uploads still
require all validation jobs to succeed.
