// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
#include "input_preflight.h"
#include <errno.h>
#include <limits.h>
#include <stdlib.h>
#include <string.h>

// Arrow Go constructs Map and sparse Union children before its full validator
// runs. Reject invalid headers/schema codes and NULL map keys before importing,
// so untrusted input cannot reach those constructor panics and poison a driver.
// The C Data ABI supplies pointers, not allocation sizes: producers still own
// the obligation to provide readable buffers for their declared dimensions.
static const char* schema_check(const struct ArrowSchema* s, int depth) {
  if (!s || !s->format || !s->release || depth > 17 || s->n_children < 0 || s->n_children > 1024 || (s->n_children && !s->children))
    return "invalid Arrow schema header or nesting";
  if (!strncmp(s->format, "+ud:", 4) || !strncmp(s->format, "+us:", 4)) {
    if (s->n_children < 1 || s->n_children > 128) return "invalid Arrow Union child count";
    const char* p = s->format + 4;
    unsigned char seen[128] = {0};
    for (int64_t i = 0; i < s->n_children; ++i) {
      if (*p < '0' || *p > '9') return "invalid Arrow Union type codes";
      int code = 0, digits = 0;
      while (*p >= '0' && *p <= '9') {
        if (++digits > 3) return "invalid Arrow Union type codes";
        code = code * 10 + (*p++ - '0');
      }
      if (code > 127 || seen[code]++) return "invalid Arrow Union type codes";
      if (i + 1 < s->n_children) { if (*p++ != ',') return "invalid Arrow Union type codes"; }
      else if (*p) return "invalid Arrow Union type codes";
    }
  }
  for (int64_t i = 0; i < s->n_children; ++i) {
    const char* err = schema_check(s->children[i], depth + 1);
    if (err) return err;
  }
  if (!strcmp(s->format, "+m") && (s->n_children != 1 || strcmp(s->children[0]->format, "+s") || s->children[0]->n_children != 2))
    return "invalid Arrow Map child schema";
  if (!strcmp(s->format, "+m") && ((s->children[0]->flags & ARROW_FLAG_NULLABLE) || (s->children[0]->children[0]->flags & ARROW_FLAG_NULLABLE)))
    return "invalid Arrow Map: entries and key schemas must be non-nullable";
  if (s->dictionary) return schema_check(s->dictionary, depth + 1);
  return NULL;
}
static int has_nulls(const struct ArrowArray* a) {
  if (a->null_count >= 0) return a->null_count != 0;
  if (!a->n_buffers || !a->buffers || !a->buffers[0]) return 0;
  const unsigned char* bits = (const unsigned char*)a->buffers[0];
  for (int64_t i = a->offset; i < a->offset + a->length; ++i)
    if (!(bits[i >> 3] & (1u << (i & 7)))) return 1;
  return 0;
}
static const char* array_check(const struct ArrowArray* a, const struct ArrowSchema* s, int depth) {
  if (!a || !a->release || depth > 17 || a->length < 0 || a->offset < 0 || a->offset > INT64_MAX - a->length || a->null_count < -1 || a->null_count > a->length || a->n_children != s->n_children || (a->n_children && !a->children) || a->n_buffers < 0 || (a->n_buffers && !a->buffers))
    return "invalid Arrow array header or child count";
  // Validate descendants before dereferencing Map child/key headers below.
  for (int64_t i = 0; i < a->n_children; ++i) {
    const char* err = array_check(a->children[i], s->children[i], depth + 1);
    if (err) return err;
  }
  if ((a->dictionary != NULL) != (s->dictionary != NULL)) return "invalid Arrow dictionary child";
  if (a->dictionary) { const char* err = array_check(a->dictionary, s->dictionary, depth + 1); if (err) return err; }
  if (!strcmp(s->format, "+m")) {
    if (has_nulls(a->children[0]) || has_nulls(a->children[0]->children[0])) return "invalid Arrow Map: entries and keys must not be NULL";
  }
  if (!strncmp(s->format, "+us:", 4)) {
    for (int64_t i = 0; i < a->n_children; ++i)
      if (a->children[i]->length < a->offset + a->length) return "invalid Arrow sparse Union child length";
  }
  return NULL;
}
const char* RedisInputPreflight(const struct ArrowArray* a, const struct ArrowSchema* s) {
  const char* err = schema_check(s, 0);
  return err ? err : array_check(a, s, 0);
}

struct checked_input {
  struct ArrowArrayStream source;
  const char* error;
};
static int checked_schema(struct ArrowArrayStream* stream, struct ArrowSchema* out) {
  struct checked_input* in = (struct checked_input*)stream->private_data;
  int code = in->source.get_schema(&in->source, out);
  if (code) return code;
  in->error = schema_check(out, 0);
  if (in->error) { if (out->release) out->release(out); memset(out, 0, sizeof(*out)); return EINVAL; }
  return 0;
}
static int checked_next(struct ArrowArrayStream* stream, struct ArrowArray* out) {
  struct checked_input* in = (struct checked_input*)stream->private_data;
  int code = in->source.get_next(&in->source, out);
  if (code || !out->release) return code;
  struct ArrowSchema schema = {0};
  code = in->source.get_schema(&in->source, &schema);
  if (!code) in->error = RedisInputPreflight(out, &schema);
  if (schema.release) schema.release(&schema);
  if (code || in->error) {
    out->release(out); memset(out, 0, sizeof(*out));
    return code ? code : EINVAL;
  }
  return 0;
}
static const char* checked_error(struct ArrowArrayStream* stream) {
  struct checked_input* in = (struct checked_input*)stream->private_data;
  return in->error ? in->error : (in->source.get_last_error ? in->source.get_last_error(&in->source) : "Arrow input stream error");
}
static void checked_release(struct ArrowArrayStream* stream) {
  if (!stream->release) return;
  struct checked_input* in = (struct checked_input*)stream->private_data;
  if (in->source.release) in->source.release(&in->source);
  free(in); memset(stream, 0, sizeof(*stream));
}
int RedisInputCheckedStream(struct ArrowArrayStream* stream) {
  if (!stream || !stream->release || !stream->get_schema || !stream->get_next) return EINVAL;
  struct checked_input* in = (struct checked_input*)calloc(1, sizeof(*in));
  if (!in) return ENOMEM;
  in->source = *stream;
  stream->get_schema = checked_schema; stream->get_next = checked_next;
  stream->get_last_error = checked_error; stream->release = checked_release; stream->private_data = in;
  return 0;
}
