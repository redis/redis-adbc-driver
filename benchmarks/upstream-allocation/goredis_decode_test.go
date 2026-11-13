// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

// The runner injects this file into a PRIVATE COPY of pinned go-redis source.
// This deliberately exercises its actual internal parser, never a Redis server.
package redis

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"testing"

	"github.com/redis/go-redis/v9/internal/proto"
)

const upstreamColumns, upstreamSlotBytes = 105, 64

func upstreamValue(row, col int) (string, bool) {
	if (row+col)%17 == 0 {
		return "", false
	}
	if (row+col)%13 == 0 {
		return "", true
	}
	if col%7 == 0 {
		return fmt.Sprintf("r%d:c%d:\x00\r\n☃", row, col), true
	}
	return strconv.FormatInt(9007199254740993+int64(row*upstreamColumns+col), 10), true
}

func upstreamWire(rows int) []byte {
	var out bytes.Buffer
	for row := range rows {
		fmt.Fprintf(&out, "*%d\r\n", upstreamColumns)
		for col := range upstreamColumns {
			v, valid := upstreamValue(row, col)
			if !valid {
				out.WriteString("_\r\n")
				continue
			}
			fmt.Fprintf(&out, "$%d\r\n", len(v))
			out.WriteString(v)
			out.WriteString("\r\n")
		}
	}
	return out.Bytes()
}

type upstreamPage struct {
	rows    int
	generic [][]interface{}
	strings [][]string
	valid   [][]bool
	storage []byte
	lengths [][]int
}

func upstreamNewPage(rows int, mode string) upstreamPage {
	p := upstreamPage{rows: rows}
	if mode == "slice_cmd" {
		p.generic = make([][]interface{}, rows)
		return p
	}
	p.valid = make([][]bool, rows)
	if mode == "string_slice_reuse" {
		p.strings = make([][]string, rows)
	} else {
		p.lengths = make([][]int, rows)
		p.storage = make([]byte, rows*upstreamColumns*(upstreamSlotBytes+2))
	}
	for i := range rows {
		p.valid[i] = make([]bool, upstreamColumns)
		if mode == "string_slice_reuse" {
			p.strings[i] = make([]string, upstreamColumns)
		} else {
			p.lengths[i] = make([]int, upstreamColumns)
		}
	}
	return p
}

func (p *upstreamPage) slot(row, col int) []byte {
	start := (row*upstreamColumns + col) * (upstreamSlotBytes + 2)
	return p.storage[start : start+upstreamSlotBytes+2]
}

func (p *upstreamPage) decode(rd *proto.Reader, mode string) error {
	for row := range p.rows {
		if mode == "slice_cmd" {
			var cmd SliceCmd
			if err := cmd.readReply(rd); err != nil {
				return err
			}
			p.generic[row] = cmd.val
			continue
		}
		n, err := rd.ReadArrayLen()
		if err != nil {
			return err
		}
		if n != upstreamColumns {
			return fmt.Errorf("wrong width: %d", n)
		}
		for col := range upstreamColumns {
			var err error
			if mode == "string_slice_reuse" {
				p.strings[row][col], err = rd.ReadString()
			} else {
				p.lengths[row][col], err = rd.ReadStringInto(p.slot(row, col))
			}
			p.valid[row][col] = err != Nil
			if err != nil && err != Nil {
				return err
			}
		}
	}
	return nil
}

func (p *upstreamPage) validate(mode string) error {
	for row := range p.rows {
		if mode == "slice_cmd" && len(p.generic[row]) != upstreamColumns {
			return fmt.Errorf("wrong generic width")
		}
		for col := range upstreamColumns {
			want, valid := upstreamValue(row, col)
			var got string
			var present bool
			switch mode {
			case "slice_cmd":
				v := p.generic[row][col]
				present = v != nil
				if present {
					var ok bool
					got, ok = v.(string)
					if !ok {
						return fmt.Errorf("unexpected type %T", v)
					}
				}
			case "string_slice_reuse":
				got, present = p.strings[row][col], p.valid[row][col]
			default:
				got, present = string(p.slot(row, col)[:p.lengths[row][col]]), p.valid[row][col]
			}
			if got != want || present != valid {
				return fmt.Errorf("incorrect bytes/null at %d/%d", row, col)
			}
		}
	}
	return nil
}

// Consume every field without charging fixture formatting or a second parser.
// Complete byte/type/null equality is checked before and after each timed case.
func (p *upstreamPage) consume(mode string) uint64 {
	var sum uint64
	for row := range p.rows {
		for col := range upstreamColumns {
			switch mode {
			case "slice_cmd":
				if v := p.generic[row][col]; v != nil {
					s := v.(string)
					sum += 1 + uint64(len(s))
					if len(s) > 0 {
						sum += uint64(s[0])
					}
				}
			case "string_slice_reuse":
				if p.valid[row][col] {
					s := p.strings[row][col]
					sum += 1 + uint64(len(s))
					if len(s) > 0 {
						sum += uint64(s[0])
					}
				}
			default:
				if p.valid[row][col] {
					n := p.lengths[row][col]
					sum += 1 + uint64(n)
					if n > 0 {
						sum += uint64(p.slot(row, col)[0])
					}
				}
			}
		}
	}
	return sum
}

func TestUpstreamRESP3Fixture(t *testing.T) {
	for _, mode := range []string{"slice_cmd", "string_slice_reuse", "byte_slots_prototype"} {
		for _, size := range []int{32 * 1024, 1024 * 1024} {
			p := upstreamNewPage(37, mode)
			wire := upstreamWire(37)
			r := bytes.NewReader(wire)
			rd := proto.NewReaderSize(r, size)
			for range 2 {
				if err := p.decode(rd, mode); err != nil {
					t.Fatal(mode, err)
				}
				if err := p.validate(mode); err != nil {
					t.Fatal(mode, err)
				}
				if _, err := rd.PeekReplyType(); err != io.EOF {
					t.Fatal("reply not completely consumed", err)
				}
				r.Reset(wire)
				rd.Reset(r)
			}
		}
	}
	// Existing internal bounded-buffer primitive rejects oversized payloads and
	// drains them, rather than exposing truncated bytes as a successful result.
	long := bytes.Repeat([]byte("x"), upstreamSlotBytes+10)
	wire := []byte(fmt.Sprintf("$%d\r\n%s\r\n+aligned\r\n", len(long), long))
	rd := proto.NewReader(bytes.NewReader(wire))
	if _, err := rd.ReadStringInto(make([]byte, upstreamSlotBytes)); err == nil {
		t.Fatal("oversized value accepted")
	}
	if got, err := rd.ReadString(); err != nil || got != "aligned" {
		t.Fatal("oversized value lost protocol alignment", got, err)
	}
}

var upstreamSink uint64

func BenchmarkUpstreamHMGETDecode(b *testing.B) {
	for _, rows := range []int{256, 1024, 4096} {
		wire := upstreamWire(rows)
		for _, buffer := range []int{32 * 1024, 1024 * 1024} {
			for _, mode := range []string{"slice_cmd", "string_slice_reuse", "byte_slots_prototype"} {
				b.Run(fmt.Sprintf("rows_%d/buffer_%d/%s", rows, buffer, mode), func(b *testing.B) {
					p := upstreamNewPage(rows, mode)
					r := bytes.NewReader(wire)
					rd := proto.NewReaderSize(r, buffer)
					if err := p.decode(rd, mode); err != nil {
						b.Fatal(err)
					}
					if err := p.validate(mode); err != nil {
						b.Fatal(err)
					}
					b.ReportAllocs()
					b.SetBytes(int64(len(wire)))
					b.ReportMetric(float64(rows), "rows/op")
					b.ResetTimer()
					var sum uint64
					for range b.N {
						r.Reset(wire)
						rd.Reset(r)
						if err := p.decode(rd, mode); err != nil {
							b.Fatal(err)
						}
						sum += p.consume(mode)
					}
					b.StopTimer()
					if err := p.validate(mode); err != nil {
						b.Fatal(err)
					}
					upstreamSink = sum
				})
			}
		}
	}
}
