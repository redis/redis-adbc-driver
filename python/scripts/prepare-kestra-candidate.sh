#!/usr/bin/env bash
# Download the pinned official local Kestra runtime; requires Java 25+.
set -euo pipefail
runtime="${1:?usage: prepare-kestra-candidate.sh RUNTIME_DIRECTORY}"
java_bin="${KESTRA_JAVA:-java}"
jar="${KESTRA_JAR:-$runtime/kestra-2.0.7}"
plugins="${KESTRA_PLUGINS_PATH:-$runtime/plugins}"
mkdir -p "$runtime" "$plugins"
python3 - "$java_bin" <<'PY'
import re
import subprocess
import sys
result = subprocess.run([sys.argv[1], "-version"], capture_output=True, text=True, check=True)
version = re.search(r'version "(\d+)', result.stderr + result.stdout)
if not version or int(version[1]) < 25:
    raise SystemExit("Kestra 2.0.7 requires Java 25+: set KESTRA_JAVA to that java executable")
PY
if [[ ! -f "$jar" ]]; then
  curl --fail --location --retry 2 \
    https://github.com/kestra-io/kestra/releases/download/v2.0.7/kestra-2.0.7 \
    --output "$jar"
fi
python3 - "$jar" <<'PY'
import hashlib
from pathlib import Path
import sys
expected = "a70c858d4d8aabe9532c92ea5c5c6982b77312286b846adc47e6c7eb969326b3"
actual = hashlib.sha256(Path(sys.argv[1]).read_bytes()).hexdigest()
if actual != expected:
    raise SystemExit("Kestra 2.0.7 checksum mismatch; remove the cached executable and retry")
PY
if [[ ! -f "$plugins/io_kestra_plugin__plugin-script-python__1_18_6.jar" ]]; then
  "$java_bin" -XX:ActiveProcessorCount=2 -jar "$jar" plugins install --locally \
    -p "$plugins" io.kestra.plugin:plugin-script-python:1.18.6
fi
