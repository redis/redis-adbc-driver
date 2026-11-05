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

import "math/big"

// divideDecimal follows PostgreSQL's select_div_scale: at least 16 significant
// decimal digits, without reducing either operand's fractional precision.
func divideDecimal(a *big.Int, as int32, b *big.Int, bs int32) (Value, error) {
	if b.Sign() == 0 {
		return Value{}, stateErr(stateDivisionByZero, "division by zero")
	}
	w1, d1 := decimalLeading(a, as)
	w2, d2 := decimalLeading(b, bs)
	weight := w1 - w2
	if d1 <= d2 {
		weight--
	}
	scale := min(max(16-weight*4, as, bs, 0), int32(maxNumericScale))
	numerator := new(big.Int).Abs(a)
	denominator := new(big.Int).Abs(b)
	if shift := bs - as + scale; shift >= 0 {
		numerator.Mul(numerator, pow10(shift))
	} else {
		denominator.Mul(denominator, pow10(-shift))
	}
	q, rem := new(big.Int).QuoRem(numerator, denominator, new(big.Int))
	if new(big.Int).Lsh(rem, 1).Cmp(denominator) >= 0 {
		q.Add(q, big.NewInt(1))
	}
	if a.Sign()*b.Sign() < 0 {
		q.Neg(q)
	}
	if int64(decimalDigits(q))-int64(scale) > maxNumericIntDigits {
		return Value{}, stateErr(stateNumericRange, "numeric value out of range")
	}
	return decimalValue(q, max(decimalDigits(q), scale, 1), scale), nil
}
