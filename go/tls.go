// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
package redis

import (
	"crypto/x509"
	"os"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

// Explicit roots work identically on platforms whose native TLS verifier does
// not honor SSL_CERT_FILE. Never edit global roots or weaken hostname checking.
func configureTLSRoots(opts *goredis.Options, path string) error {
	if path == "" {
		return nil
	}
	if opts.TLSConfig == nil || opts.TLSConfig.InsecureSkipVerify {
		return errorf(adbc.StatusInvalidArgument, "%s requires rediss with certificate verification enabled", OptionStringTLSCAFile)
	}
	pem, err := os.ReadFile(path)
	if err != nil {
		return errorf(adbc.StatusInvalidArgument, "%s: cannot read trust bundle", OptionStringTLSCAFile)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(pem) {
		return errorf(adbc.StatusInvalidArgument, "%s: trust bundle contains no valid certificates", OptionStringTLSCAFile)
	}
	opts.TLSConfig = opts.TLSConfig.Clone()
	opts.TLSConfig.RootCAs = roots
	return nil
}
