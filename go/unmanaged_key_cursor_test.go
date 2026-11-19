// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

func keyBlob(s string) string { return fmt.Sprintf("$%d\r\n%s\r\n", len(s), s) }
func keyCursorFrame(keys []string, warning string, cursor int64) []byte {
	s := "*2\r\n%3\r\n" + keyBlob("warning") + warning + keyBlob("results") + fmt.Sprintf("*%d\r\n", len(keys))
	for _, key := range keys {
		s += "%2\r\n" + keyBlob("values") + "*0\r\n" + keyBlob("extra_attributes") + "%1\r\n" + keyBlob(keyField) + keyBlob(key)
	}
	s += keyBlob("total_results") + fmt.Sprintf(":%d\r\n:%d\r\n", len(keys), cursor)
	return []byte(s)
}
func TestUnmanagedKeyCursorExactFrames(t *testing.T) {
	want := []string{"app:" + string([]byte{0}) + "λ\r\n", "app:other"}
	for _, warning := range []string{"*0\r\n", "_\r\n", "$-1\r\n", "*-1\r\n", keyBlob("None"), "*1\r\n" + keyBlob("None")} {
		keys, cursor, w, err := parseUnmanagedKeyCursor(keyCursorFrame(want, warning, 42))
		if err != nil || w || cursor != 42 || !reflect.DeepEqual(keys, want) {
			t.Fatal(keys, cursor, w, err)
		}
	}
	_, cursor, w, err := parseUnmanagedKeyCursor(keyCursorFrame(want, "*1\r\n"+keyBlob("Timeout limit was reached"), 42))
	if err != nil || !w || cursor != 42 {
		t.Fatal(cursor, w, err)
	}
	_, _, w, err = parseUnmanagedKeyCursor(keyCursorFrame(want, "*1\r\n_\r\n", 42))
	if err != nil || !w {
		t.Fatal("nested nil warning was ignored", w, err)
	}
	frame := keyCursorFrame(want, "*0\r\n", 42)
	for end := 0; end < len(frame); end++ {
		if _, _, _, err := parseUnmanagedKeyCursor(frame[:end]); err == nil {
			t.Fatal("accepted truncated frame", end)
		}
	}
	for _, bad := range []string{
		strings.Replace(string(frame), keyBlob(keyField), keyBlob("missing"), 1),
		strings.Replace(string(frame), "%1\r\n"+keyBlob(keyField), "*2\r\n"+keyBlob(keyField), 1),
		strings.Replace(string(frame), keyBlob(want[0]), keyBlob(metaPrefix+"private"), 1),
	} {
		if _, cursor, _, err := parseUnmanagedKeyCursor([]byte(bad)); err == nil || cursor != 42 {
			t.Fatal("missing semantic refusal or cleanup cursor", cursor, err)
		}
	}
	if _, _, _, err := parseUnmanagedKeyCursor(append(frame, []byte("extra")...)); err == nil {
		t.Fatal("accepted trailing bytes")
	}
}

type rawCursorProcessor struct {
	t           *testing.T
	reply       []byte
	loss        error
	reads, dels int
}

func (p *rawCursorProcessor) Process(ctx context.Context, cmd goredis.Cmder) error {
	p.t.Helper()
	if cmd.Name() == "ft.cursor" && strings.EqualFold(fmt.Sprint(cmd.Args()[1]), "DEL") {
		p.dels++
		return nil
	}
	if once, ok := cmd.(interface{ NoRetry() bool }); !ok || !once.NoRetry() {
		p.t.Fatal("raw cursor read permits retry")
	}
	p.reads++
	cmd.SetErr(p.loss)
	if p.loss == nil {
		cmd.(interface{ SetVal([]byte) }).SetVal(p.reply)
	}
	return p.loss
}
func TestUnmanagedKeyCursorRefusalClosesWithoutReplay(t *testing.T) {
	for _, test := range []struct {
		name  string
		reply []byte
		loss  error
	}{
		{name: "warning", reply: keyCursorFrame([]string{"app:one"}, "*1\r\n"+keyBlob("partial result"), 42)},
		{name: "invalid key", reply: keyCursorFrame([]string{metaPrefix + "private"}, "*0\r\n", 42)},
		{name: "lost reply", loss: errors.New("unknown read outcome")},
		{name: "malformed reply", reply: []byte("*2\r\n")},
		{name: "native error", reply: []byte("-NOPERM original refusal\r\n")},
		{name: "blob error", reply: []byte(strings.Replace(keyBlob("NOPERM original refusal"), "$", "!", 1))},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := &rawCursorProcessor{t: t, reply: test.reply, loss: test.loss}
			c := &aggCursor{snapshot: true, node: searchConn{p}, index: "idx", cursor: 42, keyOnly: true}
			if _, err := c.nextKeys(context.Background()); err == nil {
				t.Fatal("missing refusal")
			}
			if p.reads != 1 || p.dels != 1 || c.cursor != 0 {
				t.Fatal("replayed or leaked cursor", p.reads, p.dels, c.cursor)
			}
		})
	}
}

func FuzzUnmanagedKeyCursor(f *testing.F) {
	f.Add(keyCursorFrame([]string{"app:" + string([]byte{0}) + "key"}, "*0\r\n", 42))
	f.Add([]byte("*2\r\n%1\r\n$7\r\nresults\r\n*0\r\n:0\r\n"))
	f.Fuzz(func(t *testing.T, frame []byte) {
		if len(frame) > 1<<20 {
			t.Skip()
		}
		_, _, _, _ = parseUnmanagedKeyCursor(frame)
	})
}
