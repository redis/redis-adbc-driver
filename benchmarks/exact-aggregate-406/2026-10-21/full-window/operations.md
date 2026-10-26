# Reserved #406 window operations

Do not execute remotely until the owner chat sends explicit ready and the gate
confirms complete backup and successful restore. Cutoff 2026-10-22T06:55:00Z;
resource expiry 07:16:14.601018Z is immutable.

1. Receive exact schema, Parquet path, gate, bucket/report prefix and credential
   path (path only). Validate source/binary checksums and available seconds.
2. Transfer the pinned runner dependencies, native libraries and provenance to a
   new /opt/clickbench/performance-406 directory; do not overwrite original/fixed
   benchmark artifacts or source files. Use its existing Python environment.
3. Use the owner's root-only JSON credential file directly when it already
   contains the single URI field. Otherwise make a private adapter without
   displaying credentials. Configuration contains paths only. Copy the ready
   gate, preserving all original fields.
4. Choose one exact supervisor unit name, journal it in both local and remote
   active_job_units BEFORE systemd-run. Launch the Python supervisor with memory
   accounting, MemoryMax=24GiB, MemorySwapMax=0, KillMode=control-group,
   TimeoutStopSec=5s and RuntimeMaxSec no later than the fixed cutoff. The controller cap leaves room for its conservative available-memory preflight;
   Oracle itself uses DuckDB's 8GB/4-thread limits and closes before trials; trials are sibling 16GiB isolated units with 16
   internal aggregate workers. No other benchmark runs concurrently.
5. Mirror only remote job_units/active_job_units into the owner chat's local gate,
   retaining the owner's readiness/backup/restore/release fields. The comparison
   journals each exact random trial unit before launch. Preserve stdout progress,
   every raw result/failure, diagnostics and independently computed oracle.
6. Primary acceptance requires all 24 trials exact and complete. Any timeout,
   OOM, mismatch or insufficient window stays visible. Optional Go CPU/allocation
   profiles follow only after primary completion and adequate remaining reserve.
7. Verify every journaled service is inactive and no owned process remains.
   Preserve report, raw evidence, libraries/provenance and operation scripts in
   S3 under full-c6a-20261021/performance-406/. Verify the report object exists.
8. Only then update local gate released=true, results_preserved=true,
   no_active_jobs=true, report_key=<verified key>, active_job_units=[]. Tell the
   owner chat the exact report key and no-active-job evidence. Never signal
   release on a running, unpreserved or unverified job.

The actual deployment uses `/opt/clickbench/performance-406`, its existing
`/opt/clickbench/venv/bin/python`, and the owner's private credential file outside
that directory. `preserve.py` checks every journaled unit and owned worker before
archiving, rejects an echoed connection URI, uploads with the existing instance
role and AES256 encryption, and verifies current S3 HEAD metadata. It additionally
checks the report's current GET version/size/metadata/content hash. Version-specific
S3 reads are outside the existing role's permission scope; no IAM changes or
operator credential copies are needed. Release applies only to #406 and lets the
owner resume its separate API trials.
