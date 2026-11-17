# Dependency refresh plan and evidence

Date: 2026-11-17. Base: `b63a010d`. Status: implemented and locally verified; hosted CI and human review pending.
See [intent](intent.md) and [specification](spec.md).

## Work

1. Read repository invariants, prior refresh evidence, and current upstream code.
2. Query `go list -m -u -json all` and PyPI JSON metadata. Refresh active module
   requirements with `go get -u -t ./...`, explicitly align driverbase/testutil,
   and run `go mod tidy`. Refresh pgdiff using `uv lock --upgrade`.
3. Update precise Python floors and current validation pins. Leave historical
   benchmark requirements, Rust native-module probe revision
   and coupled external source pins unchanged: those reproduce specific work.
   Refresh the AWS provider from 6.72.0 to 6.73.0 with backend-disabled
   `terraform init -upgrade`; validate locally without planning or applying.
4. Consolidate key sorting through the Go standard library. Preserve cancellation,
   ARE, Substrait and Superset adapters for the reasons in the specification.
5. Verify builds, dependency integrity, Go/Python checks, and self-review under
   REVIEW.md. Record completed results below; hosted CI/human review remain pending.

## Selected versions

| Dependency | Previous | Selected |
|---|---|---|
| Go toolchain | 1.27.3 | 1.27.4 |
| driverbase / testutil | 4c82882827b5 | 809dff5b62b1 |
| Go/Python ADBC | 1.12.0 | 1.13.0 |
| Arrow Go | 18.8.0 | 18.9.0 |
| regexp2 | 2.9.1 | 2.13.0 |
| PyArrow main floor | 26.0.0 | 26.0.1 |
| pandas main floor | 3.1.0 | 3.1.1 |
| Polars | 2.0.0 | 2.1.0 |
| SQLAlchemy main floor | 2.1.6 | 2.1.9 |
| Dagster | 1.13.26 | 1.13.30 |
| Prefect | 3.9.0 | 3.9.3 |
| dlt | 1.31 | 1.32.0 |
| pytest | 9.1.1 | 9.2.0 |
| Airflow test pin | 3.3.2 | 3.4.0 |
| Arrow provider | 0.1.1 | 0.2.0 |
| Ray pin | 2.59.0 | 2.60.0 |
| MLflow analysis floor | 3.17.2 | 3.18.0 |
| psycopg / sqlparse | 3.3.6 / 0.6.0 | 3.3.9 / 0.6.1 |
| Analytics Redis Python client | >=5,<8 | >=8.1,<9 |
| Terraform AWS provider | 6.72.0 | 6.73.0 |

Go transitive updates are recorded in go.mod/go.sum. Broad Python ranges such as
boto3, dnspython and the optional model/workflow candidates already admit newer
releases; their lower bounds are unchanged. Analytics advances its Redis Python client to >=8.1.0,<9.0; its RESP3, pooling
and WATCH/MULTI behavior is covered by the collector regressions. mkdocs 1.6.1 and the package build tools
are already current. The official Substrait Go module remains v0.85.0, behind the
repository's pinned v0.108.0 descriptors, so it cannot replace generated bindings.

## Sources

- [ADBC 1.13.0 release](https://arrow.apache.org/blog/2026/10/26/adbc-25-release/)
- [Go module versions](https://proxy.golang.org/)
- [Python package metadata](https://pypi.org/)
- [driverbase statement wrapper](https://github.com/adbc-drivers/driverbase-go/blob/809dff5b62b1/driverbase/statement.go)

## Security-driven toolchain update

The initial scan on Go 1.27.3 reported
[GO-2026-6859](https://pkg.go.dev/vuln/GO-2026-6859). Raised the module's Go
requirement and CLAUDE.md to Go 1.27.4, the current stable patch listed by
[go.dev](https://go.dev/dl/?mode=json). The follow-up scan reports no known
vulnerabilities. This is scanner evidence, not a demonstrated application exploit.
The already running broad Software pass uses its initial Go 1.27.3 binary;
final builds, unit checks and focused integration verification use Go 1.27.4.

## Verification

| Check | Result |
|---|---|
| `go test -count=1 -p 1 ./...` without REDIS_URI, Go 1.27.4 | Pass; Redis-dependent cases skip |
| `make -C go build adbc-cli`, Go 1.27.4 | Pass; versioned shared library and CLI |
| `go mod verify` | All modules verified |
| `govulncheck ./...`, Go 1.27.4 | No known vulnerabilities found |
| pgdiff locked harness unit tests | 36 passed |
| Selected core Python checks without Redis, Python 3.14 | 45 passed, 34 skipped |
| Airflow 3.4.0 / Arrow provider 0.2.0 without Redis, Python 3.12 | 5 passed, 7 skipped; upstream provider-discovery deprecation warnings |
| Ray 2.60.0 without Redis, Python 3.12 | 10 passed, 10 skipped |
| `analysis-tests` dependency resolution, Python 3.12 | Pass; 104 packages resolved |
| CI matrix / acceptance runner unit tests | 9 / 13 passed |
| actionlint on four changed workflows | Pass |
| `terraform init -backend=false -upgrade` / `terraform validate` | Pass; AWS provider 6.73.0 |

Broad Software main-package pass: 1,193 passing top-level cases, 393 skips,
857.997 seconds, initial Go 1.27.3 binary. The overall `make test-software`
command exited 1 after that successful package because an earlier interrupt
stopped the runner before the remaining packages. This is not recorded as a
successful whole-command run. A final Software pass on Go 1.27.4 completed
focused main-package regex, lateral, stream-binding, cancellation and HASH checks,
then all remaining packages, exit 0 (42 passing top-level cases, two Software ACL
skips). The final unit-only pass covers the entire
final source tree on Go 1.27.4.

Software skips include separate snapshot/cluster/Flex/operator/profile fixtures
and protocol ACL operations blocked by Software. Those profiles were not enabled
or weakened. Selected Redis-backed core Python integration run on Python 3.14: 79 passed,
zero skips, 78.47 seconds. This includes DB-API, SQLAlchemy, dlt, Dagster, Dask,
pandas, Polars and Prefect. Main extras also resolve on Python 3.12 (160 packages).
The retained SQLAlchemy 1.4/Arrow 18 import and float/NULL conversion checks pass.
Airflow 3.4.0 / Arrow provider 0.2.0 Redis-backed run on Python 3.12: 12
passed, zero skips, 25.39 seconds, including two full local DAG executions.
Two warnings concern deprecated upstream provider discovery APIs.

The first Ray integration invocation via `uv run --with-editable ../python`
completed three cases, then workers could not resolve that relative source path
inside uv's reconstructed worker environment. Interrupted that invocation and
reran from an ordinary isolated venv, matching CI's existing execution model;
no test assertion or product workaround was added. Final Ray 2.60.0 integration on Python 3.12: 20 passed, zero skips, 23.44
seconds, including workers, snapshots/retries, cancellation and required private-CA
TLS checks. Three upstream Ray Data deprecation warnings remain. Analytics Redis Python 8.1.0: all 18 collector/backup tests pass against
Software on Python 3.12, zero skips, 1.321 seconds. Backup tests use a fake S3
client; no cloud backup or deployment was performed. All Redis testing uses an owned disposable Redis Software
fixture created through `make -C go redis-up`, with effective 4x QPF verification.
Local environment: macOS arm64, Redis Software Redis 8.8.0, three shards, TLS
through Docker published ports.
Operational logs, TLS trust material and endpoint identifiers remain private.

## Review and remaining limits

Self-review followed REVIEW.md. The sorting change preserves deterministic
lexical order and lateral memo keys; existing consumers only iterate results.
Dependency changes preserve module major versions except the explicitly tested
analytics Redis client. CI now uses the same Airflow/Ray extras tested locally.
No storage format, ownership protocol, cancellation path or C wrapper changed.
No actionable correctness finding remains from this review.

The broad Software package pass and final focused/remainder pass are reported
separately above. Separate snapshot-cluster/Flex/operator profiles, full upstream
ADBC and SQLAlchemy acceptance suites, model/analysis integrations, hosted CI,
release wheels and cross-platform builds were not run. Existing Superset
compatibility/security limits remain as documented in the October refresh; the
Superset app itself was not rerun. Terraform validation does not establish a
cloud deployment plan. No benchmark, release or deployment was created.

The owned disposable Software fixture was removed after verification. Generated
operational files and local logs are private and are not included in this change.
