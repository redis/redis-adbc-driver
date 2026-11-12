# Redis ADBC feature matrices

Detailed capability and limitation tables for the current source tree. Start with
the [README](../README.md) for installation and first queries. Published wheels
may differ from the source tree; check the linked feature guide for build requirements.

## Apache Arrow feature matrix

🟢 Supported · 🟡 Partial, restricted or experimental · 🔴 Unsupported · ⚪ N/A (outside driver scope)

This matrix describes the current source tree, not every published wheel. It
covers the [Apache Arrow format and interfaces](https://arrow.apache.org/docs/format/Columnar.html)
and the [ADBC API](https://arrow.apache.org/adbc/current/format/specification.html);
these are separate capabilities. Redis stores rows as HASHes; Arrow is the
client interchange format. N/A identifies responsibilities of another service or
execution engine; it does not describe a missing driver capability. Tracking links
for those rows cover scope or companion integration decisions.

Supported means the documented SQL/Arrow contract is implemented, including
normalization and explicit result-layout options where described. It does not
mean the original input encoding or metadata persists unchanged. Closed tracking
issues record completed contracts. See [extensions and remaining limits](arrow-adbc-limits.md)
for their implemented scope and boundaries. Restrictions stay in the behavior column.

| Feature | Support | Driver behavior and limits | Tracking |
|---|---|---|---|
| Columnar record batches, schemas and NULL values | 🟢 Supported | Queries return typed Arrow batches with validity information. | — |
| Arrow C Data and C Stream interfaces | 🟢 Supported | The native ADBC boundary exchanges Arrow schemas, arrays and `ArrowArrayStream` results/bindings. | — |
| Arrow bulk ingestion | 🟢 Supported | Bind a batch or stream; create, append, replace and create-or-append modes are available. Writes autocommit. | — |
| Arrow IPC stream and file formats | 🟢 Supported | Native import/export through statement options and local files; [IPC contract and examples](arrow-ipc.md). | — |
| Incremental scans, key lookups and UNION ALL | 🟢 Supported | Eligible indexed single-table queries, unordered key lookups and eligible `UNION ALL` trees fetch pages/chunks as batches are consumed. Blocking plans can materialize results; this is finite streaming. See [streamed results](driver-reference.md#streamed-results). | [#198](https://github.com/redis/redis-adbc-driver/issues/198) |
| In-process buffer interchange and ownership | 🟢 Supported | C export shares Arrow buffers under release callbacks; binding/ingest retain or copy as required for lifetime safety. [Ownership contract](arrow-interoperability.md#ownership-and-extension-policy). | [#199](https://github.com/redis/redis-adbc-driver/issues/199) |
| Redis wire zero-copy transport | 🔴 Unsupported | Redis replies require decoding/conversion; no end-to-end zero-copy storage/network path. | [#199](https://github.com/redis/redis-adbc-driver/issues/199) |
| Boolean, signed integers, floating point, UTF-8 and binary | 🟢 Supported | Standard SQL/Arrow mappings; int8 and float16 inputs widen to int16 and float32. | — |
| Unsigned integers | 🟢 Supported | uint8/16/32 widen to signed integers; full-range uint64 normalizes exactly to NUMERIC(20,0). Unsigned physical identity is not preserved. | [#200](https://github.com/redis/redis-adbc-driver/issues/200) |
| Decimal32/64/128/256 | 🟢 Supported | Inputs normalize to SQL NUMERIC up to 76 digits; explicit result layouts select Decimal32/64/128/256 when precision fits. | [#201](https://github.com/redis/redis-adbc-driver/issues/201) |
| Date, time and timestamp | 🟢 Supported | Date64 and zoned timestamps normalize to SQL DATE/UTC instants; explicit output selects Date64 or a timestamp zone. | [#202](https://github.com/redis/redis-adbc-driver/issues/202) |
| Intervals and durations | 🟢 Supported | Inputs normalize to SQL INTERVAL; explicit duration/month/day-time output rejects lossy conversions. | [#203](https://github.com/redis/redis-adbc-driver/issues/203) |
| Large/view strings and binary; fixed-size binary | 🟢 Supported | Accepted on input; explicit result layouts select large/view or fixed-width output with byte-width checks. | [#204](https://github.com/redis/redis-adbc-driver/issues/204) |
| Dictionary input and string/binary output | 🟢 Supported | Input dictionaries decode; opt-in string/binary output uses bounded per-batch int32 dictionaries. IPC file and partition restrictions apply. | [#205](https://github.com/redis/redis-adbc-driver/issues/205) |
| List and LargeList input normalization | 🟢 Supported | Recursive ragged Arrow lists and named-record elements normalize to List; physical offset widths are not retained. | [#206](https://github.com/redis/redis-adbc-driver/issues/206) |
| Named Struct ingestion and output | 🟢 Supported | Named struct ingestion/storage preserves names and nullability with normalized child types; anonymous SQL ROW expressions retain their existing restrictions. | [#207](https://github.com/redis/redis-adbc-driver/issues/207) |
| FixedSizeList, ListView/LargeListView and run-end input normalization | 🟢 Supported | Input normalizes to canonical lists/values with decoded-expansion limits; physical layout identity is not retained. See the [layout assessment](arrow-interoperability.md#layout-assessment-and-decoded-input-limits). | [#208](https://github.com/redis/redis-adbc-driver/issues/208) |
| Map | 🟢 Supported | Ordered Map storage preserves duplicate/non-null scalar keys and NULL/empty containers; native Map output, IPC and live partitions. Retained partitions reject. | [#267](https://github.com/redis/redis-adbc-driver/issues/267) |
| Dense/sparse Union | 🟢 Supported | Tagged storage preserves codes and selected-child NULLs; both inputs normalize to dense Union output. Exact-schema append, IPC and live partitions; retained partitions reject. | [#268](https://github.com/redis/redis-adbc-driver/issues/268) |
| UUID/JSON extension storage normalization | 🟢 Supported | Reject by default; explicit `extension_policy=storage` normalizes allowlisted top-level UUID/JSON extensions to canonical SQL storage. Unknown extensions reject; generic storage normalization does not persist extension identity. Native SQL JSON fields retain their owned type marker. [Policy](arrow-interoperability.md#explicit-extension-storage-normalization). | [#209](https://github.com/redis/redis-adbc-driver/issues/209) |
| SQL execution, prepared statements and Arrow parameter binding | 🟢 Supported | Supports `Prepare`, `Bind`, `BindStream`, parameter schemas and result-schema discovery for supported SQL. | — |
| ADBC metadata and row/NULL statistics | 🟢 Supported | Driver info, objects, table schemas/types and approximate managed-table row counts are available. Discovery and visible row/NULL counts share a pinned generation; results retain the approximate API classification. UNIQUE/FOREIGN KEY metadata depends on [#114](https://github.com/redis/redis-adbc-driver/issues/114). See [statistics](adbc-statistics.md). | [#210](https://github.com/redis/redis-adbc-driver/issues/210) |
| ADBC partitioned reads | 🟢 Supported | Portable row-range descriptors cover a restricted SELECT subset; retained-result mode supports broader SQL with quotas/expiry. Managed live partition sets retain one producer generation; retained capture uses a pinned source. Rebuild from source; see [partitioned reads](partitioned-reads.md). | [#211](https://github.com/redis/redis-adbc-driver/issues/211) |
| Statement cancellation | 🟢 Supported | Cancels driver work and interrupts execution-owned socket waits, including blocked cursor pages/pipelines. Remote server work may continue; sorts finish before checking cancellation. See [cancellation](driver-reference.md#cancelling-a-statement). | [#212](https://github.com/redis/redis-adbc-driver/issues/212) |
| Transactions and rollback through ADBC | 🔴 Unsupported | Autocommit only; disabling autocommit is rejected. | [#113](https://github.com/redis/redis-adbc-driver/issues/113) |
| Substrait plans | 🟡 Bounded subset | Binary Substrait 0.108.0 read/filter/project support; [subset and limits](substrait.md). | [#213](https://github.com/redis/redis-adbc-driver/issues/213) |
| Arrow Flight / Flight SQL endpoint | ⚪ N/A | A Flight endpoint belongs to a separately deployed service, such as the [Redis Flight server](https://github.com/redis/redis-apache-arrow-flight). This native ADBC driver supplies an in-process client interface. | [#214](https://github.com/redis/redis-adbc-driver/issues/214) |
| Continuous subscriptions / CDC | ⚪ N/A | Durable change feeds and snapshot-to-live handoff need a separately designed source/service contract. Finite SQL query results are the driver's current interface; [#196](https://github.com/redis/redis-adbc-driver/issues/196) evaluates where CDC should live. | [#196](https://github.com/redis/redis-adbc-driver/issues/196) |
| Arrow C Device interface | 🟡 Experimental | Native extensions stage flat numeric/boolean CPU/CUDA arrays, bind device input and export device result streams. Explicit synchronization/ownership contract; Validated on NVIDIA A10G with real producer/consumer tests. [API and limits](gpu-execution.md). | [#293](https://github.com/redis/redis-adbc-driver/issues/293), [#294](https://github.com/redis/redis-adbc-driver/issues/294), [#295](https://github.com/redis/redis-adbc-driver/issues/295) |
| GPU query execution | 🟡 Experimental | Opt-in Linux CUDA integer predicates; fetching, grouping and exact reducers remain on CPU. Restricted SQL subset, no automatic fallback or speedup claim. [Scope](gpu-execution.md). | [#291](https://github.com/redis/redis-adbc-driver/issues/291), [#292](https://github.com/redis/redis-adbc-driver/issues/292) |

See [Arrow normalization, result-layout options and ownership](arrow-interoperability.md) for the precise contracts and remaining acceptance limits.

Type mappings are implemented in [types.go](../go/types.go) and
[arrowconv.go](../go/arrowconv.go); ADBC methods in
[statement.go](../go/statement.go) and [connection.go](../go/connection.go).
Supported interchange does not imply support for every Arrow layout or every
SQL operator. See the [SQL reference](driver-reference.md#supported-sql)
for type and execution details. [Generated identifiers, UUIDs, modifying CTEs, named indexes and materialized views](sql-101-105.md) describe the additional SQL contracts and limits.

## Redis Software feature matrix

🟢 Supported through the driver · 🟡 Partial or restricted ·
🔴 Unsupported driver capability/compatibility · ⚪ N/A (outside driver scope)

These statuses describe **this driver's SQL/Arrow interface**, not whether Redis
Software offers a feature. They summarize source behavior and linked validation records. Historical evidence
retains its recorded environment and date; moving these tables did not rerun
deployment tests.
N/A identifies work owned by server administration or companion services. It is
not a compatibility failure. Red identifies a missing driver capability or an
unsupported deployment; yellow identifies limits in an applicable driver path.

| Redis Software feature | Support | Driver behavior and limits |
|---|---|---|
| Query Performance Factor (QPF) | 🟢 Supported | Search queries use the server's configured QPF. Development and tests require verified **4× QPF**; `make -C go redis-up` verifies active scaling factor 4, six Search workers, `mnp` routing and 32 connections. QPF is separate from driver query-worker settings and does not promise a 4× query speedup. [Configuration](https://redis.io/docs/latest/operate/oss_and_stack/stack-with-enterprise/search/query-performance-factor/), [driver performance](driver-reference.md#query-performance-factor-qpf). |
| RESP3 | 🟢 Required | Required for single-endpoint and Cluster API clients, including every node connection; explicit `protocol=3` is accepted. |
| RESP2 compatibility | 🔴 Unsupported | `?protocol=2` is rejected; RESP3 is required. See [URI parameters](driver-reference.md#uri-parameters). |
| Redis Query Engine / Search | 🟢 Supported | Required at connection time. The driver creates HASH indexes and uses Search for supported SQL filters, ordering and aggregation, with driver-side execution where needed. Vector and full-text SQL operations are assessed separately below. [Execution](driver-reference.md#query-execution). |
| Redis HASH tables | 🟢 Supported | Driver-managed HASH rows support SQL reads/writes, DDL and Arrow bulk ingestion. Indexes accelerate queries; HASH values remain authoritative. [Layout](driver-reference.md#rows-and-indexes). |
| Existing application HASHes and indexes | 🟡 Partial | `redis-adbc` can inspect, copy or adopt supported HASH collections; configured foreign indexes enable discovery. Layout, ownership and mutation restrictions apply. [Existing HASH collections](driver-reference.md#existing-hash-collections-redis-adbc). |
| Single endpoint / Redis Software proxy | 🟢 Supported | Connect to a supported Search-enabled RAM database through its Redis endpoint; the proxy routes database operations. |
| Sharding / OSS Cluster API | 🟢 Supported | Auto-detects the Cluster API or accepts `adbc.redis.cluster=true`; metadata shares one hash slot and table searches are coordinated across shards. This is client connectivity, not a shard-management API. [Cluster behavior](driver-reference.md#prerequisites). |
| TLS and private certificate authorities | 🟢 Supported | `rediss://` verifies server certificates; `adbc.redis.tls_ca_file` supplies explicit trust roots, including cluster nodes and independent partition workers. [TLS evidence](partitioned-reads.md#tls-and-independent-worker-validation). |
| Mutual TLS with client certificates | 🔴 Unsupported | No public driver options configure a client certificate/private key; server-certificate verification alone does not provide mutual TLS. |
| Password / ACL-user authentication | 🟢 Supported | URI credentials or username/password options authenticate Redis connections. [ACL requirements](driver-reference.md#acl-users). |
| Role-based access control (RBAC) | 🟡 Partial | Administrator-configured SQL `GRANT SELECT` / `REVOKE SELECT` update a dedicated Redis Software ACL for supported permanent managed tables. Provisioned roles and isolated reader leases are required; other privilege forms and table kinds are restricted. [Design and limits](sql-rbac.md). |
| Replication | ⚪ N/A | Redis Software manages primary/replica synchronization and replica configuration. The driver connects to the database endpoint; replica read routing is assessed separately below. [Server replication](https://redis.io/docs/latest/operate/rs/databases/durability-ha/). |
| Automatic failover (client recovery) | 🟢 Supported | Uses go-redis connection recovery and Cluster routing for supported Redis Software proxy / Cluster API connections. Applications must handle interrupted operations, as with standard Redis clients. An [owned three-node Software run](sdlc/changes/snapshot-cluster/validation.md) verifies planned primary promotion and AOF restart recovery; this does not promise transparent recovery of interrupted operations. [Retry and timeout limits](driver-reference.md#timeouts), [official client guidance](https://redis.io/docs/latest/develop/clients/go/error-handling/). |
| Read scaling through replicas | 🔴 Unsupported | `read_only=true`, `route_by_latency=true` and `route_randomly=true` are rejected because replicas can lag the driver's writes. |
| Persistence configuration (AOF / snapshots) | ⚪ N/A | Persistence policy is configured in Redis Software, outside the ADBC interface. Stored driver data follows that server policy; [retained partition control state](retained-partition-storage.md) has additional durability requirements. [Server settings](https://redis.io/docs/latest/operate/rs/databases/configure/database-persistence/). |
| Backup, restore and disaster recovery | ⚪ N/A | These are server/operator responsibilities. Restore must keep table metadata, HASHes and Search definitions consistent. [Server administration](https://redis.io/docs/latest/operate/rs/databases/configure/). |
| Active-Active geo-distribution | 🔴 Unsupported / unvalidated | No supported multi-region contract for concurrent DDL, metadata/row-ID allocation, writes or partition consistency. Redis Software supports Search on Active-Active, but indexes must be created per instance; driver compatibility cannot be inferred from command availability. The driver does not guarantee automatic rejection of an Active-Active endpoint. [Redis Active-Active Search](https://redis.io/docs/latest/operate/oss_and_stack/stack-with-enterprise/search/search-active-active/). |
| Redis Flex / Auto Tiering | 🟡 Experimental, opt-in | Tested Redis Software Flex builds support plain indexes and bounded reads with `adbc.redis.flex=true`. SQL restrictions remain; Cloud SQL is unvalidated. [Scope and evidence](redis-flex-stopgap.md). |
| SQL JSON / JSONB | 🟡 Partial | Native HASH columns and canonical Arrow JSON metadata; extraction, constructors, existence/deletion/path mutation, containment and JSON SRFs. [Scope and validation](sdlc/changes/postgresql18-json/validation.md). Broader function coverage remains open. [#98](https://github.com/redis/redis-adbc-driver/issues/98). |
| JSON document collections / ON JSON indexes | 🔴 Unsupported | Table storage and collection adoption target HASHes; RedisJSON document collections are outside that path. This is separate from SQL JSON functions. [#65](https://github.com/redis/redis-adbc-driver/issues/65). |
| Vector and full-text SQL operations | 🔴 Unsupported | No supported SQL operations for Redis vector KNN/range search or full-text relevance scoring. Ordinary SQL string predicates do not provide those query semantics. |
| Streams, Pub/Sub and keyspace notifications | ⚪ N/A | Native messaging and event-subscription APIs belong to Redis clients or a companion service, outside this SQL/Arrow interface. Finite Arrow result streaming is separate; [CDC placement is tracked in #196](https://github.com/redis/redis-adbc-driver/issues/196). |
| SQL tables over other Redis data types | 🔴 Unsupported | No SQL/Arrow table mapping for application lists, sets, sorted sets, time-series or probabilistic structures. Internal use of a Redis data type does not expose it as an application table. |
| Cluster and database administration | ⚪ N/A | Provisioning, resharding, upgrades, license management and monitoring configuration belong to the [Redis Software management tools](https://redis.io/docs/latest/operate/rs/databases/configure/). |

Implementation references: [connection and protocol configuration](../go/database.go),
[TLS trust configuration](../go/tls.go), [QPF verification](../go/software-qpf.sh), and
[the detailed driver reference](driver-reference.md). Green entries do not
claim every server version or failover scenario has been validated.
