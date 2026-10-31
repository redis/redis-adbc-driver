# Redis integration guide for Apache Spark

Complete the [shared example setup](example-setup.md) first. It builds this
checkout's driver, verifies Redis Software with Search and active 4x QPF, and
sets the environment used below. Use a separate environment for this tool.

## Install and run

Install JDK 21 and set `JAVA_HOME` to its installation before running. The recorded workflow uses Spark 4.3.0; older Spark versions may not accept an Arrow table directly.
```sh
python -m pip install 'pyspark>=4' pandas
```

Save the following as `example.py` in your working directory and run
`python example.py`. The shared helper creates a unique Redis table with three
rows, including NULLs and Unicode, and removes it when the example finishes.

```python
from integration_guide_data import example_table, read_rows
import os
import sys
from pyspark.sql import SparkSession

os.environ['SPARK_LOCAL_IP'] = '127.0.0.1'
os.environ['PYSPARK_PYTHON'] = sys.executable
with example_table() as name:
    table = read_rows(name)
spark = (SparkSession.builder.master('local[1]').appName('redis-adbc-guide')
         .config('spark.ui.enabled', 'false')
         .config('spark.driver.bindAddress', '127.0.0.1')
         .config('spark.driver.host', '127.0.0.1')
         .config('spark.sql.shuffle.partitions', '1').getOrCreate())
try:
    frame = spark.createDataFrame(table)
    assert [r.asDict() for r in frame.orderBy('id').collect()] == table.to_pylist()
    frame.createOrReplaceTempView('redis_rows')
    row = spark.sql('SELECT count(*), sum(amount) FROM redis_rows').collect()[0]
    assert tuple(row) == (3, 10.0)
    print(tuple(row))
finally:
    spark.stop()
```

## Expected result

The local Spark job prints `(3, 10.0)` after checking the converted rows.

## Limits and validation

This fetches Arrow on the Python client before constructing a Spark DataFrame. It is not a Spark database datasource, JDBC path, or distributed Redis reader. The recorded Spark version warns about pandas 3.x compatibility.

The example follows the [saved workflow](../../python/tests/test_spark_candidate.py). Historical execution is
recorded in the [native sweep](../sdlc/changes/candidate-test-runner/native-sweep.md)
or [follow-up](../sdlc/changes/native-candidate-followup/validation.md).
The [guide validation record](../sdlc/changes/integration-guides-262/validation.md)
reports the checks of these actual instructions.

From the repository root, with this tool installed, rerun the actual guide block:

```sh
python python/scripts/check_integration_guide.py spark
```

The broader workflow can also be checked with
`python/scripts/run-candidate-tests.sh spark` after installing its test extra.

[All integration guides](README.md)
