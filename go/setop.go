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

// Set operations: UNION [ALL], INTERSECT [ALL], EXCEPT [ALL].
//
// Each branch is planned and run as its own query (so each still uses its
// table's index); the driver combines the results. Branch columns are
// matched by position and widened to a common type. Duplicates are detected
// with the hash-join equality rules (1 = 1.0 = 1.00), and NULLs count as
// equal to each other, as SQL requires for set operations. ORDER BY on the
// combined result may use output column names or positions.

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/apache/arrow-adbc/go/adbc"
)

type setOpPlan struct {
	op          string
	all         bool
	left, right *selectPlan
	order       []setOrderKey
}

type setOrderKey struct {
	col        int
	desc       bool
	nullsFirst bool
}

// setOpType is the common type of two branch columns.
func setOpType(a, b ColType) (ColType, bool) {
	switch {
	case a.Kind == KindNull:
		return b, true
	case b.Kind == KindNull || a == b:
		return a, true
	case a.Kind.isNumeric() && b.Kind.isNumeric():
		return commonType(a, b), true
	case a.Kind == b.Kind && (a.isTimeOfDay() || a.Kind == KindTimestamp):
		t := finerTime(a, b)
		if a.TZ != "" || b.TZ != "" {
			t.TZ = "UTC"
		}
		return t, true
	case a.isTimeOfDay() && b.isTimeOfDay():
		// A TIME and a time with time zone: a time with time zone, as in
		// Postgres.
		t := finerTime(a, b)
		t.Kind = KindTimeTZ
		return t.withFracDigits(t.fracDigits()), true
	case a.Kind == b.Kind:
		return commonType(a, b), true
	}
	return ColType{}, false
}

func (e *executor) planSetOp(ctx context.Context, sel *SelectStmt, plan *selectPlan) (*selectPlan, error) {
	op := sel.SetOp
	// Both branches are the body of the same subquery (if any), so outer
	// references from either one mark it correlated.
	sq := e.pendingSq
	e.pendingSq = sq
	left, err := e.planSelect(ctx, op.Left, e.paramTypes)
	if err != nil {
		return nil, err
	}
	e.pendingSq = sq
	right, err := e.planSelect(ctx, op.Right, e.paramTypes)
	e.pendingSq = nil
	if err != nil {
		return nil, err
	}
	if len(left.items) != len(right.items) {
		return nil, errorf(adbc.StatusInvalidArgument, "each %s query must have the same number of columns (%d and %d)",
			op.Op, len(left.items), len(right.items))
	}
	for i := range left.items {
		t, ok := setOpType(left.items[i].typ, right.items[i].typ)
		if !ok {
			return nil, errorf(adbc.StatusInvalidArgument, "%s column %d: types %s and %s cannot be matched",
				op.Op, i+1, left.items[i].typ.SQLName(), right.items[i].typ.SQLName())
		}
		plan.items = append(plan.items, planItem{name: left.items[i].name, typ: t})
	}
	sp := &setOpPlan{op: op.Op, all: op.All, left: left, right: right}
	for _, o := range sel.OrderBy {
		col := -1
		switch x := o.Expr.(type) {
		case *Literal:
			if x.V.T.Kind.isInteger() && !x.V.Null && x.V.I >= 1 && int(x.V.I) <= len(plan.items) {
				col = int(x.V.I) - 1
			}
		case *ColumnRef:
			if x.Qualifier == "" {
				for i, it := range plan.items {
					if strings.EqualFold(it.name, x.Name) {
						if col >= 0 {
							return nil, errorf(adbc.StatusInvalidArgument, "ORDER BY %q is ambiguous", x.Name)
						}
						col = i
					}
				}
			}
		}
		if col < 0 {
			return nil, errorf(adbc.StatusInvalidArgument, "ORDER BY on a %s result must use output column names or positions", op.Op)
		}
		sp.order = append(sp.order, setOrderKey{col: col, desc: o.Desc, nullsFirst: o.Nulls == NullsFirst})
	}
	plan.setop = sp
	return plan, nil
}

// rowKey identifies a row for duplicate elimination (NULLs are equal).
func rowKey(row []Value) string {
	var b strings.Builder
	for _, v := range row {
		writeKey(&b, v)
	}
	return b.String()
}

// writeKey appends v's part of a row key, as rowKey, GROUP BY and DISTINCT
// aggregates build them: values that compare equal have the same part
// (joinKey: 1 and 1.0, 0.0 and -0.0, '1 mon' and '30 days'), and so do
// NULLs. Each part starts with its length, so the parts of two rows can't
// run into each other, whatever bytes their strings hold.
func writeKey(b *strings.Builder, v Value) {
	if v.Null {
		b.WriteByte('N')
		return
	}
	k, _ := joinKey(v)
	b.WriteString(strconv.Itoa(len(k)))
	b.WriteByte(':')
	b.WriteString(k)
}

// keyTypes returns the type of each expression whose values keyAs keys,
// typeNull where it isn't known.
func keyTypes(exprs []Expr, types map[string]ColType) []ColType {
	ts := make([]ColType, len(exprs))
	for i, x := range exprs {
		if t, err := inferType(x, types, nil); err == nil {
			ts[i] = t
		}
	}
	return ts
}

// keyAs returns v as a value of t, the type of the expression that gave
// it, for writeKey. A CASE or COALESCE can give values of different kinds
// (COALESCE(n, '0') of an integer n gives integers and the string '0'),
// which its type holds alike, as its results show them, so they are keyed
// alike. v is unchanged if t isn't known or v doesn't convert to it.
func keyAs(v Value, t ColType, z tzZone) Value {
	if v.Null || t.Kind == KindNull || (v.T.Kind == t.Kind && v.T.TZ == t.TZ) {
		return v
	}
	if c, err := coerceIn(v, t, z); err == nil {
		return c
	}
	return v
}

func (e *executor) runSetOp(ctx context.Context, plan *selectPlan, params []Value) ([][]Value, error) {
	sp := plan.setop
	branch := func(p *selectPlan) ([][]Value, error) {
		rows, err := e.runSelect(ctx, p, params)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			if err := cancelled(ctx); err != nil {
				return nil, err
			}
			for i := range row {
				if row[i], err = coerceIn(row[i], plan.items[i].typ, e.zone()); err != nil {
					return nil, invalidArg(err)
				}
			}
		}
		return rows, nil
	}
	left, err := branch(sp.left)
	if err != nil {
		return nil, err
	}
	right, err := branch(sp.right)
	if err != nil {
		return nil, err
	}

	var out [][]Value
	seen := map[string]bool{}
	distinct := func(row []Value) {
		if k := rowKey(row); !seen[k] {
			seen[k] = true
			out = append(out, row)
		}
	}
	counts := func(rows [][]Value) map[string]int {
		m := make(map[string]int, len(rows))
		for _, r := range rows {
			m[rowKey(r)]++
		}
		return m
	}
	switch sp.op {
	case "UNION":
		if sp.all {
			out = append(left, right...)
		} else {
			for _, r := range left {
				if err := cancelled(ctx); err != nil {
					return nil, err
				}
				distinct(r)
			}
			for _, r := range right {
				if err := cancelled(ctx); err != nil {
					return nil, err
				}
				distinct(r)
			}
		}
	case "INTERSECT":
		inRight := counts(right)
		for _, r := range left {
			if err := cancelled(ctx); err != nil {
				return nil, err
			}
			k := rowKey(r)
			if inRight[k] == 0 {
				continue
			}
			if sp.all {
				inRight[k]--
				out = append(out, r)
			} else {
				distinct(r)
			}
		}
	case "EXCEPT":
		inRight := counts(right)
		for _, r := range left {
			if err := cancelled(ctx); err != nil {
				return nil, err
			}
			k := rowKey(r)
			if inRight[k] > 0 {
				if sp.all {
					inRight[k]--
				}
				continue
			}
			if sp.all {
				out = append(out, r)
			} else {
				distinct(r)
			}
		}
	default:
		return nil, errorf(adbc.StatusNotImplemented, "unsupported set operation %s", sp.op)
	}

	if len(sp.order) > 0 {
		order := make([]planOrder, len(sp.order))
		keys := make([][]Value, len(out))
		for i, k := range sp.order {
			order[i] = planOrder{desc: k.desc, nullsFirst: k.nullsFirst}
		}
		for r, row := range out {
			for _, k := range sp.order {
				keys[r] = append(keys[r], row[k.col])
			}
		}
		idx := make([]int, len(out))
		for i := range idx {
			idx[i] = i
		}
		if err := cancelled(ctx); err != nil {
			return nil, err
		}
		sort.SliceStable(idx, func(a, b int) bool { return lessKeys(keys[idx[a]], keys[idx[b]], order) })
		sorted := make([][]Value, len(out))
		for i, j := range idx {
			sorted[i] = out[j]
		}
		out = sorted
	}
	return applyLimit(out, plan.sel.Offset, plan.sel.Limit), nil
}
