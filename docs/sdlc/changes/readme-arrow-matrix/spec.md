# Specification: README and Arrow feature matrix

Date: 2026-10-22. Status: implemented. Author: Codex.

- Move the requested sections to `docs/driver-reference.md`, together with the
  related PostgreSQL catalog reference. Preserve their content and historical
  server/settings labels, adjusting relative links for the new location.
- Retain compatibility history, integration links and the quick start in README.
- Add a matrix with green supported, yellow partial/restricted, and red
  unsupported status labels. Distinguish Arrow interchange from ADBC methods,
  type conversion limits, finite streaming, and separate Flight deployments.
- Base implementation claims on current Go source and existing feature guides;
  do not imply fresh runtime validation or availability in every release wheel.
- Update internal links to moved sections and check local destinations/anchors.

No driver behavior, benchmark or server configuration changes are in scope.
See the [intent](intent.md) and [verification plan](plan.md).
