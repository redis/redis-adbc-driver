// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/array"
	goredis "github.com/redis/go-redis/v9"
)

func TestManagedStatementTimeoutOptions(t *testing.T) {
	ctx := context.Background()
	d := &databaseImpl{}
	if got, _ := d.GetOption(ctx, OptionStringStatementTimeout); got != "15m0s" {
		t.Fatal(got)
	}
	if err := d.SetOption(ctx, OptionStringStatementTimeout, "2h"); err != nil {
		t.Fatal(err)
	}
	sess := d.newSession()
	if sess.statementTimeout() != 2*time.Hour || sess.value("statement_timeout").text != "2h" {
		t.Fatal(sess.value("statement_timeout"))
	}
	c := &connectionImpl{sess: sess}
	if err := c.SetOption(ctx, OptionStringStatementTimeout, "30m"); err != nil {
		t.Fatal(err)
	}
	v, err := settings["statement_timeout"].parse([]SetValue{{Text: "125ms"}}, paramValue{})
	if err != nil {
		t.Fatal(err)
	}
	sess.vals["statement_timeout"] = v
	s := &statementImpl{conn: c}
	if got, _ := s.GetOption(ctx, OptionStringStatementTimeout); got != "125ms" {
		t.Fatal(got)
	}
	if err := s.SetOption(ctx, OptionStringStatementTimeout, "1h"); err != nil {
		t.Fatal(err)
	}
	op, cancel := statementOperationContext(s.statementTimeoutContext(ctx), sess)
	defer cancel()
	deadline, _ := op.Deadline()
	if left := time.Until(deadline); left < 59*time.Minute || left > time.Hour {
		t.Fatal(left)
	}
	for _, bad := range []string{"-1", "bad", "-1s"} {
		if s.SetOption(ctx, OptionStringStatementTimeout, bad) == nil {
			t.Fatal(bad)
		}
	}
	if err := c.SetOption(ctx, OptionStringStatementTimeout, "0"); err != nil {
		t.Fatal(err)
	}
	if sess.statementTimeout() != 0 {
		t.Fatal("connection option must reset session override")
	}
	if d.newSession().statementTimeout() != 2*time.Hour {
		t.Fatal("connection changed database default")
	}
	delete(sess.vals, "statement_timeout")
	if sess.value("statement_timeout").text != "0" {
		t.Fatal("RESET default")
	}
}

func TestManagedStatementTimeoutCallerWins(t *testing.T) {
	for _, value := range []string{"0", "1h"} {
		s := &statementImpl{}
		if err := s.SetOption(context.Background(), OptionStringStatementTimeout, value); err != nil {
			t.Fatal(err)
		}
		parent, stop := context.WithCancel(context.Background())
		ctx, cancel := statementOperationContext(s.statementTimeoutContext(parent), nil)
		stop()
		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Fatal(ctx.Err())
		}
		cancel()
		parent, stop = context.WithTimeout(context.Background(), time.Millisecond)
		ctx, cancel = statementOperationContext(s.statementTimeoutContext(parent), nil)
		<-ctx.Done()
		if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Fatal(ctx.Err())
		}
		cancel()
		stop()
	}
}

// Delay one actual client command without issuing or replaying mutations.
type statementDeadlineHook struct{ delay atomic.Int64 }

func (h *statementDeadlineHook) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (h *statementDeadlineHook) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return next
}
func (h *statementDeadlineHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		delay := time.Duration(h.delay.Swap(0))
		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				cmd.SetErr(ctx.Err())
				return ctx.Err()
			}
		}
		return next(ctx, cmd)
	}
}

type statementDeadlineReader struct {
	array.RecordReader
	delay time.Duration
}

func (r *statementDeadlineReader) Next() bool { time.Sleep(r.delay); return r.RecordReader.Next() }

func TestManagedStatementTimeoutSQL(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id BIGINT)")
	h.exec("INSERT INTO t VALUES (1)")
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(h.ctx)
	hook := &statementDeadlineHook{}
	st.(*statementImpl).conn.store.client.AddHook(hook)
	for _, tc := range []struct {
		timeout string
		want    adbc.Status
	}{{"100ms", adbc.StatusTimeout}, {"30s", adbc.StatusOK}, {"0", adbc.StatusOK}} {
		h.exec("SET statement_timeout = '" + tc.timeout + "'")
		h.expectRows("SHOW statement_timeout", tc.timeout)
		if err := st.SetSqlQuery(h.ctx, "INSERT INTO t VALUES (2)"); err != nil {
			t.Fatal(err)
		}
		hook.delay.Store(int64(200 * time.Millisecond))
		_, err = st.ExecuteUpdate(h.ctx)
		if tc.want == adbc.StatusOK {
			if err != nil {
				t.Fatal(err)
			}
		} else {
			requireStatementTimeout(t, err)
		}
	}
	h.expectRows("SELECT COUNT(*) FROM t", "3")
	noAdmissionPins(t, h, raw)
}

func TestManagedStatementTimeoutArrow(t *testing.T) {
	for _, tc := range []struct {
		name, timeout string
		caller        string
		want          adbc.Status
	}{
		{"short", "100ms", "", adbc.StatusTimeout}, {"long", "30s", "", adbc.StatusOK}, {"unlimited", "0", "", adbc.StatusOK}, {"caller_cancel", "30s", "cancel", adbc.StatusCancelled}, {"caller_deadline", "30s", "deadline", adbc.StatusTimeout},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, raw, db := newSnapshotHarnessMode(t, false, false)
			h.exec("CREATE TABLE t(id BIGINT NOT NULL)")
			h.exec("INSERT INTO t VALUES (1),(3)")
			old, err := h.conn.NewStatement(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer old.Close(h.ctx)
			if err = old.SetSqlQuery(h.ctx, "SELECT id FROM t"); err != nil {
				t.Fatal(err)
			}
			if err = old.SetOption(h.ctx, OptionStringStreamRequired, "true"); err != nil {
				t.Fatal(err)
			}
			if err = old.SetOption(h.ctx, OptionIntStreamBatchRows, "1"); err != nil {
				t.Fatal(err)
			}
			reader, count, err := old.ExecuteQuery(h.ctx)
			if err != nil || count != -1 {
				t.Fatal(count, err)
			}
			defer func() {
				if reader != nil {
					reader.Release()
				}
			}()
			conn := admissionConnection(t, db)
			st, input := admissionStatement(t, conn, "t", adbc.OptionValueIngestModeAppend, 2, nil)
			// Rebind a slow producer; the driver's reference to the old input is released.
			slow := &statementDeadlineReader{RecordReader: input, delay: 200 * time.Millisecond}
			if err = st.BindStream(h.ctx, slow); err != nil {
				t.Fatal(err)
			}
			if err = st.SetOption(h.ctx, OptionStringStatementTimeout, tc.timeout); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(h.ctx)
			defer cancel()
			if tc.caller == "deadline" {
				ctx, cancel = context.WithTimeout(h.ctx, 50*time.Millisecond)
				defer cancel()
			}
			if tc.caller == "cancel" {
				time.AfterFunc(50*time.Millisecond, cancel)
			}
			n, err := st.ExecuteUpdate(ctx)
			wantCount := "2"
			if tc.want == adbc.StatusOK {
				if err != nil || n != 1 {
					t.Fatal(n, err)
				}
				wantCount = "3"
			} else if !isStatus(err, tc.want) {
				t.Fatal(err)
			}
			h.expectRows("SELECT COUNT(*) FROM t", wantCount)
			rows := 0
			for reader.Next() {
				rows += int(reader.RecordBatch().NumRows())
			}
			if reader.Err() != nil || rows != 2 {
				t.Fatal(rows, reader.Err())
			}
			reader.Release()
			reader = nil
			noAdmissionPins(t, h, raw)
		})
	}
}

func TestStatementTimeoutApplicationOwnedModes(t *testing.T) {
	for _, live := range []bool{false, true} {
		t.Run(map[bool]string{false: "application_indexes", true: "driver_indexes"}[live], func(t *testing.T) {
			var h *sqlHarness
			var raw *goredis.Client
			var schema string
			if live {
				h, raw, _ = newSnapshotHarnessMode(t, false, false)
				enableLiveIngest(t, h)
				st, _ := unmanagedArrowInput(t, h, liveSchema, adbc.OptionValueIngestModeCreate, []string{"liveapp:seed"}, []int64{10})
				setLiveSource(t, h, st, unmanagedIngestSource{Prefix: "liveapp:"})
				if _, err := st.ExecuteUpdate(h.ctx); err != nil {
					t.Fatal(err)
				}
				schema = liveSchema
			} else {
				h, raw, schema = unmanagedIngestFixture(t, false)
			}
			for _, value := range []string{"100ms", "30s"} {
				// The session setting must also govern Arrow's direct source path.
				h.exec("SET statement_timeout = '" + value + "'")
				key := "app:" + value
				if live {
					key = "liveapp:" + value
				}
				st, input := unmanagedArrowInput(t, h, schema, adbc.OptionValueIngestModeAppend, []string{key}, []int64{20})
				slow := &statementDeadlineReader{RecordReader: input, delay: 200 * time.Millisecond}
				if err := st.BindStream(h.ctx, slow); err != nil {
					t.Fatal(err)
				}
				n, err := st.ExecuteUpdate(h.ctx)
				if value == "100ms" {
					if !isStatus(err, adbc.StatusTimeout) || n != 0 || raw.Exists(h.ctx, key).Val() != 0 {
						t.Fatal(n, err)
					}
				} else if err != nil || n != 1 {
					t.Fatal(n, err)
				}
			}
			st, err := h.conn.NewStatement(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close(h.ctx)

			for _, value := range []string{"100ms", "30s"} {
				if err = st.SetOption(h.ctx, OptionStringStatementTimeout, value); err != nil {
					t.Fatal(err)
				}
				if err = st.SetSqlQuery(h.ctx, "UPDATE "+schema+".live SET n = 99"); err != nil {
					t.Fatal(err)
				}
				impl := st.(*statementImpl)
				ctx, transport := impl.conn.executionContext(h.ctx)
				hook := &statementDeadlineHook{}
				hook.delay.Store(int64(200 * time.Millisecond))
				transport.client.AddHook(hook)
				impl.conn.store.client.AddHook(hook)
				_, err = st.ExecuteUpdate(ctx)
				transport.finish()
				if value == "100ms" {
					requireStatementTimeout(t, err)
				} else if err != nil {
					t.Fatal(err)
				}
			}

			noAdmissionPins(t, h, raw)
		})
	}
}

func requireStatementTimeout(t *testing.T, err error) {
	t.Helper()
	var ae adbc.Error
	if !errors.As(err, &ae) || ae.Code != adbc.StatusTimeout || string(ae.SqlState[:]) != "57014" {
		t.Fatalf("want TIMEOUT/57014, got %v", err)
	}
}

func TestStatementTimeoutSessionDefaults(t *testing.T) {
	h, _, _ := newSnapshotHarnessOptions(t, false, false, map[string]string{OptionStringStatementTimeout: "2h"})
	h.expectRows("SHOW statement_timeout", "2h")
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(h.ctx)
	opts := h.conn.(adbc.GetSetOptionsWithContext)
	if err = opts.SetOption(h.ctx, OptionStringStatementTimeout, "30m"); err != nil {
		t.Fatal(err)
	}
	h.expectRows("SHOW statement_timeout", "30min")
	h.exec("SET statement_timeout = '125ms'")
	if got, _ := st.(adbc.GetSetOptionsWithContext).GetOption(h.ctx, OptionStringStatementTimeout); got != "125ms" {
		t.Fatal(got)
	}
	if err = st.SetOption(h.ctx, OptionStringStatementTimeout, "1h"); err != nil {
		t.Fatal(err)
	}
	h.expectRows("SHOW statement_timeout", "125ms")
	if got, _ := st.(adbc.GetSetOptionsWithContext).GetOption(h.ctx, OptionStringStatementTimeout); got != "1h0m0s" {
		t.Fatal(got)
	}
	h.exec("RESET statement_timeout")
	h.expectRows("SHOW statement_timeout", "30min")
	h.exec("SET statement_timeout = 0; RESET ALL")
	h.expectRows("SHOW statement_timeout", "30min")
	h.exec("SET statement_timeout = '1ms'")
	if err = opts.SetOption(h.ctx, OptionStringStatementTimeout, "0"); err != nil {
		t.Fatal(err)
	}
	h.expectRows("SHOW statement_timeout", "0")
}

func TestStatementTimeoutStreamLifetime(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id BIGINT)")
	h.exec("INSERT INTO t VALUES(1),(2)")
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(h.ctx)
	for k, v := range map[string]string{OptionStringStatementTimeout: "500ms", OptionIntStreamBatchRows: "1", OptionStringStreamRequired: "true"} {
		if err = st.SetOption(h.ctx, k, v); err != nil {
			t.Fatal(err)
		}
	}
	if err = st.SetSqlQuery(h.ctx, "SELECT id FROM t"); err != nil {
		t.Fatal(err)
	}
	r, n, err := st.ExecuteQuery(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Release()
	if n != -1 {
		t.Fatal("expected owned streaming result", n)
	}
	time.Sleep(600 * time.Millisecond)
	if r.Next() {
		t.Fatal("deadline allowed buffered result after expiry")
	}
	requireStatementTimeout(t, r.Err())
	noAdmissionPins(t, h, raw)
}
