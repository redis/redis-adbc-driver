// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package redis

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"

	goredis "github.com/redis/go-redis/v9"
)

func textEncodedKind(k Kind) bool {
	return k == KindBool || k == KindDate || k == KindTime || k == KindTimeTZ || k == KindTimestamp
}

func (c columnMeta) decode(s string) (Value, error) {
	if c.Encoding != "text" {
		return decodeStored(s, c.Type)
	}
	v, err := convertValue(s, c.Type)
	if err != nil || v.Null {
		return v, err
	}
	return Coerce(v, c.Type)
}

func textFits(s string, t ColType) error {
	_, err := (columnMeta{Type: t, Encoding: "text"}).decode(s)
	return err
}

// encode preserves the boolean vocabulary/case and ISO timestamp separator
// and offset used by the application. Existing rows supply their own format;
// a newly populated field uses the format sampled at adoption.
func (c columnMeta) encode(v Value, format string) string {
	if c.Encoding != "text" {
		return encodeStored(v)
	}
	if c.Type.Kind == KindBool {
		pairs := [][2]string{{"true", "false"}, {"t", "f"}, {"yes", "no"}, {"on", "off"}, {"1", "0"}}
		out := v.Text()
		for _, p := range pairs {
			if f := strings.ToLower(format); f == p[0] || f == p[1] {
				out = p[1]
				if v.I != 0 {
					out = p[0]
				}
				break
			}
		}
		if format == strings.ToUpper(format) && format != "" {
			return strings.ToUpper(out)
		}
		if len(format) > 0 && unicode.IsUpper(rune(format[0])) {
			return strings.ToUpper(out[:1]) + out[1:]
		}
		return out
	}
	z := utcZone
	if c.Type.Kind == KindTimestamp && c.Type.TZ != "" {
		if _, _, _, off, has, err := parseTimestamp(format); err == nil && has {
			z.off = int(off)
		}
	}
	out := v.textIn(z)
	// Keep the original fractional-second width without losing newly written
	// precision. A .000 timestamp remains .000 when the next value is integral.
	if dot := strings.IndexByte(format, '.'); dot >= 0 {
		width := 0
		for i := dot + 1; i < len(format) && format[i] >= '0' && format[i] <= '9'; i++ {
			width++
		}
		at := 8
		if c.Type.Kind == KindTimestamp {
			at = 19
		}
		if width > 0 && len(out) >= at {
			end := at
			if end < len(out) && out[end] == '.' {
				end++
				for end < len(out) && out[end] >= '0' && out[end] <= '9' {
					end++
				}
			}
			digits := end - at - 1
			if digits < 0 {
				digits = 0
			}
			if digits < width {
				padding := strings.Repeat("0", width-digits)
				if end == at {
					padding = "." + padding
				}
				out = out[:end] + padding + out[end:]
			}
		}
	}
	if c.Type.Kind == KindTimestamp {
		if c.Type.TZ != "" && format != "" {
			_, _, _, _, has, err := parseTimestamp(format)
			if err == nil && !has {
				out = strings.TrimSuffix(out, "+00")
			}
			if len(format) > 19 {
				if i := strings.LastIndexAny(format[19:], "+-"); i >= 0 {
					original := format[19+i:]
					sign := "+"
					offset := z.off
					if offset < 0 {
						sign = "-"
						offset = -offset
					}
					suffix := fmt.Sprintf("%s%02d", sign, offset/3600)
					switch len(original) {
					case 5:
						suffix += fmt.Sprintf("%02d", offset/60%60)
					case 6:
						suffix += fmt.Sprintf(":%02d", offset/60%60)
					case 9:
						suffix += fmt.Sprintf(":%02d:%02d", offset/60%60, offset%60)
					}
					if j := strings.LastIndexAny(out[19:], "+-"); j >= 0 {
						out = out[:19+j] + suffix
					}
				}
			}
		}
		if strings.Contains(format, "T") {
			out = strings.Replace(out, " ", "T", 1)
		}
		if strings.Contains(format, "t") {
			out = strings.Replace(out, " ", "t", 1)
		}
		if strings.HasSuffix(format, "Z") {
			out = strings.TrimSuffix(out, "+00") + "Z"
		}
		if strings.HasSuffix(format, "z") {
			out = strings.TrimSuffix(out, "+00") + "z"
		}
	}
	return out
}

func (s *store) preserveTextFormats(ctx context.Context, meta *tableMeta, changes []rowChange) error {
	cols := map[string]columnMeta{}
	for _, c := range meta.Columns {
		if c.Encoding == "text" {
			cols[c.field()] = c
		}
	}
	if len(cols) == 0 {
		return nil
	}
	for start := 0; start < len(changes); start += pipelineChunk {
		pipe := s.redis(ctx).Pipeline()
		type field struct {
			change, pos int
			col         columnMeta
			cmd         *goredis.StringCmd
		}
		var fields []field
		for i := start; i < min(start+pipelineChunk, len(changes)); i++ {
			for j := 0; j < len(changes[i].set); j += 2 {
				if c, ok := cols[changes[i].set[j].(string)]; ok {
					fields = append(fields, field{i, j + 1, c, pipe.HGet(ctx, changes[i].key, c.field())})
				}
			}
		}
		if len(fields) == 0 {
			continue
		}
		if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, goredis.Nil) {
			return wrapRedis(err, "failed to read application text formats")
		}
		for _, f := range fields {
			old, err := f.cmd.Result()
			if errors.Is(err, goredis.Nil) {
				continue
			}
			if err != nil {
				return err
			}
			v, err := f.col.decode(changes[f.change].set[f.pos].(string))
			if err != nil {
				return err
			}
			changes[f.change].set[f.pos] = f.col.encode(v, old)
		}
	}
	return nil
}
