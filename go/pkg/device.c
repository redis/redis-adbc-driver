//go:build driverlib

// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
#include "device.h"
#include "../internal/cuda/runtime.h"
#include <errno.h>
#include <limits.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

struct array_owner { int device; int on_device; };
static int failure(char* error,size_t capacity,int code,const char* message) {
  if(error&&capacity)snprintf(error,capacity,"%s",message);
  return code;
}
static void release_array(struct ArrowArray* array) {
  if(!array||!array->release)return;
  array->release=NULL;
  struct array_owner* owner=array->private_data;
  for(int64_t i=0;i<array->n_children;++i)if(array->children&&array->children[i]) {
    release_array(array->children[i]);free(array->children[i]);
  }
  for(int64_t i=0;i<array->n_buffers;++i)if(array->buffers&&array->buffers[i]) {
    if(owner->on_device)redis_cuda_free(owner->device,(void*)array->buffers[i]);else free((void*)array->buffers[i]);
  }
  free((void*)array->buffers);free(array->children);free(owner);
  memset(array,0,sizeof(*array));
}
static int format_bits(const char* format) {
  if(!format||!format[0]||format[1])return 0;
  switch(format[0]){
    case 'b':return 1;
    case 'c':case 'C':return 8;
    case 's':case 'S':return 16;
    case 'i':case 'I':case 'f':return 32;
    case 'l':case 'L':case 'g':return 64;
    default:return 0;
  }
}
static int offset_width(const char* format) {
  if(!strcmp(format,"u")||!strcmp(format,"z"))return 4;
  if(!strcmp(format,"U")||!strcmp(format,"Z"))return 8;
  return 0;
}
static int schema_supported(const struct ArrowSchema* schema,int depth,char* error,size_t capacity) {
  if(!schema||!schema->format||schema->dictionary||depth>1)
    return failure(error,capacity,ENOTSUP,"C Device staging supports flat numeric, boolean, UTF-8/binary arrays or record batches only");
  if(!strcmp(schema->format,"+s")) {
    if(depth||schema->n_children<0||schema->n_children>1024||(!schema->children&&schema->n_children))
      return failure(error,capacity,ENOTSUP,"unsupported C Device record schema");
    for(int64_t i=0;i<schema->n_children;++i){int rc=schema_supported(schema->children[i],depth+1,error,capacity);if(rc)return rc;}
    return 0;
  }
  if(schema->n_children||(!format_bits(schema->format)&&!offset_width(schema->format)))return failure(error,capacity,ENOTSUP,"unsupported C Device Arrow physical type");
  return 0;
}
// Arrow's Go importer consumes schemas recursively. These views own only their
// wrapper structs, never the caller's strings, metadata or child schemas.
static void release_schema_view(struct ArrowSchema* schema) {
  if(!schema||!schema->release)return;
  schema->release=NULL;
  for(int64_t i=0;i<schema->n_children;++i)if(schema->children&&schema->children[i]){
    release_schema_view(schema->children[i]);free(schema->children[i]);
  }
  free(schema->children);memset(schema,0,sizeof(*schema));
}
static int borrow_schema(const struct ArrowSchema* source,struct ArrowSchema* out) {
  *out=*source;out->children=NULL;out->private_data=NULL;out->release=release_schema_view;
  if(!source->n_children)return 0;
  out->children=calloc((size_t)source->n_children,sizeof(struct ArrowSchema*));
  if(!out->children){release_schema_view(out);return ENOMEM;}
  for(int64_t i=0;i<source->n_children;++i){
    out->children[i]=calloc(1,sizeof(struct ArrowSchema));
    if(!out->children[i]||borrow_schema(source->children[i],out->children[i])){release_schema_view(out);return ENOMEM;}
  }
  return 0;
}
int RedisDeviceSchemaBorrow(const struct ArrowSchema* source,struct ArrowSchema* out,char* error,size_t capacity){
  if(!out||out->release)return failure(error,capacity,EINVAL,"schema view must be zero initialized");
  int rc=schema_supported(source,0,error,capacity);if(rc)return rc;
  rc=borrow_schema(source,out);
  return rc?failure(error,capacity,rc,"schema view allocation failed"):0;
}
static int copy_bytes(int device,int direction,void* dst,const void* src,size_t bytes,char* error,size_t capacity) {
  if(!bytes)return 0;
  if(!direction){memcpy(dst,src,bytes);return 0;}
  return redis_cuda_copy(device,dst,src,bytes,direction==1,error,capacity);
}
static int allocate_buffer(struct ArrowArray* out,int index,size_t bytes,int device,int direction,size_t* budget,char* error,size_t capacity) {
  if(bytes>*budget)return failure(error,capacity,EOVERFLOW,"C Device staging batch exceeds 512 MiB");
  *budget-=bytes;
  if(!bytes)return 0;
  void* buffer=NULL;
  int rc=0;
  if(direction==1)rc=redis_cuda_alloc(device,&buffer,bytes,error,capacity);
  else if(!(buffer=malloc(bytes)))rc=failure(error,capacity,ENOMEM,"Arrow staging allocation failed");
  if(!rc)out->buffers[index]=buffer;
  return rc;
}
// Variable layouts are normalized to offset zero. Only offsets and bytes in
// the selected range are read; unused prefixes need not be accessible.
static int clone_variable(const struct ArrowArray* input,struct ArrowArray* out,int width,int device,int direction,size_t* budget,char* error,size_t capacity) {
  uint64_t length=(uint64_t)input->length, offset=(uint64_t)input->offset;
  if(length+offset>SIZE_MAX/(unsigned)width-1)
    return failure(error,capacity,EOVERFLOW,"C Device offset size overflow");
  size_t bytes=((size_t)length+1)*(unsigned)width;
  if(bytes>*budget)return failure(error,capacity,EOVERFLOW,"C Device staging batch exceeds 512 MiB");
  if(!input->buffers[1])return failure(error,capacity,EINVAL,"missing C Device offsets buffer");
  // Reserve temporary host offsets too, including during host-to-device copy.
  *budget-=bytes;
  void* offsets=malloc(bytes);
  if(!offsets)return failure(error,capacity,ENOMEM,"Arrow offsets allocation failed");
  int rc=copy_bytes(device,direction==2?2:0,offsets,(const char*)input->buffers[1]+(size_t)offset*(unsigned)width,bytes,error,capacity);
  int64_t first=0,previous=-1;
  if(rc)goto done;
  for(uint64_t j=0;j<=length;++j){
    int64_t value=width==4?((int32_t*)offsets)[j]:((int64_t*)offsets)[j];
    if(value<0||value<previous){rc=failure(error,capacity,EINVAL,"negative or decreasing C Device offsets");goto done;}
    if(!j)first=value;
    previous=value;
    if(width==4)((int32_t*)offsets)[j]=(int32_t)(value-first);
    else ((int64_t*)offsets)[j]=value-first;
  }
  uint64_t span=(uint64_t)(previous-first);
  if((uint64_t)first>SIZE_MAX||span>SIZE_MAX||span>*budget){rc=failure(error,capacity,EOVERFLOW,"C Device variable data exceeds staging budget");goto done;}
  if(span&&!input->buffers[2]){rc=failure(error,capacity,EINVAL,"missing required C Device data buffer");goto done;}
  out->offset=0;
  rc=allocate_buffer(out,1,bytes,device,direction,budget,error,capacity);
  if(rc)goto done;
  // Offsets have already been staged and normalized on the host.
  rc=copy_bytes(device,direction==1?1:0,(void*)out->buffers[1],offsets,bytes,error,capacity);
  if(rc)goto done;
  rc=allocate_buffer(out,2,(size_t)span,device,direction,budget,error,capacity);
  if(rc)goto done;
  if(span)rc=copy_bytes(device,direction,(void*)out->buffers[2],(const char*)input->buffers[2]+(size_t)first,(size_t)span,error,capacity);
  if(rc)goto done;
  if(!input->buffers[0]){
    if(input->null_count>0)rc=failure(error,capacity,EINVAL,"missing required C Device validity buffer");
    goto done;
  }
  size_t validity=(size_t)((length+7)/8), selected=length?(size_t)((length+(offset%8)+7)/8):0;
  if(selected>*budget){rc=failure(error,capacity,EOVERFLOW,"C Device validity exceeds staging budget");goto done;}
  *budget-=selected;
  unsigned char* bitmap=malloc(selected?selected:1);
  if(!bitmap){rc=failure(error,capacity,ENOMEM,"Arrow validity allocation failed");goto done;}
  rc=copy_bytes(device,direction==2?2:0,bitmap,(const unsigned char*)input->buffers[0]+offset/8,selected,error,capacity);
  if(!rc){
    for(size_t i=0;i<validity;++i){
      unsigned value=bitmap[i]>>(offset%8);
      if(offset%8&&i+1<selected)value|=(unsigned)bitmap[i+1]<<(8-offset%8);
      bitmap[i]=(unsigned char)value;
    }
    rc=allocate_buffer(out,0,validity,device,direction,budget,error,capacity);
    if(!rc)rc=copy_bytes(device,direction==1?1:0,(void*)out->buffers[0],bitmap,validity,error,capacity);
  }
  free(bitmap);*budget+=selected;
done:
  free(offsets);*budget+=bytes;
  return rc;
}
static int clone_array(const struct ArrowArray* input,const struct ArrowSchema* schema,struct ArrowArray* out,int device,int direction,size_t* budget,char* error,size_t capacity) {
  int structure=!strcmp(schema->format,"+s");
  int width=offset_width(schema->format);
  if(!input||input->length<0||input->offset<0||input->length>INT64_MAX-input->offset||input->null_count < -1||input->null_count>input->length||input->dictionary||input->n_children!=schema->n_children||input->n_buffers!=(structure?1:(width?3:2))||!input->buffers||(!input->children&&input->n_children))
    return failure(error,capacity,EINVAL,"invalid C Device Arrow array metadata");
  if(structure) {
    if(input->offset)return failure(error,capacity,ENOTSUP,"C Device record batch root offsets must be zero; child slices are supported");
    for(int64_t i=0;i<input->n_children;++i)if(!input->children[i]||input->children[i]->length<input->length)
      return failure(error,capacity,EINVAL,"C Device child is shorter than its record batch");
  }
  struct array_owner* owner=calloc(1,sizeof(*owner));
  if(!owner)return failure(error,capacity,ENOMEM,"Arrow owner allocation failed");
  owner->device=device;owner->on_device=direction==1;
  out->length=input->length;out->offset=input->offset;out->null_count=input->null_count;
  out->n_buffers=input->n_buffers;out->n_children=input->n_children;out->private_data=owner;out->release=release_array;
  out->buffers=calloc((size_t)out->n_buffers,sizeof(void*));
  if(out->n_children)out->children=calloc((size_t)out->n_children,sizeof(struct ArrowArray*));
  if(!out->buffers||(out->n_children&&!out->children))goto memory_error;
  uint64_t total=(uint64_t)input->length+(uint64_t)input->offset;
  if(width){
    int rc=clone_variable(input,out,width,device,direction,budget,error,capacity);
    if(rc)release_array(out);
    return rc;
  }
  for(int64_t i=0;i<out->n_buffers;++i){
    int bits=i==0?1:format_bits(schema->format);
    if(total>(uint64_t)(SIZE_MAX-7)/(unsigned)bits){release_array(out);return failure(error,capacity,EOVERFLOW,"C Device buffer size overflow");}
    size_t bytes=(size_t)((total*(unsigned)bits+7)/8);
    const void* source=input->buffers[i];
    if(!source){
      if((i==0&&input->null_count>0)||(i!=0&&input->length>0)){release_array(out);return failure(error,capacity,EINVAL,"missing required C Device buffer");}
      continue;
    }
    if(bytes>*budget){release_array(out);return failure(error,capacity,EOVERFLOW,"C Device staging batch exceeds 512 MiB");}
    *budget-=bytes;
    if(!bytes)continue;
    void* destination=NULL;
    if(direction==1){int rc=redis_cuda_alloc(device,&destination,bytes,error,capacity);if(rc){release_array(out);return rc;}}
    else {destination=malloc(bytes);if(!destination)goto memory_error;}
    out->buffers[i]=destination;
    if(direction==0)memcpy(destination,source,bytes);
    else {int rc=redis_cuda_copy(device,destination,source,bytes,direction==1,error,capacity);if(rc){release_array(out);return rc;}}
  }
  for(int64_t i=0;i<out->n_children;++i){
    out->children[i]=calloc(1,sizeof(struct ArrowArray));if(!out->children[i])goto memory_error;
    int rc=clone_array(input->children[i],schema->children[i],out->children[i],device,direction,budget,error,capacity);
    if(rc){release_array(out);return rc;}
  }
  return 0;
memory_error:
  release_array(out);return failure(error,capacity,ENOMEM,"Arrow staging allocation failed");
}
int RedisDeviceInterfaceVersion(void) { return 1; }
int RedisDeviceArrayEventReady(const struct ArrowDeviceArray* input,char* error,size_t capacity) {
  if(!input||!input->array.release)return failure(error,capacity,EINVAL,"invalid device input");
  if(input->device_type==ARROW_DEVICE_CPU)
    return input->sync_event?failure(error,capacity,EINVAL,"CPU arrays must not supply CUDA events"):0;
  if(input->device_type!=ARROW_DEVICE_CUDA)return failure(error,capacity,ENOTSUP,"unsupported Arrow device type; only CPU and CUDA are accepted");
  if(input->device_id<0||input->device_id>65535)return failure(error,capacity,EINVAL,"invalid CUDA device ID");
  return redis_cuda_event_ready((int)input->device_id,input->sync_event,error,capacity);
}
int RedisDeviceArrayFromHost(const struct ArrowArray* input,const struct ArrowSchema* schema,int64_t device,struct ArrowDeviceArray* out,char* error,size_t capacity) {
  if(!out||out->array.release||!input||!input->release||device<0||device>65535)return failure(error,capacity,EINVAL,"invalid device export arguments");
  memset(out,0,sizeof(*out));
  int rc=schema_supported(schema,0,error,capacity);if(rc)return rc;
  if((rc=redis_cuda_probe((int)device,error,capacity)))return rc;
  size_t budget=512u*1024u*1024u;
  rc=clone_array(input,schema,&out->array,(int)device,1,&budget,error,capacity);
  if(rc)return rc;
  out->device_id=device;out->device_type=ARROW_DEVICE_CUDA;
  // cudaMemcpy is synchronous; no outstanding event is necessary.
  return 0;
}
int RedisDeviceArrayToHost(const struct ArrowDeviceArray* input,const struct ArrowSchema* schema,struct ArrowArray* out,char* error,size_t capacity) {
  if(!out||out->release||!input||!input->array.release)return failure(error,capacity,EINVAL,"invalid device import arguments");
  memset(out,0,sizeof(*out));
  if(input->device_type!=ARROW_DEVICE_CPU&&input->device_type!=ARROW_DEVICE_CUDA)return failure(error,capacity,ENOTSUP,"unsupported Arrow device type; only CPU and CUDA are accepted");
  if(input->device_type==ARROW_DEVICE_CUDA&&(input->device_id<0||input->device_id>65535))return failure(error,capacity,EINVAL,"invalid CUDA device ID");
  if(input->device_type==ARROW_DEVICE_CPU&&input->sync_event)return failure(error,capacity,EINVAL,"CPU arrays must not supply CUDA events");
  int rc=schema_supported(schema,0,error,capacity);if(rc)return rc;
  if(input->device_type==ARROW_DEVICE_CUDA&&(rc=redis_cuda_wait((int)input->device_id,input->sync_event,error,capacity)))return rc;
  size_t budget=512u*1024u*1024u;
  return clone_array(&input->array,schema,out,(int)input->device_id,input->device_type==ARROW_DEVICE_CPU?0:2,&budget,error,capacity);
}

struct device_stream {
  struct ArrowArrayStream cpu;
  struct ArrowSchema schema;
  int device;
  char error[256];
};
static int stream_schema(struct ArrowDeviceArrayStream* stream,struct ArrowSchema* out){
  struct device_stream* state=stream->private_data;
  int rc=state->cpu.get_schema(&state->cpu,out);
  if(rc){const char* message=state->cpu.get_last_error(&state->cpu);snprintf(state->error,sizeof(state->error),"%s",message?message:"CPU schema export failed");}
  return rc;
}
static int stream_next(struct ArrowDeviceArrayStream* stream,struct ArrowDeviceArray* out){
  struct device_stream* state=stream->private_data;
  if(!out||out->array.release)return failure(state->error,sizeof(state->error),EINVAL,"device output must be released before reuse");
  memset(out,0,sizeof(*out));
  struct ArrowArray host={0};
  int rc=state->cpu.get_next(&state->cpu,&host);
  if(rc){const char* message=state->cpu.get_last_error(&state->cpu);snprintf(state->error,sizeof(state->error),"%s",message?message:"CPU stream failed");if(host.release)host.release(&host);return rc;}
  if(!host.release)return 0;
  rc=RedisDeviceArrayFromHost(&host,&state->schema,state->device,out,state->error,sizeof(state->error));
  host.release(&host);
  return rc;
}
static const char* stream_error(struct ArrowDeviceArrayStream* stream){return ((struct device_stream*)stream->private_data)->error;}
static void stream_release(struct ArrowDeviceArrayStream* stream){
  if(!stream||!stream->release)return;
  struct device_stream* state=stream->private_data;stream->release=NULL;
  if(state->cpu.release)state->cpu.release(&state->cpu);
  if(state->schema.release)state->schema.release(&state->schema);
  free(state);memset(stream,0,sizeof(*stream));
}
int RedisDeviceStreamInit(struct ArrowArrayStream* input,int64_t device,struct ArrowDeviceArrayStream* out,char* error,size_t capacity){
  if(!input||!input->release||!out||out->release||device<0||device>65535)return failure(error,capacity,EINVAL,"invalid device stream arguments");
  struct device_stream* state=calloc(1,sizeof(*state));if(!state)return failure(error,capacity,ENOMEM,"device stream allocation failed");
  int rc=input->get_schema(input,&state->schema);
  if(rc){const char* message=input->get_last_error(input);failure(error,capacity,rc,message?message:"CPU schema export failed");}
  if(!rc)rc=schema_supported(&state->schema,0,error,capacity);
  if(!rc)rc=redis_cuda_probe((int)device,error,capacity);
  if(rc){if(state->schema.release)state->schema.release(&state->schema);free(state);return rc;}
  state->cpu=*input;memset(input,0,sizeof(*input));state->device=(int)device;
  memset(out,0,sizeof(*out));out->device_type=ARROW_DEVICE_CUDA;out->private_data=state;
  out->get_schema=stream_schema;out->get_next=stream_next;out->get_last_error=stream_error;out->release=stream_release;
  return 0;
}
