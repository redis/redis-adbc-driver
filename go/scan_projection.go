// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import "github.com/apache/arrow-adbc/go/adbc"

// scanProjection binds bare output columns to HASH fields once. The ordinary
// scan still owns fetching, existence/readiness checks and cancellation. Only
// its final decode/map/evaluate step is replaced, for managed row-local scans
// with no residual, late materialization or tie sorting.
type scanProjection struct {
	columns []projectionColumn
	rowID   []int
	width   int
	rows    [][]Value
	// consume is installed while a stream fills Arrow builders or a scalar
	// aggregate consumes an ephemeral decoded row. Other
	// projection users retain the row-returning diagnostic path.
	consume func(aggRow) error
	// Positional HASH bindings are private to wide, simple projections.
	consumeHash   func([]any) error
	hashExclusive bool // scalar consumer: every needed field comes from HMGET
	hashFields    []string
	hashPositions []int
}

type projectionColumn struct {
	column  columnMeta
	field   string
	outputs []int
	decode  func(string) (Value, error)
}

func newScanProjection(it *scanIter, items []planItem) *scanProjection {
	if !managedRowIDTable(it.meta) || it.residual != nil || it.ties != nil || it.late != nil || len(items) == 0 {
		return nil
	}
	p := &scanProjection{width: len(items)}
	slots := make(map[string]int, len(it.need))
	for _, c := range it.meta.Columns {
		if it.need[c.Name] {
			slots[c.Name] = len(p.columns)
			decode := c.decode
			if len(items) >= 64 {
				decode = bindStoredDecode(c)
			}
			p.columns = append(p.columns, projectionColumn{column: c, field: c.field(), decode: decode})
		}
	}
	for i, item := range items {
		ref, ok := item.expr.(*ColumnRef)
		if !ok || ref.Outer != 0 || ref.item != "" || ref.outerAgg != nil {
			return nil
		}
		if ref.Name == rowIDField {
			p.rowID = append(p.rowID, i)
		} else if slot, ok := slots[ref.Name]; ok {
			p.columns[slot].outputs = append(p.columns[slot].outputs, i)
		} else {
			return nil
		}
	}
	if it.hashFetch && !p.bindHash(it.fetch) {
		return nil
	}
	return p
}

// Bind only a complete authoritative fetch, including missing-value markers.
// A failed bind leaves no partial positional state.
func (p *scanProjection) bindHash(fields []string) bool {
	positions := map[string]int{}
	for i, f := range fields {
		positions[f] = i + 1
	}
	var slots []int
	for _, c := range p.columns {
		at, ok := positions[c.field]
		if !ok {
			return false
		}
		if c.column.MissingThrough > 0 {
			if _, ok := positions[nullMarker(c.field)]; !ok {
				return false
			}
		}
		slots = append(slots, at)
	}
	p.hashFields = append([]string(nil), fields...)
	p.hashPositions = slots
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
		for _, i := range slot.outputs {
			row[i] = v
		}
	}
	return nil
}

// decodeHashInto preserves decodeInto's metadata order and missing-value rules.
// Only missing-default rows need a temporary map; ordinary values use bound slots.
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
	var missing aggRow
	for i := range p.columns {
		c := &p.columns[i]
		var v Value
		var err error
		if raw, ok := vals[p.hashPositions[i]].(string); ok {
			v, err = c.decode(raw)
		} else if c.column.MissingThrough == 0 {
			v = nullValue(c.column.Type)
		} else {
			if missing == nil {
				missing = aggRow{}
				if raw, ok := vals[0].(string); ok {
					missing[rowIDField] = raw
				}
				for j, f := range p.hashFields {
					if raw, ok := vals[j+1].(string); ok {
						missing[f] = raw
					}
				}
			}
			v, err = c.column.missingValue(missing)
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
