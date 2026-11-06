# DROP SCHEMA reclamation lifecycle

The macOS job at `431dcb2` fails `TestSQLDropSchemaCascade` because two physical
rows still exist immediately after the logical drop. Snapshot storage retires
unreferenced generations through collection rather than promising synchronous
physical deletion during DDL.
[The completed macOS job](https://github.com/redis/redis-adbc-driver/actions/runs/42152094727/job/127677748093)
records the two-row assertion failure.

The test now pins the old root before CASCADE. It retains all logical-schema,
view/table absence and name-reuse assertions, explicitly collects while the old
root is pinned, and requires the pin to remain valid and both rows to remain.
After release, the existing deterministic reclamation helper requires rows,
index, row directory and ownership/reference graph fields to disappear. This
checks the snapshot lifetime and actual cleanup without racing the background
collector or replacing deletion with a weaker eventual logical check.

On an owned strict-TLS Redis Software deployment with effective 4x QPF,
noeviction and the ordinary aggregate cap, ten consecutive `-race` executions
pass, root **8.989 seconds**, no skipped cases or data races. Production behavior
is unchanged. The replacement hosted macOS run remains required.
