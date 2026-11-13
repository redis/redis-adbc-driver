// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import "github.com/apache/arrow-adbc/go/adbc"

// scanProjection binds bare output columns to HASH fields once. The ordinary
// scan still owns fetching, existence/readiness checks and cancellation. Only
// its final decode/map/evaluate step is replaced, for managed or explicit
// unmanaged row-local scans with no late materialization or tie sorting.
// Managed residuals stay generic; unmanaged scans may bind a small pure predicate.
type scanProjection struct {
	columns []projectionColumn
	rowID   []int
	key     []int
	keyRows bool // explicit unmanaged rows have application keys, never driver IDs
	width   int
	rows    [][]Value
	// A filtered unmanaged projection keeps one reusable decoded output row and
	// only the predicate's bound scalar inputs. Required columns still decode in
	// metadata order before any predicate can reject a row.
	predicate      *scanPredicate
	predicateRow   []Value
	consumeMatched func() error
	// consume is installed while a stream fills Arrow builders or a scalar
	// aggregate consumes an ephemeral decoded row. Other
	// projection users retain the row-returning diagnostic path.
	consume func(aggRow) error
	// Positional HASH bindings are private to authoritative simple projections.
	consumeHash       func([]any) error
	hashExclusive     bool // scalar consumer: every needed field comes from HMGET
	hashFields        []string
	hashPositions     []int
	hashNullPositions []int
}

type projectionColumn struct {
	column    columnMeta
	field     string
	outputs   []int
	decode    func(string) (Value, error)
	predicate int // bound predicate input, or -1
}

func newScanProjection(it *scanIter, items []planItem) *scanProjection {
	unmanaged := unmanagedScanProjection(it)
	if (!managedRowIDTable(it.meta) && !unmanaged) || it.residual != nil && !unmanaged || it.ties != nil || it.late != nil || len(items) == 0 {
		return nil
	}
	p := &scanProjection{width: len(items), keyRows: unmanaged}
	slots := make(map[string]int, len(it.need))
	for _, c := range it.meta.scanColumns() {
		if it.need[c.Name] {
			slots[c.Name] = len(p.columns)
			decode := c.decode
			if len(items) >= 64 && !unmanaged {
				decode = bindStoredDecode(c)
			}
			p.columns = append(p.columns, projectionColumn{column: c, field: c.field(), decode: decode, predicate: -1})
		}
	}
	for i, item := range items {
		ref, ok := item.expr.(*ColumnRef)
		if !ok || ref.Outer != 0 || ref.item != "" || ref.outerAgg != nil {
			return nil
		}
		if unmanaged && ref.Name == keyField {
			p.key = append(p.key, i)
		} else if !unmanaged && ref.Name == rowIDField {
			p.rowID = append(p.rowID, i)
		} else if slot, ok := slots[ref.Name]; ok {
			p.columns[slot].outputs = append(p.columns[slot].outputs, i)
		} else {
			return nil
		}
	}
	if it.residual != nil {
		if it.subBudget != nil || it.gpu != nil || it.env == nil || it.env.exec != nil && it.env.exec.retained != nil {
			return nil
		}
		p.predicate = bindScanPredicate(it.residual, it.meta.types(), it.env.params, it.env.zone())
		if p.predicate == nil {
			return nil
		}
		for i, name := range p.predicate.fields {
			at, ok := slots[name]
			if !ok {
				return nil
			}
			p.columns[at].predicate = i
		}
	}
	if it.hashFetch && !p.bindHash(it.fetch) {
		return nil
	}
	return p
}

// Explicit unmanaged registrations retain their established live LOAD */
// HGETALL retrieval and existence checks. They can bypass the decoded Value map
// without assuming a managed row ID, NULL marker, source-key provenance or
// immutable layout. Other application-owned layouts keep the ordinary path.
func unmanagedScanProjection(it *scanIter) bool {
	m := it.meta
	return m != nil && m.Unmanaged && !m.isMem && m.join == nil && m.group == nil &&
		!m.Rewriting && !it.hashFetch && it.flexUnmanaged == nil && validateUnmanagedDescriptor(m) == nil
}

// Bind only a complete authoritative fetch, including missing-value markers.
// A failed bind leaves no partial positional state.
func (p *scanProjection) bindHash(fields []string) bool {
	positions := map[string]int{}
	for i, f := range fields {
		positions[f] = i + 1
	}
	var slots, nullSlots []int
	for _, c := range p.columns {
		at, ok := positions[c.field]
		if !ok {
			return false
		}
		nullAt := 0
		if c.column.MissingThrough > 0 {
			var ok bool
			if nullAt, ok = positions[nullMarker(c.field)]; !ok {
				return false
			}
		}
		slots = append(slots, at)
		nullSlots = append(nullSlots, nullAt)
	}
	p.hashFields = append([]string(nil), fields...)
	p.hashPositions = slots
	p.hashNullPositions = nullSlots
	return true
}

func (p *scanProjection) decode(raw aggRow) ([]Value, error) {
	row := make([]Value, p.width)
	if err := p.decodeInto(raw, row); err != nil {
		return nil, err
	}
	return row, nil
}

// decodeInto reuses one row of scratch storage. Decode all required columns
// before appending any Arrow values so errors cannot leave a half-built row.
func (p *scanProjection) decodeInto(raw aggRow, row []Value) error {
	// decodeRow always validates a present internal ID, even when it is not
	// selected. Likewise decode every needed column, including sort inputs,
	// in metadata order so malformed stored values retain the same errors.
	if p.keyRows {
		key := raw[keyField]
		if !unmanagedSourceKey(key) {
			return errorf(adbc.StatusInvalidArgument, "unmanaged index contains a key overlapping driver storage")
		}
		for _, i := range p.key {
			row[i] = stringValue(key)
		}
	} else {
		id := nullValue(typeInt64)
		if s, ok := raw[rowIDField]; ok {
			var err error
			id, err = decodeStored(s, typeInt64)
			if err != nil {
				return err
			}
		}
		for _, i := range p.rowID {
			row[i] = id
		}
	}
	for i := range p.columns {
		slot := &p.columns[i]
		var v Value
		var err error
		if s, ok := raw[slot.field]; ok {
			v, err = slot.decode(s)
		} else {
			v, err = slot.column.missingValue(raw)
		}
		if err != nil {
			return errorf(adbc.StatusInternal, "column %q: %v", slot.column.Name, err)
		}
		if slot.predicate >= 0 {
			p.predicate.values[slot.predicate] = v
		}
		for _, i := range slot.outputs {
			row[i] = v
		}
	}
	return nil
}

// decodeHashInto preserves decodeInto's metadata order and missing-value rules.
// Bound NULL-marker positions preserve defaults without constructing row maps.
func (p *scanProjection) decodeHashInto(vals []any, row []Value) error {
	if len(vals) != len(p.hashFields)+1 {
		return errorf(adbc.StatusInternal, "unexpected HMGET row width %d", len(vals))
	}
	id := nullValue(typeInt64)
	if raw, ok := vals[0].(string); ok {
		var err error
		id, err = decodeStored(raw, typeInt64)
		if err != nil {
			return err
		}
	}
	for _, output := range p.rowID {
		row[output] = id
	}
	for i := range p.columns {
		c := &p.columns[i]
		var v Value
		var err error
		if raw, ok := vals[p.hashPositions[i]].(string); ok {
			v, err = c.decode(raw)
		} else {
			v = nullValue(c.column.Type)
			if c.column.MissingThrough > 0 && !id.Null {
				if _, explicitNull := vals[p.hashNullPositions[i]].(string); !explicitNull {
					v, err = c.column.missingValueForID(id.I)
				}
			}
		}
		if err != nil {
			return errorf(adbc.StatusInternal, "column %q: %v", c.column.Name, err)
		}
		for _, output := range c.outputs {
			row[output] = v
		}
	}
	return nil
}
