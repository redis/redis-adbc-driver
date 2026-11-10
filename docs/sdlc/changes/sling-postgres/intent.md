# Intent: Sling PostgreSQL round trip

Date: 2026-11-10. Source: `85509485dbc07b2e2e0ea9e1b8e08bd11a290587`.
Requestor: user in this Codex chat. Author: Codex. Status: locally verified.
[Specification](spec.md), [plan](plan.md), [validation](validation.md).

Test Sling moving data into and out of PostgreSQL with the Redis ADBC driver.
Deliver reproducible bidirectional tests, a working configuration, and explicit
compatibility gaps. The user subsequently requested upstream publication and
listing Sling as an integration; results were posted in
[Sling issue #836](https://github.com/slingdata-io/sling-cli/issues/836), and the
README, guide index, installation inventory, support comparison and standalone
test registry now include Sling. Driver behavior, benchmark claims, CDC, upserts
and deployment remain outside scope.
