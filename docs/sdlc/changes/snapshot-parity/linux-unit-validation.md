# Current-protocol Linux ARM64 Go unit checkpoint

Date: 2026-11-04. Driver/test source 21e8509, storage protocol 15.
Full [parity](plan.md) remains incomplete.

Built the root Go package test client with Go 1.27.3, GOOS=linux,
GOARCH=arm64 and CGO_ENABLED=0. Ran that executable in the cached Redis Software
8.2.0-78.18 image in a disposable container with networking disabled. No Redis URI
or fixture credentials were passed; source/documentation inputs were mounted
read-only and the working directory matched the Go package. No database was
accessed, so this does not depend on or claim a live QPF/server validation.

The complete root-package unit invocation passes **942 test events, 863 skips,
zero failures**, including the ordinary fuzz seeds (not a fuzzing campaign).
Skips include database-gated integrations/profiles and a timezone enumeration
case whose cross-compiler host GOROOT directory does not exist in the container.
A separate focused invocation mounts the matching Go 1.27.3 zoneinfo.zip read-only
and sets the container GOROOT/ZONEINFO paths: TestTimeZoneCaseInsensitive passes,
zero skips/failures, 0.91 s. These are separate invocations, not one skip-free run.

The initial container invocation omitted source/documentation fixtures and the
package working directory: 940 passes, 864 skips and one failure. That invocation
is not passing platform evidence. Correcting the harness inputs produced the
complete passing root-package result above; no driver or assertion change was
made to obtain it.

Reviewed source revision/toolchain, actual Linux ARM64 execution, isolated local
fake transports, required relative fixtures and timezone data, no live database
admission, and exact pass/skip/failure accounting. This is local Go root-package
coverage with CGO disabled, not a Linux race run, internal-package matrix, native
C/ADBC, hosted CI or Cluster API/failover/persistence proof. The compiled client
is prepared for the separate owned current-protocol Cluster tests; compilation
and these unit skips do not establish that runtime coverage. Full ordinary,
platform/recovery, restricted readers, public resume, sustained compaction and
comparative performance remain open. Author review is not maintainer approval.
