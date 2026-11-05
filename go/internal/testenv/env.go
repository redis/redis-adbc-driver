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

// Package testenv enforces the connection policy for integration test targets.
package testenv

import (
	"crypto/x509"
	"fmt"
	"net/url"
	"os"

	"github.com/adbc-drivers/redis/go/internal/tlstrust"
)

// ValidateURI permits an unset target for unit-only runs. Errors never include
// the URI, since it contains credentials.
func ValidateURI(value string) error {
	if value == "" {
		return nil
	}
	u, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("integration target must be a valid authenticated TLS Redis Enterprise URI")
	}
	password, _ := u.User.Password()
	if u.Scheme != "rediss" || u.Hostname() == "" || u.User.Username() == "" || password == "" {
		return fmt.Errorf("integration target must use rediss:// with a username and password on Redis Enterprise")
	}
	for _, key := range []string{"skip_verify", "ssl_cert_reqs", "ssl_check_hostname"} {
		if u.Query().Has(key) {
			return fmt.Errorf("integration target must verify its TLS certificate; configure CA trust instead")
		}
	}
	return nil
}

// SetupRoots also configures raw go-redis clients used by integration fixtures.
func SetupRoots() error {
	roots, err := tlstrust.Roots()
	if err != nil {
		return err
	}
	if roots != nil {
		if err := os.Setenv("GODEBUG", os.Getenv("GODEBUG")+",x509usefallbackroots=1"); err != nil {
			return err
		}
		x509.SetFallbackRoots(roots)
	}
	return nil
}
