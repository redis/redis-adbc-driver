#!/bin/bash
# Run as root through SSM. Update application files without rotating secrets/data.
set -euo pipefail
commit=${1:?Usage: update.sh FULL_COMMIT_SHA}
[[ "$commit" =~ ^[a-f0-9]{40}$ ]] || { echo 'Expected a full commit SHA' >&2; exit 2; }
app=/opt/redis-adbc-analytics
systemctl start redis-adbc-backup.service
stage=$(mktemp -d /tmp/adbc-update.XXXXXX)
trap 'rm -rf "$stage"' EXIT
files='collector.py downloads.py dashboard.html dashboard.js dashboard.css backup.py restore.py requirements.txt'
for file in $files; do
    curl --fail --show-error --silent --retry 3 "https://raw.githubusercontent.com/redis/redis-adbc-driver/$commit/analytics/$file" -o "$stage/$file"
done
"$app/venv/bin/pip" install --no-cache-dir -r "$stage/requirements.txt"
"$app/venv/bin/python" -m py_compile "$stage/collector.py" "$stage/backup.py" "$stage/restore.py"
rollback="$app/releases/$(date -u +%Y%m%dT%H%M%SZ)"
install -d -m 700 "$rollback"
for file in $files; do cp "$app/$file" "$rollback/$file"; done
cp /etc/redis-adbc-analytics/collector.env "$rollback/collector.env"
export ANALYTICS_UPDATE_COMMIT="$commit"
"$app/venv/bin/python" <<'PY'
import os,re
from pathlib import Path
import redis
acl_path=Path('/etc/redis/users.acl')
acl=acl_path.read_text()
password=re.search(r'^user admin on >(\S+)',acl,re.M).group(1)
lines=acl.splitlines()
for i,line in enumerate(lines):
    if line.startswith('user collector '):
        for command in ['+hget','+hsetnx','+hdel','+zrange','+zrem']:
            if command not in line.split(): line+=' '+command
        lines[i]=line
acl_path.write_text('\n'.join(lines)+'\n')
client=redis.Redis(host='127.0.0.1',username='admin',password=password,protocol=3)
client.acl_load();client.close()
env_path=Path('/etc/redis-adbc-analytics/collector.env')
values=dict(line.split('=',1) for line in env_path.read_text().splitlines())
values['ANALYTICS_TRUST_PROXY']='1'
values['ANALYTICS_SOURCE_COMMIT']=os.environ['ANALYTICS_UPDATE_COMMIT']
env_path.write_text(''.join(f'{key}={value}\n' for key,value in values.items()))
nginx=Path('/etc/nginx/sites-available/adbc-analytics')
config=nginx.read_text()
if 'proxy_set_header X-Real-IP' not in config:
    config=config.replace('        include /etc/nginx/adbc-write-token.conf;',
                          '        proxy_set_header X-Real-IP $remote_addr;\n        include /etc/nginx/adbc-write-token.conf;')
nginx.write_text(config)
PY
nginx -t
for file in $files; do
    install -m 644 "$stage/$file" "$app/$file.new"
    mv "$app/$file.new" "$app/$file"
done
systemctl restart redis-adbc-analytics
systemctl reload nginx
ready=false
for attempt in $(seq 1 20); do
    if curl --fail --silent --show-error http://127.0.0.1:8787/health; then ready=true; break; fi
    sleep 1
done
if [ "$ready" != true ]; then
    for file in $files; do cp "$rollback/$file" "$app/$file"; done
    cp "$rollback/collector.env" /etc/redis-adbc-analytics/collector.env
    systemctl restart redis-adbc-analytics
    echo 'Application health failed; restored the previous application and environment.' >&2
    exit 1
fi
printf '\nUpdated application to %s; rollback files: %s\n' "$commit" "$rollback"
