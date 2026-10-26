# October 21 c6a full-scale run records

This directory records an ongoing, explicitly requested benchmark. It is not a
completed full-run score or a generic deployment tool.

- [Current full-run checkpoint](report.md): 99,997,497 rows loaded; query sweep
  and whole-run correctness validation are pending.
- [Independently validated preliminary comparison](early-comparison/report.md):
  first 11 queries only, both hot outputs checked for each query. The complete
  43-query comparison remains pending. Redis uses 49 instances in total; the
  selected published baselines each use one c6a.4xlarge.
- [Existing backup assessment](backup-assessment.md) and [restore instructions](restore.md).
- `full_job.py`, `dual_path_job.py`, `backup_full.py`, `uploader.py`,
  `cleanup_lambda.py`, `restore_drill.py`, `restore_check_client.py`, and
  `finish_run.py` preserve the actual orchestration used or scheduled for this
  run. They contain run-specific paths, topology, dates and account references;
  they are evidence, not a supported CLI. Inspect and adapt them before reuse.
  The approval/configuration files, SSH keys, credentials and process state
  required by local orchestration are deliberately not committed.

The driver binary used here is pinned to source commit
`9f5188c411f1e3a3bf1423a72089e2441b7995bd`. This PR adds harness functionality;
rebasing the PR does not change the source revision of the running driver.
The separate supported-scan comparison waits for the original run and its
validation. Snapshotting waits for that comparison, followed by the approved
restore test and exact-owned-resource cleanup. The backup is not complete until
its S3 manifest explicitly says so. Export/checksum success alone does not prove
restoration success.
