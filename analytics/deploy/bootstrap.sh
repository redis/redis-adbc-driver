#!/bin/bash
# EC2 bootstrap; secrets are generated on-host and never enter Terraform state.
set -euo pipefail
export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y curl ca-certificates gpg lsb-release nginx certbot python3 python3-venv
curl --fail --silent --show-error https://packages.redis.io/gpg -o /tmp/redis-signing-key
gpg --dearmor --yes -o /usr/share/keyrings/redis-archive-keyring.gpg /tmp/redis-signing-key
echo "deb [signed-by=/usr/share/keyrings/redis-archive-keyring.gpg] https://packages.redis.io/deb $(lsb_release -cs) main" > /etc/apt/sources.list.d/redis.list
apt-get update
apt-get install -y redis-server
systemctl stop redis-server
id adbc-analytics >/dev/null 2>&1 || useradd --system --home /opt/redis-adbc-analytics --shell /usr/sbin/nologin adbc-analytics
install -d -m 755 /opt/redis-adbc-analytics /etc/redis-adbc-analytics
for file in collector.py downloads.py dashboard.html dashboard.js dashboard.css backup.py restore.py requirements.txt; do
    curl --fail --show-error --silent --retry 5 "https://raw.githubusercontent.com/redis/redis-adbc-driver/$ANALYTICS_SOURCE_COMMIT/analytics/$file" -o "/opt/redis-adbc-analytics/$file"
done
python3 -m venv /opt/redis-adbc-analytics/venv
/opt/redis-adbc-analytics/venv/bin/pip install --no-cache-dir -r /opt/redis-adbc-analytics/requirements.txt
python3 <<'PY'
import os, secrets
from pathlib import Path
password, backup, admin = [secrets.token_hex(32) for _ in range(3)]
write, read = [secrets.token_hex(32) for _ in range(2)]
env = dict(ANALYTICS_REDIS_URL=f'redis://collector:{password}@127.0.0.1:6379/0',
           ANALYTICS_REDIS_BACKUP_PASSWORD=backup, ANALYTICS_WRITE_TOKEN=write, ANALYTICS_READ_TOKEN=read,
           ANALYTICS_BACKUP_BUCKET=os.environ['ANALYTICS_BACKUP_BUCKET'],
           ANALYTICS_SOURCE_COMMIT=os.environ['ANALYTICS_SOURCE_COMMIT'], AWS_DEFAULT_REGION=os.environ['AWS_DEFAULT_REGION'], ANALYTICS_TRUST_PROXY='1')
Path('/etc/redis-adbc-analytics/collector.env').write_text(''.join(f'{k}={v}\n' for k,v in env.items()))
Path('/etc/redis/redis.conf').write_text('''bind 127.0.0.1 ::1
protected-mode yes
port 6379
daemonize no
supervised systemd
dir /var/lib/redis
logfile /var/log/redis/redis-server.log
appendonly yes
appendfsync everysec
save 900 1
maxmemory 512mb
maxmemory-policy noeviction
aclfile /etc/redis/users.acl
''')
Path('/etc/redis/users.acl').write_text(
    'user default off\n'
    f'user admin on >{admin} ~* &* +@all\n'
    f'user collector on >{password} ~adbc:{{analytics}}:* +ping +hello +select +get +set +watch +unwatch +multi +exec +discard +xadd +xrange +xrevrange +sadd +sscan +hset +hget +hsetnx +hdel +hmget +zadd +zrem +zrange +zrevrange +client|setinfo\n'
    f'user backup on >{backup} +ping +hello +psync +replconf +sync +client|setinfo\n')
# Store the write token in a root-readable proxy snippet, never public clients.
Path('/etc/nginx/adbc-write-token.conf').write_text(f'proxy_set_header Authorization "Bearer {write}";\n')
PY
chown root:adbc-analytics /etc/redis-adbc-analytics/collector.env
chmod 640 /etc/redis-adbc-analytics/collector.env
chown redis:redis /etc/redis/users.acl
chmod 600 /etc/redis/users.acl /etc/nginx/adbc-write-token.conf
cat > /etc/systemd/system/redis-adbc-analytics.service <<'UNIT'
[Unit]
Description=Redis ADBC product usage collector
After=network-online.target redis-server.service
Requires=redis-server.service
[Service]
User=adbc-analytics
Group=adbc-analytics
EnvironmentFile=/etc/redis-adbc-analytics/collector.env
ExecStart=/opt/redis-adbc-analytics/venv/bin/python /opt/redis-adbc-analytics/collector.py
Restart=on-failure
RestartSec=3
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
[Install]
WantedBy=multi-user.target
UNIT
cat > /etc/systemd/system/redis-adbc-backup.service <<'UNIT'
[Unit]
Description=Checked Redis RDB backup to private S3
After=network-online.target redis-server.service
[Service]
Type=oneshot
EnvironmentFile=/etc/redis-adbc-analytics/collector.env
ExecStart=/opt/redis-adbc-analytics/venv/bin/python /opt/redis-adbc-analytics/backup.py
TimeoutStartSec=900
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
UNIT
cat > /etc/systemd/system/redis-adbc-backup.timer <<'UNIT'
[Unit]
Description=Hourly Redis ADBC S3 backup
[Timer]
OnBootSec=10min
OnCalendar=hourly
Persistent=true
RandomizedDelaySec=120
[Install]
WantedBy=timers.target
UNIT
systemctl daemon-reload
systemctl enable --now redis-server redis-adbc-analytics redis-adbc-backup.timer
# Expose only ACME while TLS is being configured; no plaintext usage ingestion.
rm -f /etc/nginx/sites-enabled/default
cat > /etc/nginx/sites-available/adbc-analytics <<NGINX
server {
    listen 80;
    server_name $ANALYTICS_HOSTNAME;
    access_log off;
    error_log /dev/null;
    location /.well-known/acme-challenge/ { root /var/www/html; }
    location / { return 503; }
}
NGINX
ln -sf /etc/nginx/sites-available/adbc-analytics /etc/nginx/sites-enabled/adbc-analytics
nginx -t
systemctl reload nginx
for attempt in $(seq 1 20); do
    if certbot certonly --webroot --webroot-path /var/www/html --non-interactive --agree-tos --register-unsafely-without-email -d "$ANALYTICS_HOSTNAME"; then break; fi
    sleep 30
done
test -s "/etc/letsencrypt/live/$ANALYTICS_HOSTNAME/fullchain.pem"
cat > /etc/nginx/conf.d/adbc-limits.conf <<'NGINX'
limit_req_zone $binary_remote_addr zone=adbc_ingest:1m rate=60r/m;
limit_conn_zone $binary_remote_addr zone=adbc_connections:1m;
NGINX
cat > /etc/nginx/sites-available/adbc-analytics <<NGINX
server {
    listen 80;
    server_name $ANALYTICS_HOSTNAME;
    access_log off;
    error_log /dev/null;
    location /.well-known/acme-challenge/ { root /var/www/html; }
    location / { return 308 https://$ANALYTICS_HOSTNAME\$request_uri; }
}
server {
    listen 443 ssl;
    server_name $ANALYTICS_HOSTNAME;
    ssl_certificate /etc/letsencrypt/live/$ANALYTICS_HOSTNAME/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/$ANALYTICS_HOSTNAME/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;
    access_log off;
    error_log /dev/null;
    client_max_body_size 64k;
    client_body_buffer_size 64k;
    client_body_timeout 5s;
    client_header_timeout 5s;
    keepalive_timeout 10s;
    limit_conn adbc_connections 8;
    proxy_connect_timeout 2s;
    proxy_read_timeout 10s;
    location = /v1/usage {
        limit_req zone=adbc_ingest burst=10 nodelay;
        limit_req_status 429;
        limit_except POST { deny all; }
        proxy_set_header X-Real-IP \$remote_addr;
        include /etc/nginx/adbc-write-token.conf;
        proxy_pass http://127.0.0.1:8787;
    }
    location / {
        proxy_pass http://127.0.0.1:8787;
    }
}
NGINX
nginx -t
systemctl reload nginx
install -d /etc/letsencrypt/renewal-hooks/deploy
printf '#!/bin/sh\nsystemctl reload nginx\n' > /etc/letsencrypt/renewal-hooks/deploy/reload-nginx
chmod 755 /etc/letsencrypt/renewal-hooks/deploy/reload-nginx
systemctl enable --now certbot.timer
systemctl start redis-adbc-backup.service
printf 'Collector and first S3 backup ready: https://%s/health\n' "$ANALYTICS_HOSTNAME"
