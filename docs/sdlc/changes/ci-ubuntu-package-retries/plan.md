# CI Ubuntu package reliability plan and evidence

Date: 2026-11-11. Author: Codex. Status: local checks passed; hosted CI pending.
See [intent](intent.md) and [spec](spec.md).

1. Read actual failed logs and confirm failures precede compilation/validation.
2. Add bounded strict apt transaction retries in the composite cache action and
   release distribution containers; guard optional cache statistics.
3. Add offline shell execution regression cases and run them in the Test helper job.
4. Correct the cold scanner source pin exposed by PR validation, retaining all
   upstream overlay patches and mandatory checksum verification.
5. Open a PR and inspect hosted checks. Rerun the original failed jobs to recover
   transient failures independently of this hardening change.

Local evidence: `python3 -m unittest discover -s scripts -p test_ci_apt_retries.py -v`
passed all four cases, covering both setup paths. No Redis server tests run.
Review: self-review against REVIEW.md found no driver/data ownership or permission
changes; retries remain bounded and persistent package failures remain fatal.
Human maintainer review remains pending.

Both original failing workflow runs passed on attempt 2. PR #388 helper checks
passed; its first cold scanner build failed the upstream tag archive checksum.
Downloaded the official tag and its resolved commit archives, confirmed all 1,750
files match, and pinned the immutable commit with measured SHA512. Two pin-script
tests pass, including refusal of unexpected overlays without modification.
Full hosted checks for the final change remain pending. Source evidence:
https://github.com/apache/arrow-adbc/tree/a3f81c6feb1133ba80600ac32e6d7c0b0b407e18
