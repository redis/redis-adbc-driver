# Intent

Date: 2026-11-19. Status: implemented, integration verification in progress.
Requestor: repository user. Source baseline: `3e45955b`.
Issues: [#538](https://github.com/redis/redis-adbc-driver/issues/538),
[#539](https://github.com/redis/redis-adbc-driver/issues/539),
[#540](https://github.com/redis/redis-adbc-driver/issues/540),
[#541](https://github.com/redis/redis-adbc-driver/issues/541).

ADBC clients need a real second catalog for connection selection, metadata and
Arrow creation/replacement. Logical schemas masquerading as catalogs cannot
satisfy isolation. Preserve existing default-catalog storage while adding a
persisted catalog lifecycle and independent catalog/schema/table identity.

See [contract](spec.md) and [implementation/validation](plan.md).
