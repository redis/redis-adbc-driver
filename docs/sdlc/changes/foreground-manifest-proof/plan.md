# Manifest proof implementation plan

See [intent](intent.md) and [spec](spec.md).

1. Preserve the clean baseline native library and source identity; add an opt-in
   fixed-write profile using existing bounded command-category hooks.
2. Run controlled baseline attribution on verified Software QPF4/TLS/noeviction.
3. Add proof-corruption regressions and pipeline the unchanged native reads.
4. Run focused metadata/publication/conflict/Arrow/permission checks and snapshot
   races appropriate to the changed admission path.
5. Build the candidate with `make -C go build`, then compare independent native
   processes in both orders, including no-op/small writes and unrelated catalogs.
6. Review, publish evidence and create a focused PR against #336. Keep partial
   attribution results, negative measurements and remaining work explicit.

Local implementation, complete snapshot races, actual RBAC, native validation and
the matched comparison are complete. See [validation](validation.md) for every
source boundary, skip, initial candidate error and retained negative batch result.
PR #346 is open; hosted checks/review remain pending. No further optimization
infrastructure or operation-resume protocol is included.
