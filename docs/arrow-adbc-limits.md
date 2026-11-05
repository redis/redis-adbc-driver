# Arrow and ADBC extensions and remaining limits

The scoped extensions below build on the [supported Arrow and ADBC contracts](../README.md#apache-arrow-feature-matrix).
Trackers #300–#306 are closed as completed; their change records document
validation and remaining boundaries. Map and Union
have explicit stored contracts and partition limits described in the
[Arrow guide](arrow-interoperability.md#stored-map-and-union-contract).

| Follow-up capability | Implemented scope and remaining boundary | Tracker |
|---|---|---|
| Incremental execution for more SQL plans | Bounded unordered DISTINCT is incremental with `stream_required`; general HASH discovery and other blocking operators keep documented fallback/rejection. | [#300](https://github.com/redis/redis-adbc-driver/issues/300) |
| Dictionary-encoded IPC files | Bounded file-wide dictionary construction/remapping is implemented; per-batch dictionaries remain available for IPC streams. | [#301](https://github.com/redis/redis-adbc-driver/issues/301) |
| Explicit result layouts in partitioned reads | Non-dictionary layouts replay through versioned live/retained descriptors; dictionary partition output remains rejected. | [#302](https://github.com/redis/redis-adbc-driver/issues/302) |
| Column-level statistics | Authoritative bounded NULL counts join row counts; distinct/min/max/width/size remain omitted. | [#303](https://github.com/redis/redis-adbc-driver/issues/303) |
| Broader partition SQL coverage | Live byte-length functions expand the deterministic subset; regex SUBSTRING, recursion, generators and lateral/table functions remain excluded. | [#304](https://github.com/redis/redis-adbc-driver/issues/304) |
| Nested extensions and additional explicit mappings | UUID/JSON normalization extends through bounded STRUCT nesting; unknown and other nested extension containers reject. | [#305](https://github.com/redis/redis-adbc-driver/issues/305) |
| Cancellation during driver-side sorts | Query sorting checks every 256 comparisons and aborts before result publication. | [#306](https://github.com/redis/redis-adbc-driver/issues/306) |

Transaction and constraint gaps remain in [#113](https://github.com/redis/redis-adbc-driver/issues/113)
and [#114](https://github.com/redis/redis-adbc-driver/issues/114). No shared source
snapshot, remote server-work termination or rollback guarantee is added by the
support labels. Experimental GPU/device expansions have their own open trackers
in the [feature matrix](../README.md#apache-arrow-feature-matrix).
