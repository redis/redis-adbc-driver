// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
#ifndef REDIS_INPUT_PREFLIGHT_H
#define REDIS_INPUT_PREFLIGHT_H
#include "adbc.h"
// C ownership remains with the caller on direct preflight failure.
const char* RedisInputPreflight(const struct ArrowArray*, const struct ArrowSchema*);
// Moves a caller stream into an owned checking adapter.
int RedisInputCheckedStream(struct ArrowArrayStream*);
#endif
