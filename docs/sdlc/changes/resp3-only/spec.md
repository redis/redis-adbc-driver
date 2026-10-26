# RESP3-only specification

Date: 2026-10-22. Source: `6508626`. Status: implemented locally.

- Omitted protocol uses RESP3. The only accepted explicit value is `protocol=3`;
  other values and repeated protocol parameters return ADBC InvalidArgument.
- Each driver connection verifies `HELLO 3` and a reply reporting protocol 3.
  Reject go-redis's silent RESP2 fallback for single endpoints and cluster nodes,
  including connections recreated after timeout changes.
- The additional connection verification adds one handshake round trip. It does
  not repeat credentials or client naming. A server that cannot negotiate RESP3
  is unsupported.
- Remove current RESP2 support claims and the wide-transfer runner's RESP2
  choice. Preserve historical evidence and internal reply-shape parsers; list
  containers are also used within RESP3 and do not imply protocol support.

Acceptance: reject unsupported URI values, verify default and explicit RESP3,
reject failed or incorrect negotiation for both client types, and exercise SQL
query/metadata operations on Redis Software with verified 4x QPF.

See [intent](intent.md), [plan and validation](plan.md).
