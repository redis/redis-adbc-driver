// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

// Injected only into a private pinned go-redis copy. This is an owned-result
// contract experiment, not a public command API or a production driver change.
package redis

import (
	"bytes"
	"fmt"
	"io"
	"sync"
	"testing"

	"github.com/redis/go-redis/v9/internal/proto"
)

type upstreamOwnedStrings struct {
	values []string
	valid  []bool
}

// Fresh slots and ReadString payloads belong to this result. Neither later
// replies nor reuse of a parser/command may mutate a retained result. Publish
// only after a complete array; failed decoding never replaces the old result.
func upstreamReadOwnedStrings(rd *proto.Reader, width int) (upstreamOwnedStrings, error) {
	n, err := rd.ReadArrayLen()
	if err != nil {
		return upstreamOwnedStrings{}, err
	}
	if n != width {
		return upstreamOwnedStrings{}, fmt.Errorf("unexpected HMGET width: %d", n)
	}
	r := upstreamOwnedStrings{values: make([]string, n), valid: make([]bool, n)}
	for i := range n {
		r.values[i], err = rd.ReadString()
		if err == Nil {
			continue
		}
		if err != nil {
			return upstreamOwnedStrings{}, err
		}
		r.valid[i] = true
	}
	return r, nil
}

func upstreamOwnedDecode(rd *proto.Reader, rows []upstreamOwnedStrings) error {
	for i := range rows {
		row, err := upstreamReadOwnedStrings(rd, upstreamColumns)
		if err != nil {
			return err
		}
		rows[i] = row
	}
	return nil
}

func upstreamOwnedValidate(rows []upstreamOwnedStrings) error {
	for i, r := range rows {
		if len(r.values) != upstreamColumns || len(r.valid) != upstreamColumns {
			return fmt.Errorf("wrong owned width")
		}
		for j := range upstreamColumns {
			want, valid := upstreamValue(i, j)
			if r.values[j] != want || r.valid[j] != valid {
				return fmt.Errorf("owned bytes/null mismatch at %d/%d", i, j)
			}
		}
	}
	return nil
}

func upstreamOwnedConsume(rows []upstreamOwnedStrings) uint64 {
	var sum uint64
	for _, r := range rows {
		for j, s := range r.values {
			if r.valid[j] {
				sum += 1 + uint64(len(s))
				if len(s) > 0 {
					sum += uint64(s[0])
				}
			}
		}
	}
	return sum
}

func TestUpstreamOwnedHMGET(t *testing.T) {
	for _, size := range []int{32 * 1024, 1024 * 1024} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			const count = 37
			wire := upstreamWire(count)
			rd := proto.NewReaderSize(bytes.NewReader(wire), size)
			rows := make([]upstreamOwnedStrings, count)
			if err := upstreamOwnedDecode(rd, rows); err != nil {
				t.Fatal(err)
			}
			if err := upstreamOwnedValidate(rows); err != nil {
				t.Fatal(err)
			}
			if _, err := rd.PeekReplyType(); err != io.EOF {
				t.Fatal("unconsumed owned reply", err)
			}
			// Keep every original result, overwrite the parser input and decode
			// entirely different values/null masks into reused command slots.
			retained := append([]upstreamOwnedStrings(nil), rows...)
			for i := range wire {
				wire[i] = 'x'
			}
			var changed bytes.Buffer
			for range count {
				fmt.Fprintf(&changed, "*%d\r\n", upstreamColumns)
				for range upstreamColumns {
					changed.WriteString("$7\r\nchanged\r\n")
				}
			}
			rd.Reset(bytes.NewReader(changed.Bytes()))
			if err := upstreamOwnedDecode(rd, rows); err != nil {
				t.Fatal(err)
			}
			if err := upstreamOwnedValidate(retained); err != nil {
				t.Fatal("retained result changed", err)
			}
			rows[0].values[0], rows[0].valid[0] = "mutated", false
			if rows[1].values[0] != "changed" || !rows[1].valid[0] {
				t.Fatal("independent results share mutable slots")
			}
			var wg sync.WaitGroup
			for range 4 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					if err := upstreamOwnedValidate(retained); err != nil {
						t.Error(err)
					}
				}()
			}
			wg.Wait()
		})
	}
	// No fixed-slot length ceiling. NULL, empty and large binary values differ.
	large := bytes.Repeat([]byte{'x', 0, '\r', '\n'}, 4096)
	wire := []byte(fmt.Sprintf("*3\r\n_\r\n$0\r\n\r\n$%d\r\n%s\r\n+next\r\n", len(large), large))
	rd := proto.NewReader(bytes.NewReader(wire))
	r, err := upstreamReadOwnedStrings(rd, 3)
	if err != nil || r.valid[0] || !r.valid[1] || r.values[1] != "" || !r.valid[2] || r.values[2] != string(large) {
		t.Fatal("large/null/empty mismatch", err)
	}
	if got, err := rd.ReadString(); err != nil || got != "next" {
		t.Fatal("lost protocol alignment", got, err)
	}
	for _, bad := range []string{"-ERR fixture\r\n", "*1\r\n$3\r\nx", "*1\r\n-ERR field\r\n", "_\r\n"} {
		if r, err := upstreamReadOwnedStrings(proto.NewReader(bytes.NewBufferString(bad)), 1); err == nil || r.values != nil || r.valid != nil {
			t.Fatalf("partial/error published an owned result: %q %v", bad, err)
		}
	}
}

func BenchmarkUpstreamHMGETOwned(b *testing.B) {
	for _, count := range []int{256, 1024, 4096} {
		wire := upstreamWire(count)
		for _, buffer := range []int{32 * 1024, 1024 * 1024} {
			for _, mode := range []string{"slice_cmd", "strings_owned"} {
				b.Run(fmt.Sprintf("rows_%d/buffer_%d/%s", count, buffer, mode), func(b *testing.B) {
					generic := upstreamNewPage(count, "slice_cmd")
					owned := make([]upstreamOwnedStrings, count)
					r := bytes.NewReader(wire)
					rd := proto.NewReaderSize(r, buffer)
					decode := func() error {
						r.Reset(wire)
						rd.Reset(r)
						if mode == "slice_cmd" {
							return generic.decode(rd, mode)
						}
						return upstreamOwnedDecode(rd, owned)
					}
					validate := func() error {
						if mode == "slice_cmd" {
							return generic.validate(mode)
						}
						return upstreamOwnedValidate(owned)
					}
					if err := decode(); err != nil {
						b.Fatal(err)
					}
					if err := validate(); err != nil {
						b.Fatal(err)
					}
					b.ReportAllocs()
					b.SetBytes(int64(len(wire)))
					b.ResetTimer()
					var sum uint64
					for range b.N {
						if err := decode(); err != nil {
							b.Fatal(err)
						}
						if mode == "slice_cmd" {
							sum += generic.consume(mode)
						} else {
							sum += upstreamOwnedConsume(owned)
						}
					}
					b.StopTimer()
					if err := validate(); err != nil {
						b.Fatal(err)
					}
					upstreamSink = sum
				})
			}
		}
	}
}
