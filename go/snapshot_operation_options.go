// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
)

const (
	OptionStringOperationAction = "adbc.redis.operation_action"
	OptionStringOperationHandle = "adbc.redis.operation_handle"
	OptionStringOperationStatus = "adbc.redis.operation_status"
	OptionIntOperationRetention = "adbc.redis.operation_retention_ms"
)

type snapshotOperationRequestKey struct{}
type snapshotOperationRequest struct {
	Retention time.Duration
	Handle    string
	Status    *SnapshotOperationStatus
	Noop      bool
}

func snapshotOperationRequestOf(ctx context.Context) *snapshotOperationRequest {
	request, _ := ctx.Value(snapshotOperationRequestKey{}).(*snapshotOperationRequest)
	return request
}

// SnapshotOperationError carries a queryable handle when an operation's
// retention/continuation reply is unavailable. Unwrap preserves ADBC status and
// SQLSTATE mapping across the C boundary; the message contains no endpoint.
type SnapshotOperationError struct {
	Handle string
	Err    error
}

func (e *SnapshotOperationError) Error() string {
	return fmt.Sprintf("operation %s: %v", e.Handle, e.Err)
}
func (e *SnapshotOperationError) Unwrap() error { return e.Err }

// The C adapter serializes the matched ADBC error rather than the outer Go
// error. Preserve the handle in that message as well as the original status,
// SQLSTATE and structured details.
func (e *SnapshotOperationError) As(target any) bool {
	value, ok := target.(*adbc.Error)
	if !ok || !errors.As(e.Err, value) {
		return false
	}
	value.Msg = e.Error()
	return true
}

func operationError(id string, err error) error {
	if err == nil || id == "" {
		return err
	}
	var existing *SnapshotOperationError
	if errors.As(err, &existing) {
		return err
	}
	return &SnapshotOperationError{Handle: id, Err: err}
}

func operationRetention(value time.Duration) (time.Duration, error) {
	if value == 0 {
		return snapshotOperationRetention, nil
	}
	if value < snapshotLease || value > snapshotOperationMaxRetention || value%time.Millisecond != 0 {
		return 0, errorf(adbc.StatusInvalidArgument, "operation retention must be 120000..86400000 milliseconds")
	}
	return value, nil
}

func (s *store) prepareSnapshotOperation(ctx context.Context, request *snapshotOperationRequest, kind string, affected int64, source *tableMeta) error {
	if request.Handle != "" {
		return errorf(adbc.StatusInvalidState, "one preparation request may retain only one operation")
	}
	retention, err := operationRetention(request.Retention)
	if err != nil {
		return err
	}
	plan, err := captureSnapshotOperationPlan(ctx, kind, affected, source)
	if err != nil {
		return err
	}
	// Session-scoped objects cannot transfer their authority to another process.
	// Ordinary SQL/ingest support is unchanged; this only refuses detachment.
	for key := range plan.Changed {
		parts := strings.SplitN(key, ":", 3)
		if len(parts) > 1 && isTempSchema(parts[1]) {
			return errorf(adbc.StatusNotImplemented, "temporary objects cannot be detached from their owning session")
		}
	}
	if plan.TempDead != "" || snapshotOf(ctx).maintenance {
		return errorf(adbc.StatusNotImplemented, "internal maintenance is not a detachable user operation")
	}
	request.Handle = snapshotOf(ctx).id
	action := "create"
	if request.Noop {
		action = "noop"
	}
	deadline, err := s.operationControl(ctx, action, request.Handle, snapshotOf(ctx).pin.token, snapshotOf(ctx).pin.root, retention, plan).Int64()
	if err != nil {
		return operationError(request.Handle, wrapRedis(err, "preparation retention outcome unavailable; inspect the operation handle"))
	}
	request.Status = &SnapshotOperationStatus{Handle: request.Handle, State: "ready", Kind: kind, Rows: affected, ExpiresAt: time.UnixMilli(deadline).UTC()}
	if source != nil {
		request.Status.RemainingRows = affected
	}
	if request.Noop {
		request.Status.State = "committed"
	}
	return nil
}

func (s *store) resumeSnapshotOperation(ctx context.Context, id string) (*SnapshotOperationStatus, error) {
	defer s.wakeSnapshotCompactor()
	status, err := s.snapshotOperationStatus(ctx, id)
	if err != nil {
		return nil, operationError(id, err)
	}
	if status.State == "committed" {
		return status, nil
	}
	if status.State == "aborted" {
		return status, operationError(id, errorf(adbc.StatusInvalidState, "operation was aborted"))
	}
	if status.State == "abort_only" {
		return status, operationError(id, errorf(adbc.StatusInvalidState, "operation is abort-only"))
	}
	if status.OwnerActive {
		return status, operationError(id, errorf(adbc.StatusInvalidState, "operation has a live owner"))
	}
	owner, terminal, err := s.takeSnapshotOperation(ctx, id)
	if err != nil {
		return status, operationError(id, err)
	}
	if terminal != nil {
		if terminal.State == "aborted" {
			return terminal, operationError(id, errorf(adbc.StatusInvalidState, "operation was aborted"))
		}
		return terminal, nil
	}
	defer owner.Pin.release()
	err = s.prepareOperationPublication(owner)
	if err == nil {
		err = s.publishPreparedSnapshot(owner.Context, owner.Pin, id)
	}
	if err == nil {
		status.State, status.OwnerActive, status.RemainingRows = "committed", false, 0
		status.AffectedRows = status.Rows
		return status, nil
	}
	// A successful publication wins a racing cancellation/lost reply. Otherwise
	// release only this token; pending native work remains explicitly abort-only.
	cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if observed, e := s.snapshotOperationStatus(cleanup, id); e == nil && observed.State == "committed" {
		return observed, nil
	}
	_ = s.operationControl(cleanup, "release", id, owner.Pin.token, owner.Pin.root, 0, nil).Err()
	if observed, e := s.snapshotOperationStatus(cleanup, id); e == nil {
		status = observed
	}
	return status, operationError(id, err)
}

func (s *store) abortSnapshotOperation(ctx context.Context, id string) (*SnapshotOperationStatus, error) {
	status, err := s.snapshotOperationStatus(ctx, id)
	if err != nil {
		return nil, operationError(id, err)
	}
	if status.State == "committed" || status.State == "aborted" {
		return status, nil
	}
	if status.OwnerActive {
		return status, operationError(id, errorf(adbc.StatusInvalidState, "operation has a live owner"))
	}
	result, err := s.operationControl(ctx, "abort", id, "", "", 0, nil).Result()
	if err != nil {
		return status, operationError(id, wrapRedis(err, "abort outcome unavailable; inspect the handle"))
	}
	var raw string
	switch v := result.(type) {
	case string:
		raw = v
	case []any:
		if len(v) == 2 && v[0] == "terminal" {
			raw, _ = v[1].(string)
		}
	}
	outcome, err := decodeSnapshotOperationStatus(id, raw)
	return outcome, operationError(id, err)
}

// GetOperationStatus reads a handle without replaying input or publishing work.
func GetOperationStatus(ctx context.Context, options map[string]string, handle string) (*SnapshotOperationStatus, error) {
	if !snapshotMetadataID(handle) {
		return nil, errorf(adbc.StatusInvalidArgument, "invalid operation handle")
	}
	c, err := dialHashesWithPin(ctx, options, false, false)
	if err != nil {
		return nil, err
	}
	defer c.close(ctx)
	if !c.st.snapshots {
		return nil, errorf(adbc.StatusNotFound, "operation catalog does not exist")
	}
	return c.st.snapshotOperationStatus(ctx, handle)
}

// ResumeOperation explicitly finishes one completed capture. It never reads
// application input again. Interrupted unsafe phases require explicit abort.
func ResumeOperation(ctx context.Context, options map[string]string, handle string) (*SnapshotOperationStatus, error) {
	if !snapshotMetadataID(handle) {
		return nil, errorf(adbc.StatusInvalidArgument, "invalid operation handle")
	}
	c, err := dialHashesWithPin(ctx, options, false, false)
	if err != nil {
		return nil, err
	}
	defer c.close(ctx)
	if !c.st.snapshots {
		return nil, errorf(adbc.StatusNotFound, "operation catalog does not exist")
	}
	ctx, cancel := context.WithTimeout(ctx, snapshotTimeout)
	defer cancel()
	return c.st.resumeSnapshotOperation(ctx, handle)
}

// AbortOperation retires only unpublished private work. A committed operation
// returns its original outcome; abort cannot undo a committed publication.
func AbortOperation(ctx context.Context, options map[string]string, handle string) (*SnapshotOperationStatus, error) {
	if !snapshotMetadataID(handle) {
		return nil, errorf(adbc.StatusInvalidArgument, "invalid operation handle")
	}
	c, err := dialHashesWithPin(ctx, options, false, false)
	if err != nil {
		return nil, err
	}
	defer c.close(ctx)
	if !c.st.snapshots {
		return nil, errorf(adbc.StatusNotFound, "operation catalog does not exist")
	}
	return c.st.abortSnapshotOperation(ctx, handle)
}

func (s *statementImpl) operationOption(ctx context.Context, key string) (string, error) {
	switch key {
	case OptionStringOperationAction:
		return s.operationAction, nil
	case OptionStringOperationHandle:
		return s.operationHandle, nil
	case OptionStringOperationStatus:
		status, err := s.conn.store.snapshotOperationStatus(ctx, s.operationHandle)
		if err != nil {
			return "", err
		}
		raw, err := json.Marshal(status)
		return string(raw), err
	case OptionIntOperationRetention:
		value, err := operationRetention(s.operationRetention)
		return strconv.FormatInt(value.Milliseconds(), 10), err
	}
	return "", errorf(adbc.StatusNotFound, "operation option does not exist")
}

func (s *statementImpl) executeOperationAction(ctx context.Context) (int64, error) {
	ctx, transport := s.conn.executionContext(ctx, &s.streams, &s.conn.streams)
	if transport != nil {
		defer transport.finish()
	}
	ctx, cancel := context.WithTimeout(ctx, snapshotTimeout)
	defer cancel()
	var status *SnapshotOperationStatus
	var err error
	switch s.operationAction {
	case "status":
		status, err = s.conn.store.snapshotOperationStatus(ctx, s.operationHandle)
	case "resume":
		status, err = s.conn.store.resumeSnapshotOperation(ctx, s.operationHandle)
	case "abort":
		status, err = s.conn.store.abortSnapshotOperation(ctx, s.operationHandle)
	default:
		return -1, errorf(adbc.StatusInvalidArgument, "invalid operation action")
	}
	if err != nil {
		return -1, err
	}
	if s.operationAction == "status" || s.operationAction == "abort" {
		return 0, nil
	}
	return status.AffectedRows, nil
}

// PrepareCopyHashes captures and evaluates the complete native copy without
// publishing it. The second result is the retained operation; ResumeOperation
// is the explicit publication action. Caller-owned source HASHes are untouched.
func PrepareCopyHashes(ctx context.Context, options map[string]string, o HashCopyOptions, retention time.Duration) (*HashCopy, *SnapshotOperationStatus, error) {
	retention, err := operationRetention(retention)
	if err != nil {
		return nil, nil, err
	}
	request := &snapshotOperationRequest{Retention: retention}
	o.operation = request
	copied, err := CopyHashes(ctx, options, o)
	if err == nil && request.Status == nil {
		err = errorf(adbc.StatusInternal, "copy preparation did not retain an operation")
	}
	return copied, request.Status, operationError(request.Handle, err)
}

// PrepareAdoptHashes captures one managed import/refresh without publishing it.
// The returned adoption is not Applied until an explicit ResumeOperation commits
// it. Retained source images preserve typed values and original binary keys.
func PrepareAdoptHashes(ctx context.Context, options map[string]string, o HashAdoptOptions, retention time.Duration) (*HashAdoption, *SnapshotOperationStatus, error) {
	retention, err := operationRetention(retention)
	if err != nil {
		return nil, nil, err
	}
	request := &snapshotOperationRequest{Retention: retention}
	o.operation = request
	o.Apply = true
	adopted, err := AdoptHashes(ctx, options, o)
	if err == nil && request.Status == nil {
		err = errorf(adbc.StatusInternal, "import preparation did not retain an operation")
	}
	return adopted, request.Status, operationError(request.Handle, err)
}

// PrepareForeignIndexRefresh captures one external index as a managed copy.
// Other external indexes are not refreshed or removed by this operation.
func PrepareForeignIndexRefresh(ctx context.Context, options map[string]string, index string, retention time.Duration) (*SnapshotOperationStatus, error) {
	retention, err := operationRetention(retention)
	if err != nil {
		return nil, err
	}
	if index == "" || snapshotDataID(index) != "" {
		return nil, errorf(adbc.StatusInvalidArgument, "an application-owned index is required")
	}
	c, err := dialHashes(ctx, options)
	if err != nil {
		return nil, err
	}
	defer c.close(ctx)
	if err := c.st.initializeSnapshotImport(ctx); err != nil {
		return nil, err
	}
	source := &store{client: c.client, foreignIndexes: true}
	sctx, pin, err := c.st.acquireSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	defer pin.release()
	meta, err := source.foreignTable(sctx, index)
	if err != nil {
		return nil, err
	}
	reply, err := source.searchDo(sctx, index, "FT.INFO", index).Result()
	if err != nil {
		return nil, err
	}
	for _, prefix := range indexInfoOf(reply).prefixes {
		if prefixesOverlap(prefix, metaPrefix) {
			return nil, errorf(adbc.StatusInvalidArgument, "source index overlaps driver control storage")
		}
	}
	reader, err := c.st.snapshotForeignImportReader(sctx, source, meta)
	if err != nil {
		return nil, err
	}
	request := &snapshotOperationRequest{Retention: retention}
	execution := context.WithValue(ctx, snapshotOperationRequestKey{}, request)
	err = c.st.importSnapshotTable(execution, meta, foreignSchema, index, true, nil, reader)
	return request.Status, operationError(request.Handle, err)
}
