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

import "strings"

// jsonConcat applies PostgreSQL's top-level jsonb concatenation rules.
func jsonConcat(l, r Value) (Value, error) {
	if l.Null || r.Null {
		return nullValue(typeString), nil
	}
	a, err := parseJSON(l.Text(), true)
	if err != nil {
		return Value{}, err
	}
	b, err := parseJSON(r.Text(), true)
	if err != nil {
		return Value{}, err
	}
	var out *jsonNode
	if a.kind == jsonObject && b.kind == jsonObject {
		out = &jsonNode{kind: jsonObject, keys: append(a.keys, b.keys...), items: append(a.items, b.items...)}
	} else {
		array := func(n *jsonNode) []*jsonNode {
			if n.kind == jsonArray {
				return n.items
			}
			return []*jsonNode{n}
		}
		out = &jsonNode{kind: jsonArray, items: append(array(a), array(b)...)}
	}
	toJSONB(out)
	var text strings.Builder
	if err := writeJSONB(&text, out); err != nil {
		return Value{}, err
	}
	return stringValue(text.String()), nil
}
