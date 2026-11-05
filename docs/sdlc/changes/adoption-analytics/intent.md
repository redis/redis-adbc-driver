# Intent

Date: 2026-10-29. Source: public main `820d5af`. Requestor: repository user.
Status: implemented and locally verified; no release or deployment requested.
[Specification](spec.md), [plan](plan.md).

Understand companies and teams adopting the driver, download frequency, query
quantity/types, retention and reliability. Use product usage feedback to understand user usage and guide product
improvement. Build opt-out aggregate runtime
collection, a self-hosted collector/dashboard and download snapshots. Preserve
query behavior, Arrow ownership, cancellation and customer data confidentiality.
Company labels are self-reported; company inference and verified customer account
mapping require external identity sources and are outside this implementation.
Collection defaults on; users can disable it explicitly. No raw SQL collection,
remote provisioning or publishing.

Updated 2026-11-05 at the request of Albert Wong in [PR #244](https://github.com/redis/redis-adbc-driver/pull/244).
