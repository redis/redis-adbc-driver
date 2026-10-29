# Arrow bulk loading versus redis-py

`run.py` compares ADBC Arrow append with redis-py/hiredis pipelined HSET using
the same 100000-row × 105-column typed Arrow fixture and identical managed
HASH/Search layout. One/four fresh writers run a warmup plus six measured rounds.
The timer spans common empty-table/index setup through worker startup, input
conversion, writes, teardown and index readiness. Full independent validation
runs outside timing. Native writes omit generic SQL safeguards.

Use an empty, dedicated Redis Software fixture created and verified with
`make -C go redis-up`: active 4× QPF, six Search workers, `mnp` routing, 32
connections, and RESP3. The runner itself checks RESP3 and empty-database
ownership; Software/QPF verification is a required external preflight. It refuses
an initially nonempty server, then FLUSHDBs its owned fixture between trials.
Never target an application database. Keep raw run outputs private until reviewed.

```sh
python3 benchmarks/bulk-load/run.py --help
python3 -m unittest discover -s benchmarks/bulk-load -p 'test_*.py' -v
```

Required arguments are `--driver`, `--uri` (explicit `?protocol=3`), `--parquet`,
`--clickbench`, `--work` and `--dedicated-empty-server`; use a fresh output
directory. Install PyArrow, redis-py, hiredis and ADBC driver manager first.

See the [v0.0.18 report](../v0018-refresh/2026-10-29/report.md) and
[reproduction matrix](../v0018-refresh/2026-10-29/README.md) for the measured
versions, CPU/memory limits, fixture hashes and timing limitations. This runner
is unchanged from the measured implementation.
