# Contract

See [intent](intent.md) for request provenance and [plan](plan.md) for execution.
The tests must consume complete results and assert ordered values, Unicode,
NULLs, timestamps, Arrow types, default pandas conversion, index selection, and
empty-result columns through `pd.read_sql` with a native ADBC connection.
The SQLAlchemy route must support bound parameters and chunked reads.

Pandas 3.1.0 rejects `params` and `chunksize` through its direct ADBC reader;
those operations are tested through SQLAlchemy instead. Other pandas versions
are outside the local verification evidence. Tests use a dedicated table and
drop it after use; use a disposable Redis database and run sequentially.
