# Plan: Redis ADBC provider

Date: 2026-11-12. Base: `b4783b64`. Status: completed locally.
See [intent](intent.md), [spec](spec.md), and [validation](validation.md).

1. Reproduce missing provider registration with a failing discovery test.
2. Add lightweight metadata and the packaging entry point; specialize hook
   connection identity, default dialect and UI field behavior.
3. Exercise management and SDK discovery, standard SQL operator selection and
   transaction rejection, plus legacy and new connection-type read/write paths.
4. Build and install a wheel, verify optional-dependency isolation, run live
   tests on a fresh Software fixture with verified 4x QPF, and execute the guide.
5. Update current guides and support tables; preserve historical evidence.

The user subsequently requested a pull request. Commit and submit this change
from a dedicated branch; no release or separate GitHub repository is included.
