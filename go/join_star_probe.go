// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import "context"

// The shape proof permits only fact-column keys on every star edge. Resolve
// each bucket from the original fact once; dimension selections cannot affect
// another edge's lookup. Scratch belongs to one worker, never to emitted rows.
type starRowProbe struct {
	shape    *starShape
	matches  [][]map[string]Value
	selected []map[string]Value
}

func newStarRowProbe(shape *starShape) *starRowProbe {
	return &starRowProbe{shape: shape, matches: make([][]map[string]Value, len(shape.dims)), selected: make([]map[string]Value, len(shape.dims))}
}
func (p *starRowProbe) row(ctx context.Context, fact map[string]Value, emit func(map[string]Value) error) error {
	for i := range p.shape.dims {
		if err := cancelled(ctx); err != nil {
			return err
		}
		dim := &p.shape.dims[i]
		key, ok := starRowKey(fact, dim.factCols)
		if !ok {
			return nil
		}
		p.matches[i] = dim.buckets[key]
		if len(p.matches[i]) == 0 {
			return nil
		}
	}
	return p.walk(ctx, fact, 0, len(fact), emit)
}
func (p *starRowProbe) walk(ctx context.Context, fact map[string]Value, d, width int, emit func(map[string]Value) error) error {
	if err := cancelled(ctx); err != nil {
		return err
	}
	if d == len(p.matches) {
		if d == 0 {
			return emit(fact)
		} // Preserve the internal zero-edge contract.
		out := make(map[string]Value, width)
		for k, v := range fact {
			out[k] = v
		}
		// Match mergeRows' dimension order and last-right-row overwrite rules.
		for _, right := range p.selected {
			for k, v := range right {
				out[k] = v
			}
		}
		return emit(out)
	}
	for _, right := range p.matches[d] {
		p.selected[d] = right
		if err := p.walk(ctx, fact, d+1, width+len(right), emit); err != nil {
			return err
		}
	}
	return nil
}
