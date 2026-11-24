# Verification

2026-11-24. Clean public-main documentation branch. No SQL, native/C++ build,
server/container operation or benchmark was run for this publication.

Read the saved Source 13 Q4 query record, metadata and allowlisted scanner/API
proof fields. Recomputed both completed medians and the direct Redis/reference
ratio; checked original SQL SHA-256 and exact source/library/scanner identities.
The frontend has an empty timed sample list and a recorded DuckDB interrupt timer
cause. Its JSON median/ratio/validated shape are explicitly null, and its summary
marks all_requested_engines_validated false despite direct-path validation.

Read isolation.py engine_worker and validation_worker sequencing directly:
engine_worker fetches/exports all four repetitions before publishing executed;
only then does the coordinator invoke independent validation. A first-timed
failure therefore leaves the completed warm-up independently unvalidated. An
earlier inference of warm-up validation was corrected immediately in issue #627
before preparing this report. The report makes only the full-fetch observation;
reference dimensions are never relabeled as frontend output.

Deduplication checked repository Q4 titles and TPC-DS/scanner matches. Created
issue #627 with existing enhancement and needs-triage labels included in the
creation request, separate from direct-path #590. Linked actual upstream PR #22.

Fast static checks passed: saved values/medians/ratio/SQL checksum, explicit
frontend nonvalidation, preservation of every existing historical report line
except the evidence-snapshot/section heading, exact Candidate 12 first-success
row, local Markdown targets, JSON privacy allowlist and git diff whitespace.
The unchanged historical verifier still matches all 134 expected query rows and
all suite summaries. No operational paths, PIDs, container/infrastructure IDs,
credentials, endpoints, raw worker objects, licenses or backup manifests are
included. Root workspace/user files remain untouched.
