#!/usr/bin/env bash
# Run direct Redis ADBC candidates in dependency-compatible environments.
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
uv_bin="${UV:-uv}"
env_root="${CANDIDATE_ENV_ROOT:-${TMPDIR:-/tmp}/redis-adbc-native-candidates}"
results="${CANDIDATE_RESULTS_DIR:-$env_root/results}"
mkdir -p "$results"
if ! command -v "$uv_bin" >/dev/null 2>&1; then
  echo "Install uv or set UV to its executable before running all candidates" >&2
  exit 2
fi
export UV_PYTHON_INSTALL_DIR="${UV_PYTHON_INSTALL_DIR:-$env_root/python}"
export UV_CACHE_DIR="${UV_CACHE_DIR:-$env_root/cache}"
export RAY_USAGE_STATS_ENABLED=0
export AIRFLOW_HOME="$env_root/airflow-home"
export AIRFLOW__CORE__LOAD_EXAMPLES=False
export TF_CPP_MIN_LOG_LEVEL=2
export OMP_NUM_THREADS=1
export CANDIDATE_SDK_CACHE="${CANDIDATE_SDK_CACHE:-$env_root/fivetran-tester}"
status=0
profiles=(core workflows models orchestrators spark airflow superset dbt documents analysis beam mage kestra ray-tls)
if [[ -n "${CANDIDATE_PROFILES:-}" ]]; then
  read -r -a profiles <<< "$CANDIDATE_PROFILES"
fi
if [[ ${#profiles[@]} -eq 0 ]]; then
  echo "CANDIDATE_PROFILES must include at least one profile" >&2
  exit 2
fi
for profile in "${profiles[@]}"; do
  case "$profile" in
    core|workflows|models|orchestrators|spark|airflow|superset|dbt|documents|analysis|beam|mage|kestra|ray-tls) ;;
    *) echo "Unknown native candidate profile: $profile" >&2; exit 2 ;;
  esac
done

run_profile() {
  local profile=$1 python_version=$2 extras=$3
  shift 3
  local profile_env="$env_root/$profile"
  # Avoid treating a stale report from an earlier run as current evidence.
  rm -f "$results/$profile.xml"
  if ! "$uv_bin" venv --allow-existing --python "$python_version" "$profile_env"; then
    return 1
  fi
  if ! "$uv_bin" pip install --python "$profile_env/bin/python" -e "$repo_root/python[$extras]"; then
    return 1
  fi
  if [[ "$profile" == dbt ]]; then
    local adapter="${CANDIDATE_DBT_ADAPTER:-git+https://github.com/redis/redis-dbt-adapter.git@c9574b98762f4ff80b60a92b2d4137c2b03bf9df#subdirectory=v1/adapter}"
    if ! "$uv_bin" pip install --python "$profile_env/bin/python" "$adapter"; then
      return 1
    fi
  fi
  if [[ "$profile" == kestra ]]; then
    export KESTRA_JAR="${KESTRA_JAR:-$env_root/kestra-runtime/kestra-2.0.7}"
    export KESTRA_PLUGINS_PATH="${KESTRA_PLUGINS_PATH:-$env_root/kestra-runtime/plugins}"
    if ! bash "$repo_root/python/scripts/prepare-kestra-candidate.sh" "$env_root/kestra-runtime"; then
      return 1
    fi
  fi
  PYTHON="$profile_env/bin/python" CANDIDATE_JUNIT_XML="$results/$profile.xml" \
    "$repo_root/python/scripts/run-candidate-tests.sh" "$@"
}

for profile in "${profiles[@]}"; do
  echo "Running native candidate profile: $profile"
  rm -f "$results/$profile.xml" "$results/$profile.exitcode"
  : > "$results/$profile.log"
  case "$profile" in
    core) args=(3.12 'test,candidate-tests' driver pandas polars ml sqlalchemy arrow duckdb dask dagster prefect dlt ray) ;;
    ray-tls) args=(3.12 ray-tests ray-tls) ;;
    workflows) args=(3.12 workflow-tests jupyter datasets streamlit scipy) ;;
    models) args=(3.12 'plotting-tests,model-tests' plotly matplotlib xgboost pytorch tensorflow keras transformers) ;;
    orchestrators) args=(3.12 orchestrator-tests kubeflow metaflow fivetran) ;;
    spark) args=(3.12 spark-tests spark) ;;
    airflow) args=(3.14 airflow-tests airflow) ;;
    superset) args=(3.12 'superset,test-runner' superset) ;;
    dbt) args=(3.14 dbt-tests dbt) ;;
    analysis) args=(3.12 analysis-tests ibis mlflow) ;;
    beam) args=(3.12 beam-tests beam) ;;
    mage) args=(3.12 mage-tests mage) ;;
    kestra) args=(3.12 kestra-tests kestra) ;;
    documents)
      args=(3.12 workflow-tests r quarto)
      if ! command -v "${RSCRIPT:-Rscript}" >/dev/null 2>&1 || \
          ! command -v "${QUARTO:-quarto}" >/dev/null 2>&1; then
        rm -f "$results/$profile.xml"
        echo "Rscript and Quarto are required for the documents profile" | tee "$results/$profile.log" >&2
        echo 2 > "$results/$profile.exitcode"
        status=1
        continue
      fi
      export R_LIBS_USER="${R_LIBS_USER:-$env_root/r-library}"
      mkdir -p "$R_LIBS_USER"
      if ! "${RSCRIPT:-Rscript}" -e 'p <- c("DBI", "adbi", "adbcdrivermanager"); missing <- p[!vapply(p, requireNamespace, logical(1), quietly=TRUE)]; if (length(missing)) install.packages(missing, lib=Sys.getenv("R_LIBS_USER"), repos="https://cloud.r-project.org")' 2>&1 | tee "$results/$profile.log"; then
        status=1
        echo 1 > "$results/$profile.exitcode"
        continue
      fi
      ;;
  esac
  if run_profile "$profile" "${args[@]}" 2>&1 | tee "$results/$profile.log"; then
    echo 0 > "$results/$profile.exitcode"
  else
    profile_status=$?
    echo "$profile_status" > "$results/$profile.exitcode"
    status=1
  fi
done
if ! python3 "$repo_root/python/scripts/summarize-candidate-results.py" "$results" "${profiles[@]}"; then
  status=1
fi
echo "Native candidate reports: $results"
exit "$status"
