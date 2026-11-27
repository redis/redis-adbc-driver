// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"

	"github.com/apache/arrow-adbc/go/adbc"
)

// The shape proof permits only fact-column keys on every star edge. Resolve
// each bucket from the original fact once; dimension selections cannot affect
// another edge's lookup. Scratch belongs to one worker, never to emitted rows.
type starRowProbe struct {
	shape    *starShape
	matches  [][]map[string]Value
	selected []map[string]Value
	// env evaluates a snowflake residual; it belongs to this probe's worker.
	env *evalEnv
}

func newStarRowProbe(shape *starShape) *starRowProbe {
	p := &starRowProbe{shape: shape, matches: make([][]map[string]Value, len(shape.dims)), selected: make([]map[string]Value, len(shape.dims))}
	if shape.residual != nil && shape.newEnv != nil {
		p.env = shape.newEnv()
	}
	return p
}

// emit sends a complete output row that satisfies the shape's residual.
func (p *starRowProbe) emitChecked(row map[string]Value, emit func(map[string]Value) error) error {
	if p.shape.residual != nil {
		if p.env == nil {
			return errorf(adbc.StatusInternal, "streamed join residual has no evaluator")
		}
		ok, err := starResidualPasses(p.env, p.shape.residual, row)
		if err != nil || !ok {
			return err
		}
	}
	return emit(row)
}

// outerMiss is the one empty match of an outer dimension for a fact row it
// does not match: the row continues without the dimension's columns, which
// read as NULL. It is shared and never written.
var outerMiss = []map[string]Value{{}}

func (p *starRowProbe) lookup(ctx context.Context, fact map[string]Value) (matched, single bool, err error) {
	single = true
	for i := range p.shape.dims {
		if err := cancelled(ctx); err != nil {
			return false, false, err
		}
		dim := &p.shape.dims[i]
		key, ok := starRowKey(fact, dim.factCols)
		if ok {
			p.matches[i] = dim.buckets[key]
		} else {
			p.matches[i] = nil
		}
		if len(p.matches[i]) == 0 {
			if !dim.outer {
				return false, false, nil
			}
			p.matches[i] = outerMiss
		}
		single = single && len(p.matches[i]) == 1
	}
	return true, single, nil
}
func (p *starRowProbe) row(ctx context.Context, fact map[string]Value, emit func(map[string]Value) error) error {
	matched, _, err := p.lookup(ctx, fact)
	if err != nil || !matched {
		return err
	}
	return p.walk(ctx, fact, 0, len(fact), emit)
}

// ownedRow is opt-in only for freshly decoded, single-use native fact maps.
// Resolve every key first. Any fanout retains independent fresh leaf maps;
// only an exactly one-leaf result can safely extend/transfer the fact itself.
func (p *starRowProbe) ownedRow(ctx context.Context, fact map[string]Value, emit func(map[string]Value) error) error {
	matched, single, err := p.lookup(ctx, fact)
	if err != nil || !matched {
		return err
	}
	if !single {
		return p.walk(ctx, fact, 0, len(fact), emit)
	}
	for _, bucket := range p.matches {
		if err := cancelled(ctx); err != nil {
			return err
		}
		for k, v := range bucket[0] {
			fact[k] = v
		}
	}
	if err := cancelled(ctx); err != nil {
		return err
	}
	return p.emitChecked(fact, emit)
}

func (p *starRowProbe) walk(ctx context.Context, fact map[string]Value, d, width int, emit func(map[string]Value) error) error {
	if err := cancelled(ctx); err != nil {
		return err
	}
	if d == len(p.matches) {
		if d == 0 {
			return p.emitChecked(fact, emit)
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
		return p.emitChecked(out, emit)
	}
	for _, right := range p.matches[d] {
		p.selected[d] = right
		if err := p.walk(ctx, fact, d+1, width+len(right), emit); err != nil {
			return err
		}
	}
	return nil
}
