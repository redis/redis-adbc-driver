// Copyright (c) 2026 ADBC Drivers Contributors
// SPDX-License-Identifier: Apache-2.0
#pragma once

#include "duckdb.hpp"

namespace duckdb {
class RedisAdbcExtension : public Extension {
public:
  void Load(ExtensionLoader &loader) override;
  std::string Name() override { return "redis_adbc"; }
  std::string Version() const override { return "v0.1.0"; }
};
} // namespace duckdb
