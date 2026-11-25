Target: apache/arrow-go — new issue (Bug Report form; Component: Other)
Title: arrow/cdata: ImportCArray panics on malformed sparse union input (short children, duplicate type codes)
Labels: per upstream template ("Type: bug")
Status: reproduced locally on v18.9.0 (Go 1.27.2, darwin/arm64); upstream main a7fe8f8ba4 (2026-11-24) has the same code paths
---
### Describe the bug

`cdata.ImportCArray` panics, instead of returning an error, when a C Data producer passes a malformed sparse union. A consumer that imports arrays from untrusted or foreign producers cannot reject the input without recovering a panic.

The two cases below both panic inside `ImportCArray`:

| Input | Expected | Actual |
|---|---|---|
| Sparse union, `length=4`, one child with `length=2` (the child buffers still hold 4 values) | `arrow.ErrInvalid` | `panic: arrow/array: index out of range` (`array.NewSliceData` ← `union.setData` ← `array.MakeFromData` ← `cdata.ImportCArray`) |
| Schema format `+us:0,0` (duplicate type codes) | `arrow.ErrInvalid` | `panic: arrow: union type codes must be unique` (`arrow.UnionOf` ← `cdata` format parsing ← `cdata.ImportCArray`) |

The same arrays round-trip correctly before they are made malformed. Related fixes for other shapes: #1557/#1570 (Map NULL keys now return an error), #1568 (IPC reader panics on malformed input), and #1641 (the export side no longer produces short sparse-union children, but import still trusts producer-supplied child lengths).

### Reproduction

Standalone module, `go run .` with `github.com/apache/arrow-go/v18 v18.9.0`. The C structs are a copy of the C Data ABI, used only to edit the exported struct before re-importing it.

```go
package main

/*
#include <stdint.h>
struct A { int64_t length, null_count, offset, n_buffers, n_children;
  const void** buffers; struct A** children; struct A* dictionary;
  void (*release)(struct A*); void* private_data; };
struct S { const char *format, *name, *metadata; int64_t flags, n_children;
  struct S** children; struct S* dictionary;
  void (*release)(struct S*); void* private_data; };
static void shorten_child(struct A* a, int64_t i, int64_t n) { a->children[i]->length = n; }
static void set_format(struct S* s, const char* f) { s->format = f; }
*/
import "C"

import (
	"fmt"
	"unsafe"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/cdata"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func union() arrow.Array {
	dt := arrow.SparseUnionOf([]arrow.Field{
		{Name: "i", Type: arrow.PrimitiveTypes.Int32, Nullable: true},
		{Name: "s", Type: arrow.BinaryTypes.String, Nullable: true},
	}, []arrow.UnionTypeCode{0, 1})
	b := array.NewSparseUnionBuilder(memory.DefaultAllocator, dt)
	defer b.Release()
	for i := 0; i < 4; i++ {
		b.Append(0)
		b.Child(0).(*array.Int32Builder).Append(int32(i))
		b.Child(1).(*array.StringBuilder).AppendNull()
	}
	return b.NewArray()
}

func try(label string, mutate func(*C.struct_A, *C.struct_S)) {
	arr := union()
	defer arr.Release()
	var ca cdata.CArrowArray
	var cs cdata.CArrowSchema
	cdata.ExportArrowArray(arr, &ca, &cs)
	mutate((*C.struct_A)(unsafe.Pointer(&ca)), (*C.struct_S)(unsafe.Pointer(&cs)))
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("%s: PANIC: %v\n", label, r)
		}
	}()
	_, out, err := cdata.ImportCArray(&ca, &cs)
	if err != nil {
		fmt.Printf("%s: error: %v\n", label, err)
		return
	}
	defer out.Release()
	fmt.Printf("%s: ok, ValidateFull=%v\n", label, array.ValidateFull(out))
}

func main() {
	try("valid", func(*C.struct_A, *C.struct_S) {})
	try("short sparse child", func(a *C.struct_A, _ *C.struct_S) { C.shorten_child(a, 0, 2) })
	try("duplicate type codes", func(_ *C.struct_A, s *C.struct_S) { C.set_format(s, C.CString("+us:0,0")) })
}
```

Output:

```text
valid: ok, ValidateFull=<nil>
short sparse child: PANIC: arrow/array: index out of range
duplicate type codes: PANIC: arrow: union type codes must be unique
```

### Suggested fix

In the importer:

- Check that each sparse-union child has `length >= offset + length` before constructing the array.
- Check that the union type codes are unique while parsing the format string. Negative codes are already rejected there.
- Return `arrow.ErrInvalid` for both, as #1570 does for Map keys.

A producer can also declare a dense-union type code outside the declared set. That case may deserve the same treatment, but I have not tested it.

### Context

The Redis ADBC driver currently checks for these cases in C before calling `ImportCArray`: [input_preflight.c](https://github.com/redis/redis-adbc-driver/blob/682fae1f73419b3efb4d5e011b9da0a4ca1d5588/go/pkg/input_preflight.c#L9-L30). It would like to drop that check once the importer returns errors.

### Component(s)

Other
