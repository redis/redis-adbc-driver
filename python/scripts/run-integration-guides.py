"""Run every README integration guide with preinstalled compatible environments.

Build the native library and verify the Software fixture with make -C go
redis-up first. Set REDIS_URI and REDIS_ADBC_LIBRARY. Environment directories
use the profile names of run-all-native-candidates.sh; this runner does not
install packages or change existing environments. R, Quarto, Java, Kestra and
their runtime paths must already be configured as their guides describe.
"""

import argparse
import json
import os
from pathlib import Path
import subprocess
import time

PROFILES = {
    "core": ("pandas", "polars", "sqlalchemy", "pyarrow", "numpy", "scikit-learn",
             "dagster", "dask", "prefect", "dlt", "ray", "duckdb"),
    "workflows": ("jupyter", "huggingface-datasets", "streamlit", "scipy"),
    "models": ("pytorch", "tensorflow", "keras", "xgboost", "transformers", "plotly", "matplotlib"),
    "spark": ("spark",), "orchestrators": ("kubeflow", "metaflow", "fivetran"),
    "analysis": ("ibis", "mlflow"), "beam": ("beam",), "mage": ("mage",),
    "kestra": ("kestra",), "documents": ("r-dbi", "rstudio", "quarto"),
    "airflow": ("airflow",), "superset": ("superset",), "dbt": ("dbt",),
}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--env-root", required=True, type=Path)
    parser.add_argument("--results", required=True, type=Path)
    args = parser.parse_args()
    if not os.environ.get("REDIS_URI") or not os.environ.get("REDIS_ADBC_LIBRARY"):
        parser.error("set REDIS_URI and REDIS_ADBC_LIBRARY to the verified fixture and current build")
    root = Path(__file__).resolve().parents[2]
    # Never count a report from an earlier run as current success.
    args.results.mkdir(parents=True, exist_ok=True)
    report = {}
    summary = args.results / "summary.json"
    summary.write_text("{}\n")
    for profile, tools in PROFILES.items():
        python = (args.env_root / profile / "bin/python").absolute()
        for tool in tools:
            start = time.monotonic()
            env = {**os.environ, "PATH": str(python.parent) + os.pathsep + os.environ["PATH"]}
            with (args.results / f"{tool}.log").open("w") as output:
                try:
                    completed = subprocess.run(
                        [str(python), str(root / "python/scripts/check_integration_guide.py"), tool],
                        cwd=root, env=env, stdout=output, stderr=subprocess.STDOUT, timeout=420,
                    )
                    code = completed.returncode
                except (OSError, subprocess.TimeoutExpired) as error:
                    output.write(str(error) + "\n")
                    code = 124 if isinstance(error, subprocess.TimeoutExpired) else 127
            report[tool] = {"profile": profile, "exit_code": code,
                            "seconds": round(time.monotonic() - start, 1)}
            summary.write_text(json.dumps(report, indent=2) + "\n")
            print(f"{tool}: {'passed' if code == 0 else 'FAILED'}", flush=True)
    return int(any(result["exit_code"] != 0 for result in report.values()))


if __name__ == "__main__":
    raise SystemExit(main())
