// CPU-only fault injection for the native staging allocator. This does not
// emulate CUDA or count as real-device validation.
#include <assert.h>
#include <errno.h>
#include <stdlib.h>
#include <string.h>
static size_t calls, fail_at, live;
static void* staging_malloc(size_t bytes) {
  if(++calls==fail_at)return NULL;
  void* p=malloc(bytes);if(p)++live;return p;
}
static void* staging_calloc(size_t n,size_t bytes) {
  if(++calls==fail_at)return NULL;
  void* p=calloc(n,bytes);if(p)++live;return p;
}
static void staging_free(void* p) {if(p){assert(live);--live;}free(p);}
#define malloc staging_malloc
#define calloc staging_calloc
#define free staging_free
#include "../go/pkg/device.c"
#undef malloc
#undef calloc
#undef free

int redis_cuda_probe(int d,char* e,size_t n){(void)d;(void)e;(void)n;return ENOTSUP;}
int redis_cuda_alloc(int d,void** p,size_t b,char* e,size_t n){(void)d;(void)p;(void)b;(void)e;(void)n;abort();}
int redis_cuda_copy(int d,void* a,const void* b,size_t z,int t,char* e,size_t n){(void)d;(void)a;(void)b;(void)z;(void)t;(void)e;(void)n;abort();}
int redis_cuda_wait(int d,void* p,char* e,size_t n){(void)d;(void)p;(void)e;(void)n;return ENOTSUP;}
int redis_cuda_event_ready(int d,void* p,char* e,size_t n){(void)d;(void)p;(void)e;(void)n;return ENOTSUP;}
void redis_cuda_free(int d,void* p){(void)d;(void)p;abort();}
static void borrowed(struct ArrowArray* array){(void)array;abort();}
int main(void) {
  // Nonzero first offset, non-byte-aligned validity slice and empty values.
  int32_t offsets[]={0,3,3,3,5};
  unsigned char bitmap=0x15;
  const void* buffers[]={&bitmap,offsets,"prehi"};
  struct ArrowArray child={.length=3,.offset=1,.null_count=1,.n_buffers=3,.buffers=buffers,.release=borrowed};
  struct ArrowSchema field={.format="u"};
  struct ArrowSchema* fields[]={&field,&field};
  struct ArrowSchema schema={.format="+s",.n_children=2,.children=fields};
  const void* root_buffers[]={NULL};
  struct ArrowArray* children[]={&child,&child};
  struct ArrowDeviceArray input={.device_type=ARROW_DEVICE_CPU,.device_id=-1,
    .array={.length=3,.n_buffers=1,.buffers=root_buffers,.n_children=2,.children=children,.release=borrowed}};
  size_t allocation_count=0;
  for(size_t attempt=0;attempt<=allocation_count;++attempt){
    struct ArrowArray output={0};char error[256]={0};
    calls=0;fail_at=attempt;
    int rc=RedisDeviceArrayToHost(&input,&schema,&output,error,sizeof(error));
    if(!attempt){
      assert(!rc);allocation_count=calls;
      assert(output.children[0]->offset==0);
      assert(((const int32_t*)output.children[0]->buffers[1])[3]==2);
      assert(!memcmp(output.children[0]->buffers[2],"hi",2));
      assert((*(const unsigned char*)output.children[0]->buffers[0]&7)==2);
      output.release(&output);
    }else {assert(rc==ENOMEM);assert(!output.release);}
    assert(!live);assert(child.release==borrowed);assert(input.array.release==borrowed);
  }
  allocation_count=0;
  for(size_t attempt=0;attempt<=allocation_count;++attempt){
    struct ArrowSchema view={0};char error[256]={0};calls=0;fail_at=attempt;
    int rc=RedisDeviceSchemaBorrow(&schema,&view,error,sizeof(error));
    if(!attempt){assert(!rc);allocation_count=calls;view.release(&view);}
    else {assert(rc==ENOMEM);assert(!view.release);}
    assert(!live);
  }
  return 0;
}
