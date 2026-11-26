#!/usr/bin/env bash
# Build an arm64 Python 3.12 Lambda zip outside the checkout.
set -euo pipefail
analytics=$(cd "$(dirname "$0")/.." && pwd)
python=${PYTHON:-python3}
output=${1:?Usage: package-serverless.sh /absolute/path/collector.zip}
case "$output" in /*) ;; *) echo 'Use an absolute output path' >&2; exit 2 ;; esac
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
"$python" -m pip install --quiet --target "$stage" --only-binary=:all: \
  --platform manylinux2014_aarch64 --implementation cp --python-version 3.12 \
  -r "$analytics/requirements.txt"
for file in collector.py serverless.py downloads.py migrate_serverless.py dashboard.html dashboard.js dashboard.css; do
  cp "$analytics/$file" "$stage/$file"
done
"$python" - "$stage" "$output" <<'PY'
from pathlib import Path
import sys
import zipfile
root, output = Path(sys.argv[1]), Path(sys.argv[2])
with zipfile.ZipFile(output, 'w', zipfile.ZIP_DEFLATED) as archive:
    for path in sorted(root.rglob('*')):
        if path.is_file() and '__pycache__' not in path.parts and path.suffix != '.pyc':
            info = zipfile.ZipInfo(path.relative_to(root).as_posix(), (2026, 1, 1, 0, 0, 0))
            info.compress_type = zipfile.ZIP_DEFLATED
            info.external_attr = 0o644 << 16
            archive.writestr(info, path.read_bytes())
print('Lambda package built.')
PY
