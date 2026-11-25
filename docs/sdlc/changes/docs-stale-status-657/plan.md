# Plan

Date: 2026-11-24. Source: `0cde7e38`. Status: complete.
[Intent](intent.md), [specification](spec.md), [validation](validation.md).

1. Re-check every issue and PR that #657 cites, plus all eighteen upstream
   documentation PRs, through `gh` on 2026-11-24.
2. Change the Superset registry test to expect the moved message; confirm it
   fails; remove the override in `python/scripts/integration_registry.py`.
3. Update the Superset references listed in #657, plus `docs/setup.md`'s tool
   table row, which also pointed readers to the "upstream dependency blocker".
4. Update the closed-tracker and upstream-report references listed in #657, and
   add the optional dated notes to `Q19-FOLLOWUP.md` and
   `foreground-manifest-proof/validation.md`.
5. Fix stale references found during the re-check that #657 did not list (see
   below).
6. Run the registry tests, strict docs build, docs checker, a relative-link check
   and `git diff --check`.

## Found during the re-check

- apache/superset#45852 closed without merge on 2026-11-21; `status.md` listed
  it as open. The status counts are now four merged, ten open, four closed.
- #273 closed on 2026-11-17 through PR #477 (worker-local Dask writes, now
  `redis_adbc_integrations.dask.write_dataframe`). `status.md` and
  `docs/integrations/example-setup.md` still said #273 tracked the gap.
- `docs/partition-validation.md:3` and `docs/retained-partition-results.md:10`
  described #170 as open or tracking. #170 closed on 2026-10-26 with no comment
  and no successor issue.
- `docs/postgresql18-compatibility.md` (header and the #359 paragraph) and the
  query profile's parent line described #118 as tracking open work.
- Query-farm/adbc_scanner#21 had already been retargeted to `v2.0` on
  2026-11-10 at the maintainer's request before it closed without merge.

## Left as history

Change records under `docs/sdlc/changes/` other than the two dated notes, and
`go/pgdiff/README.md`'s chronological #361 progress lines (its later line says
all nineteen pass), keep the state they recorded at the time.

The moved Superset guide in redis-adbc-integrations still says #471 tracks
restoration. That file is in another repository and is not changed here.
