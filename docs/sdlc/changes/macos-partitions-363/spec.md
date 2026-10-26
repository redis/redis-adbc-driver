# Specification

2026-10-21; #363; see [intent](intent.md), [plan](plan.md).

- The macOS arm64 release job starts an isolated native Redis 8.x process with
  an explicit Search module, not Docker or a globally installed brew service.
  Homebrew supplies Redis/Search in the ephemeral CI runner; require module
  availability and record actual server/module versions rather than assume it.
- Start on a dedicated localhost port with persistence off and all state/logs
  in a new task directory. Verify process identity before configuration or
  shutdown, and bound startup/CLI/cleanup waits. Preserve other Redis services.
- Run full Go integration with packages sequential and default aggregate cap.
  Then lower the real cap to 137 and run the full configured ADBC validation
  against the installed macOS wheel with its positive cap contract 137. Require
  all partition cases through the collection gate; record feature skips.
- Test the installed native wheel with the configured client and minimum-supported ADBC manager 1.8.0. Retain
  cross-process schema/value, lifecycle, parameters, incremental and selective
  partition tests; do not weaken assertions or skip unimplemented checks.
- Upload native version/log and validation evidence; cleanup the owned process
  even after failure. Guard new steps for older release tags lacking the helper.
- No new runtime API, published release, Cloud resources, or macOS TLS guarantee.
  #363 stays open for the remaining deployment/TLS compatibility work.
