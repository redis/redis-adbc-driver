"""Apply Redis's private CA only after uv finishes resolving dependencies."""

import os
import sys

if os.environ.get("REDIS_ACCEPTANCE_CA_FILE"):
    os.environ["SSL_CERT_FILE"] = os.environ["REDIS_ACCEPTANCE_CA_FILE"]
# Go shared libraries snapshot the process environment. Start a fresh test
# interpreter with the CA already present, rather than changing it after start.
os.execv(sys.executable, [sys.executable, *sys.argv[1:]])
