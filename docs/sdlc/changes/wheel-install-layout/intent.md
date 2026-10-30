# Intent: unblock valid wheel publication

Date: 2026-10-29. Source: `9f18518`. Discovered while executing the user's
v0.0.19 release request. See [spec](spec.md), [plan](plan.md).

The PyPI gate assumes the bundled library is at the wheel archive root.
Setuptools placed the released package under its standard `.data/purelib`
installation scheme. Those wheels already installed and passed native tests;
the publication validator rejects their archive layout before publication.
Fix the validator without rebuilding or changing the release artifacts.
