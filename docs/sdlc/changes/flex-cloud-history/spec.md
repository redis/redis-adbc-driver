# Historical evidence contract

See [intent](intent.md) and [verification](plan.md).

- Exclude the October 6 Cloud database without Search from Search-on-Flex results.
- Attribute the Terraform failure and resource inventory to the original PR's
  recorded attempt; claim neither a current provisioning blocker nor current
  resource state. Retain no infrastructure identifiers or credentials.
- Preserve November 4 Software results and the separate experimental stopgap.
- Describe RESP2 only as historical; the current driver requires RESP3 and
  rejects managed Flex snapshot execution before dialing.
- Require actual Search probes before any future Cloud compatibility claim,
  without claiming that Search probes establish SQL/snapshot compatibility.
- Keep the Word report explicitly historical; no binary or Terraform changes.

Acceptance: review the source PR, current driver guard and report diff; check
relative Markdown links and whitespace. No live Redis or Cloud tests required
for this documentation correction.
