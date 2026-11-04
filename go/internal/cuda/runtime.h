#ifndef REDIS_CUDA_RUNTIME_H
#define REDIS_CUDA_RUNTIME_H
#include <stdint.h>
#include <stddef.h>
typedef struct redis_cuda_filter redis_cuda_filter;
int redis_cuda_probe(int device, char* error, size_t capacity);
redis_cuda_filter* redis_cuda_filter_new(int device, char* error, size_t capacity);
int redis_cuda_filter_run(redis_cuda_filter*, const int64_t*, const uint8_t*, uint8_t*, int64_t, int64_t, int, char*, size_t);
void redis_cuda_filter_free(redis_cuda_filter*);
int redis_cuda_alloc(int device, void** out, size_t bytes, char*, size_t);
int redis_cuda_copy(int device, void* dst, const void* src, size_t bytes, int to_device, char*, size_t);
int redis_cuda_wait(int device, void* event, char*, size_t);
void redis_cuda_free(int device, void* pointer);
#endif
