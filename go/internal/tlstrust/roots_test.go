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

package tlstrust

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExplicitCA(t *testing.T) {
	t.Setenv("SSL_CERT_FILE", "")
	if roots, err := Roots(); roots != nil || err != nil {
		t.Fatalf("platform defaults: %v", err)
	}
	path := filepath.Join(t.TempDir(), "ca.pem")
	t.Setenv("SSL_CERT_FILE", path)
	if _, err := Roots(); err == nil {
		t.Fatal("missing CA must fail")
	}
	if err := os.WriteFile(path, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Roots(); err == nil {
		t.Fatal("invalid CA must fail")
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	cert, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert}), 0600); err != nil {
		t.Fatal(err)
	}
	roots, err := Roots()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(cert)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parsed.Verify(x509.VerifyOptions{Roots: roots}); err != nil {
		t.Fatalf("configured CA not trusted: %v", err)
	}
}
