// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"math"
	"strings"
)

func pgRegrName(name string) bool {
	return name == "CORR" || name == "COVAR_POP" || name == "COVAR_SAMP" || strings.HasPrefix(name, "REGR_")
}

type pgRegrAgg struct {
	name                     string
	n, sx, sy, sxx, syy, sxy float64
}

func (a *pgRegrAgg) add(in []Value) error {
	if in[0].Null || in[1].Null {
		return nil
	}
	y, err := Coerce(in[0], typeFloat64)
	if err != nil {
		return err
	}
	x, err := Coerce(in[1], typeFloat64)
	if err != nil {
		return err
	}
	old := a.n
	oldSX, oldSY := a.sx, a.sy
	a.n++
	a.sx += x.F
	a.sy += y.F
	finiteX := !math.IsNaN(x.F) && !math.IsInf(x.F, 0)
	finiteY := !math.IsNaN(y.F) && !math.IsInf(y.F, 0)
	if !finiteX {
		a.sxx = math.NaN()
	}
	if !finiteY {
		a.syy = math.NaN()
	}
	if !finiteX || !finiteY {
		a.sxy = math.NaN()
	}
	if old > 0 {
		dx := x.F*a.n - a.sx
		dy := y.F*a.n - a.sy
		factor := 1 / (a.n * old)
		if finiteX {
			a.sxx += dx * dx * factor
		}
		if finiteY {
			a.syy += dy * dy * factor
		}
		if finiteX && finiteY {
			a.sxy += dx * dy * factor
		}
	}
	if finiteX && (math.IsInf(a.sx, 0) && !math.IsInf(oldSX, 0) || math.IsInf(a.sxx, 0)) ||
		finiteY && (math.IsInf(a.sy, 0) && !math.IsInf(oldSY, 0) || math.IsInf(a.syy, 0)) ||
		finiteX && finiteY && math.IsInf(a.sxy, 0) {
		return stateErr(stateNumericRange, "value out of range: overflow")
	}

	return nil
}
func (a *pgRegrAgg) result(_ *evalEnv, t ColType) (Value, error) {
	if a.name == "REGR_COUNT" {
		return intValue(typeInt64, int64(a.n)), nil
	}
	if a.n == 0 {
		return nullValue(t), nil
	}
	var v float64
	switch a.name {
	case "CORR":
		if a.sxx <= 0 || a.syy <= 0 {
			return nullValue(t), nil
		}
		v = a.sxy / math.Sqrt(a.sxx) / math.Sqrt(a.syy)
	case "COVAR_POP":
		v = a.sxy / a.n
	case "COVAR_SAMP":
		if a.n <= 1 {
			return nullValue(t), nil
		}
		v = a.sxy / (a.n - 1)
	case "REGR_SXX":
		v = a.sxx
	case "REGR_SYY":
		v = a.syy
	case "REGR_SXY":
		v = a.sxy
	case "REGR_AVGX":
		v = a.sx / a.n
	case "REGR_AVGY":
		v = a.sy / a.n
	case "REGR_SLOPE":
		if a.sxx <= 0 {
			return nullValue(t), nil
		}
		v = a.sxy / a.sxx
	case "REGR_INTERCEPT":
		if a.sxx <= 0 {
			return nullValue(t), nil
		}
		v = (a.sy - a.sx*a.sxy/a.sxx) / a.n
	case "REGR_R2":
		if a.sxx <= 0 {
			return nullValue(t), nil
		}
		if a.syy <= 0 {
			v = 1
		} else {
			r := a.sxy / math.Sqrt(a.sxx) / math.Sqrt(a.syy)
			v = r * r
		}
	}
	return floatValue(typeFloat64, v), nil
}

type pgBitAgg struct {
	name  string
	value int64
	seen  bool
}

func (a *pgBitAgg) add(in []Value) error {
	if in[0].Null {
		return nil
	}
	if !a.seen {
		a.value = in[0].I
		a.seen = true
		return nil
	}
	switch a.name {
	case "BIT_AND":
		a.value &= in[0].I
	case "BIT_OR":
		a.value |= in[0].I
	case "BIT_XOR":
		a.value ^= in[0].I
	}
	return nil
}
func (a *pgBitAgg) result(_ *evalEnv, t ColType) (Value, error) {
	if !a.seen {
		return nullValue(t), nil
	}
	return intValue(t, a.value), nil
}
