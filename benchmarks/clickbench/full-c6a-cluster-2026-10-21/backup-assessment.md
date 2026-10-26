# Existing S3 backups

Inspected 2026-10-21 while the fresh full-scale load continued.

The October 9 backup at `s3://clickbench-backup-3444273-735486936198/full-99997497-d83d504/` has 40 compressed RDB files totaling 50,181,080,429 bytes (46.73 GiB). Its manifest records 99,997,497 rows, connector revision `d83d504d65cd5b3bd14fd9bd559f5acad6ffedf2`, and the same source Parquet SHA256 used in the current run (`a390f6cb782f6aaef278c72fc1dd86c4f30bc843ebab3c159e9bd4d45ddb079f`). All 40 listed files exist with their declared sizes. The manifest contains per-file SHA256 values; the compressed objects have not been downloaded and rehashed during this inspection.

An October 8 backup also exists at `s3://adbc-clickbench-backup-3443283-735486936198/20261008/rdb/`: 40 files totaling 50,854,818,953 bytes (47.36 GiB). Its manifest records Redis 8.6 and 99,997,497 rows, and explicitly says restoration was not tested.

The backups have 40 shard files with explicit hash-slot ranges. The current deployment has 48 primaries. Restoration into the current image has not been tested and would require verifying the RDB/module format, driver catalog/schema compatibility, distributing keys to the current slot owners, and independently checking row counts and results. Replacing the active partial load would discard ongoing progress. At inspection the fresh load had already passed 36 million rows, with all 48 primaries reachable, so it continued. Neither backup was modified or deleted.

The full typed DuckDB reference outputs from the October 13 S3 run are already reused by this run after source/query provenance checks; these eliminate reference generation work but do not eliminate Redis ingestion.
