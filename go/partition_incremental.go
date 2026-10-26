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
	"context"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
)

const OptionStringIncremental = "adbc.statement.exec.incremental"

// There is no asynchronous execution: deferred range discovery is already a
// bounded metadata operation. All descriptors become available together. Keep
// only the schema and terminal error, never descriptors, cursors or connections.
type incrementalPartitions struct {
	schema *arrow.Schema
	err    error
}

func (s *statementImpl) ExecutePartitions(ctx context.Context) (*arrow.Schema, adbc.Partitions, int64, error) {
	if s.ipc.active() {
		return nil, adbc.Partitions{}, -1, errorf(adbc.StatusInvalidState, "IPC input/output requires ExecuteUpdate")
	}
	if err := s.checkExecution(); err != nil {
		return nil, adbc.Partitions{}, -1, err
	}
	if s.conn.store == nil || s.conn.store.isClosed() {
		return nil, adbc.Partitions{}, -1, errConnClosed
	}
	if s.incremental && s.partitionExecution != nil {
		return s.partitionExecution.schema, adbc.Partitions{}, -1, s.partitionExecution.err
	}
	ctx, transport := s.conn.executionContext(ctx, &s.streams, &s.conn.streams)
	if transport != nil {
		defer transport.finish()
	}
	if err := cancelled(ctx); err != nil {
		if s.incremental {
			s.partitionExecution = &incrementalPartitions{err: err}
		}
		return nil, adbc.Partitions{}, -1, err
	}
	var schema *arrow.Schema
	var parts adbc.Partitions
	var n int64
	var err error
	if s.resultMode() == "retained" {
		schema, parts, n, err = s.executeRetainedPartitions(ctx)
	} else {
		schema, parts, n, err = s.executePartitions(ctx)
	}
	if err == nil {
		err = cancelled(ctx)
	}
	if s.incremental {
		s.partitionExecution = &incrementalPartitions{schema: schema, err: err}
	}
	if err != nil {
		return nil, adbc.Partitions{}, -1, err
	}
	return schema, parts, n, nil
}

// Discovery has no meaningful intermediate execution progress: metadata planning
// returns every descriptor together. Reader consumption is separate.
func (s *statementImpl) GetOptionDouble(ctx context.Context, key string) (float64, error) {
	switch key {
	case "adbc.statement.exec.progress", "adbc.statement.exec.max_progress":
		return 0, errorf(adbc.StatusNotImplemented, "partition discovery progress is not supported")
	}
	return s.StatementImplBase.GetOptionDouble(ctx, key)
}
