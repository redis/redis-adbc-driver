# Plan and evidence

2026-11-16. Base: `225e0056`. Author: Codex. Status: implemented; maintainer review pending.
See [intent](intent.md), [spec](spec.md).

1. Add a failing declaration/physical-type/IPC/ingestion regression.
2. Populate descriptive TYPE_NAME in the shared field builder; preserve JSON.
3. Cover empty case-sensitive tables/views, temporary relations, query schema
   and reader paths, existing GetObjects declarations and comments.
4. Run unit checks, verified Software checks and native/Flight client probes.
5. Review correctness, lifecycle, ownership and evidence per REVIEW.md.

Before the fix, TestSQLTypeFieldMetadata failed for ordinary SQL declarations.
The existing JSON IPC decoder consumes extension metadata and materializes the
registered Arrow JSON extension; its SQL TYPE_NAME survives separately.

## Validation

Checks ran on 2026-11-16 (America/Los_Angeles), base plus this working diff.

- Before fix: `env -u REDIS_URI go test -count=1 -p 1 -run
  '^TestSQLTypeFieldMetadata$' .` reproduced missing ordinary declarations.
- After fix: `cd go && env -u REDIS_URI go test -count=1 -p 1 ./...` passed
  across all packages. Redis-dependent tests skip in this unit-only run.
- `make -C go redis-up` provisioned a dedicated Redis Software 8.8.0 RAM fixture
  with three shards, authenticated verified TLS and Search. The setup gate
  verified active scaling factor 4, six Search workers, mnp routing, 32
  connections, noeviction and active primary roles before tests. Custom local
  ports/project/subnet isolated this run. An initial attempt to reuse the
  default fixture failed certificate setup; Docker default network exhaustion
  then required an explicit subnet. No default-fixture tests ran.
- Through `software-setup.sh --exec go test -count=1 -v -p 1 -run ... .`:
  the declaration regression, normalized Arrow storage, all selected comments
  and TIME WITH TIME ZONE checks passed (20 top-level tests, no skips).
- The nested Arrow layout storage inventory and extension storage replay/binding
  policy passed (three top-level tests with their subcases, no skips). Input
  fixtures remain metadata-free; output expectations include canonical field
  and nested-child declarations while retaining independent value arrays.
- Final expanded Software pass: declaration surfaces, all comment cases,
  all empty-result cases, parameter schema and native JSON round-trip/invalid
  input/parameter/array checks passed (24 top-level tests, no skips). The
  empty-result helper continues comparing physical types/nullability and other
  metadata; descriptive TYPE_NAME is checked by its dedicated regressions.
- `make -C go build` passed using the versioned shared-library build.
- `git diff --check` and local Markdown link-target checks passed.
- `scripts/check_schema_declarations.py` passed through the native C library and
  standard ADBC Flight SQL client: declarations, independent physical-type
  assertions, empty case-sensitive tables/views, comments, GetObjects and a
  complete value fetch. Native ingestion of a VARCHAR(17)-annotated string longer
  than 17 characters also passed, confirming descriptive metadata does not
  create an extension or impose SQL constraints on Arrow-created tables.
- Client versions: PyArrow 26.0.1, ADBC driver manager and Flight SQL 1.13.0.
  Flight public source `4d91e37135080be7f0eeb770fc89fa7adeb14bc8` was built
  in a disposable checkout with only a temporary local driver replacement.
  The service implementation was unchanged. Flight frontend used loopback gRPC;
  Redis backend used authenticated verified TLS. Client OS was macOS, using a
  Docker published backend port. This is correctness validation, not a benchmark.

Run the native/Flight probe after independently verifying the Software fixture:

```sh
uv run --with pyarrow --with adbc-driver-manager --with adbc-driver-flightsql \
  python scripts/check_schema_declarations.py --library "$REDIS_ADBC_LIBRARY" \
  --flight-uri "$FLIGHT_URI"
```

Omit `--flight-uri` for a native-only probe. The script uses the exported
`REDIS_URI` and `SSL_CERT_FILE`; the retained-control policy declaration must
come from the verified fixture setup. Operational artifacts, endpoints,
infrastructure identifiers and credentials remain private outside tracked source.

## Review and limitations

Self-review followed REVIEW.md: the only production change adds descriptive
field metadata in the shared builder. It changes no storage, permissions,
ownership, row values or lifecycle code. JSON's extension metadata and the
UUID/timetz input interpretation remain intact; ordinary TYPE_NAME does not
change ingestion type mapping. Comment metadata coexists with declarations.
Existing physical-schema test oracles were updated for the intentional addition;
exact declaration and IPC regressions check the metadata separately.

No actionable findings remain from self-review. This is not maintainer approval.
The entire Software suite, other Flight client implementations and remote
Flight TLS/authentication deployment were not run. Temporary relation metadata
was verified through Go ADBC; the cross-client probe covers permanent tables
and views. Optional client metadata display and original alias spelling remain
outside the contract, as described in [spec](spec.md).
