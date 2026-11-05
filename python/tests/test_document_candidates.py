"""Native R/DBI and Quarto document execution against Redis ADBC."""

import os
from pathlib import Path
import shutil
import subprocess
import sys
import textwrap

import pytest


def test_r_dbi_native_driver(uri, workflow_rows, tmp_path):
    rscript = os.environ.get("RSCRIPT") or shutil.which("Rscript")
    if not rscript:
        pytest.skip("Rscript and the adbi/adbcdrivermanager packages are required")
    name, _ = workflow_rows
    script = Path(__file__).resolve().parents[1] / "examples" / "r_dbi_smoke.R"
    result = subprocess.run(
        [rscript, str(script)], env={**os.environ, "REDIS_URI": uri, "REDIS_ADBC_TEST_TABLE": name},
        cwd=tmp_path, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=120,
    )
    assert result.returncode == 0, result.stdout
    assert "REDIS_ADBC_R_DBI_OK" in result.stdout


class RBindingSchemaCompatibilityError(AssertionError):
    pass


@pytest.mark.xfail(
    raises=RBindingSchemaCompatibilityError,
    strict=True,
    reason="adbi 0.1.3 treats unnamed ADBC parameter fields as named placeholders",
)
@pytest.mark.parametrize("predicate, params, expected", [
    ("id >= ?", "list(2L)", "c(2, 3)"),
    ("id >= ? AND id <= ?", "list(2L, 3L)", "c(2, 3)"),
    ("label = ?", "list(NA_character_)", "numeric(0)"),
    ("label IS NOT DISTINCT FROM ? AND id >= ?", "list(NA_character_, 1L)", "2"),
])
def test_r_dbi_parameter_binding(uri, workflow_rows, tmp_path, predicate, params, expected):
    rscript = os.environ.get("RSCRIPT") or shutil.which("Rscript")
    if not rscript:
        pytest.skip("Rscript and the adbi/adbcdrivermanager packages are required")
    name, _ = workflow_rows
    script = tmp_path / "binding.R"
    script.write_text(textwrap.dedent("""\
        library(DBI)
        library(adbi)
        driver <- adbcdrivermanager::adbc_driver(Sys.getenv("REDIS_ADBC_LIBRARY"))
        con <- dbConnect(adbi(driver), uri=Sys.getenv("REDIS_URI"))
        name <- Sys.getenv("REDIS_ADBC_TEST_TABLE")
        result <- dbSendQuery(con, sprintf('SELECT id FROM "%s" WHERE {predicate} ORDER BY id', name))
        cat("ADBI_VERSION=", as.character(packageVersion("adbi")), "\\n", sep="")
        schema <- nanoarrow::nanoarrow_schema_parse(
          adbcdrivermanager::adbc_statement_get_parameter_schema(result@statement), recursive=TRUE)
        stopifnot(length(schema$children) == length({params}), all(names(schema$children) == ""))
        for (attempt in 1:2) {
          dbBind(result, {params})
          stopifnot(identical(as.numeric(dbFetch(result)$id), {expected}))
        }
        dbClearResult(result)
        dbDisconnect(con)
        """).replace("{predicate}", predicate).replace("{params}", params).replace("{expected}", expected))
    result = subprocess.run(
        [rscript, str(script)], env={**os.environ, "REDIS_URI": uri, "REDIS_ADBC_TEST_TABLE": name},
        cwd=tmp_path, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=120,
    )
    if (result.returncode != 0 and "ADBI_VERSION=0.1.3" in result.stdout
            and "Expecting named `params` for named placeholders" in result.stdout):
        raise RBindingSchemaCompatibilityError(result.stdout)
    assert result.returncode == 0, result.stdout


def test_quarto_python_document(uri, workflow_rows, tmp_path):
    quarto = os.environ.get("QUARTO") or shutil.which("quarto")
    if not quarto:
        pytest.skip("Quarto CLI is required; set QUARTO to its executable")
    pytest.importorskip("ipykernel")
    name, _ = workflow_rows
    document = tmp_path / "redis_adbc.qmd"
    document.write_text(textwrap.dedent("""\
        ---
        title: Redis ADBC candidate
        format: html
        jupyter: python3
        execute:
          echo: false
        ---

        ```{python}
        import os
        from adbc_driver_redis import dbapi
        with dbapi.connect(os.environ["REDIS_URI"]) as conn, conn.cursor() as cur:
            name = os.environ["REDIS_ADBC_TEST_TABLE"]
            cur.execute(f'SELECT * FROM "{name}" ORDER BY id')
            data = cur.fetch_arrow_table()
        assert data["label"].to_pylist() == ["alpha", None, "雪"]
        assert data["amount"].to_pylist() == [2.5, None, 7.5]
        print("ADBC_ROWS_CHECKED=" + str(data.num_rows))
        ```
        """))
    env = {**os.environ, "REDIS_URI": uri, "REDIS_ADBC_TEST_TABLE": name,
           "QUARTO_PYTHON": sys.executable, "JUPYTER_RUNTIME_DIR": str(tmp_path / "runtime")}
    result = subprocess.run(
        [quarto, "render", str(document), "--to", "html"], cwd=tmp_path, env=env,
        text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=180,
    )
    assert result.returncode == 0, result.stdout
    assert "ADBC_ROWS_CHECKED=3" in document.with_suffix(".html").read_text()
