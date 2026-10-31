# Specification: release prerequisite download limits

Date: 2026-10-30. Inputs: [intent](intent.md). Execution: [plan](plan.md).

The release platform matrix must retain package signature and TLS validation,
the existing retry count, and all validation tests. Its DNF invocation must
abandon transfers below 1 MiB/s for 30 seconds so another mirror can be tried.
The previous 60-second timeout with DNF's default 1000-byte/s minimum allowed
the observed slow transfers to continue indefinitely. These option semantics
are documented in the [DNF configuration reference](https://dnf.readthedocs.io/en/latest/conf_ref.html).

Verify installation of Git using the exact corrected command in disposable
AlmaLinux 9 containers on amd64 and arm64, and on AlmaLinux 8 and Amazon Linux
2023 arm64. Lint the workflow. These setup checks do not establish full driver
compatibility; the release matrix remains responsible for that coverage.
