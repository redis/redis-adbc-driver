# Native client wrapper intent

Date: 2026-11-11. Requestor: alberttwong. Author: Codex.
Status: initial implementation for review.
Base source: `61043e1ae12a0c9331b8e4ce14c3d6e05196b723`.
Issue: https://github.com/redis/redis-adbc-driver/issues/385.

Expose familiar managed HASH reads without SQL translation or a proxy service.
Reuse the driver's native snapshot authority and visibility implementation.
Deliver one usable Go reader and Python binding first. Exclude application-key
mapping, Search, write methods, and full Redis client compatibility.

See [spec](spec.md), [plan](plan.md), and [verification](verification.md).
