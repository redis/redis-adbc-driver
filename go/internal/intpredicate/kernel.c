//go:build cgo && (amd64 || arm64) && !purego

// Copyright (c) 2026 ADBC Drivers Contributors
// SPDX-License-Identifier: Apache-2.0
#include <stdint.h>
#include <stddef.h>
#if defined(__x86_64__)
#include <immintrin.h>
#define TARGET __attribute__((target("sse4.2")))
#else
#include <arm_neon.h>
#define TARGET
#endif

// Two exact signed int64 lanes. There is no subtraction (which could overflow)
// and no floating conversion. Dispatch occurs in Go before entering this code.
TARGET void integer_match(const int64_t *v, const uint64_t *valid,
                          uint64_t *out, size_t n, int64_t rhs, int op) {
 size_t i = 0;
#if defined(__x86_64__)
 __m128i r = _mm_set1_epi64x(rhs);
#else
 int64x2_t r = vdupq_n_s64(rhs);
#endif
 for (; i+1<n; i+=2) {
  unsigned bits;
#if defined(__x86_64__)
  __m128i a = _mm_loadu_si128((const __m128i *)(v+i)), cmp;
  switch (op) {
   case 0: case 1: cmp = _mm_cmpeq_epi64(a,r); break;
   case 2: case 5: cmp = _mm_cmpgt_epi64(r,a); break;
   default: cmp = _mm_cmpgt_epi64(a,r); break;
  }
  bits = (unsigned)_mm_movemask_pd(_mm_castsi128_pd(cmp));
#else
  int64x2_t a = vld1q_s64(v+i);
  uint64x2_t cmp;
  switch (op) {
   case 0: case 1: cmp = vceqq_s64(a,r); break;
   case 2: case 5: cmp = vcgtq_s64(r,a); break;
   default: cmp = vcgtq_s64(a,r); break;
  }
  bits = (unsigned)(vgetq_lane_u64(cmp,0)&1) |
         (unsigned)((vgetq_lane_u64(cmp,1)&1)<<1);
#endif
  if (op == 1 || op == 3 || op == 5) bits ^= 3;
  out[i/64] |= (uint64_t)bits << (i%64);
 }
 if (i<n) {
  int64_t a = v[i];
  int match = op==0 ? a==rhs : op==1 ? a!=rhs : op==2 ? a<rhs :
              op==3 ? a<=rhs : op==4 ? a>rhs : a>=rhs;
  out[i/64] |= (uint64_t)match << (i%64);
 }
 // Intersect comparison and NULL bitmaps with vector AND, then a word tail.
 if (valid) {
  size_t words = (n+63)/64, w=0;
  for (; w+1<words; w+=2) {
#if defined(__x86_64__)
   _mm_storeu_si128((__m128i *)(out+w), _mm_and_si128(
      _mm_loadu_si128((const __m128i *)(out+w)),
      _mm_loadu_si128((const __m128i *)(valid+w))));
#else
   vst1q_u64(out+w, vandq_u64(vld1q_u64(out+w), vld1q_u64(valid+w)));
#endif
  }
  if (w<words) out[w] &= valid[w];
 }
}
