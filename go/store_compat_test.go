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

func TestResolveLegacyMixedCaseColumn(t *testing.T) {
	legacy := &tableMeta{Columns: []columnMeta{{Name: "AdvEngineID"}}}
	if got, ok := legacy.resolve("advengineid"); !ok || got != 0 {
		t.Fatalf("legacy mixed-case column resolved to (%d, %v), want (0, true)", got, ok)
	}

	current := &tableMeta{
		Columns:           []columnMeta{{Name: "AdvEngineID"}},
		FoldedIdentifiers: true,
	}
	if got, ok := current.resolve("advengineid"); ok || got != -1 {
		t.Fatalf("current quoted mixed-case column resolved to (%d, %v), want (-1, false)", got, ok)
	}
	if got, ok := current.resolve("AdvEngineID"); !ok || got != 0 {
		t.Fatalf("exact mixed-case column resolved to (%d, %v), want (0, true)", got, ok)
	}
}
