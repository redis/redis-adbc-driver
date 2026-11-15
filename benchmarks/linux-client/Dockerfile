# Native Linux benchmark client; Redis Software runs separately.
FROM python:3.14-slim-bookworm AS python-runtime
FROM golang:1.27.3-bookworm
COPY --from=python-runtime /usr/local/ /usr/local/
COPY requirements.txt /tmp/benchmark-requirements.txt
RUN python3 -m venv /opt/benchmark-venv \
    && /opt/benchmark-venv/bin/pip install --no-cache-dir -r /tmp/benchmark-requirements.txt
ENV PATH="/opt/benchmark-venv/bin:/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin"
