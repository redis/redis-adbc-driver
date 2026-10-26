# Project defaults

Read [CLAUDE.md](CLAUDE.md) for repository commands and invariants.

- Submit every GitHub issue with at least one existing repository label. When using the CLI or API, include labels in the creation request; do not submit an unlabeled issue and label it afterward.
- Develop and run Redis Software tests with **Redis Software and active, verified 4x QPF**. Use `make -C go redis-up`; verify effective configuration before tests.
- Run ClickBench only on Redis Software with verified 4x QPF, never Redis Community. For repeat query runs, restore a verified compatible full-scale backup; do not reload unless loading performance is explicitly requested.
- Do not support or test Valkey, AWS ElastiCache, or GCP Memorystore.
- Keep historical results labeled by their actual server and settings. Keep operational artifacts, infrastructure identifiers, credentials, and license files private.
