# Plan

Date: 2026-11-24. Source: `ea16062cf9d997b83137930acfa12842db814ac8`.
Status: steps 1–6 complete; step 7 waits for the official release.
[Intent](intent.md), [specification](spec.md), [validation](validation.md).

1. Read the upstream fixes on the `v1.6.8` branch (`a89addef`, `c525e3e0`) and
   the driver's `GetObjects` column metadata.
2. Rewrite `scripts/test_sling_postgres.py` for a stock connection and the cases
   in the specification; check its outcome recording offline with stub commands.
3. Per the user's request, build Sling from the `v1.6.8` branch head and run the
   harness on a dedicated verified Software 4x QPF fixture.
4. Narrow the empty-source cases after the first run showed Sling's documented
   empty-stream default, and report the source build's missing `conns discover`
   as unavailable.
5. Update `docs/integrations/sling.md`, the Python integration summary, the guide
   index, the installation, support and acceptance inventories; remove
   `scripts/sling/adbc.yaml`.
6. Run documentation, inventory and registry checks.
7. After Sling 1.6.8 is released, rerun the harness with the official binary
   (which includes `conns discover`), update the tested-version line, binary
   checksum and this record, then mark the PR ready.

No driver change was needed. No upstream comment was posted: nothing failed on
the Redis side.
