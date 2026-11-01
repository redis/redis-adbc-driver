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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"reflect"
	"strconv"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
)

// Keep this allowlist independent of the ordinary evaluator's function catalog:
// adding a function there does not establish portable partition semantics.
var partitionFunctions = map[string]bool{
	"MOD": true, "ABS": true, "LOWER": true, "UPPER": true, "LENGTH": true,
	"COALESCE": true, "NULLIF": true, "GREATEST": true, "LEAST": true,
}

const maxPartitionParamBytes = 256 << 10
const maxPartitionParams = 1024

// Data is base64 in JSON, preserving arbitrary binary and invalid UTF-8 bytes.
// Scalar payloads use the driver's exact HASH representation, never SQL text.
type partitionParam struct {
	Type ColType `json:"type"`
	Null bool    `json:"null"`
	Data []byte  `json:"data"`
}

// Reject unknown type attributes/units instead of inheriting the forgiving
// metadata decoder used to read old on-disk table schemas.
func (p *partitionParam) UnmarshalJSON(data []byte) error {
	var wire struct {
		Type json.RawMessage `json:"type"`
		Null bool            `json:"null"`
		Data []byte          `json:"data"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&wire); err != nil {
		return err
	}
	var j colTypeJSON
	dec = json.NewDecoder(bytes.NewReader(wire.Type))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&j); err != nil {
		return err
	}
	if j.Unit != "" && j.Unit != "s" && j.Unit != "ms" && j.Unit != "us" && j.Unit != "ns" {
		return fmt.Errorf("invalid parameter time unit")
	}
	if err := json.Unmarshal(wire.Type, &p.Type); err != nil {
		return err
	}
	if (p.Type.Kind == KindTime || p.Type.Kind == KindTimestamp) != (j.Unit != "") {
		return fmt.Errorf("invalid parameter time unit")
	}
	if !validPartitionParamType(p.Type) {
		return fmt.Errorf("invalid parameter type")
	}
	canonical, err := json.Marshal(p.Type)
	if err != nil {
		return fmt.Errorf("invalid parameter type")
	}
	var normalized colTypeJSON
	if err := json.Unmarshal(canonical, &normalized); err != nil || !reflect.DeepEqual(normalized, j) {
		return fmt.Errorf("noncanonical parameter type")
	}

	p.Null, p.Data = wire.Null, wire.Data
	return nil
}

func validPartitionParamType(t ColType) bool {
	if t.Rec != nil || t.Alias != "" || t.Collation != "" || t.Length != 0 || t.Fixed {
		return false
	}
	switch t.Kind {
	case KindNull, KindBool, KindInt16, KindInt32, KindInt64, KindFloat32, KindFloat64, KindString, KindBinary, KindDate, KindInterval:
		return t.Precision == 0 && t.Scale == 0 && t.TZ == "" && t.Unit == 0
	case KindDecimal:
		return t.Precision >= 1 && t.Precision <= 76 && t.Scale >= -76 && t.Scale <= t.Precision && t.TZ == "" && t.Unit == 0
	case KindTime, KindTimestamp:
		return t.Unit >= 0 && t.Unit <= 3 && (t.TZ == "" || t.Kind == KindTimestamp && t.TZ == "UTC") && t.Precision >= 0 && t.Precision <= 9 && t.Scale == 0
	}
	return false
}

func decodePartitionParams(encoded []partitionParam) ([]Value, []ColType, error) {
	bad := func() ([]Value, []ColType, error) {
		return nil, nil, errorf(adbc.StatusInvalidArgument, "invalid partition parameters")
	}
	if len(encoded) == 0 {
		return nil, nil, nil
	}
	data, err := json.Marshal(encoded)
	if err != nil || len(data) > maxPartitionParamBytes || len(encoded) > maxPartitionParams {
		return bad()
	}
	values, types := make([]Value, len(encoded)), make([]ColType, len(encoded))
	for i, p := range encoded {
		if !validPartitionParamType(p.Type) {
			return bad()
		}
		types[i] = p.Type
		if p.Null {
			if len(p.Data) != 0 {
				return bad()
			}
			values[i] = nullValue(p.Type)
			continue
		}
		if p.Type.Kind == KindNull {
			return bad()
		}
		// decodeStored tolerates numeric Search reducer doubles. Partition integers
		// must instead be exact, canonical decimal integers of the advertised width.
		bits := 0
		switch p.Type.Kind {
		case KindBool:
			bits = 8
		case KindInt16:
			bits = 16
		case KindInt32, KindDate:
			bits = 32
		case KindInt64, KindTime, KindTimestamp:
			bits = 64
		}
		if bits != 0 {
			n, err := strconv.ParseInt(string(p.Data), 10, bits)
			if err != nil || p.Type.Kind == KindBool && n != 0 && n != 1 {
				return bad()
			}
		}
		v, err := decodeStored(string(p.Data), p.Type)
		if err != nil || encodeStored(v) != string(p.Data) {
			return bad()
		}
		if p.Type.Kind == KindDecimal && len(new(big.Int).Abs(v.D).Text(10)) > int(p.Type.Precision) {
			return bad()
		}
		v.T = p.Type
		values[i] = v
	}
	return values, types, nil
}

func checkPartitionParams(query string, n int) error {
	parsed, err := ParseScript(query)
	if err != nil {
		return invalidArg(err)
	}
	if len(parsed) != 1 || parsed[0].NumParams > n {
		return errorf(adbc.StatusInvalidArgument, "partition query has unbound parameters")
	}
	return nil
}

// Consume and release the binding on every outcome. One parameter row is the
// portable contract; do not drain a multi-row stream into unbounded memory.
func (s *statementImpl) partitionParams(ctx context.Context) ([]partitionParam, []Value, []ColType, error) {
	if s.params == nil {
		return nil, nil, nil, nil
	}
	defer s.clearParams()
	if err := s.prepareExtensionInput(); err != nil {
		return nil, nil, nil, err
	}
	bad := func() ([]partitionParam, []Value, []ColType, error) {
		return nil, nil, nil, errorf(adbc.StatusInvalidArgument, "partition binding requires exactly one row, at most 1024 fields/batches and 256 KiB of encoded parameters")
	}
	schema := s.params.Schema()
	if schema.NumFields() > maxPartitionParams {
		return bad()
	}
	encoded := make([]partitionParam, schema.NumFields())
	for i, f := range schema.Fields() {
		t, err := colTypeFromField(f)
		if f.Type.ID() == arrow.NULL {
			t, err = typeNull, nil
		}
		if err != nil || !validPartitionParamType(t) {
			return nil, nil, nil, errorf(adbc.StatusNotImplemented, "unsupported partition parameter type at field %d", i+1)
		}
		encoded[i].Type = t
	}
	rows, batches := int64(0), 0
	payloadBytes := 0
	for s.params.Next() {
		if err := cancelled(ctx); err != nil {
			return nil, nil, nil, err
		}
		batches++
		rec := s.params.RecordBatch()
		if err := validateInputRecord(rec); err != nil {
			return nil, nil, nil, invalidArg(err)
		}
		rows += rec.NumRows()
		if rows > 1 || batches > 1024 {
			return bad()
		}
		if rec.NumRows() == 0 {
			continue
		}
		for i := range encoded {
			if s.resultMode() == "retained" && !rec.Column(i).IsNull(0) {
				if sized, ok := rec.Column(i).(interface{ ValueLen(int) int }); ok && sized.ValueLen(0) > 64<<10 {
					return bad()
				}
				if fixed, ok := rec.Column(i).DataType().(*arrow.FixedSizeBinaryType); ok && fixed.ByteWidth > 64<<10 {
					return bad()
				}
			}
			v, err := valueAt(rec.Column(i), 0)
			if err != nil {
				return bad()
			}
			p := &encoded[i]
			p.Null = v.Null
			if !v.Null {
				if len(v.S) > maxPartitionParamBytes {
					return bad()
				}
				p.Data = []byte(encodeStored(v))
				payloadBytes += len(p.Data)
				if payloadBytes > maxPartitionParamBytes {
					return bad()
				}
			}
		}
	}
	if err := s.params.Err(); err != nil && !errors.Is(err, io.EOF) {
		var ae adbc.Error
		if errors.As(err, &ae) {
			return nil, nil, nil, err
		}
		return nil, nil, nil, errorf(adbc.StatusIO, "failed to read partition parameter stream")
	}
	if rows != 1 {
		return bad()
	}
	values, types, err := decodePartitionParams(encoded)
	return encoded, values, types, err
}
