// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"encoding/binary"
	"io"

	"github.com/apache/arrow-adbc/go/adbc"
)

// spillRowCodec writes rows of a query-local spill (sort runs, join
// partitions) as their fields' storage encodings. Field names and types are
// interned in tables that stay in memory for the spill's lifetime and are
// never persisted; a field is its name index, type index, NULL flag and
// encodeStored text. Rows must hold values that encodeStored/decodeStored
// round-trip exactly: values decoded from storage, as table scans and their
// joins produce.
type spillRowCodec struct {
	names   []string
	nameIdx map[string]uint16
	types   []ColType
	typeIdx map[ColType]uint16
}

func newSpillRowCodec() spillRowCodec {
	return spillRowCodec{nameIdx: map[string]uint16{}, typeIdx: map[ColType]uint16{}}
}

func (c *spillRowCodec) intern(name string, t ColType) (uint16, uint16, error) {
	n, ok := c.nameIdx[name]
	if !ok {
		if len(c.names) >= 1<<16 {
			return 0, 0, errorf(adbc.StatusInternal, "spill field table is full")
		}
		n = uint16(len(c.names))
		c.names = append(c.names, name)
		c.nameIdx[name] = n
	}
	i, ok := c.typeIdx[t]
	if !ok {
		if len(c.types) >= 1<<16 {
			return 0, 0, errorf(adbc.StatusInternal, "spill type table is full")
		}
		i = uint16(len(c.types))
		c.types = append(c.types, t)
		c.typeIdx[t] = i
	}
	return n, i, nil
}

// writeFields writes a row's field count and fields.
func (c *spillRowCodec) writeFields(w io.Writer, row map[string]Value) error {
	if err := binary.Write(w, binary.LittleEndian, uint32(len(row))); err != nil {
		return err
	}
	for name, v := range row {
		n, t, err := c.intern(name, v.T)
		if err != nil {
			return err
		}
		flag := byte(0)
		text := ""
		if v.Null {
			flag = 1
		} else {
			text = encodeStored(v)
		}
		if err := binary.Write(w, binary.LittleEndian, [2]uint16{n, t}); err != nil {
			return err
		}
		if _, err := w.Write([]byte{flag}); err != nil {
			return err
		}
		if err := writeSpillString(w, text); err != nil {
			return err
		}
	}
	return nil
}

// readFields reads what writeFields wrote. A record that ends early is
// io.ErrUnexpectedEOF.
func (c *spillRowCodec) readFields(r io.Reader) (map[string]Value, error) {
	var n uint32
	if err := binary.Read(r, binary.LittleEndian, &n); err != nil {
		return nil, spillRecordError(err)
	}
	row := make(map[string]Value, n)
	for range n {
		var idx [2]uint16
		if err := binary.Read(r, binary.LittleEndian, &idx); err != nil {
			return nil, spillRecordError(err)
		}
		var flag [1]byte
		if _, err := io.ReadFull(r, flag[:]); err != nil {
			return nil, spillRecordError(err)
		}
		text, err := readSpillString(r)
		if err != nil {
			return nil, spillRecordError(err)
		}
		if int(idx[0]) >= len(c.names) || int(idx[1]) >= len(c.types) {
			return nil, errorf(adbc.StatusInternal, "invalid spill record")
		}
		t := c.types[idx[1]]
		v := nullValue(t)
		if flag[0] == 0 {
			if v, err = decodeStored(text, t); err != nil {
				return nil, errorf(adbc.StatusInternal, "spill field %q: %v", c.names[idx[0]], err)
			}
		}
		row[c.names[idx[0]]] = v
	}
	return row, nil
}
