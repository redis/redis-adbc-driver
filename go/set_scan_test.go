// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"errors"
	goredis "github.com/redis/go-redis/v9"
	"reflect"
	"testing"
)

type scanSetFixture struct {
	t       *testing.T
	calls   int
	failure error
}

func (f *scanSetFixture) SScan(ctx context.Context, key string, cursor uint64, match string, count int64) *goredis.ScanCmd {
	f.t.Helper()
	if key != "set" || match != "" || count != 256 || cursor != uint64(f.calls) {
		f.t.Fatal("changed scan key/cursor", key, cursor)
	}
	cmd := goredis.NewScanCmd(ctx, nil)
	f.calls++
	switch f.calls {
	case 1:
		cmd.SetVal([]string{"a", "b"}, 1)
	case 2:
		cmd.SetVal(nil, 2)
		if f.failure != nil {
			cmd.SetErr(f.failure)
		}
	case 3:
		cmd.SetVal([]string{"a", "c"}, 0)
	default:
		f.t.Fatal("scan restarted")
	}
	return cmd
}
func TestScanSetPagesAndFailures(t *testing.T) {
	lost := errors.New("lost page")
	for _, failure := range []error{nil, lost} {
		fixture := &scanSetFixture{t: t, failure: failure}
		values, err := scanSet(context.Background(), fixture, "set").Result()
		if failure != nil {
			if err != failure || len(values) != 0 || fixture.calls != 2 {
				t.Fatal("partial inventory accepted", values, err)
			}
		} else if err != nil || !reflect.DeepEqual(values, []string{"a", "b", "c"}) || fixture.calls != 3 {
			t.Fatal(values, err, fixture.calls)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fixture := &scanSetFixture{t: t}
	if err := scanSet(ctx, fixture, "set").Err(); err != context.Canceled || fixture.calls != 0 {
		t.Fatal("canceled scan sent", err)
	}
}
