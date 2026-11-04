# Implementation and verification plan

Date: 2026-11-04. See [intent](intent.md), [spec](spec.md), [validation](validation.md).

1. Extend device.c/h with variable offsets, bounded slice normalization and V1
   discovery/event polling. Keep recursion and unsupported device rejection.
2. Add optional device.py adapter with capsule ownership and ordinary manager Bind.
3. Add CPU/real-device tests and allocation fault injection with sanitizers.
4. Validate CPU suite and verified Software TLS/RESP3/4x QPF integration.
5. Run independent real CUDA producers/consumers and pending-event cancellation
   on hardware. Record versions/source/hardware, failures and skips accurately.
6. Review final diff and publish PR; close issues only after required evidence.

Steps 1–4 implemented/run locally. Step 5 remains an external hardware gate;
prior milestone evidence is not evidence for this change.
