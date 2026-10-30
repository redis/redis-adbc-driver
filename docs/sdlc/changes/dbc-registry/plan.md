# Plan and evidence: dbc registry preparation

Date: 2026-10-29. Source: `95d6583`. Requestor: repository user.
Status: implemented locally; public registry publication pending.

1. Read dbc registry, manifest and archive contracts. Complete.
2. Add package builder, tests, release wiring and catalog proposal. Complete.
3. Document local installation and maintainer submission. Complete.
4. Publish packages and merge the catalog via registry maintainers. Pending
   onboarding, package publication and signing arrangements. The official
   Foundry guide identifies https://github.com/orgs/adbc-drivers/discussions
   as a submission contact, and says administrators check release packages
   and upload them to the dbc CDN. The submission draft now uses this route.
   Existing onboarding was subsequently found in discussion #9:
   https://github.com/orgs/adbc-drivers/discussions/9 . On October 26 the
   maintainer acknowledged the Redis-owned repository and said setup was
   pending their availability. No duplicate discussion or follow-up was posted.

Verification: `python3 -m unittest discover -s scripts -p test_dbc_package.py -v`
passed all 3 tests. `git diff --check` passed. dbc v0.3.1 installed a generated
package renamed redis.tar.gz into an isolated temporary ADBC_DRIVER_PATH;
`dbc list --json` reported redis 0.0.18 and redis.toml had the expected
entrypoint. This used a fixture library: no native loading, Redis query or
remote CI/release run was performed. Live registry search was unavailable due
to DNS restrictions; no claim is made that an existing Redis entry was ruled
out by a current registry download.

Review: checked the archive contract, actual available release asset names,
platform scope, missing signatures, local filename identity, and preservation
of existing release assets. No separate human review occurred.

See [intent](intent.md), [spec](spec.md), and [submission](../../../dbc.md).
