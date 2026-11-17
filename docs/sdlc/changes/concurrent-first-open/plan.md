# Implementation and verification plan

Date: November 17, 2026. Base: v0.0.28. Status: implemented and locally validated.
See [intent](intent.md) and [specification](spec.md).

1. Capture deterministic failing startup cases in go/snapshot_open_test.go.
2. Share the existing catalog-format validation across startup guards and
   revalidate catalog presence before the existing capability path.
3. Test current/old formats, original nonempty legacy storage refusals, and
   read-error propagation. Repeat focused tests with the Go race detector.
4. Build with the release version linker flag and run the dbt adapter suite.
5. Review ownership, ACL, unknown-reply and cluster behavior; record evidence.

All five steps are complete. See [validation and self-review](validation.md).
A corrected release and maintainer review remain outside this local fix.

Use the dedicated Redis Software fixture, verified 4x QPF and noeviction through
software-setup.sh. Never clear or reconfigure another checkout's database.
