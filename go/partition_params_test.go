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
	"errors"
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func TestPartitionParameterEncoding(t *testing.T) {
	values := []Value{intValue(typeInt64, 1<<60+1), nullValue(typeString), binaryValue("\x00\xff\""), floatValue(typeFloat64, 1.5)}
	encoded := make([]partitionParam, len(values))
	for i, v := range values {
		encoded[i] = partitionParam{Type: v.T, Null: v.Null}
		if !v.Null {
			encoded[i].Data = []byte(encodeStored(v))
		}
	}
	data, err := json.Marshal(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &encoded); err != nil {
		t.Fatal(err)
	}
	decoded, _, err := decodePartitionParams(encoded)
	if err != nil {
		t.Fatal(err)
	}
	for i, v := range decoded {
		if v.Null != values[i].Null || encodeStored(v) != encodeStored(values[i]) {
			t.Fatalf("parameter %d changed", i)
		}
	}
	for _, p := range []partitionParam{
		{Type: typeInt64, Data: []byte("9007199254740993.0")},
		{Type: typeInt16, Data: []byte("32768")},
		{Type: typeBool, Data: []byte("2")},
		{Type: typeInt64, Null: true, Data: []byte("1")},
		{Type: ColType{Kind: KindDecimal, Precision: 99999}, Data: []byte("1")},
		{Type: typeString, Data: []byte(strings.Repeat("x", maxPartitionParamBytes))},
	} {
		if _, _, err := decodePartitionParams([]partitionParam{p}); !isStatus(err, adbc.StatusInvalidArgument) {
			t.Fatalf("accepted invalid param: %v", err)
		}
	}
}

func TestPartitionBindingLimits(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)
	for _, n := range []int{0, 1, 2} {
		b := array.NewInt64Builder(mem)
		for i := range n {
			b.Append(int64(i))
		}
		a := b.NewArray()
		b.Release()
		rec := array.NewRecordBatch(arrow.NewSchema([]arrow.Field{{Name: "", Type: arrow.PrimitiveTypes.Int64}}, nil), []arrow.Array{a}, int64(n))
		a.Release()
		s := &statementImpl{}
		if err := s.Bind(context.Background(), rec); err != nil {
			t.Fatal(err)
		}
		rec.Release()
		_, v, _, err := s.partitionParams(context.Background())
		if n == 1 && (err != nil || len(v) != 1) {
			t.Fatal(err)
		}
		if n != 1 && !isStatus(err, adbc.StatusInvalidArgument) {
			t.Fatal(err)
		}
		if s.params != nil {
			t.Fatal("binding retained")
		}
	}
}

func TestPartitionIncrementalOptions(t *testing.T) {
	s := &statementImpl{}
	ctx := context.Background()
	if v, err := s.GetOption(ctx, OptionStringIncremental); v != "false" || err != nil {
		t.Fatal(v, err)
	}
	if err := s.SetOption(ctx, OptionStringIncremental, "yes"); !isStatus(err, adbc.StatusInvalidArgument) {
		t.Fatal(err)
	}
	for _, v := range []string{"true", "false"} {
		s.partitionExecution = &incrementalPartitions{}
		if err := s.SetOption(ctx, OptionStringIncremental, v); err != nil {
			t.Fatal(err)
		}
		if s.partitionExecution != nil {
			t.Fatal("execution retained")
		}
	}
	for _, v := range []int64{-1, 1<<31 + 1} {
		if err := s.SetOptionInt(ctx, OptionIntPartitionScanCap, v); !isStatus(err, adbc.StatusInvalidArgument) {
			t.Fatal(err)
		}
	}
}

type failingPartitionParams struct{ array.RecordReader }

func (r failingPartitionParams) Err() error { return errors.New("sensitive stream diagnostic") }

func TestPartitionParameterStreamError(t *testing.T) {
	schema := arrow.NewSchema([]arrow.Field{{Name: "", Type: arrow.PrimitiveTypes.Int64}}, nil)
	r, err := array.NewRecordReader(schema, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := &statementImpl{}
	if err := s.BindStream(context.Background(), failingPartitionParams{r}); err != nil {
		t.Fatal(err)
	}
	_, _, _, err = s.partitionParams(context.Background())
	if !isStatus(err, adbc.StatusIO) || strings.Contains(err.Error(), "sensitive") {
		t.Fatal(err)
	}
	if s.params != nil {
		t.Fatal("failed stream retained")
	}
}

func TestPartitionParameterTypeMetadata(t *testing.T) {
	for _, data := range []string{
		`{"type":{"kind":"timestamp","unit":"s","precision":9},"null":true}`,
		`{"type":{"kind":"timestamp","unit":"garbage"},"null":true}`,
		`{"type":{"kind":"string","precision":10},"null":true}`,
		`{"type":{"kind":"string","unknown":true},"null":true}`,
	} {
		var p partitionParam
		if err := json.Unmarshal([]byte(data), &p); err == nil {
			t.Fatal("accepted malformed type metadata")
		}
	}
}
