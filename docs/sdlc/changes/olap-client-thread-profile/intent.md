# Intent

Date: 2026-11-24. Status: 17 synthetic checks passed, ready for review.

Make the SF1 harness's DuckDB client thread count explicit so an authorized larger-CPU profile applies consistently to native DuckDB and DuckDB over Redis. Preserve historical default 4 and sanitize the new provenance. No full benchmark suite or database operation is started by this change.
