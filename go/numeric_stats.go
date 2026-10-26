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
	"math/big"
	"strings"
)

// decimalLeading is PostgreSQL's normalized base-10000 weight and first
// digit, used by select_div_scale in numeric.c (PostgreSQL 16).
func decimalLeading(n *big.Int, scale int32) (int32, int64) {
	if n.Sign() == 0 {
		return 0, 0
	}
	digits := decimalDigits(n)
	weight := int32(floorDiv(int64(digits-scale-1), 4))
	shift := scale + weight*4
	v := new(big.Int).Abs(n)
	if shift >= 0 {
		v.Quo(v, pow10(shift))
	} else {
		v.Mul(v, pow10(-shift))
	}
	return weight, v.Int64()
}

func (s *exactSum) statValue(name string, t ColType) Value {
	if t.Kind != KindDecimal {
		return s.varianceValue(name)
	}
	pop := name == "VAR_POP" || name == "STDDEV_POP"
	if s.n == 0 || s.n == 1 && !pop {
		return nullValue(t)
	}
	n := big.NewInt(s.n)
	num := new(big.Int).Mul(n, &s.sq)
	num.Sub(num, new(big.Int).Mul(&s.sum, &s.sum))
	den := new(big.Int).Set(n)
	if pop {
		den.Mul(den, n)
	} else {
		den.Mul(den, big.NewInt(s.n-1))
	}
	// Express the numerator as a decimal at twice the input scale.
	if shift := 2*s.exp + int(2*s.scale); shift < 0 {
		num.Rsh(num, uint(-shift))
	} else {
		num.Lsh(num, uint(shift))
	}
	scale := 2 * s.scale
	if num.Sign() <= 0 {
		return decimalValue(new(big.Int), 1, 0)
	}
	w1, d1 := decimalLeading(num, scale)
	w2, d2 := decimalLeading(den, 0)
	w := w1 - w2
	if d1 <= d2 {
		w--
	}
	rs := min(max(16-w*4, scale, 0), 1000)
	den.Mul(den, pow10(scale))
	scaled := new(big.Int).Mul(num, pow10(rs))
	q, rem := new(big.Int).QuoRem(scaled, den, new(big.Int))
	if new(big.Int).Lsh(rem, 1).Cmp(den) >= 0 {
		q.Add(q, big.NewInt(1))
	}
	if strings.HasPrefix(name, "STDDEV") {
		// PostgreSQL takes the square root of the rounded variance. Integer
		// square root plus a midpoint test rounds exactly without any doubles.
		radicand := new(big.Int).Mul(q, pow10(rs))
		q.Sqrt(radicand)
		midpoint := new(big.Int).Lsh(new(big.Int).Set(q), 1)
		midpoint.Add(midpoint, big.NewInt(1))
		midpoint.Mul(midpoint, midpoint)
		if new(big.Int).Lsh(radicand, 2).Cmp(midpoint) >= 0 {
			q.Add(q, big.NewInt(1))
		}
	}
	return decimalValue(q, max(decimalDigits(q), rs, 1), rs)
}

// Arrow decimals require a single scale per column; choose it from the
// computed numeric results, rather than rounding them to a planning scale.
func resolveNumericColumns(cols []resultColumn, rows [][]Value) {
	for i := range cols {
		if cols[i].Type.Kind != KindDecimal || cols[i].Type.Precision != 0 {
			continue
		}
		scale, integers := int32(0), int32(1)
		found := false
		for _, row := range rows {
			if i >= len(row) || row[i].Null {
				continue
			}
			v := row[i]
			if v.T.Kind != KindDecimal {
				continue
			}
			found = true
			scale = max(scale, v.T.Scale)
			integers = max(integers, decimalDigits(v.D)-v.T.Scale)
		}
		if !found {
			scale = 16
		}
		cols[i].Type = decimalType(max(integers+scale, 1), scale)
	}
}
