# Plan and evidence

Date: 2026-11-04. See [intent](intent.md) and [spec](spec.md).
Base: `f1e1f033d8e90b3d07de64d63d36b185944228f6`.

## Reconciliation

The runner and documented command already landed through the Arrow work
([#266](https://github.com/redis/redis-adbc-driver/pull/266), `054a788`),
followed by TLS fixture updates. No parallel helper was added.
The existing mock omitted `tls_mode` and expected a plaintext URI, so its
success test failed before the change with the topology rejection message.
The updated mock models TLS and asserts URI/password encoding, CA propagation,
inherited endpoint replacement and non-persistence of the declaration. It also
checks refusal for wrong topology/TLS, ineffective QPF, inactive primary and
unsafe or unknown policy. Temporary fixture copies avoid changing local TLS or
test environment artifacts during mock checks.

## Verification

- `python3 -m unittest discover -s scripts -p test_software_setup_exec.py -v`:
  five tests pass, including six negative topology/readiness subcases.
- `python3 -m unittest discover -s scripts -p test_ci_test_matrix.py -v`:
  five tests pass.
- Isolated disposable fixture: pinned Software image `redislabs/redis:8.2.0-78.18`,
  default three primary shards, authenticated TLS, noeviction, effective 4x QPF.
  Operational identifiers and credentials remain private.
- `make -C go test-software TEST_ARGS='-run TestRetained -json'`:
  141 passing test/subtest events, six skips, zero failures. Root package 6.848 s.
  Publication/replay/revoke, update-after-capture, source capture and policy
  unit checks passed. Skips: three subprocess helper entry points, two ACL
  capability tests and CONFIG policy admission (Software filters CONFIG).
- `make -C go test-software TEST_ARGS=-json`: exit 0; **1,736 passing
  test/subtest events, 43 skips, zero failures**. All six test-bearing packages
  passed; root package 301.773 s. The CUDA package has no tests in this build.
  No unrelated failures or transport EOFs occurred in this run; this does not
  close the separate intermittent EOF investigation.
- `bash -n go/software-setup.sh go/software-qpf.sh` and `git diff --check`: pass.

## Review and cleanup

Reviewed the test, workflow and documentation diff against the three passes in
REVIEW.md. Production driver/fixture behavior, policy checks and capability skips
are unchanged. The helper checks now run in CI on changes to their own file.
No actionable correctness, ownership or security findings remained in this
review; this is agent review, not a separate human approval.

The task-owned Docker fixture and network are removed after validation; existing
fixtures are left running. Raw logs remain private; the counts above are the
public validation evidence.

No driver assertions or capability skips were weakened. The separate transport
EOF work remains [#217](https://github.com/redis/redis-adbc-driver/issues/217)
and [#265](https://github.com/redis/redis-adbc-driver/issues/265).
