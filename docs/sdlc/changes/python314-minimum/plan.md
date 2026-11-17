# Validation and review

Date: 2026-11-17. See [intent](intent.md) and [specification](spec.md).

- Updated packaging/installer minimum, all supported registry profiles, current
  CI environments, Fivetran acceptance invocation and Kubeflow base image.
- Removed unsupported Superset 6.1 extra/profile/CI and documented blocker #471.
  Registry/guide commands explicitly explain unsupported status before execution.
- Python 3.14 offline checks: 11 registry/runner tests, 19 installer tests,
  13 acceptance-runner tests, 4 CI split-suite contracts: **47 passed**.
- Built a metadata-validation wheel: Requires-Python is >=3.14 and Provides-Extra
  excludes superset. This source wheel is not a published native platform artifact.
- Independently resolved all **13 supported** profiles for Python 3.14 /
  x86_64-manylinux_2_28. Initial all-extras resolution failed on intentionally
  incompatible isolated Arrow groups; profiles were resolved separately. This
  does not prove installability/runtime execution of every optional integration.
- Superset negative probes retained privately: requiring PyArrow wheels rejects
  <19; requiring newer PyArrow conflicts with Superset's published <19 bound.
- Diff review checks runtime labels, current docs, registry/test selector coverage,
  before-install minimum enforcement and removal of older-Python CI paths. No new
  live Redis or performance run was performed for the packaging/profile change.
  Full integration qualification remains with the normal CI/live acceptance gates.
