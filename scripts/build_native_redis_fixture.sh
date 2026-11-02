#!/usr/bin/env bash
# Build the disposable macOS fixture server from the verified Redis release.
# Search is loaded separately from Homebrew's matching Redis release.
set -euo pipefail
build_root=$1
openssl_prefix=$2
# Use exactly the release providing the installed Search module. Accept only
# the official release URL and a SHA-256 from Homebrew's formula metadata.
read -r version archive_sha archive_url < <(python3 - "$3" <<'PYINFO'
import json, re, sys
formula = json.load(open(sys.argv[1]))['formulae'][0]
version = formula['versions']['stable']
source = formula['urls']['stable']
assert re.fullmatch(r'[0-9]+\.[0-9]+\.[0-9]+', version)
assert source['url'] == f'https://download.redis.io/releases/redis-{version}.tar.gz'
assert re.fullmatch(r'[0-9a-f]{64}', source['checksum'])
print(version, source['checksum'], source['url'])
PYINFO
)
mkdir -p "$build_root"
curl --fail --location --retry 3 "$archive_url" -o "$build_root/redis.tar.gz"
printf '%s  %s\n' "$archive_sha" "$build_root/redis.tar.gz" | shasum -a 256 -c -
tar xzf "$build_root/redis.tar.gz" -C "$build_root"
make -C "$build_root/redis-$version" -j3 BUILD_TLS=yes MALLOC=libc \
  CFLAGS="-I$openssl_prefix/include" LDFLAGS="-L$openssl_prefix/lib" redis-server redis-cli
