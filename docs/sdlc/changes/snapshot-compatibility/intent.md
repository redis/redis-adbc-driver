# Intent: snapshot compatibility

Date: 2026-11-02. Requestor: user. Source: 7c1a7ca. Status: implemented and locally validated; draft PR pending human review.
PR: https://github.com/redis/redis-adbc-driver/pull/284

Restore ALTER TABLE, connection-owned temporary objects, application HASH adoption,
foreign index access, HASH tools and partition production alongside mandatory
managed-table snapshots. Preserve external ownership and existing reader lifetimes.
The user chose managed copies with explicit refreshes for application data.
External capture cannot become atomic with application writes merely by being
queried through this driver; published managed copies have atomic snapshots.

See [spec](spec.md) and [plan](plan.md). No migration of old catalogs is requested.
