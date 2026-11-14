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
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
)

// metaFromArrow derives table metadata from an Arrow schema.
func metaFromArrow(schemaName, table string, schema *arrow.Schema) (*tableMeta, error) {
	meta := &tableMeta{Schema: schemaName, Name: table}
	for _, f := range schema.Fields() {
		t, err := colTypeFromField(f)
		if err != nil {
			return nil, errorf(adbc.StatusNotImplemented, "column %q: %v", f.Name, err)
		}
		meta.Columns = append(meta.Columns, columnMeta{Name: f.Name, Type: t, Nullable: f.Nullable})
	}
	return meta, nil
}

// executeIngest implements bulk ingestion: every Arrow row becomes a HASH.
func (s *statementImpl) executeIngest(ctx context.Context) (count int64, resultErr error) {
	ctx, transport := s.conn.executionContext(ctx, &s.streams, &s.conn.streams)
	if transport != nil {
		defer transport.finish()
	}
	if s.params == nil {
		return -1, errorf(adbc.StatusInvalidState, "bulk ingest requires data to be bound")
	}
	if s.unmanagedSource != nil {
		if scope := snapshotOf(ctx); scope == nil {
			return s.executeUnmanagedSourceIngest(ctx)
		} else if scope.write {
			return -1, unmanagedUnsupported("nested or staged Arrow creation")
		}
	}
	if s.conn.store.snapshots && snapshotOf(ctx) == nil {
		ctx, cancel := context.WithTimeout(ctx, snapshotTimeout)
		defer cancel()
		var target *tableMeta
		sctx, pin, err := s.conn.store.acquireIngestSnapshot(ctx, func(c context.Context) (*tableMeta, error) {
			var err error
			target, err = s.snapshotIngestTarget(c)
			return target, err
		})
		if err != nil {
			return -1, err
		}
		defer pin.release()
		if err := s.conn.store.refreshSnapshotTemp(sctx); err != nil {
			return -1, err
		}
		if target != nil && target.Unmanaged {
			if snapshotOperationRequestOf(sctx) != nil {
				return -1, unmanagedUnsupported("retained operation preparation")
			}
			// Pin the registration only. Application HASH writes must not enter
			// a managed workspace, publication or ingest admission gate.
			sctx = context.WithValue(sctx, unmanagedWriteKey{}, metaKey(target.Schema, target.Name))
			return s.executeIngest(sctx)
		}
		published := false
		defer func() {
			if !published {
				_ = s.conn.store.refreshSnapshotTemp(sctx)
			}
		}()
		wctx, id, err := s.conn.store.beginSnapshotWrite(sctx, pin)
		if err != nil {
			return -1, err
		}
		defer s.conn.store.abortSnapshot(pin, id)
		n, err := s.executeIngest(wctx)
		if err != nil {
			return -1, err
		}
		if request := snapshotOperationRequestOf(wctx); request != nil {
			if err := s.conn.store.prepareSnapshotOperation(wctx, request, "arrow", n, nil); err != nil {
				return -1, err
			}
			return 0, nil
		}
		if err := s.conn.store.publishSnapshot(wctx, pin, id); err != nil {
			return -1, err
		}
		published = true
		return n, nil
	}
	opts := s.ingest
	defer s.clearParams()
	if err := s.prepareExtensionInput(); err != nil {
		return -1, err
	}
	if opts.CatalogName != "" && opts.CatalogName != catalogName {
		return -1, errorf(adbc.StatusNotFound, "catalog %q does not exist", opts.CatalogName)
	}
	st := s.conn.store
	// adbc.ingest.temporary (or schema pg_temp) targets the connection's
	// temporary table; otherwise the target is always a permanent table,
	// even if a temporary table of the same name shadows it in SQL.
	schemaName := opts.SchemaName
	switch {
	case opts.Temporary || isTempAlias(schemaName):
		if schemaName != "" && !isTempAlias(schemaName) {
			return -1, errorf(adbc.StatusInvalidState, "cannot specify a schema name for a temporary table")
		}
		if opts.Mode == adbc.OptionValueIngestModeAppend {
			schemaName = st.tempSchema()
		} else {
			var err error
			if schemaName, err = st.ensureTempSchema(ctx); err != nil {
				return -1, err
			}
		}
	case isTempSchema(schemaName):
		return -1, stateErrorf(stateUndefinedSchema, "schema %q does not exist", schemaName)
	case schemaName == "":
		schemaName = s.conn.sess.currentSchema()
	}
	var total int64
	if exists, err := st.tableExists(ctx, schemaName, opts.TableName); err != nil {
		return -1, err
	} else if exists {
		meta, err := st.getTableMetadata(ctx, schemaName, opts.TableName)
		if err != nil {
			return -1, err
		}
		if meta.Unmanaged {
			if opts.Mode == adbc.OptionValueIngestModeCreate {
				return -1, stateErrorf(stateDuplicateTable, "table %q already exists", meta.Name)
			}
			if opts.Mode != adbc.OptionValueIngestModeAppend && opts.Mode != adbc.OptionValueIngestModeCreateAppend {
				return -1, unmanagedUnsupported("Arrow replace")
			}
			if !unmanagedWriteAllowed(ctx, meta) || snapshotOf(ctx) == nil || snapshotOf(ctx).write {
				return -1, unmanagedUnsupported("nested or staged Arrow ingestion")
			}
			defer func() {
				if resultErr != nil {
					count = total
					resultErr = unmanagedIngestError(total, resultErr)
				}
			}()
		}
	}
	arrowSchema := s.params.Schema()
	wanted, err := metaFromArrow(schemaName, opts.TableName, arrowSchema)
	if err != nil {
		return -1, err
	}
	if err := wanted.applyIndexPolicy(s.indexColumns, nil); err != nil {
		return -1, err
	}

	if _, hasKey := wanted.resolve(keyField); hasKey && opts.Mode == adbc.OptionValueIngestModeReplace {
		return -1, errorf(adbc.StatusInvalidArgument, "__key ingestion requires append or create_append mode")
	}
	var meta *tableMeta
	switch opts.Mode {
	case adbc.OptionValueIngestModeCreate:
		if _, err := st.createTable(ctx, wanted, false); err != nil {
			return -1, err
		}
		meta = wanted
	case adbc.OptionValueIngestModeReplace:
		exists, err := st.tableExists(ctx, schemaName, opts.TableName)
		if err != nil {
			return -1, err
		}
		if exists {
			current, err := st.getTable(ctx, schemaName, opts.TableName)
			if err != nil {
				return -1, err
			}
			if current.MaterializedSQL != "" {
				return -1, matViewWriteError(current)
			}
			if st.snapshots {
				// Explicit refresh still addresses the externally owned source.
				wanted.SnapshotAccessID = current.SnapshotAccessID
				wanted.OID = current.OID
				wanted.SnapshotImportSource = current.SnapshotImportSource
				wanted.SnapshotSourceKey = current.SnapshotSourceKey
			}
		}
		if err := st.dropTable(ctx, schemaName, opts.TableName, true); err != nil {
			return -1, err
		}
		if _, err := st.createTable(ctx, wanted, false); err != nil {
			return -1, err
		}
		meta = wanted
	case adbc.OptionValueIngestModeAppend:
		meta, err = st.getTable(ctx, schemaName, opts.TableName)
		if err != nil {
			return -1, err
		}
	case adbc.OptionValueIngestModeCreateAppend:
		// __key is an input to existing adopted tables, never a stored
		// column of a newly created table.
		if _, hasKey := wanted.resolve(keyField); hasKey {
			current, getErr := st.getTable(ctx, schemaName, opts.TableName)
			if getErr == nil && current.Adopted && current.KeyRows {
				meta = current
				break
			}
			var ae adbc.Error
			if getErr != nil && (!errors.As(getErr, &ae) || ae.Code != adbc.StatusNotFound) {
				return -1, getErr
			}
		}
		created, err := st.createTable(ctx, wanted, true)
		if err != nil {
			return -1, err
		}
		if created {
			meta = wanted
		} else if meta, err = st.getTable(ctx, schemaName, opts.TableName); err != nil {
			return -1, err
		}
	default:
		return -1, errorf(adbc.StatusInvalidArgument, "unknown ingest mode %q", opts.Mode)
	}

	if st.snapshots && !meta.Unmanaged && !snapshotOwns(ctx, meta.prefix()) {
		if err := st.prepareSnapshotTable(ctx, schemaName, opts.TableName); err != nil {
			return -1, err
		}
		meta, err = st.getTable(ctx, schemaName, opts.TableName)
		if err != nil {
			return -1, err
		}
	}
	meta = insertMeta(meta)

	// Map each Arrow field onto a table column; the other columns get their
	// defaults, with the whole ingest as one statement. Values are converted
	// as INSERT converts them, in the session time zone: text without an
	// offset, or a timestamp without a zone, written to a TIMESTAMP WITH
	// TIME ZONE column is a local time.
	mapping := make([]int, arrowSchema.NumFields())
	given := make([]bool, len(meta.Columns))
	for i, f := range arrowSchema.Fields() {
		idx, ok := meta.resolve(f.Name)
		if !ok {
			return -1, errorf(adbc.StatusAlreadyExists, "column %q does not exist in table %q", f.Name, meta.Name)
		}
		if meta.Unmanaged {
			if given[idx] {
				return -1, errorf(adbc.StatusInvalidArgument, "duplicate Arrow input column %q", f.Name)
			}
			if meta.Columns[idx].Name == keyField && wanted.Columns[i].Type.Kind != KindString {
				return -1, errorf(adbc.StatusInvalidArgument, "unmanaged Arrow __key must be a string")
			}
		}
		if !storedRecordAppendCompatible(wanted.Columns[i].Type, meta.Columns[idx].Type) {
			return -1, stateErrorf(stateDatatypeMismatch, "stored struct schema must match field names, types and nullability (column %q)", f.Name)
		}
		mapping[i], given[idx] = idx, true
	}
	if meta.Unmanaged {
		ki, ok := meta.resolve(keyField)
		if !ok || !given[ki] {
			return -1, errorf(adbc.StatusInvalidArgument, "unmanaged Arrow append requires an explicit __key column")
		}
		if err := st.checkWritable(ctx, meta); err != nil {
			return -1, err
		}
		if meta.UnmanagedForeign && meta.UnmanagedFilter != "" {
			return -1, unmanagedUnsupported("INSERT through a filtered foreign index")
		}
	}
	for i, yes := range given {
		if yes && meta.Columns[i].Identity == "ALWAYS" {
			return -1, identityValueError(meta.Columns[i].Name)
		}
	}
	if meta.MaterializedSQL != "" {
		return -1, matViewWriteError(meta)
	}
	e := s.executor()
	zone := e.zone()
	defs := e.columnDefaults(ctx, meta)
	checks, err := e.tableChecks(ctx, meta)
	if err != nil {
		return -1, err
	}

	writeRecord := func(rec arrow.RecordBatch) (int64, error) {
		n := int(rec.NumRows())
		rows := make([][]Value, n)
		values := make([]Value, n*len(meta.Columns))
		for r := 0; r < n; r++ {
			start, end := r*len(meta.Columns), (r+1)*len(meta.Columns)
			row := values[start:end:end]
			if err := defs.fill(row, given); err != nil {
				return -1, err
			}
			rows[r] = row
		}
		for c := 0; c < int(rec.NumCols()); c++ {
			col := meta.Columns[mapping[c]]
			arr := rec.Column(c)
			if err := ingestColumn(arr, col.Type, zone, rows, mapping[c]); err != nil {
				return -1, columnErr(col.Name, err)
			}
		}
		// Each conversion slice is checked before writing. Snapshot slices stay
		// private until the entire input succeeds and publishes once.
		for _, row := range rows {
			if err := checkNewRow(meta, checks, row); err != nil {
				return -1, err
			}
		}
		return st.insertRows(ctx, meta, rows)
	}
	for {
		if meta.Unmanaged && ctx.Err() != nil {
			return -1, cancelled(ctx)
		}
		if !s.params.Next() {
			break
		}
		rec := s.params.RecordBatch()
		if err := validateInputRecord(rec); err != nil {
			return -1, errorf(adbc.StatusInvalidArgument, "invalid Arrow input: %v", err)
		}
		if meta.Unmanaged && !rec.Schema().Equal(arrowSchema) {
			return -1, errorf(adbc.StatusInvalidArgument, "unmanaged Arrow stream schema changed between batches")
		}
		if scope := snapshotOf(ctx); meta.Unmanaged || scope != nil && scope.write {
			for start := int64(0); start < rec.NumRows(); start += snapshotIngestConvertRows {
				part := rec.NewSlice(start, min(start+snapshotIngestConvertRows, rec.NumRows()))
				written, err := writeRecord(part)
				part.Release()
				total += max(written, 0)
				if err != nil {
					return -1, err
				}
			}
		} else {
			written, err := writeRecord(rec)
			if err != nil {
				return -1, err
			}
			total += written
		}
	}
	if err := s.params.Err(); err != nil && !errors.Is(err, io.EOF) {
		var ae adbc.Error
		if errors.As(err, &ae) {
			return -1, err
		}
		return -1, errorf(adbc.StatusIO, "failed to read ingest data: %v", err)
	}
	return total, nil
}

// Acknowledged rows are a lower bound: an unknown reply can hide an applied
// submitted batch. Preserve the original status/SQLSTATE; never replay writes.
func unmanagedIngestError(acknowledged int64, err error) error {
	var ae adbc.Error
	if !errors.As(err, &ae) {
		ae = adbc.Error{Code: adbc.StatusIO, Msg: err.Error()}
	}
	ae.Msg = "unmanaged Arrow write stopped after " + strconv.FormatInt(acknowledged, 10) +
		" acknowledged rows; source writes may be partially applied; inspect source state before retrying: " + ae.Msg
	ae.Details = append(ae.Details, &adbc.TextErrorDetail{Name: "adbc.redis.ingest.acknowledged_rows", Detail: strconv.FormatInt(acknowledged, 10)})
	return ae
}

// ingestColumn resolves common Arrow array types once per column, rather than
// once per cell. Strings still own their bytes after the Arrow batch is freed,
// and every value passes the same coercion and later row constraint checks.
func ingestColumn(arr arrow.Array, target ColType, zone tzZone, rows [][]Value, column int) error {
	if err := validateInputArray(arr); err != nil {
		return errorf(adbc.StatusInvalidArgument, "invalid Arrow input: %v", err)
	}
	if target == typeString && len(rows) >= 2 {
		switch a := arr.(type) {
		case *array.String:
			if ingestStringArena(a, rows, column) {
				return nil
			}
		case *array.LargeString:
			if ingestStringArena(a, rows, column) {
				return nil
			}
		}
	}
	var read func(int) Value
	exactInteger := false
	switch a := arr.(type) {
	case *array.Int16:
		read = func(i int) Value { return intValue(typeInt16, int64(a.Value(i))) }
		exactInteger = target == typeInt16
	case *array.Int32:
		read = func(i int) Value { return intValue(typeInt32, int64(a.Value(i))) }
		exactInteger = target == typeInt32
	case *array.Int64:
		read = func(i int) Value { return intValue(typeInt64, a.Value(i)) }
		exactInteger = target == typeInt64
	case *array.String:
		read = func(i int) Value { return stringValue(strings.Clone(a.Value(i))) }
	case *array.LargeString:
		read = func(i int) Value { return stringValue(strings.Clone(a.Value(i))) }
	}
	// A validated signed integer array already has the complete target type
	// and range. Bind that fact once per column; aliases, narrowing, other
	// conversions and every non-integer layout retain coerceIn below.
	if exactInteger {
		for i := range rows {
			if arr.IsNull(i) {
				rows[i][column] = nullValue(target)
			} else {
				rows[i][column] = read(i)
			}
		}
		return nil
	}
	for i := range rows {
		var v Value
		var err error
		switch {
		case read == nil:
			v, err = valueAt(arr, i)
		case arr.IsNull(i):
			v = nullValue(target)
		default:
			v = read(i)
		}
		if err != nil {
			return err
		}
		v, err = coerceIn(v, target, zone)
		if err != nil {
			return err
		}
		rows[i][column] = v
	}
	return nil
}
