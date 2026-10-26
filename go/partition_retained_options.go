// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package redis

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
)

const (
	OptionStringPartitionResultNamespace = "adbc.redis.partition_result_namespace"
	OptionStringPartitionReaderOnly      = "adbc.redis.partition_reader_only"
	OptionStringPartitionResultLimits    = "adbc.redis.partition_result_limits"
	OptionStringPartitionResultMode      = "adbc.redis.partition_result_mode"
	OptionIntPartitionResultTTL          = "adbc.redis.partition_result_ttl"
	OptionStringPartitionExecutionLimits = "adbc.redis.partition_execution_limits"
)

func retainedJSON(value any) string { data, _ := json.Marshal(value); return string(data) }
func retainedDecodeOptions(data string, value any) error {
	if len(data) > 4096 || !strings.HasPrefix(strings.TrimSpace(data), "{") {
		return errorf(adbc.StatusInvalidArgument, "retained options exceed size limit")
	}
	decoder := json.NewDecoder(bytes.NewBufferString(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil || decoder.Decode(new(any)) != io.EOF {
		return errorf(adbc.StatusInvalidArgument, "invalid retained resource options")
	}
	return nil
}
func (d *databaseImpl) resultLimits() retainedLimits {
	if d.retainedLimits.JobBytes == 0 {
		return defaultRetainedLimits()
	}
	return d.retainedLimits
}
func (s *statementImpl) resultMode() string {
	if s.retainedMode == "" {
		return "live"
	}
	return s.retainedMode
}
func (s *statementImpl) resultTTL() time.Duration {
	if s.retainedTTL == 0 {
		return 5 * time.Minute
	}
	return s.retainedTTL
}
func (s *statementImpl) executionLimits() retainedExecutionLimits {
	if s.retainedExecutionLimits.Work == 0 {
		return defaultRetainedExecutionLimits()
	}
	return s.retainedExecutionLimits
}
func (s *statementImpl) checkExecution() error {
	if err := s.checkOpen(); err != nil {
		return err
	}
	if s.conn.readerOnly {
		return errorf(adbc.StatusInvalidState, "retained reader-only connections do not execute SQL or ingestion")
	}
	return nil
}
