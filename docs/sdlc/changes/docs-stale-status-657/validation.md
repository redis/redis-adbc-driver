# Validation

Date: 2026-11-24. Source: `0cde7e38` plus this change. Author: Claude Code;
agent verification is not human approval.
[Intent](intent.md), [specification](spec.md), [plan](plan.md).

## GitHub state on 2026-11-24

Read with `gh issue view` / `gh pr view` and issue timelines.

| Reference | State |
|---|---|
| #471 | Closed (completed) 2026-11-17; the only comment reports Superset 7.x works, with no run recorded here |
| #599 | Closed 2026-11-24 after PR #637 (merged 2026-11-24); #648 open for the SF1 rerun |
| #118, #361, #329 | Closed 2026-11-10 |
| #170 | Closed 2026-10-26, no comment; no open successor found |
| #273 | Closed 2026-11-17 through merged PR #477 |
| #345 | Closed as not planned 2026-11-07 |
| #93, #98, #107, #110, #113, #114, #116, #360, #362, #363, #272 | Open |
| RediSearch/RediSearch#12337, docker/desktop-feedback#854 | Open; created 2026-11-15 |
| redis/go-redis#4324 | Open |
| apache/airflow#76273 | Open; labels `kind:bug`, `needs-triage` |
| Query-farm/adbc_scanner#21 | Base `v2.0` since 2026-11-10; closed without merge 2026-11-24 |
| apache/superset#45852 | Closed without merge 2026-11-21; no explanatory comment |

The other sixteen upstream documentation PRs in `status.md` were also re-read:
Ray, SQLAlchemy #13707, dbt and NumPy are merged; scikit-learn and SciPy are
closed without merge; pandas, Polars, Airflow, Prefect, dlt, Dagster, Dask,
Jupyter, Hugging Face Datasets and Streamlit are open and non-draft.

## Checks

| Command | Result |
|---|---|
| `python3 -m unittest scripts/test_integration_registry.py` before the registry change, with the updated test | 4 failures: all three CLIs still print the #471 blocker message |
| Same, after the change (Python 3.14.8) | 11 tests OK |
| `python scripts/prepare_docs.py && python -m mkdocs build --strict && python scripts/check_docs.py` (uv, Python 3.14, `requirements-docs.txt`) | Exit 0; 45 pages; no warnings |
| Relative-link and anchor check over the 23 changed Markdown files (scratch script) | All changed links resolve. Eight hits in the existing table of contents of `data-engineering-adbc-support.md` came from the script's slug rules; the built HTML has those ids |
| `gofmt -l go/partition_retained_resp.go`; `cd go && go build .` | Clean; exit 0 (comment-only Go change) |
| `git diff --check` | Clean |

`python -m pytest` was not used because pytest is not installed in the local
Python 3.14; the file is a `unittest` suite and ran directly.

## Limits

No Redis server, integration suite or benchmark ran; no runtime behavior changed.
External links were not fetched apart from the GitHub API reads above. The
moved Superset guide in redis-adbc-integrations still describes #471 as open.
