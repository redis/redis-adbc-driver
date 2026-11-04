// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
#include <assert.h>
#include <stdint.h>
#include <stdlib.h>
#include "libadbc_driver_redis.h"

static void release_error(struct AdbcError* e) {
  if (e->release) e->release(e);
}
int main(void) {
  struct AdbcDatabase db = {0};
  struct AdbcConnection conn = {0};
  struct AdbcStatement stmt = {0};
  struct AdbcError err = {0};
  assert(RedisDatabaseNew(&db, &err) == ADBC_STATUS_OK);
  assert(RedisDatabaseSetOption(&db, "uri", getenv("REDIS_URI"), &err) == ADBC_STATUS_OK);
  assert(RedisDatabaseInit(&db, &err) == ADBC_STATUS_OK);
  assert(RedisConnectionNew(&conn, &err) == ADBC_STATUS_OK);
  assert(RedisConnectionInit(&conn, &db, &err) == ADBC_STATUS_OK);
  assert(RedisStatementNew(&conn, &stmt, &err) == ADBC_STATUS_OK);
  assert(RedisStatementSetSqlQuery(&stmt, "SELECT 42", &err) == ADBC_STATUS_OK);
  // Neither address may be dereferenced, narrowed or copied.
  assert(RedisStatementSetSubstraitPlan(&stmt, (const uint8_t*)1, (size_t)1024*1024+1, &err) == ADBC_STATUS_INVALID_ARGUMENT);
  release_error(&err);
  assert(RedisStatementSetSubstraitPlan(&stmt, NULL, 1, &err) == ADBC_STATUS_INVALID_ARGUMENT);
  release_error(&err);
  assert(RedisStatementSetSubstraitPlan(&stmt, (const uint8_t*)1, SIZE_MAX, &err) == ADBC_STATUS_INVALID_ARGUMENT);
  release_error(&err);
  // Invalid plans preserve SQL and must not poison the process.
  int64_t affected = -1;
  assert(RedisStatementExecuteQuery(&stmt, NULL, &affected, &err) == ADBC_STATUS_OK);
  assert(RedisStatementRelease(&stmt, &err) == ADBC_STATUS_OK);
  assert(RedisConnectionRelease(&conn, &err) == ADBC_STATUS_OK);
  assert(RedisDatabaseRelease(&db, &err) == ADBC_STATUS_OK);
  return 0;
}
