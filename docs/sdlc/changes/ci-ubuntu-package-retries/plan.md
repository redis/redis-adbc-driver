# CI Ubuntu package reliability plan and evidence

Date: 2026-11-11. Author: Codex. Status: local checks passed; hosted CI pending.
See [intent](intent.md) and [spec](spec.md).

1. Read actual failed logs and confirm failures precede compilation/validation.
2. Add bounded strict apt transaction retries in the composite cache action and
   release distribution containers; guard optional cache statistics.
3. Add offline shell execution regression cases and run them in the Test helper job.
4. Open a PR and inspect hosted checks. Rerun the original failed jobs to recover
   transient failures independently of this hardening change.

Local evidence: `python3 -m unittest discover -s scripts -p test_ci_apt_retries.py -v`
passed all four cases, covering both setup paths. No Redis server tests run.
Review: self-review against REVIEW.md found no driver/data ownership or permission
changes; retries remain bounded and persistent package failures remain fatal.
Human maintainer review remains pending.
