"""Execute an integration guide's actual code or linked workflow against REDIS_URI.

Requires a dedicated Redis with Search. Install the selected tool and driver
in the current interpreter. For a source build, set REDIS_ADBC_LIBRARY and
PYTHONPATH before running, so child processes and kernels use that build.
"""

import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile

TOOLS = ("numpy", "scikit-learn", "jupyter", "huggingface-datasets", "streamlit", "scipy",
         "pyarrow", "pytorch", "tensorflow", "keras", "xgboost", "transformers", "plotly",
         "matplotlib", "ibis", "mlflow", "spark", "kubeflow", "metaflow", "fivetran",
         "beam", "mage", "r-dbi", "rstudio", "quarto", "kestra", "pandas", "polars",
         "sqlalchemy", "prefect", "dask", "ray", "dlt", "dagster", "airflow", "superset",
         "dbt", "duckdb")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("tool", choices=TOOLS)
    parser.add_argument("--require-read-write", action="store_true",
                        help="fail unless the executed workflow verifies an output write and read-back")
    args = parser.parse_args()
    if args.require_read_write:
        result = subprocess.run([sys.executable, __file__, args.tool], text=True,
                                stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=400)
        print(result.stdout, end="", flush=True)
        if result.returncode:
            raise SystemExit(result.returncode)
        if "REDIS_ADBC_READ_WRITE_OK" not in result.stdout:
            raise RuntimeError(f"{args.tool}: workflow passed without read/write output evidence")
        print(json.dumps({"guide": args.tool, "read": "passed", "write": "passed"}))
        return
    if not os.environ.get("REDIS_URI"):
        parser.error("set REDIS_URI to a dedicated Redis database with Search")
    root = Path(__file__).resolve().parents[2]
    os.environ["REDIS_ADBC_REPO"] = str(root)
    os.environ["PYTHONPATH"] = os.pathsep.join((str(root / "python/examples"), str(root / "python"),
                                              os.environ.get("PYTHONPATH", "")))
    os.environ["PATH"] = str(Path(sys.executable).parent) + os.pathsep + os.environ["PATH"]
    if args.tool in {"dagster", "dask", "airflow", "superset", "dbt", "duckdb"}:
        from guide_checks import check_linked_workflow
        check_linked_workflow(args.tool, root)
        print(json.dumps({"guide": args.tool, "status": "passed"}))
        return
    guide = root / "docs" / "integrations" / f"{args.tool}.md"
    blocks = re.findall(r"```python\n(.*?)```", guide.read_text(), re.DOTALL)
    if not blocks and args.tool not in {"r-dbi", "rstudio", "quarto", "kestra"}:
        raise ValueError(f"Expected Python examples in {guide.name}")
    code = "\n\n".join(blocks)
    with tempfile.TemporaryDirectory(prefix="redis-guide-") as directory:
        work = Path(directory)
        example = work / "example.py"
        example.write_text(code)
        if args.tool in {"r-dbi", "rstudio"}:
            r_blocks = re.findall(r"```r\n(.*?)```", guide.read_text(), re.DOTALL)
            script = work / "example.R"
            script.write_text(r_blocks[-1])
            env = {key: value for key, value in os.environ.items() if key != "REDIS_ADBC_TEST_TABLE"}
            subprocess.run([os.environ.get("RSCRIPT", "Rscript"), str(script)], env=env,
                           cwd=directory, check=True, timeout=120)
        elif args.tool == "quarto":
            document = work / "redis-report.qmd"
            document.write_text(re.findall(r"````qmd\n(.*?)````", guide.read_text(), re.DOTALL)[0])
            env = {**os.environ, "QUARTO_PYTHON": sys.executable}
            subprocess.run([os.environ.get("QUARTO", "quarto"), "render", str(document), "--to", "html"],
                           env=env, cwd=directory, check=True, timeout=180)
            rendered = document.with_suffix(".html").read_text()
            assert "ADBC_ROWS_CHECKED=3" in rendered
            assert "REDIS_ADBC_READ_WRITE_OK" in rendered
            print("REDIS_ADBC_READ_WRITE_OK")
        elif args.tool == "kestra":
            subprocess.run([sys.executable, str(root / "python/examples/kestra_guide.py")],
                           cwd=directory, check=True, timeout=300)
        elif args.tool == "jupyter":
            import nbformat
            from nbclient import NotebookClient

            kernel = work / "jupyter" / "kernels" / "redis-guide"
            kernel.mkdir(parents=True)
            (kernel / "kernel.json").write_text(json.dumps({
                "argv": [sys.executable, "-m", "ipykernel_launcher", "-f", "{connection_file}"],
                "display_name": "Redis guide validation",
                "language": "python",
            }))
            env = {
                **os.environ,
                "JUPYTER_PATH": str(work / "jupyter"),
                "JUPYTER_CONFIG_DIR": str(work / "config"),
                "JUPYTER_RUNTIME_DIR": str(work / "runtime"),
                "IPYTHONDIR": str(work / "ipython"),
            }
            # Kernel discovery occurs in this process before launching the child.
            os.environ.update({key: env[key] for key in (
                "JUPYTER_PATH", "JUPYTER_CONFIG_DIR", "JUPYTER_RUNTIME_DIR", "IPYTHONDIR"
            )})
            notebook = nbformat.v4.new_notebook(cells=[nbformat.v4.new_code_cell(code)])
            executed = NotebookClient(notebook, kernel_name="redis-guide", timeout=60).execute(
                cwd=directory, env=env
            )
            assert executed.cells[0].execution_count == 1
            assert any("REDIS_ADBC_READ_WRITE_OK" in output.get("text", "")
                       for output in executed.cells[0].outputs)
            print("REDIS_ADBC_READ_WRITE_OK")
        elif args.tool == "streamlit":
            from streamlit.testing.v1 import AppTest

            result = AppTest.from_file(str(example), default_timeout=30).run()
            assert not result.exception, result.exception
            assert result.metric[0].value == "3"
            assert result.dataframe[0].value["id"].tolist() == [1, 2, 3]
            result.number_input[0].set_value(2).run()
            assert not result.exception, result.exception
            assert result.metric[0].value == "2"
            assert result.dataframe[0].value["id"].tolist() == [2, 3]
        else:
            subprocess.run([sys.executable, str(example)], cwd=directory, check=True, timeout=300)
    print(json.dumps({"guide": guide.name, "status": "passed"}))


if __name__ == "__main__":
    main()
