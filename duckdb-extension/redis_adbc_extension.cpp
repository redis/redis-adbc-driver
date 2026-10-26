// Copyright (c) 2026 ADBC Drivers Contributors
// SPDX-License-Identifier: Apache-2.0
#define DUCKDB_EXTENSION_MAIN

#include "redis_adbc_extension.hpp"
#include "adbc.h"
#include "duckdb/common/arrow/arrow_wrapper.hpp"
#include "duckdb/function/table/arrow.hpp"
#include "duckdb/main/client_context.hpp"
#include "duckdb/main/extension/extension_loader.hpp"
#include "duckdb/main/settings.hpp"

extern "C" {
AdbcStatusCode AdbcDriverRedisInit(int version, void *driver, AdbcError *error);
AdbcStatusCode RedisDuckDBValidateReadQuery(const char *query, AdbcError *error);
}

namespace duckdb {
namespace {

#ifdef REDIS_ADBC_DUCKDB_V2
using RedisColumnName = Identifier;
#else
using RedisColumnName = string;
#endif

struct RedisError {
  AdbcError value = {};
  ~RedisError() {
    if (value.release) {
      value.release(&value);
    }
  }
  void Check(AdbcStatusCode status) {
    if (status != ADBC_STATUS_OK) {
      throw IOException("redis_adbc: %s", value.message ? value.message : "ADBC operation failed");
    }
  }
};

// Member destruction releases the stream before the statement, connection and
// database. Flags cover failures part way through initialization, too.
struct RedisSession {
  AdbcDriver driver = {};
  AdbcDatabase database = {};
  AdbcConnection connection = {};
  AdbcStatement statement = {};
  bool database_created = false;
  bool connection_created = false;
  bool statement_created = false;

  void Open(const string &uri, const string &query) {
    RedisError error;
    error.Check(AdbcDriverRedisInit(ADBC_VERSION_1_1_0, &driver, &error.value));
    error.Check(driver.DatabaseNew(&database, &error.value));
    database_created = true;
    error.Check(driver.DatabaseSetOption(&database, "uri", uri.c_str(), &error.value));
    error.Check(driver.DatabaseSetOption(&database, "adbc.redis.stream_results", "true", &error.value));
    error.Check(driver.DatabaseInit(&database, &error.value));
    error.Check(driver.ConnectionNew(&connection, &error.value));
    connection_created = true;
    error.Check(driver.ConnectionInit(&connection, &database, &error.value));
    error.Check(driver.StatementNew(&connection, &statement, &error.value));
    statement_created = true;
    error.Check(driver.StatementSetSqlQuery(&statement, query.c_str(), &error.value));
  }

  ~RedisSession() {
    if (statement_created) {
      RedisError error;
      driver.StatementRelease(&statement, &error.value);
    }
    if (connection_created) {
      RedisError error;
      driver.ConnectionRelease(&connection, &error.value);
    }
    if (database_created) {
      RedisError error;
      driver.DatabaseRelease(&database, &error.value);
    }
    if (driver.release) {
      RedisError error;
      driver.release(&driver, &error.value);
    }
  }
};

struct RedisBindData : TableFunctionData {
  string uri;
  string query;
  vector<LogicalType> types;
  vector<string> names;
};

struct RedisScanState : GlobalTableFunctionState {
  RedisSession session;
  ArrowArrayStreamWrapper stream;
  ArrowSchemaWrapper schema;
  ArrowTableSchema table;
  bool finished = false;
};

void CheckExternalAccess(ClientContext &context) {
  if (!Settings::Get<EnableExternalAccessSetting>(context)) {
    throw PermissionException("redis_adbc requires enable_external_access=true");
  }
}

unique_ptr<FunctionData> RedisBind(ClientContext &context, TableFunctionBindInput &input,
                                  vector<LogicalType> &types, vector<RedisColumnName> &names) {
  CheckExternalAccess(context);
  if (input.inputs[0].IsNull() || input.inputs[1].IsNull()) {
    throw InvalidInputException("redis_adbc_scan URI and query must not be NULL");
  }
  auto result = make_uniq<RedisBindData>();
  result->uri = input.inputs[0].GetValue<string>();
  result->query = input.inputs[1].GetValue<string>();
  if (result->uri.compare(0, 8, "redis://") != 0 && result->uri.compare(0, 9, "rediss://") != 0) {
    throw InvalidInputException("redis_adbc_scan requires a redis:// or rediss:// URI");
  }
  RedisError error;
  error.Check(RedisDuckDBValidateReadQuery(result->query.c_str(), &error.value));
  RedisSession session;
  session.Open(result->uri, result->query);
  ArrowSchemaWrapper schema;
  error.Check(session.driver.StatementExecuteSchema(&session.statement, &schema.arrow_schema, &error.value));
  ArrowTableSchema table;
  ArrowTableFunction::PopulateArrowTableSchema(context, table, schema.arrow_schema);
  for (int64_t i = 0; i < schema.arrow_schema.n_children; i++) {
    auto &field = *schema.arrow_schema.children[i];
    names.emplace_back(field.name ? field.name : "column" + to_string(i));
    result->names.emplace_back(field.name ? field.name : "column" + to_string(i));
    types.emplace_back(table.GetColumns().at(i)->GetDuckType());
  }
  result->types = types;
  return std::move(result);
}

unique_ptr<GlobalTableFunctionState> RedisInit(ClientContext &context, TableFunctionInitInput &input) {
  CheckExternalAccess(context);
  auto &bind = input.bind_data->Cast<RedisBindData>();
  auto result = make_uniq<RedisScanState>();
  result->session.Open(bind.uri, bind.query);
  RedisError error;
  int64_t affected = -1;
  error.Check(result->session.driver.StatementExecuteQuery(&result->session.statement,
              &result->stream.arrow_array_stream, &affected, &error.value));
  result->stream.GetSchema(result->schema);
  ArrowTableFunction::PopulateArrowTableSchema(context, result->table, result->schema.arrow_schema);
  if (result->schema.arrow_schema.n_children != int64_t(bind.types.size())) {
    throw IOException("redis_adbc result schema changed after binding");
  }
  for (idx_t i = 0; i < bind.types.size(); i++) {
    auto &field = *result->schema.arrow_schema.children[i];
    if (result->table.GetColumns().at(i)->GetDuckType() != bind.types[i] ||
        (field.name ? string(field.name) : "column" + to_string(i)) != bind.names[i]) {
      throw IOException("redis_adbc result schema changed after binding");
    }
  }
  return std::move(result);
}

unique_ptr<LocalTableFunctionState> RedisInitLocal(ExecutionContext &context, TableFunctionInitInput &input,
                                                  GlobalTableFunctionState *) {
  auto result = make_uniq<ArrowScanLocalState>(make_uniq<ArrowArrayWrapper>(), context.client);
  // Projection pushdown is disabled: DuckDB applies projections and filters
  // above the scan. Arrow batches and output columns retain their original order.
  auto &bind = input.bind_data->Cast<RedisBindData>();
  for (idx_t i = 0; i < bind.types.size(); i++) {
    result->column_ids.push_back(i);
  }
  return std::move(result);
}

void RedisScan(ClientContext &context, TableFunctionInput &input, DataChunk &output) {
  auto &state = input.global_state->Cast<RedisScanState>();
  auto &local = input.local_state->Cast<ArrowScanLocalState>();
  while (!state.finished && (!local.chunk->arrow_array.release ||
         local.chunk_offset >= idx_t(local.chunk->arrow_array.length))) {
    if (context.IsInterrupted()) {
      throw InterruptException();
    }
    auto next = state.stream.GetNextChunk();
    if (!next->arrow_array.release) {
      state.finished = true;
      break;
    }
    local.Reset();
    local.chunk = std::move(next);
  }
  if (state.finished) {
    output.SetChildCardinality(0);
    return;
  }
  auto count = MinValue<idx_t>(STANDARD_VECTOR_SIZE,
                              local.chunk->arrow_array.length - local.chunk_offset);
  output.SetChildCardinality(count);
  ArrowTableFunction::ArrowToDuckDB(local, state.table.GetColumns(), output, false);
  local.chunk_offset += count;
}

void LoadRedis(ExtensionLoader &loader) {
  TableFunction scan("redis_adbc_scan", {LogicalType::VARCHAR, LogicalType::VARCHAR},
                     RedisScan, RedisBind, RedisInit, RedisInitLocal);
  scan.projection_pushdown = false;
  scan.filter_pushdown = false;
  loader.RegisterFunction(scan);
  loader.SetDescription("Read Redis SQL results through an embedded Arrow ADBC driver");
}
} // namespace

void RedisAdbcExtension::Load(ExtensionLoader &loader) {
  LoadRedis(loader);
}
} // namespace duckdb

extern "C" {
DUCKDB_CPP_EXTENSION_ENTRY(redis_adbc, loader) {
  duckdb::LoadRedis(loader);
}
}
