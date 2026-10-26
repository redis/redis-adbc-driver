# Intent: compatible automatic Active-Active migration

Date: 2026-10-22. Requestor: Albert Wong. Author: Codex. Status: proposed;
human review pending. Source: `5cbf91e9968f0d7796ab9c95e0f062fad9522c58`.
Refs [spec](spec.md), [plan](plan.md), [evidence](evidence.md),
[#491](https://github.com/alberttwong/redis-adbc-driver/issues/491).

Align ordinary and Active-Active storage around numeric identities, and make
migration as automatic as possible through opinionated defaults. Preserve keys,
IDs, schemas, values and compatible application SQL. Avoid manual region-number
assignment and export/re-ingestion. The request supersedes the unpublished
opaque-key prototype as the proposed direction.

Deliver a reviewable design before changing runtime behavior. Distinguish
automatic execution from a guarantee of zero downtime or unchanged distributed
semantics. Reject unsafe migrations before enabling regional writes. Do not
close #491 or describe prototype tests as migration qualification.

The requestor explicitly permits changing the driver's current architecture to
make automatic migration easier. Use the common format by default for new
ordinary databases; do not require a separately selected migration-ready profile.
Internal implementation compatibility is subordinate to migration correctness.

The requestor explicitly excludes a compatibility path. Introduce one new format
as a breaking change, support topology migration from that format only, and reject
old/mixed layouts. Do not implement a legacy adapter or old-format conversion.

The requestor specifies Redis Software with 4x QPF in Active-Active topology as
the architecture. The ordinary source is also Software/4x. Qualify multiple
independent Software clusters with effective QPF verified on each participating
instance; historical Cloud Pro tests do not satisfy this requirement.
