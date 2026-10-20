# Intent: SQL and Arrow access to Redis data

Date: 2026-10-20. Status: **retrospective draft for maintainer review**.
Source: local commit `6f42461`; [evidence](context.md).
Requestor: this extraction was requested by the repository user; original
product originator and approving owner are not established by this checkout.
Next record: [spec.md](spec.md).

## Problem

Redis HASH data and its Search indexes need a usable path into SQL/Arrow data
workflows. Consumers otherwise need application-specific reads, conversions and
query logic. This problem statement is inferred from the implemented driver,
CLI and examples; it is not a recovered customer interview.

## Desired outcome

Applications query Redis through ADBC, receive typed Arrow results and ingest
Arrow batches. Data engineers can assess existing HASH collections, copy them
into managed tables or adopt eligible collections in place. Query acceleration
must preserve result correctness and fall back when Search cannot represent
the SQL semantics exactly.

## Affected users and systems

- Arrow/ADBC application developers and data engineers using the driver or CLI.
- Redis Software, Redis OSS and documented Redis Cloud RAM deployments with
  Search; standalone, proxy and OSS Cluster API connection paths.
- Maintainers of the Go driver, Python validation suite, comparison harness and
  binary distributions. Tool-specific integration remains separately evidenced.

## Constraints and exclusions

- Preserve physical storage ownership, metadata slot locality and recovery from
  abandoned operations. Use disposable databases for destructive test fixtures.
- Keep SQL/Arrow types, NULLs, exact numeric behavior and error states explicit.
- Support bounded streaming and worker use, cancellation and cleanup.
- Flex, Valkey/Redis 7.2 derivatives, full PostgreSQL conformance, general
  transactions and untested ecosystem connectors are outside current claims.
- Do not infer universal speedups from a single host, dataset or worker policy.

## Success and evidence

Success means documented supported behavior passes focused Go tests and ADBC
validation on relevant Redis topologies, with comparison differences visible
and complete fetched output checked for performance changes. Public usage,
options and releases must match the implementation. [Validation](validation.md)
defines reproducible commands; [context](context.md) records dated baselines.
New product targets and performance thresholds need change-specific agreement.

## Open decisions

Who owns product scope and release acceptance? Which PostgreSQL gaps and tool
integrations should be prioritized next? What resource/latency targets apply to
real deployments? Does the team want to complete the existing autotuning matrix?
Carry these questions into a scoped change rather than inventing answers.
