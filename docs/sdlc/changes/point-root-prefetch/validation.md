# Validation

Date: November 14, 2026. Author: Codex. Status: pending execution.
[Intent](intent.md), [specification](spec.md), [plan](plan.md).

Source and regression tests are authored. No builds, Redis commands or timings
have run yet: other agents hold the root-controlled live/build slot. The existing
PR #439 archived measurements remain unchanged and are not measurements of this
prototype. No performance gain or Software compatibility is claimed yet.

Source cross-review found a combined expired-lease/malformed-prefetch precedence
case. A pure local interpretation of a successful lease reply now occurs before
new root decoding, while unaccounted pipeline failures retain their original
precedence and lease digest repair waits for every new outcome. Regression cases
cover those combinations; execution is pending.

## Source follow-up after initial timing

A separate eligibility follow-up skips `pin.reader` and restricted stores, so
known restricted readers do not probe commands outside their intended ACL. This
adds no capability cache or cross-statement permission reuse. The initial native
ABBA windows measured clean `762d4ef2`; their sources remain unchanged and are not
relabeled as this later guard-source measurement. Those native fixtures used
ordinary privileged driver connections, for which eligibility is unchanged.
