# Specification

See [intent](intent.md) and [plan](plan.md). Date: November 13, 2026. Source: `ca6858065c4cf02418d55c3aa62eb583590fe252`. Author: Codex; requested by repository user. Human approval remains pending.

Use the unchanged arrow_compare.py harness and one clean versioned build. Require an owned Redis Software fixture with active/effective 4× QPF, six workers, mnp/32, noeviction, one 4-GiB RAM shard and actual 4-CPU/8-GiB container limits; strict data TLS/RESP3. Capture exact source/input/binary/harness hashes and actual server versions.

Accept only the complete 156-trial matrix (84 reads, 40 append writes, 32 creation trials), including 30 warmups, with independent physical-type/value/count/multiplicity/ordinal validation. Preserve every attempt, failures and retries. Publish sanitized raw samples, distributions, audit and cleanup proof. Bound claims to observed fixtures and timing scopes; managed write admission serializes operations. Keep endpoints, infrastructure identities, credentials, license and private operational logs out of the public bundle. Native slide edits preserve supporting v0.0.25 and historical 100M slides.
