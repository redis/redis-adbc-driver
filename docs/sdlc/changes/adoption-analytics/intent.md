# Intent

Date: 2026-10-29. Source: public main `820d5af`. Requestor: repository user.
Status: implemented, verified and deployed to AWS through Terraform on 2026-11-05;
no release published.
[Specification](spec.md), [plan](plan.md).

Understand companies and teams adopting the driver, download frequency, query
quantity/types, retention and reliability. Use product usage feedback to understand user usage and guide product
improvement. Build opt-out aggregate runtime
collection, a self-hosted collector/dashboard and download snapshots. Preserve
query behavior, Arrow ownership, cancellation and customer data confidentiality.
Company labels are self-reported; company inference and verified customer account
mapping require external identity sources and are outside this implementation.
Collection defaults on; users can disable it explicitly. No raw SQL collection,
release publishing. The user requested a new AWS HTTPS collector with Route 53,
Redis storage and S3 backups, provisioned through Terraform. The intermediate
RDS request was canceled before any resources were created.

Updated 2026-11-05 at the request of Albert Wong in [PR #244](https://github.com/redis/redis-adbc-driver/pull/244).


Measurement clarity follow-up (2026-11-05, requested in PR #244): distinguish known
inapplicable counters from unavailable counters, preserve measured zero, and show
per-execution explanations without inferring unreported execution details. Keep
raw SQL and Substrait plans out of telemetry; plans describe structure rather
than actual runtime work and may contain identifiers/literals.

Dashboard navigation follow-up (2026-11-05): add per-table search, AND column
filters, ascending/descending sorting, 10/25/50/100-row pages and reset controls.
Operate on loaded data, disclose the 200-sample query cap, and preserve text-only
rendering and authenticated read behavior. Verify measurement labels, numeric sort,
combined filtering, page reset/clamping and empty states with Node tests and live UI.
