# Additional Python workflow validation

- Date: 2026-10-30
- PR: [#248](https://github.com/redis/redis-adbc-driver/pull/248)
- Source: `6cad4089f93498de7ba4a000b2e071797978236c` plus this PR's workflow
  tests and runner follow-up. The native driver source is unchanged by the follow-up.
- Related records: [intent](intent.md), [specification](spec.md), [plan](plan.md)

## Environment and execution

macOS arm64, Python 3.15.2, ADBC driver manager 1.13.0, PyArrow 26.0.0,
pandas 3.1.0, and NumPy 2.5.4. A separate virtual environment was installed
from `python[workflow-tests]`; the native library was built with `make -C go build`.

The runner provisioned Redis Software, Redis 8.6.2, one shard, with Search.
`make -C go redis-up` verified active scaling factor 4 and effective Search
worker/routing configuration before the tests, including when reusing the fixture.
Earlier OSS results in [plan.md](plan.md) retain their original server label.

Reproduction after installing the workflow extra:

```sh
PYTHON=/path/to/workflow-venv/bin/python python/scripts/run-candidate-tests.sh jupyter datasets streamlit scipy
```

Result: **5 passed, zero skips**. The successful command exercised provisioning,
QPF verification, native-library selection, and test invocation through the saved
runner. No model, remote dataset, or hosted application was downloaded or contacted
by the tests.

| Tool | Tested version | Checks |
|---|---|---|
| Jupyter kernel / nbclient | ipykernel 7.4.0 / nbclient 0.11.0 | Execute two notebook cells in a separate kernel using the test interpreter; fetch stored rows, retain Arrow types/NULLs/Unicode after closing the connection, and save/read the executed notebook output. |
| Hugging Face Datasets | 5.1.0 | Convert fetched Arrow results to Dataset, preserve types/NULLs/Unicode, save/reload locally, and filter rows; both populated and empty results. |
| Streamlit | 1.66.1 | AppTest reads stored Arrow rows into a dataframe and metric; change a numeric input, rerun the app, and validate the bound query's filtered rows. |
| SciPy | 1.18.1 | Fetch stored sparse matrix coefficients, construct a CSR matrix, solve a linear system, and independently check the expected solution and residual. |

## Empty Dataset persistence

The initial run reported **4 passed, 1 failed**. Datasets 5.1.0's default
`save_to_disk()` wrote zero shards for an empty table; `load_from_disk()` raised
`IndexError`. The same failure reproduced with a locally constructed empty Arrow
table without Redis or ADBC. Passing `num_shards=1` retained one empty shard and
preserved the schema on reload. The saved integration test explicitly uses this
setting for both cases; the default empty save/reload path fails in the tested
version.

## Limits

These are custom local Python data paths, not dedicated Redis connectors. The
JupyterLab browser interface and Streamlit browser/hosting behavior were not
tested. HF Hub access, remote datasets/models, distributed computation, training
at scale, Flight SQL, TLS, and other package/platform versions were not tested.
The entire suite was not run on this Python 3.15 host because the pinned Ray and
Dagster runtimes require a different supported interpreter. Existing application
records remain separate from this five-test workflow run.
