# Plan and validation: release prerequisite mirrors

Date: 2026-10-30. Base: `9564496`. Inputs: [intent](intent.md), [spec](spec.md).

1. Inspect recent Actions runs and failing logs; distinguish failures superseded
   by successful follow-ups from unresolved failures.
2. Set DNF's low-speed threshold and timeout in the release validation step.
3. Exercise the corrected installation command in disposable distribution
   containers, lint the workflow, and inspect the diff against REVIEW.md.
4. Submit a pull request with the incident links and verification evidence.

Validation:

- The corrected `dnf --setopt=timeout=30 --setopt=minrate=1M
  --setopt=retries=2 -y install git` completed successfully in AlmaLinux 9
  arm64 and amd64 (emulated), AlmaLinux 8 arm64, and Amazon Linux 2023 arm64
  disposable containers. `git --version` succeeded in each container.
- A local HTTP fixture streamed metadata at 80 KiB/s. DNF abandoned the first
  transfer and issued its next request after 30.0 seconds. The probe observed
  request timing because DNF buffers its error output across metadata retries;
  it stopped DNF after 40 seconds rather than waiting for all retries.
  Parsed DNF CLI configuration also confirmed a 1048576-byte/s minimum and
  30-second timeout on every real AlmaLinux repository.
- `actionlint -shellcheck= -pyflakes= .github/workflows/release.yml` passed.
  Separately, all 38 multiline workflow shell scripts passed `bash -n`.
- `git diff --check` passed. Full Redis/driver suites were not run locally for
  this prerequisite-download change; the PR triggers the release matrix.
- Review against REVIEW.md found no test expectation, permission, signature,
  TLS or ownership changes. The remaining risk is unavailable/slow mirrors;
  the existing step deadline still bounds their retries.

No human review or merge approval is inferred.
