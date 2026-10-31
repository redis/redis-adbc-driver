# Redis integration guide for Kestra

Complete the [shared setup](example-setup.md). Use the isolated Python environment
for Kestra and Java 25 or newer. The example runs a loopback-only disposable local
server with generated authentication, executes two tasks, and shuts it down.

## Install and run

From the repository root:

```sh
python -m pip install -e './python[kestra-tests]'
export KESTRA_JAVA=/absolute/path/to/java25/bin/java
export KESTRA_JAR=/tmp/redis-guide-kestra/kestra-2.0.7
export KESTRA_PLUGINS_PATH=/tmp/redis-guide-kestra/plugins
bash python/scripts/prepare-kestra-candidate.sh /tmp/redis-guide-kestra
python python/examples/kestra_guide.py
```

The preparation script downloads pinned Kestra 2.0.7, checks its checksum, and
installs the Python Script plugin. The [complete runnable example](../../python/examples/kestra_guide.py)
creates a flow using `io.kestra.plugin.scripts.python.Script` tasks with the
`io.kestra.plugin.core.runner.Process` runner. The first task opens native ADBC,
executes a bound query, and writes `read.json` as an `outputFiles` artifact.
The downstream task receives it through `inputFiles`, checks rows and Arrow
schema in a different process, and reports checked values with `Kestra.outputs`.

Both task environments explicitly receive `REDIS_URI`, `REDIS_ADBC_LIBRARY`,
`PYTHONPATH`, and the Python interpreter's directory in `PATH`. To adapt the
example, edit its `read` script and expected rows/schema together. Do not place
production credentials in a checked-in flow or share unredacted task output.

## Expected result and limits

Both tasks must finish with `SUCCESS`, the checked output must report three
rows with Unicode preserved, and the example prints `KESTRA_GUIDE_OK`.
The server and example table are cleaned up even if assertions fail.

This is custom Python task code, not a native ADBC Kestra plugin. Hosted servers,
container task runners and remote workers need their own packaging, credentials
and network configuration. See the [historical workflow](../../python/tests/test_kestra_candidate.py)
and [actual guide results](../sdlc/changes/integration-guides-262/validation.md).

Rerun this guide's linked example with the configured Java/runtime:

```sh
python python/scripts/check_integration_guide.py kestra
```

[All integration guides](README.md)
