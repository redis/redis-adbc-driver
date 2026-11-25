"""Integration inventory shared by guide and native-candidate entry points.

Standard library only; dependency pins stay in pyproject.toml. Guide names and
candidate aliases are separate interfaces (for example pyarrow/arrow and
r-dbi/r), and their existing execution orders are intentional. Runtime setup
and fixture ownership remain in the runners.
"""

import argparse
from dataclasses import dataclass


@dataclass(frozen=True)
class Profile:
    python: str
    extras: str
    candidates: tuple[str, ...]
    guides: tuple[str, ...] = ()


# Candidate aliases map to exact pytest selectors, not arbitrary shell code.
CANDIDATES = {
    'driver': ('test_driver.py',),
    'duckdb': ('test_duckdb.py',),
    'pandas': ('test_pandas.py', 'test_pandas_direct_reads.py', 'test_pandas_writes.py'),
    'polars': ('test_polars.py',),
    'ml': ('test_ml_ingest.py',),
    'jupyter': ('test_jupyter.py',),
    'datasets': ('test_hf_datasets.py',),
    'streamlit': ('test_streamlit.py',),
    'scipy': ('test_scipy.py',),
    'plotly': ('test_plotting_candidates.py::test_plotly_html_and_json',),
    'matplotlib': ('test_plotting_candidates.py::test_matplotlib_agg_png',),
    'xgboost': ('test_model_candidates.py::test_xgboost_train_save_reload',),
    'pytorch': ('test_model_candidates.py::test_pytorch_dataloader_and_forward',),
    'tensorflow': ('test_model_candidates.py::test_tensorflow_dataset_batches',),
    'keras': ('test_model_candidates.py::test_keras_model_input_and_persistence',),
    'transformers': ('test_model_candidates.py::test_transformers_local_tokenizer_model',),
    'spark': ('test_spark_candidate.py',),
    'r': ('test_document_candidates.py::test_r_dbi_native_driver', 'test_document_candidates.py::test_r_dbi_parameter_binding'),
    'quarto': ('test_document_candidates.py::test_quarto_python_document',),
    'kubeflow': ('test_orchestrator_candidates.py::test_kubeflow_local_component',),
    'metaflow': ('test_orchestrator_candidates.py::test_metaflow_local_flow',),
    'fivetran': ('test_orchestrator_candidates.py::test_fivetran_sdk_local_tester',),
    'sqlalchemy': ('test_sqlalchemy.py',),
    'arrow': ('test_arrays.py', 'test_ipc.py', 'test_union_stream.py', 'test_guide_output.py'),
    'dbt': ('test_dbt_candidate.py',),
    'ibis': ('test_analysis_candidates.py::test_ibis_arrow_expressions',),
    'mlflow': ('test_analysis_candidates.py::test_mlflow_dataset_tracking', 'test_analysis_candidates.py::test_mlflow_default_empty_digest'),
    'beam': ('test_beam_candidate.py',),
    'mage': ('test_mage_candidate.py',),
    'kestra': ('test_kestra_candidate.py',),
}

# External CLI integrations own their setup and cannot run as Python guide
# snippets or pytest candidate selectors. Paths are relative to the repo root.
STANDALONE_GUIDES = {
    'sling': 'scripts/test_sling_postgres.py',
}

PROFILES = {
    'core': Profile(
        '3.14', 'test,candidate-tests',
        ('driver', 'pandas', 'polars', 'ml', 'sqlalchemy', 'arrow', 'duckdb'),
        ('pandas', 'polars', 'sqlalchemy', 'pyarrow', 'numpy', 'scikit-learn', 'duckdb'),
    ),
    'workflows': Profile(
        '3.14', 'workflow-tests',
        ('jupyter', 'datasets', 'streamlit', 'scipy'),
        ('jupyter', 'huggingface-datasets', 'streamlit', 'scipy'),
    ),
    'models': Profile(
        '3.14', 'plotting-tests,model-tests',
        ('plotly', 'matplotlib', 'xgboost', 'pytorch', 'tensorflow', 'keras', 'transformers'),
        ('pytorch', 'tensorflow', 'keras', 'xgboost', 'transformers', 'plotly', 'matplotlib'),
    ),
    'orchestrators': Profile(
        '3.14', 'orchestrator-tests',
        ('kubeflow', 'metaflow', 'fivetran'),
        ('kubeflow', 'metaflow', 'fivetran'),
    ),
    'spark': Profile('3.14', 'spark-tests', ('spark',), ('spark',)),
    'dbt': Profile('3.14', 'dbt-tests', ('dbt',), ('dbt',)),
    'documents': Profile('3.14', 'workflow-tests', ('r', 'quarto'), ('r-dbi', 'rstudio', 'quarto')),
    'analysis': Profile('3.14', 'analysis-tests', ('ibis', 'mlflow'), ('ibis', 'mlflow')),
    'beam': Profile('3.14', 'beam-tests', ('beam',), ('beam',)),
    'mage': Profile('3.14', 'mage-tests', ('mage',), ('mage',)),
    'kestra': Profile('3.14', 'kestra-tests', ('kestra',), ('kestra',)),
}

# Guide execution historically visits spark before orchestrators, and analysis
# before the document/server profiles. Keep that independent of candidate order.
GUIDE_PROFILE_ORDER = ("core", "workflows", "models", "spark", "orchestrators",
                       "analysis", "beam", "mage", "kestra", "documents",
                       "dbt")
GUIDE_PROFILES = {name: PROFILES[name].guides for name in GUIDE_PROFILE_ORDER}
GUIDES = tuple(guide for group in GUIDE_PROFILES.values() for guide in group)
# These workflows do not provide the same output-write/read-back contract.
READ_WRITE_GUIDES = frozenset(GUIDES) - {"fivetran"}


MOVED_GUIDES = {
    "airflow": "https://github.com/Redislabs-Solution-Architects/redis-adbc-integrations/blob/main/docs/integrations/airflow.md",
    "dagster": "https://github.com/Redislabs-Solution-Architects/redis-adbc-integrations/blob/main/docs/integrations/dagster.md",
    "dask": "https://github.com/Redislabs-Solution-Architects/redis-adbc-integrations/blob/main/docs/integrations/dask.md",
    "ray": "https://github.com/Redislabs-Solution-Architects/redis-adbc-integrations/blob/main/docs/integrations/ray.md",
    "prefect": "https://github.com/Redislabs-Solution-Architects/redis-adbc-integrations/blob/main/docs/integrations/prefect.md",
    "dlt": "https://github.com/Redislabs-Solution-Architects/redis-adbc-integrations/blob/main/docs/integrations/dlt.md",
    "superset": "https://github.com/Redislabs-Solution-Architects/redis-adbc-integrations/blob/main/docs/integrations/superset.md",
}

UNSUPPORTED_GUIDES = {name: f"{name} moved to {url}" for name, url in MOVED_GUIDES.items()}


def main():
    """Line-oriented queries for Bash; never emit code to eval or source."""
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)
    sub.add_parser("profiles")
    sub.add_parser("tools")
    profile = sub.add_parser("profile")
    profile.add_argument("name", choices=[*PROFILES, *UNSUPPORTED_GUIDES])
    tests = sub.add_parser("tests")
    tests.add_argument("tools", nargs="+", choices=[*CANDIDATES, *UNSUPPORTED_GUIDES])
    args = parser.parse_args()
    selected = [args.name] if args.command == "profile" else args.tools if args.command == "tests" else []
    for name in selected:
        if name in UNSUPPORTED_GUIDES:
            parser.error(UNSUPPORTED_GUIDES[name])
    if args.command == "profiles":
        print("\n".join(PROFILES))
    elif args.command == "tools":
        print(" ".join(CANDIDATES))
    elif args.command == "profile":
        item = PROFILES[args.name]
        print("\n".join((item.python, item.extras, *item.candidates)))
    else:
        if "ray-tls" in args.tools:
            parser.error("ray-tls requires its separate owned-fixture runner")
        print("\n".join(test for tool in args.tools for test in CANDIDATES[tool]))


if __name__ == "__main__":
    main()
