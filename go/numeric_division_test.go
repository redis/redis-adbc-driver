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
	"strings"
	"testing"
)

func TestDecimalDivisionPrecision(t *testing.T) {
	for _, tc := range []struct{ expression, want string }{
		{"5.52 / 2.4", "2.3000000000000000"},
		{"30000000000000000000000000000000000000::numeric(38,0) / 10000000000000000000000000000000000000::numeric(38,0)", "3.0000000000000000"},
		{"0.00000000000049382716::numeric(38,20) / 0.00000000000012345679::numeric(38,20)", "4.00000000000000000000"},
		{"-1::numeric / 3::numeric", "-0.33333333333333333333"},
		{"1::numeric / -8::numeric", "-0.12500000000000000000"},
		{"1::numeric(10,2) / NULLIF(2.00::numeric(10,2), 0)", "0.50000000000000000000"},
	} {
		got, _, err := evalTestExpr(t, tc.expression)
		if err != nil || got != tc.want {
			t.Errorf("%s = %s, %v; want %s", tc.expression, got, err, tc.want)
		}
	}
	if _, _, err := evalTestExpr(t, "1.0 / 0.0"); err == nil || !strings.Contains(err.Error(), "division by zero") {
		t.Fatalf("zero divisor: %v", err)
	}
}
