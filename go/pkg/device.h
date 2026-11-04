// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
#ifndef REDIS_ARROW_DEVICE_H
#define REDIS_ARROW_DEVICE_H
#include "adbc.h"
#include <stddef.h>
static inline void RedisReleaseCPUStream(struct ArrowArrayStream* stream) { if(stream->release)stream->release(stream); }
static inline void RedisReleaseHostArray(struct ArrowArray* array) { if(array->release)array->release(array); }
static inline void RedisReleaseBorrowedSchema(struct ArrowSchema* schema) { if(schema->release)schema->release(schema); }
#ifndef ARROW_C_DEVICE_DATA_INTERFACE
#define ARROW_C_DEVICE_DATA_INTERFACE
typedef int32_t ArrowDeviceType;
#define ARROW_DEVICE_CPU 1
#define ARROW_DEVICE_CUDA 2
struct ArrowDeviceArray {
  struct ArrowArray array;
  int64_t device_id;
  ArrowDeviceType device_type;
  void* sync_event;
  int64_t reserved[3];
};
#endif
#ifndef ARROW_C_DEVICE_STREAM_INTERFACE
#define ARROW_C_DEVICE_STREAM_INTERFACE
struct ArrowDeviceArrayStream {
  ArrowDeviceType device_type;
  int (*get_schema)(struct ArrowDeviceArrayStream*, struct ArrowSchema*);
  int (*get_next)(struct ArrowDeviceArrayStream*, struct ArrowDeviceArray*);
  const char* (*get_last_error)(struct ArrowDeviceArrayStream*);
  void (*release)(struct ArrowDeviceArrayStream*);
  void* private_data;
};
#endif

#ifdef __cplusplus
extern "C" {
#endif

// Versioned standalone staging capability. No manager handle is accepted.
ADBC_EXPORT int RedisDeviceInterfaceVersion(void);
// Returns 0 when ready, EAGAIN while a producer event is pending. Never reads
// data buffers and never consumes the input. CPU descriptors must have no event.
ADBC_EXPORT int RedisDeviceArrayEventReady(const struct ArrowDeviceArray*, char*, size_t);

// Borrow input descriptors and buffers. Outputs must be zero initialized and
// released independently through array.release. Copies are synchronized.
ADBC_EXPORT int RedisDeviceArrayFromHost(const struct ArrowArray*, const struct ArrowSchema*, int64_t device, struct ArrowDeviceArray*, char* error, size_t capacity);
ADBC_EXPORT int RedisDeviceArrayToHost(const struct ArrowDeviceArray*, const struct ArrowSchema*, struct ArrowArray*, char* error, size_t capacity);

// Driver-specific extensions; these do not add slots to the standard ADBC ABI.
ADBC_EXPORT AdbcStatusCode RedisStatementExecuteDevice(struct AdbcStatement*, int64_t device, struct ArrowDeviceArrayStream*, int64_t* affected, struct AdbcError*);
ADBC_EXPORT AdbcStatusCode RedisStatementBindDevice(struct AdbcStatement*, struct ArrowDeviceArray*, struct ArrowSchema*, struct AdbcError*);

// Internal: moves ownership of a CPU stream into the device stream on success.
int RedisDeviceStreamInit(struct ArrowArrayStream*, int64_t, struct ArrowDeviceArrayStream*, char*, size_t);
// Borrow a flat schema through independently releasable wrapper structs.
// Strings and metadata remain borrowed until this view is released/imported.
ADBC_EXPORT int RedisDeviceSchemaBorrow(const struct ArrowSchema*, struct ArrowSchema*, char*, size_t);
#ifdef __cplusplus
}
#endif
#endif
