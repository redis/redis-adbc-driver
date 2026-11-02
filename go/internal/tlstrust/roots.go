// Copyright (c) 2026 ADBC Drivers Contributors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//         http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package tlstrust applies explicit CA bundles consistently across platforms.
package tlstrust

import (
	"crypto/x509"
	"fmt"
	"os"
)

// Roots returns nil to use platform defaults unless SSL_CERT_FILE is set.
// Explicit roots replace the system pool, including on macOS, whose native
// verifier otherwise ignores SSL_CERT_FILE.
func Roots() (*x509.CertPool, error) {
	path := os.Getenv("SSL_CERT_FILE")
	if path == "" {
		return nil, nil
	}
	pem, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read SSL_CERT_FILE: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("SSL_CERT_FILE contains no CA certificates")
	}
	return roots, nil
}
