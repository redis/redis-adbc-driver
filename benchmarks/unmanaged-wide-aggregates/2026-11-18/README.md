# Unmanaged full-scale aggregate validation

The first full-scale trial of candidate `b1a535fb` (merged in PR #497) failed
Q3's original 300-second wall deadline. No complete result was produced and no
query success, validated speedup or resolution of #493–#496 is claimed.

The diagnostic sampled at 299 seconds reports 76,608,000 consumed rows and
151,970,886,136 allocated bytes during execute. These are partial-run diagnostic
counters, not a completed benchmark timing. The fixed 86,918-MiB cgroup cap
remained in effect; observed trial memory peak was 183,332,864 bytes.

Environment: dedicated AWS c8g.metal-48xl, 192 vCPU / 384 GiB; Ubuntu Linux
same-host loopback Docker-published, hostname/CA-verified TLS and RESP3;
Redis Software 8.2.6-58 / Redis 8.8.0; three RAM shards, 280-GiB database,
noeviction, hash-max-listpack-value 4096, active verified 4x QPF, six Search
workers and mnp/32 routing; automatic query workers granted 32.

The user authorized the original RAM reproducer. Data came from three native
backup shards with pinned S3 versions and actual downloaded SHA-256 checks;
no fresh ingestion occurred. Restore verified exactly 99,997,497 indexed rows,
105 columns and zero indexing failures. Before Q3, the harness checked all
105 Arrow types, three complete source-row probes, independent full-data
reference identities and a complete-fetch Q2 positive control. Reference SQL
is pinned to ClickBench `e90045879276f75cc398f1ab8a73541a7716058d`.

The runner stopped at this first failure. Q4–Q6 were not run by this candidate
attempt. The follow-up removes transient native-value maps from eligible scalar
consumers while retaining complete-image fallback behavior; it requires fresh
full-scale evidence before issue closure.

Raw sanitized [execution diagnostics](b1a535fb-q3-execution.json) and
[supervised outcome](b1a535fb-q3-outcome.json) are retained here. Infrastructure
identifiers, credentials, native backups and operational journals remain private.
