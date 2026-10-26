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
	"context"
	"encoding/json"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
)

func TestPartitionSQLScope(t *testing.T) {
	for _, q := range []string{
		"SELECT * FROM t", "SELECT t.id AS x FROM t WHERE id >= 2 AND label IS NOT NULL",
		"SELECT id FROM t WHERE __rowid = 1", "SELECT * FROM t WHERE id=?",
		"SELECT id+1 FROM t", "SELECT * FROM t WHERE id::text='1'",
		"SELECT CASE WHEN id IS NULL THEN 0 ELSE ABS(id) END FROM t",
		"SELECT LOWER(label), COALESCE(id, 0) FROM t", "SELECT id FROM t WHERE id IN (1, 2, 3)",
	} {
		if _, err := partitionSelect(q); err != nil {
			t.Errorf("%s: %v", q, err)
		}
	}
	for _, q := range []string{
		"SELECT 1", "SELECT * FROM t ORDER BY id", "SELECT * FROM t LIMIT 2", "SELECT * FROM t OFFSET 2",
		"SELECT count(*) FROM t", "SELECT DISTINCT id FROM t", "SELECT id FROM t GROUP BY id",
		"SELECT * FROM t JOIN x ON t.id=x.id", "WITH x AS (SELECT * FROM t) SELECT * FROM x",
		"SELECT * FROM (SELECT * FROM t) x", "SELECT * FROM t UNION ALL SELECT * FROM t",
		"SELECT row_number() OVER () FROM t", "SELECT * FROM t WHERE id IN (SELECT id FROM x)",
		"SELECT * FROM t WHERE random() > 0.5", "SELECT current_user FROM t",
		"SELECT now() FROM t", "SELECT generate_series(1,2) FROM t", "SELECT count(id) FROM t",
		"SELECT * FROM t; DELETE FROM t", "DELETE FROM t",
	} {
		if _, err := partitionSelect(q); !isStatus(err, adbc.StatusNotImplemented) {
			t.Errorf("%s: expected NOT_IMPLEMENTED, got %v", q, err)
		}
	}
}

func TestPartitionDescriptorValidation(t *testing.T) {
	d := partitionID{Version: 1, Query: "SELECT * FROM t", Schema: "public", Table: "t",
		Target: "0123456789012345678901234567890123456789012345678901234567890123", Zone: "UTC", Lo: 1, Hi: 3}
	data, _ := json.Marshal(d)
	if _, err := decodePartition(data); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]byte{nil, []byte("null"), []byte("{}"), append(data, []byte(" {}")...),
		append(data[:len(data)-1:len(data)-1], []byte(",\"unknown\":true}")...), make([]byte, maxPartitionBytes+1)} {
		if _, err := decodePartition(bad); !isStatus(err, adbc.StatusInvalidArgument) {
			t.Fatalf("expected INVALID_ARGUMENT, got %v", err)
		}
	}
	for _, mutate := range []func(*partitionID){
		func(d *partitionID) { d.Version = 2 }, func(d *partitionID) { d.Lo = 0 },
		func(d *partitionID) { d.Hi = -1 }, func(d *partitionID) { d.Lo, d.Hi = 3, 1 },
		func(d *partitionID) { d.Hi = exactDoubleLimit }, func(d *partitionID) { d.Target = "" },
	} {
		bad := d
		mutate(&bad)
		data, _ := json.Marshal(bad)
		if _, err := decodePartition(data); !isStatus(err, adbc.StatusInvalidArgument) {
			t.Fatalf("%+v: %v", bad, err)
		}
	}
}

func TestPartitionCountOption(t *testing.T) {
	s := &statementImpl{}
	ctx := context.Background()
	if n, err := s.GetOptionInt(ctx, OptionIntPartitionCount); err != nil || n != 8 {
		t.Fatalf("default: %d %v", n, err)
	}
	for _, val := range []string{"0", "-1", "1025", "bad"} {
		if err := s.SetOption(ctx, OptionIntPartitionCount, val); !isStatus(err, adbc.StatusInvalidArgument) {
			t.Fatalf("%s: %v", val, err)
		}
	}
	if err := s.SetOptionInt(ctx, OptionIntPartitionCount, 3); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.GetOption(ctx, OptionIntPartitionCount); n != "3" {
		t.Fatal(n)
	}
	s.closed = true
	if err := s.SetOptionInt(ctx, OptionIntPartitionCount, 1); !isStatus(err, adbc.StatusInvalidState) {
		t.Fatal(err)
	}
}
