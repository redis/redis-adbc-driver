# Redis integration guide for Quarto

Complete the [shared setup](example-setup.md). Install the Quarto CLI separately
and install `ipykernel` and `nbformat` into the active Python environment:

```sh
python -m pip install ipykernel nbformat
python -m ipykernel install --sys-prefix --name python3
export QUARTO_PYTHON="$(python -c 'import sys; print(sys.executable)')"
```

## Render a report

Save this document as `redis-report.qmd`. The Python cell creates and cleans up
its own example table through the shared helper:

````qmd
---
title: Redis ADBC report
format: html
jupyter: python3
---

```{python}
from integration_guide_data import example_table, read_rows

with example_table() as name:
    table = read_rows(name)
assert table['label'].to_pylist() == ['alpha', None, '雪']
assert table['amount'].to_pylist() == [2.5, None, 7.5]
print('ADBC_ROWS_CHECKED=' + str(table.num_rows))
```
````

Run `quarto render redis-report.qmd --to html` in the configured shell. Open
`redis-report.html` and check for `ADBC_ROWS_CHECKED=3`. The command must exit
successfully; a stale output file is not proof of successful execution.

## Limits and validation

The kernel needs the same native-library path, helper, Python packages and Redis
access as the shell. The example tests local HTML rendering, not publishing,
remote kernels, other formats or Flight SQL. Query results materialize in memory.
See the [saved Quarto workflow](../../python/tests/test_document_candidates.py)
and [actual guide results](../sdlc/changes/integration-guides-262/validation.md).

From the repository root, rerun the actual document with the installed Quarto CLI:

```sh
python python/scripts/check_integration_guide.py quarto
```

[All integration guides](README.md)
