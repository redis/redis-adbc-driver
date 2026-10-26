# Plan and current evidence

1. Inspect quotas, pricing, cleanup principals and actual reaper tag logic.
2. Add and test self-hosted cluster protection preflight; preserve Cloud mode.
3. Provision tagged/protected nodes, fixed-deadline cleanup, S3 checkpoints.
4. Verify all 48 primaries, 16,384 slots, full source/reference provenance and
   native driver build. Run helper and real Linux isolation checks.
5. Load all rows with eight writers, monitor measured memory, then run the 129
   isolated query trials and independent validation.
6. Preserve final results, compare verified hot timings with published entries,
   and clean up only exact owned infrastructure.

Steps 1–4 complete. Live termination request against the protected client was
rejected with OperationNotPermitted. All 49 nodes, root volumes and security
group passed tag/protection readback. The enabled independent cleanup function
covers every instance. The 78 discovered tests include three opt-in tests skipped
in the default pass, then all three real Linux checks passed separately.
Full source SHA256 matches the complete typed reference set. The full load is
running; follow [the report](../../../../benchmarks/clickbench/full-c6a-cluster-2026-10-21/report.md).

See [intent](intent.md) and [spec](spec.md).


The full source is now loaded (99,997,497 rows, 808.79 seconds); the isolated
query sweep is running. The client has a waiting `clickbench-backup` service
that starts after timed queries, exports all 48 RDBs, checks them, and uploads
snapshots plus full inputs/provenance to the retained `backups/full-c6a-20261021/`
prefix. A separate backup-only preservation process verifies S3 objects and
saves final artifacts locally. Neither preservation process restarts instances
or tears down resources. The bucket is versioned, has no lifecycle expiration,
and has owner/reaper tags. New backup completion remains pending.

Automatic approval review rejected launching the combined restore/early-cleanup
pipeline because the transcript did not explicitly authorize terminating 49
instances and deleting their supporting resources. That pipeline was not
launched. Explicit approval has been requested for restoration testing and
early teardown; no dependent actions proceed without it. The previously
installed fixed-deadline cleanup remains unchanged. See `backup-status.json`
and `restore.md` in the full-run artifact directory.

The user subsequently explicitly approved the 48-primary restore test and cleanup of all 49 benchmark instances and owned supporting resources, preserving every S3 backup and reused IAM resource. The combined completion pipeline is now running; the redundant backup-only observer was stopped. New snapshots still wait for the timed sweep. A restore failure prevents early teardown and leaves evidence for diagnosis.
