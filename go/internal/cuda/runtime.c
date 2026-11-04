//go:build cgo

// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
#include "runtime.h"
#include <errno.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#ifdef __linux__
#include <dlfcn.h>
#include <pthread.h>

static struct {
  void* runtime;
  int (*set_device)(int);
  int (*get_device)(int*);
  int (*malloc)(void**, size_t);
  int (*free)(void*);
  int (*memcpy)(void*, const void*, size_t, int);
  int (*event_sync)(void*);
  int (*event_query)(void*);
  const char* (*error_string)(int);
} api;
static pthread_once_t once = PTHREAD_ONCE_INIT;
static void* open_library(const char* const* names) {
  for (int i = 0; names[i]; ++i) {
    void* handle = dlopen(names[i], RTLD_NOW | RTLD_LOCAL);
    if (handle) return handle;
  }
  return NULL;
}
static void initialize(void) {
  const char* names[] = {"libcudart.so.12", "libcudart.so.13", "libcudart.so", NULL};
  api.runtime = open_library(names);
  if (!api.runtime) return;
#define LOAD(field, name) do { *(void**)(&api.field) = dlsym(api.runtime, name); if (!api.field) return; } while (0)
  LOAD(set_device, "cudaSetDevice"); LOAD(get_device, "cudaGetDevice");
  LOAD(malloc, "cudaMalloc"); LOAD(free, "cudaFree"); LOAD(memcpy, "cudaMemcpy");
  LOAD(event_sync, "cudaEventSynchronize"); LOAD(event_query, "cudaEventQuery"); LOAD(error_string, "cudaGetErrorString");
#undef LOAD
}
static int report(char* error, size_t capacity, const char* message) {
  if (error && capacity) snprintf(error, capacity, "%s", message);
  return EIO;
}
static int available(char* error, size_t capacity) {
  pthread_once(&once, initialize);
  if (!api.error_string) {
    report(error, capacity, "CUDA runtime unavailable; install NVIDIA CUDA libraries or select CPU execution");
    return ENOTSUP;
  }
  return 0;
}
static int checked(int status, char* error, size_t capacity) {
  return status ? report(error, capacity, api.error_string(status)) : 0;
}
static int enter(int device, int* previous, char* error, size_t capacity) {
  int rc = available(error, capacity);
  if (rc) return rc;
  if (device < 0) return report(error, capacity, "negative CUDA device ID");
  if ((rc = checked(api.get_device(previous), error, capacity))) return rc;
  return checked(api.set_device(device), error, capacity);
}
int redis_cuda_probe(int device, char* error, size_t capacity) {
  int previous = 0, rc = enter(device, &previous, error, capacity);
  if (rc) return rc;
  rc = checked(api.free(NULL), error, capacity); // Initialize the primary context.
  api.set_device(previous);
  return rc;
}
int redis_cuda_alloc(int device, void** out, size_t bytes, char* error, size_t capacity) {
  *out = NULL;
  int previous = 0, rc = enter(device, &previous, error, capacity);
  if (rc) return rc;
  if (bytes) rc = checked(api.malloc(out, bytes), error, capacity);
  api.set_device(previous);
  return rc;
}
int redis_cuda_copy(int device, void* dst, const void* src, size_t bytes, int to_device, char* error, size_t capacity) {
  int previous = 0, rc = enter(device, &previous, error, capacity);
  if (rc) return rc;
  if (bytes) rc = checked(api.memcpy(dst, src, bytes, to_device ? 1 : 2), error, capacity);
  api.set_device(previous);
  return rc;
}
int redis_cuda_wait(int device, void* event, char* error, size_t capacity) {
  int previous = 0, rc = enter(device, &previous, error, capacity);
  if (rc) return rc;
  if (event) rc = checked(api.event_sync(*(void**)event), error, capacity);
  api.set_device(previous);
  return rc;
}
int redis_cuda_event_ready(int device, void* event, char* error, size_t capacity) {
  int previous=0,rc=enter(device,&previous,error,capacity);
  if(rc)return rc;
  int status=event?api.event_query(*(void**)event):0;
  // cudaErrorNotReady is part of the stable CUDA runtime error ABI.
  rc=status==34?EAGAIN:checked(status,error,capacity);
  api.set_device(previous);
  return rc;
}
void redis_cuda_free(int device, void* pointer) {
  if (!pointer) return;
  int previous = 0;
  if (!enter(device, &previous, NULL, 0)) { api.free(pointer); api.set_device(previous); }
}

struct redis_cuda_filter {
  int device;
  void *driver, *compiler, *module, *kernel;
  int (*module_unload)(void*);
  int (*launch)(void*, unsigned, unsigned, unsigned, unsigned, unsigned, unsigned, unsigned, void*, void**, void**);
};
static const char kernel_source[] =
  "extern \"C\" __global__ void predicate(const long long* x, const unsigned char* valid, unsigned char* out, long long n, long long rhs, int op) {"
  "long long i = (long long)blockIdx.x * blockDim.x + threadIdx.x; if(i >= n) return;"
  "long long v=x[i]; bool match = op==0?v==rhs:op==1?v!=rhs:op==2?v<rhs:op==3?v<=rhs:op==4?v>rhs:v>=rhs;"
  "out[i] = valid[i] && match; }";
redis_cuda_filter* redis_cuda_filter_new(int device, char* error, size_t capacity) {
  if (redis_cuda_probe(device, error, capacity)) return NULL;
  redis_cuda_filter* filter = calloc(1, sizeof(*filter));
  if (!filter) { report(error, capacity, "CUDA session allocation failed"); return NULL; }
  filter->device = device;
  const char* drivers[] = {"libcuda.so.1", NULL};
  const char* compilers[] = {"libnvrtc.so.12", "libnvrtc.so.13", "libnvrtc.so", NULL};
  filter->driver = open_library(drivers); filter->compiler = open_library(compilers);
  if (!filter->driver || !filter->compiler) { report(error, capacity, "CUDA driver/NVRTC compiler unavailable"); goto fail; }
  int (*create)(void**,const char*,const char*,int,const char*const*,const char*const*);
  int (*compile)(void*,int,const char*const*);
  int (*size)(void*,size_t*); int (*ptx)(void*,char*); int (*destroy)(void**);
  int (*module_load)(void**,const void*); int (*get_function)(void**,void*,const char*);
#define SYMBOL(target, handle, name) do { *(void**)(&target)=dlsym(handle,name); if(!target) { report(error,capacity,"required CUDA symbol unavailable"); goto fail; } } while(0)
  SYMBOL(create,filter->compiler,"nvrtcCreateProgram"); SYMBOL(compile,filter->compiler,"nvrtcCompileProgram");
  SYMBOL(size,filter->compiler,"nvrtcGetPTXSize"); SYMBOL(ptx,filter->compiler,"nvrtcGetPTX"); SYMBOL(destroy,filter->compiler,"nvrtcDestroyProgram");
  SYMBOL(module_load,filter->driver,"cuModuleLoadData"); SYMBOL(get_function,filter->driver,"cuModuleGetFunction");
  SYMBOL(filter->module_unload,filter->driver,"cuModuleUnload"); SYMBOL(filter->launch,filter->driver,"cuLaunchKernel");
#undef SYMBOL
  void* program = NULL;
  if (create(&program,kernel_source,"redis_predicate.cu",0,NULL,NULL)) { report(error,capacity,"NVRTC program creation failed"); goto fail; }
  const char* options[] = {"--gpu-architecture=compute_75", "--std=c++11"};
  size_t bytes = 0;
  if (compile(program,2,options) || size(program,&bytes)) { destroy(&program); report(error,capacity,"NVRTC predicate compilation failed"); goto fail; }
  char* code = malloc(bytes);
  if (!code) { destroy(&program); report(error,capacity,"PTX allocation failed"); goto fail; }
  if (ptx(program,code)) { destroy(&program); free(code); report(error,capacity,"NVRTC PTX export failed"); goto fail; }
  destroy(&program);
  int previous = 0;
  if (enter(device,&previous,error,capacity)) { free(code); goto fail; }
  int rc = module_load(&filter->module,code);
  free(code);
  if (!rc) rc = get_function(&filter->kernel,filter->module,"predicate");
  api.set_device(previous);
  if (rc) { report(error,capacity,"CUDA predicate module loading failed"); goto fail; }
  return filter;
fail:
  redis_cuda_filter_free(filter);
  return NULL;
}
int redis_cuda_filter_run(redis_cuda_filter* filter, const int64_t* values, const uint8_t* valid, uint8_t* out, int64_t n, int64_t rhs, int op, char* error, size_t capacity) {
  if (!filter || n < 0 || n > 1048576 || op < 0 || op > 5) return report(error,capacity,"invalid CUDA predicate batch");
  if (!n) return 0;
  int previous = 0, rc = enter(filter->device,&previous,error,capacity);
  if (rc) return rc;
  void *x=NULL,*v=NULL,*result=NULL;
#define CHECK(call) do { rc=checked(call,error,capacity); if(rc) goto done; } while(0)
  CHECK(api.malloc(&x,(size_t)n*8)); CHECK(api.malloc(&v,(size_t)n)); CHECK(api.malloc(&result,(size_t)n));
  CHECK(api.memcpy(x,values,(size_t)n*8,1)); CHECK(api.memcpy(v,valid,(size_t)n,1));
  void* arguments[] = {&x,&v,&result,&n,&rhs,&op};
  if (filter->launch(filter->kernel,(unsigned)((n+255)/256),1,1,256,1,1,0,NULL,arguments,NULL)) { rc=report(error,capacity,"CUDA predicate launch failed"); goto done; }
  CHECK(api.memcpy(out,result,(size_t)n,2)); // Synchronous D2H waits for this kernel.
done:
  if(result)api.free(result);
  if(v)api.free(v);
  if(x)api.free(x);
  api.set_device(previous);
  return rc;
#undef CHECK
}
void redis_cuda_filter_free(redis_cuda_filter* filter) {
  if (!filter) return;
  int previous = 0;
  if (filter->module && !enter(filter->device,&previous,NULL,0)) { filter->module_unload(filter->module); api.set_device(previous); }
  if (filter->compiler) dlclose(filter->compiler);
  if (filter->driver) dlclose(filter->driver);
  free(filter);
}
#else
struct redis_cuda_filter { int unused; };
static int unavailable(char* e,size_t n) { if(e&&n)snprintf(e,n,"CUDA execution and CUDA C Device staging require Linux"); return ENOTSUP; }
int redis_cuda_probe(int d,char* e,size_t n){return unavailable(e,n);}
redis_cuda_filter* redis_cuda_filter_new(int d,char* e,size_t n){unavailable(e,n);return NULL;}
int redis_cuda_filter_run(redis_cuda_filter* f,const int64_t* x,const uint8_t* v,uint8_t* o,int64_t n,int64_t r,int p,char* e,size_t c){return unavailable(e,c);}
void redis_cuda_filter_free(redis_cuda_filter* f){}
int redis_cuda_alloc(int d,void** o,size_t b,char* e,size_t n){*o=NULL;return unavailable(e,n);}
int redis_cuda_copy(int d,void* o,const void* i,size_t b,int t,char* e,size_t n){return unavailable(e,n);}
int redis_cuda_wait(int d,void* v,char* e,size_t n){return unavailable(e,n);}
int redis_cuda_event_ready(int d,void* v,char* e,size_t n){return unavailable(e,n);}
void redis_cuda_free(int d,void* p){}
#endif
