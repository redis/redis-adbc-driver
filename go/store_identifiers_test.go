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

import "testing"

func TestResolveColumnNamesExactly(t *testing.T) {
	meta := &tableMeta{Columns: []columnMeta{{Name: "AdvEngineID"}, {Name: "id"}}}
	for _, tc := range []struct {
		name  string
		index int
		found bool
	}{
		{"advengineid", -1, false}, {"AdvEngineID", 0, true}, {"id", 1, true}, {"ID", -1, false},
	} {
		if got, ok := meta.resolve(tc.name); got != tc.index || ok != tc.found {
			t.Fatalf("resolve(%q) = (%d, %v), want (%d, %v)", tc.name, got, ok, tc.index, tc.found)
		}
	}
}
