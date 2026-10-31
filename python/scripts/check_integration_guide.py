"""Execute a new integration guide's actual Python block against REDIS_URI.

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

TOOLS = ("numpy", "scikit-learn", "jupyter", "huggingface-datasets", "streamlit", "scipy")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("tool", choices=TOOLS)
    args = parser.parse_args()
    if not os.environ.get("REDIS_URI"):
        parser.error("set REDIS_URI to a dedicated Redis database with Search")
    root = Path(__file__).resolve().parents[2]
    guide = root / "docs" / "integrations" / f"{args.tool}.md"
    blocks = re.findall(r"```python\n(.*?)```", guide.read_text(), re.DOTALL)
    if len(blocks) != 1:
        raise ValueError(f"Expected one Python example in {guide.name}")
    code = blocks[0]
    with tempfile.TemporaryDirectory(prefix="redis-guide-") as directory:
        work = Path(directory)
        example = work / "example.py"
        example.write_text(code)
        if args.tool == "jupyter":
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
            subprocess.run([sys.executable, str(example)], cwd=directory, check=True, timeout=60)
    print(json.dumps({"guide": guide.name, "status": "passed"}))


if __name__ == "__main__":
    main()
