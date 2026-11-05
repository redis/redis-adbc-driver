# Plan and evidence: TIMETZ CI repair

Date: 2026-11-01. Source: `7d6fb15`. Author: Codex for Albert Wong.
Status: local verification complete; maintainer review pending.
Inputs: [intent](intent.md), [specification](spec.md).

## Files and order of work

1. Inspect the failed GitHub Actions jobs and reproduce the original test.
2. Correct filter/view expectations in `go/timetz_integration_test.go` and
   order view rows explicitly.
3. Add fixed January/July comparison cases in `go/timetz_test.go`.
4. Run focused verification, review with `REVIEW.md`, and submit a PR.

## Verification

- The original test, run through a Go source overlay on Redis Software 8.6.2
  with Search and verified QPF 4x, failed with exactly the two CI mismatches:
  IDs `[8, 10]` versus `[8]` for both the filter and view.
- `go test -count=1 -run '^TestTimeTZSessionZone$' .`: passed, including fixed
  January and July cases.
- `bash software-setup.sh --exec go test -count=1 -run '^TestTimeTZSessionZoneColumns$' .`:
  passed after fixture policy and effective QPF verification.
- `bash software-setup.sh --exec go test -count=1 -v -run '^TestTimeTZ' .`:
  all nine tests passed, with no skips, on the same verified Software fixture.
- `gofmt` and `git diff --check`: passed. Local record links resolve.

## Review and limitations

Self-review against `REVIEW.md`: expectations preserve exact rows, offsets,
and ordering; no production code, permissions, or data-ownership changes.
Maintainer approval is pending. Local integration evidence covers one Software
shard on macOS arm64; the Linux architecture/topology matrix is delegated to CI.
