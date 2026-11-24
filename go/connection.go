// Copyright (c) 2026 ADBC Drivers Contributors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//         http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package redis

import (
	"context"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/adbc-drivers/driverbase-go/driverbase"
	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	goredis "github.com/redis/go-redis/v9"
)

type connectionImpl struct {
	readSnapshotRetention       time.Duration
	usage                       *usageTelemetry
	rbac                        *rbacManager
	retainedNamespace           string
	partitionControlPersistence string
	retainedLimits              retainedLimits
	readerOnly                  bool
	retainedClient              goredis.UniversalClient
	driverbase.ConnectionImplBase

	store *store
	// sess holds the settings: the current schema, and what SET changes.
	sess             *session
	pushdown         string
	queryWorkers     int
	partitionCount   int // default copied into new statements
	workerBudget     *workerBudget
	workerGrant      atomic.Int64
	aggregateMetrics atomic.Pointer[partitionMetrics]
	version          string
	// rekey is adbc.redis.rename_rekey (from the database unless set on the
	// connection).
	rekey bool
	// stream is adbc.redis.stream_results and stream_batch_rows, from the
	// database; streams are the results being streamed (see stream.go).
	stream  streamSettings
	streams streams

	// clientCfg is what the client was made from, and timeouts its
	// timeouts (see timeout.go). writeFollows is set while the write
	// timeout follows the read timeout.
	clientCfg    *clientConfig
	timeouts     timeouts
	writeFollows bool
}

func (c *connectionImpl) executor() *executor {
	metrics := &partitionMetrics{}
	c.aggregateMetrics.Store(metrics)
	return &executor{store: c.store, rbac: c.rbac, partitionNamespace: c.retainedNamespace, sess: c.sess, schema: catalogSchema(c.sess.currentCatalog(), c.sess.currentSchema()), pushdown: c.pushdown, queryWorkers: c.queryWorkers, workerBudget: c.workerBudget, workerGrant: &c.workerGrant, aggregateMetrics: metrics, rekey: c.rekey}
}

func (c *connectionImpl) GetOption(ctx context.Context, key string) (string, error) {
	if key == OptionStringTelemetrySnapshot {
		if c.usage == nil {
			return "{}", nil
		}
		return string(c.usage.snapshot()), nil
	}
	switch key {
	case OptionStringReadSnapshotAction, OptionStringReadSnapshotStatus, OptionIntReadSnapshotRetention:
		return c.readSnapshotOption(ctx, key)
	case OptionStringStatementSnapshots:
		return strconv.FormatBool(c.store != nil && c.store.snapshots), nil
	case OptionStringPartitionResultNamespace:
		return c.retainedNamespace, nil
	case OptionStringPartitionReaderOnly:
		return strconv.FormatBool(c.readerOnly), nil
	case OptionStringPartitionResultLimits:
		return retainedJSON(c.retainedLimits), nil
	case OptionStringLiveTables:
		return strconv.FormatBool(c.store.liveTables), nil
	case OptionStringForeignIndexes:
		return strconv.FormatBool(c.store.foreignIndexes), nil
	case OptionStringForeignIndexesMode:
		return parseTableMode(c.store.foreignIndexesMode)
	case OptionStringAggregateMetrics:
		return c.aggregateMetrics.Load().json(), nil
	case OptionIntQueryWorkersLastGrant:
		return strconv.FormatInt(c.workerGrant.Load(), 10), nil
	case OptionStringQueryWorkerBudget, OptionIntQueryWorkersActive, OptionIntQueryWorkersQueued, OptionStringQueryWorkerTuning:
		return c.workerBudget.option(key), nil
	case OptionIntPartitionCount:
		return strconv.Itoa(effectivePartitionCount(c.partitionCount)), nil
	case OptionStringQueryWorkers:
		return strconv.Itoa(effectiveQueryWorkers(c.queryWorkers)), nil
	case OptionStringRenameRekey:
		return strconv.FormatBool(c.rekey), nil
	case OptionStringStatementTimeout:
		return formatTimeout(c.sess.statementTimeout()), nil
	case OptionStringReadTimeout:
		return formatTimeout(c.timeouts.read), nil
	case OptionStringWriteTimeout:
		return formatTimeout(c.timeouts.write), nil
	case OptionStringTimeZone:
		return c.sess.value("timezone").text, nil
	case OptionStringTemporaryObjects:
		return strconv.FormatBool(c.store != nil && c.store.hasTempObjects()), nil
	}
	return c.ConnectionImplBase.GetOption(ctx, key)
}

func (c *connectionImpl) SetOption(ctx context.Context, key, value string) error {
	if key == OptionStringStatementSnapshots {
		if value == "true" && c.store != nil && c.store.snapshots {
			return nil
		}
		return snapshotUnsupported()
	}
	switch key {
	case OptionIntPartitionCount:
		n, err := parsePartitionCount(value)
		if err != nil {
			return err
		}
		c.partitionCount = n
		return nil
	case OptionStringForeignIndexesMode:
		if c.store.hasSharedReadView() {
			return sharedSnapshotUnsupported()
		}
		mode, err := parseTableMode(value)
		if err != nil {
			return err
		}
		if c.store.flexEnabled() && c.store.foreignIndexes && mode != "unmanaged" {
			return flexUnsupported("managed foreign indexes")
		}
		c.store.foreignIndexesMode = mode
		return nil
	case OptionStringReadSnapshotAction, OptionStringReadSnapshotStatus, OptionIntReadSnapshotRetention:
		return c.setReadSnapshotOption(ctx, key, value)
	case OptionStringLiveTables:
		if c.store.hasSharedReadView() {
			return sharedSnapshotUnsupported()
		}
		if value != "true" && value != "false" {
			return errorf(adbc.StatusInvalidArgument, "live_tables must be true or false")
		}
		if value == "true" && c.store.flexEnabled() {
			return flexUnsupported("live HASH tables")
		}
		if value == "true" {
			if err := c.store.checkLiveNamespace(ctx); err != nil {
				return err
			}
		}
		c.store.liveTables = value == "true"
		return nil
	case OptionStringForeignIndexesRefresh:
		if c.store.hasSharedReadView() {
			return sharedSnapshotUnsupported()
		}
		if value != "true" {
			return errorf(adbc.StatusInvalidArgument, "foreign_indexes_refresh must be true")
		}
		if c.store == nil || c.store.isClosed() {
			return errConnClosed
		}
		if !c.store.snapshots || c.readerOnly {
			return errorf(adbc.StatusInvalidState, "foreign refresh requires a managed connection")
		}
		return c.store.refreshSnapshotForeign(ctx, true)
	case OptionStringForeignIndexes:
		if c.store.hasSharedReadView() {
			return sharedSnapshotUnsupported()
		}
		if c.store.flexEnabled() && value == "true" && c.store.foreignIndexesMode != "unmanaged" {
			return flexUnsupported("foreign indexes")
		}
		v, err := parseForeignIndexes(value)
		if err != nil {
			return err
		}
		if v && c.store.snapshots {
			if err := c.store.refreshSnapshotForeign(ctx, false); err != nil {
				return err
			}
		}
		c.store.foreignIndexes = v
		return nil
	case adbc.OptionKeyAutoCommit:
		switch value {
		case adbc.OptionValueEnabled:
			return nil
		case adbc.OptionValueDisabled:
			return errorf(adbc.StatusNotImplemented, "transactions are not supported (autocommit only)")
		default:
			return errorf(adbc.StatusInvalidArgument, "invalid autocommit value %q: expected true or false", value)
		}
	case OptionStringRenameRekey:
		rekey, err := parseRenameRekey(value)
		if err != nil {
			return err
		}
		c.rekey = rekey
		return nil
	case OptionStringStatementTimeout:
		d, err := parseTimeout(key, value)
		if err != nil {
			return err
		}
		c.sess.defaultStatementTimeout = &d
		delete(c.sess.vals, "statement_timeout")
		delete(c.sess.local, "statement_timeout")
		return nil
	case OptionStringReadTimeout, OptionStringWriteTimeout:
		d, err := parseTimeout(key, value)
		if err != nil {
			return err
		}
		t, follows := c.timeouts, c.writeFollows
		if key == OptionStringReadTimeout {
			t.read = d
			if follows {
				t.write = d
			}
		} else {
			t.write, follows = d, false
		}
		if err := c.setTimeouts(ctx, t); err != nil {
			return err
		}
		c.writeFollows = follows
		return nil
	case OptionStringTimeZone:
		tz, err := timeZoneParam(value)
		if err != nil {
			return err
		}
		c.sess.setDefaultZone(tz)
		return nil
	case OptionStringTemporaryObjects:
		return errorf(adbc.StatusInvalidArgument, "%s is read-only", key)
	}
	return c.ConnectionImplBase.SetOption(ctx, key, value)
}

// setTimeouts gives the connection a client with other timeouts. go-redis
// fixes them when a client is made, so this makes a new one. The old one
// stays open until the connection closes, for background work that still
// uses it (cleanups, string column checks, the temporary schema's
// heartbeat, a rename's lease).
func (c *connectionImpl) setTimeouts(ctx context.Context, t timeouts) error {
	if t == c.timeouts {
		return nil
	}
	if c.store == nil || c.store.isClosed() {
		return errConnClosed
	}
	_, cluster := c.store.client.(*goredis.ClusterClient)
	client := newClient(c.clientCfg, cluster, t)
	if c.store.snapshots {
		addSnapshotHooks(client)
	}
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return wrapRedis(err, "failed to connect to Redis")
	}
	c.store.mu.Lock()
	c.store.retired = append(c.store.retired, c.store.client)
	c.retainedClient = nil
	c.store.mu.Unlock()
	c.store.client = client
	c.timeouts = t
	return nil
}

// errConnClosed refuses what needs the connection once it is closed.
var errConnClosed = errorf(adbc.StatusInvalidState, "the connection is closed")

func (c *connectionImpl) Close(ctx context.Context) error {
	// Mark closed before taking the owner snapshot, while clients remain open
	// for cursor cleanup. A newly registered coordinator can then see closure.
	if c.store == nil || !c.store.setClosed() {
		return nil
	}
	// Renames running on other goroutines stop, and are rolled back, while
	// the client is open (see rekey.go).
	c.store.closeIdleExecutionPool()
	c.store.stopSnapshotCompactor()
	c.store.stopRekeys(ctx)
	executions := c.store.activeExecutions()
	reason := errorf(adbc.StatusInvalidState, "the connection was closed while its result was being read")
	for _, execution := range executions {
		execution.closeWith(reason)
	}
	c.streams.closeAll(reason)
	// Abort private work and release pins using the maintenance pool before
	// closing it. Bound Close even if caller-owned Arrow input ignores cancel.
	cleanup, cancelCleanup := context.WithTimeout(ctx, 5*time.Second)
	var cleanupErr error
	for _, execution := range executions {
		select {
		case <-execution.done:
		case <-cleanup.Done():
			cleanupErr = wrapRedis(cleanup.Err(), "active execution cleanup did not finish before Close")
		}
		if cleanupErr != nil {
			break
		}
	}
	cancelCleanup()
	c.store.releaseSharedReadView()
	// Best effort: whatever can't be dropped now is swept by a later
	// connection (see temp.go).
	_ = c.store.dropTempSchema(ctx)
	// The client stays set: a statement still running on another goroutine
	// fails with "redis: client is closed".
	err := c.store.client.Close()
	c.store.mu.Lock()
	retired := c.store.retired
	c.store.retired = nil
	c.store.mu.Unlock()
	for _, old := range retired {
		_ = old.Close()
	}
	if cleanupErr != nil {
		return cleanupErr
	}
	return err
}

func (c *connectionImpl) PrepareDriverInfo(ctx context.Context, infoCodes []adbc.InfoCode) error {
	if c.version == "" {
		v, err := c.store.serverVersion(ctx)
		if err != nil {
			return err
		}
		c.version = v
	}
	if err := c.DriverInfo.RegisterInfoCode(adbc.InfoVendorName, "Redis"); err != nil {
		return err
	}
	return c.DriverInfo.RegisterInfoCode(adbc.InfoVendorVersion, c.version)
}

func (c *connectionImpl) Commit(ctx context.Context) error {
	return errorf(adbc.StatusInvalidState, "transactions are not supported (autocommit only)")
}

func (c *connectionImpl) Rollback(ctx context.Context) error {
	return errorf(adbc.StatusInvalidState, "transactions are not supported (autocommit only)")
}

func (c *connectionImpl) NewStatement(ctx context.Context) (adbc.StatementWithContext, error) {
	st := &statementImpl{
		StatementImplBase: driverbase.NewStatementImplBase(&c.ConnectionImplBase, c.ErrorHelper),
		conn:              c,
		ingest:            driverbase.NewBulkIngestOptions(),
		stream:            c.stream,
		partitionCount:    c.partitionCount,
	}
	// Not driverbase.NewStatement(st): its wrapper embeds the StatementImpl
	// interface, which hides Cancel from AdbcStatementCancel (it looks for
	// driverbase.StatementCanceler).
	return st, nil
}

// ---- CurrentNamespacer ----

func (c *connectionImpl) GetCurrentCatalog(ctx context.Context) (string, error) {
	return c.sess.currentCatalog(), nil
}

func (c *connectionImpl) GetCurrentDbSchema(ctx context.Context) (string, error) {
	return c.sess.currentSchema(), nil
}

func (c *connectionImpl) SetCurrentCatalog(ctx context.Context, catalog string) error {
	if catalog != catalogName && c.store.hasSharedReadView() {
		return namedCatalogExecutionUnsupported()
	}
	ctx, pin, err := c.store.acquireSnapshot(ctx)
	if err != nil {
		return err
	}
	defer pin.release()
	id, err := c.store.catalogIdentity(ctx, catalog)
	if err != nil {
		return err
	}
	c.sess.catalog, c.sess.catalogID = catalog, id
	c.sess.setSchema(defaultSchema)
	return nil
}

func (c *connectionImpl) SetCurrentDbSchema(ctx context.Context, schema string) error {
	ctx, pin, snapshotErr := c.store.acquireSnapshot(ctx)
	if snapshotErr != nil {
		return snapshotErr
	}
	defer pin.release()
	if err := validateLogicalSchema(schema); err != nil {
		return err
	}
	if err := c.executor().validateCurrentCatalog(ctx); err != nil {
		return err
	}
	ok, err := c.store.schemaExists(ctx, catalogSchema(c.sess.currentCatalog(), schema))
	if err != nil {
		return err
	}
	if !ok {
		return stateErrorf(stateUndefinedSchema, "schema %q does not exist", schema)
	}
	c.sess.setSchema(schema)
	return nil
}

// ---- metadata ----

func (c *connectionImpl) ListTableTypes(ctx context.Context) ([]string, error) {
	return []string{"TABLE", "VIEW", "MATERIALIZED VIEW"}, nil
}

func (c *connectionImpl) GetTableSchema(ctx context.Context, catalog *string, dbSchema *string, tableName string) (*arrow.Schema, error) {
	ctx, pin, snapshotErr := c.store.acquireSnapshot(ctx)
	if snapshotErr != nil {
		return nil, snapshotErr
	}
	defer pin.release()
	if catalog == nil || *catalog == "" {
		if err := c.executor().validateCurrentCatalog(ctx); err != nil {
			return nil, err
		}
	}
	if catalog != nil && *catalog != "" {
		if _, err := c.store.catalogIdentity(ctx, *catalog); err != nil {
			return nil, err
		}
	}
	name := TableName{Name: tableName}
	if catalog != nil {
		name.Catalog = *catalog
	}
	if dbSchema != nil {
		name.Schema = *dbSchema
	}
	// Without a schema, a temporary table or view comes first.
	schema, _, err := c.executor().resolveTable(name)
	if err != nil {
		return nil, err
	}
	// Schema discovery uses immutable metadata, not row/index permissions.
	meta, err := c.store.getTableMetadata(ctx, schema, tableName)
	if err == nil && !meta.Live {
		err = validateSnapshotDescriptor(ctx, meta)
	}
	if err != nil {
		v, verr := c.store.getView(ctx, schema, tableName)
		if verr != nil {
			return nil, err
		}
		meta = &tableMeta{Schema: schema, Name: tableName, Columns: v.Columns}
	}
	fields := make([]arrow.Field, len(meta.Columns))
	for i, col := range meta.Columns {
		fields[i] = arrowField(col.Name, col.Type, col.Nullable)
		if col.Comment != "" {
			md := fields[i].Metadata
			keys := append(slices.Clone(md.Keys()), remarksKey)
			fields[i].Metadata = arrow.NewMetadata(keys, append(slices.Clone(md.Values()), col.Comment))
		}
	}
	return arrow.NewSchema(fields, nil), nil
}

// matchPattern implements SQL LIKE matching with '%' and '_'.
func matchPattern(value string, pattern *string) bool {
	if pattern == nil {
		return true
	}
	var b strings.Builder
	b.WriteString("(?s)^")
	for _, r := range *pattern {
		switch r {
		case '%':
			b.WriteString(".*")
		case '_':
			b.WriteString(".")
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return false
	}
	return re.MatchString(value)
}

func (c *connectionImpl) GetCatalogs(ctx context.Context, catalogFilter *string) ([]string, error) {
	ctx, pin, err := c.store.acquireSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	defer pin.release()
	all, err := c.store.listCatalogs(ctx)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, name := range all {
		if matchPattern(name, catalogFilter) {
			out = append(out, name)
		}
	}
	return out, nil
}

func (c *connectionImpl) GetDBSchemasForCatalog(ctx context.Context, catalog string, schemaFilter *string) ([]string, error) {
	ctx, pin, snapshotErr := c.store.acquireSnapshot(ctx)
	if snapshotErr != nil {
		return nil, snapshotErr
	}
	defer pin.release()
	if ok, err := c.store.catalogExists(ctx, catalog); err != nil {
		return nil, err
	} else if !ok {
		return nil, nil
	}
	all, err := c.store.listSchemas(ctx)
	if err != nil {
		return nil, err
	}
	// The connection's own temporary objects are listed under pg_temp.
	if catalog == catalogName && c.store.hasTempObjects() {
		all = append(all, tempAlias)
		slices.Sort(all)
	}
	var out []string
	for _, s := range all {
		if schemaCatalog(s) != catalog {
			continue
		}
		s = displaySchema(s)
		if matchPattern(s, schemaFilter) {
			out = append(out, s)
		}
	}
	return out, nil
}

func (c *connectionImpl) GetTablesForDBSchema(ctx context.Context, catalog string, schema string, tableFilter *string, columnFilter *string, includeColumns bool) ([]driverbase.TableInfo, error) {
	ctx, pin, snapshotErr := c.store.acquireSnapshot(ctx)
	if snapshotErr != nil {
		return nil, snapshotErr
	}
	defer pin.release()
	if ok, err := c.store.catalogExists(ctx, catalog); err != nil {
		return nil, err
	} else if !ok {
		return nil, nil
	}
	if err := validateLogicalSchema(schema); err != nil {
		return nil, err
	}
	if catalog != catalogName && (isTempAlias(schema) || isTempSchema(schema)) {
		return nil, nil
	}
	if isTempAlias(schema) {
		schema = c.store.tempSchema()
	} else if isTempSchema(schema) {
		return []driverbase.TableInfo{}, nil
	}
	if !isTempSchema(schema) {
		schema = catalogSchema(catalog, schema)
	}
	tables, err := c.store.listTables(ctx, schema)
	if err != nil {
		return nil, err
	}
	views, err := c.store.listViews(ctx, schema)
	if err != nil {
		return nil, err
	}
	type entry struct{ name, kind string }
	var entries []entry
	metas, err := c.store.getTables(ctx, schema, tables)
	if err != nil {
		return nil, err
	}
	var declarationTargets map[string]*tableMeta
	if includeColumns {
		declarationTargets, err = c.store.declarationTargets(ctx, metas)
		if err != nil {
			return nil, err
		}
	}
	for i, n := range tables {
		kind := "TABLE"
		if metas[i] != nil && metas[i].MaterializedSQL != "" {
			kind = "MATERIALIZED VIEW"
		}
		entries = append(entries, entry{n, kind})
	}
	for _, n := range views {
		entries = append(entries, entry{n, "VIEW"})
	}
	slices.SortFunc(entries, func(a, b entry) int { return strings.Compare(a.name, b.name) })

	out := []driverbase.TableInfo{}
	for _, ent := range entries {
		if !matchPattern(ent.name, tableFilter) {
			continue
		}
		info := driverbase.TableInfo{TableName: ent.name, TableType: ent.kind}
		if includeColumns {
			var cols []columnMeta
			var meta *tableMeta
			var err error
			if ent.kind == "VIEW" {
				var v *viewMeta
				if v, err = c.store.getView(ctx, schema, ent.name); err == nil {
					cols = v.Columns
				}
			} else if meta, err = c.store.getTableMetadata(ctx, schema, ent.name); err == nil {
				if !meta.Live {
					err = validateSnapshotDescriptor(ctx, meta)
				}
				if err == nil {
					cols = meta.Columns
				}
			}
			if err != nil {
				var ae adbc.Error
				if asAdbc(err, &ae) && ae.Code == adbc.StatusNotFound {
					continue
				}
				return nil, err
			}
			info.TableColumns = []driverbase.ColumnInfo{}
			for i, col := range cols {
				if !matchPattern(col.Name, columnFilter) {
					continue
				}
				pos := int32(i + 1)
				typeName := col.Type.SQLName()
				nullable := int16(0)
				isNullable := "NO"
				if col.Nullable {
					nullable = 1
					isNullable = "YES"
				}
				ci := driverbase.ColumnInfo{
					ColumnName:      col.Name,
					OrdinalPosition: &pos,
					XdbcTypeName:    &typeName,
					XdbcNullable:    &nullable,
					XdbcIsNullable:  &isNullable,
				}
				if n := col.Type.Length; col.Type.Kind == KindString && n > 0 {
					// Characters, and bytes at up to 4 per UTF-8 character.
					size, octets := n, 4*n
					ci.XdbcColumnSize, ci.XdbcCharOctetLength = &size, &octets
				}
				if k := col.Type.Kind; k == KindTime || k == KindTimeTZ || k == KindTimestamp {
					// The fractional-second digits, TIMESTAMP(p)'s p.
					digits := int16(col.Type.fracDigits())
					ci.XdbcDecimalDigits = &digits
				}
				if col.Default != "" {
					def := col.Default
					ci.XdbcColumnDef = &def
				}
				if col.Comment != "" {
					remarks := col.Comment
					ci.Remarks = &remarks
				}
				info.TableColumns = append(info.TableColumns, ci)
			}
			// Report persisted constraint declarations. UNIQUE/FK metadata
			// describes no enforcement or unique indexes (see declaration.go).
			info.TableConstraints = []driverbase.ConstraintInfo{}
			if meta != nil {
				for _, d := range meta.Declarations {
					info.TableConstraints = append(info.TableConstraints, declarationInfo(meta, d, declarationTargets))
				}
				if k := meta.PrimaryKey; k != nil {
					name := k.Name
					info.TableConstraints = append(info.TableConstraints, driverbase.ConstraintInfo{
						ConstraintName:        &name,
						ConstraintType:        "PRIMARY KEY",
						ConstraintColumnNames: driverbase.RequiredList(meta.keyColumns()),
					})
				}
				for _, chk := range meta.Checks {
					name := chk.Name
					info.TableConstraints = append(info.TableConstraints, driverbase.ConstraintInfo{
						ConstraintName:        &name,
						ConstraintType:        "CHECK",
						ConstraintColumnNames: driverbase.RequiredList(checkColumns(meta, chk.Expr)),
					})
				}
			}
		}
		out = append(out, info)
	}
	return out, nil
}
