# Intent: Airflow get_records

Date: 2026-10-29. Source: `95d6583`. Requestor: repository user in this chat.
Author: Codex. Status: implemented and locally verified; human review pending.
[Specification](spec.md), [plan](plan.md), [evidence](validation.md).

## Problem and consumers

Airflow Arrow provider 0.1.1 opens ADBC connections with autocommit disabled and
requests that mode again when `get_records()` calls `run()`. Redis rejects the
second request. Its packaged macOS library also escapes the provider's lookup.

## Outcomes and scope

Provide an explicitly imported Redis hook that fetches records using the package's
library resolver and autocommit mode. Preserve transaction rejection and inherited
Airflow result handling. Document configuration and retain regression coverage.

## Exclusions and unresolved decisions

No native transaction changes, generic provider monkeypatch, automatic Airflow
provider registration, upstream contribution, publication or release is included.
