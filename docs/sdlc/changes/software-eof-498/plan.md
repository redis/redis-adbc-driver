# Plan

2026-10-22. Base: `590ab0a`. Status: diagnostic tooling and evidence complete;
termination attribution and a demonstrated correction remain unresolved.

1. Read #498 and prior evidence; preserve its unresolved attribution.
2. Provision a dedicated Software 8.6.2 three-shard fixture through the Makefile;
   verify active QPF 4x and effective workers/routing.
3. Repeat NaN join/text tests sequentially through host forwarding and container
   loopback. Compare an additional host runtime/address control.
4. Add and run a reusable comparison script, retaining private raw output and a
   publishable summary. Review failure propagation and fixture ownership. After
   locating first-reply EOFs, extend the standalone read-only client control past
   the previously passing 5,000-connection workload and classify its failures.
5. Record actual results and remaining attribution limits, check links/diff,
   and remove only the owned fixture. Keep #498 open pending a demonstrated
   cause and correction.

See [intent](intent.md), [spec](spec.md), [evidence](evidence.md).
