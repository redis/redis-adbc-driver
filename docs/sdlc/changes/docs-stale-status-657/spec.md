# Specification

Date: 2026-11-24. Source: `0cde7e38`. Status: implemented.
[Intent](intent.md), [plan](plan.md), [validation](validation.md).

## Contract

- `integration_registry.UNSUPPORTED_GUIDES["superset"]` is the generated
  `superset moved to <MOVED_GUIDES["superset"]>` message. `integration_registry.py`,
  `check_integration_guide.py` and `run-integration-guides.py` still exit 2 for
  `superset`, now with that message and no #471 link.
- Superset pages keep the true statement that Superset 6.1 is unsupported on
  Python 3.14+, point to the moved guide, and say #471 is closed with no Superset
  7.x run recorded in this repository. The engine-spec description in
  `driver-reference.md` is unchanged.
- Closed trackers (#118, #170, #273, #329, #345, #361, #599) are described as
  closed. Where open work remains, the open issue is named (#648 for the SF1
  third-path rerun; #93, #98, #107, #110, #113, #114, #116, #360, #362, #363 for
  PostgreSQL 18). Where none exists (#170's partition gaps), the text says so.
- Submitted upstream reports are linked with their state on the check date:
  RediSearch/RediSearch#12337, docker/desktop-feedback#854, redis/go-redis#4324,
  apache/airflow#76273.
- `upstream-submissions/status.md` reflects a re-check of all eighteen PRs.

## Acceptance

`python3 -m unittest scripts/test_integration_registry.py` passes, and its
Superset test failed before the registry change. The strict MkDocs build and
`scripts/check_docs.py` pass. Relative links in changed Markdown resolve. Every
stated issue/PR state matches GitHub on 2026-11-24.
