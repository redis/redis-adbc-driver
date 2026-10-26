#!/usr/bin/env bash
# Create private, short-lived TLS fixture material; never install global roots.
# Usage: create_partition_tls.sh NEW_DIRECTORY HOST_PORT
set -euo pipefail
umask 077
fixture_dir=$1
fixture_port=$2
mkdir "$fixture_dir"
fixture_dir=$(cd "$fixture_dir" && pwd)
cat > "$fixture_dir/ca.conf" <<'EOF'
[req]
distinguished_name=dn
x509_extensions=ca
prompt=no
[dn]
CN=Redis partition CI CA
[ca]
basicConstraints=critical,CA:TRUE
keyUsage=critical,keyCertSign,cRLSign
subjectKeyIdentifier=hash
authorityKeyIdentifier=keyid:always
EOF
openssl req -x509 -newkey rsa:2048 -nodes -days 1 \
  -keyout "$fixture_dir/ca.key" -out "$fixture_dir/ca.crt" \
  -config "$fixture_dir/ca.conf" >/dev/null 2>&1
openssl req -newkey rsa:2048 -nodes -keyout "$fixture_dir/server.key" \
  -out "$fixture_dir/server.csr" -subj '/CN=localhost' >/dev/null 2>&1
printf '%s\n' 'subjectAltName=DNS:localhost,IP:127.0.0.1' 'extendedKeyUsage=serverAuth' \
  'basicConstraints=critical,CA:FALSE' 'keyUsage=critical,digitalSignature,keyEncipherment' \
  'subjectKeyIdentifier=hash' 'authorityKeyIdentifier=keyid:always' > "$fixture_dir/server.ext"
openssl x509 -req -in "$fixture_dir/server.csr" -CA "$fixture_dir/ca.crt" \
  -CAkey "$fixture_dir/ca.key" -CAcreateserial -out "$fixture_dir/server.crt" \
  -days 1 -extfile "$fixture_dir/server.ext" >/dev/null 2>&1
openssl req -x509 -newkey rsa:2048 -nodes -days 1 \
  -keyout "$fixture_dir/untrusted.key" -out "$fixture_dir/untrusted.crt" \
  -config "$fixture_dir/ca.conf" >/dev/null 2>&1
# A separately signed leaf proves native workers enforce hostnames as well as CA trust.
openssl req -newkey rsa:2048 -nodes -keyout "$fixture_dir/wrong-host.key" \
  -out "$fixture_dir/wrong-host.csr" -subj '/CN=not-the-fixture.invalid' >/dev/null 2>&1
printf '%s\n' 'subjectAltName=DNS:not-the-fixture.invalid' 'extendedKeyUsage=serverAuth' \
  'basicConstraints=critical,CA:FALSE' 'keyUsage=critical,digitalSignature,keyEncipherment' \
  > "$fixture_dir/wrong-host.ext"
openssl x509 -req -in "$fixture_dir/wrong-host.csr" -CA "$fixture_dir/ca.crt" \
  -CAkey "$fixture_dir/ca.key" -CAcreateserial -out "$fixture_dir/wrong-host.crt" \
  -days 1 -extfile "$fixture_dir/wrong-host.ext" >/dev/null 2>&1
fixture_password=$(openssl rand -hex 24)
printf '%s\n' "$fixture_password" > "$fixture_dir/admin.password"
printf 'rediss://default:%s@localhost:%s/0\n' "$fixture_password" "$fixture_port" > "$fixture_dir/admin.uri"
cat > "$fixture_dir/redis.conf" <<EOF
port 0
tls-port 6379
tls-cert-file /tls/server.crt
tls-key-file /tls/server.key
tls-ca-cert-file /tls/ca.crt
tls-auth-clients no
requirepass $fixture_password
save ""
appendonly no
EOF
