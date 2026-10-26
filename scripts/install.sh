#!/bin/sh
# Install manifests for an unpacked release; keep the library at this location.
set -eu
base=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
python=${PYTHON:-python3}
library=
for suffix in so dylib; do
  if [ -f "$base/libadbc_driver_redis.$suffix" ]; then
    library="$base/libadbc_driver_redis.$suffix"
    break
  fi
done
if [ -z "$library" ]; then
  echo 'No Redis shared library next to install.sh' >&2
  exit 1
fi
# The installer ships with the same manifest implementation as the wheel.
exec "$python" "$base/install_manifest.py" --library "$library" "$@"
