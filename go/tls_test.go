// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
package redis

import (
	"context"
	"crypto/tls"
	"encoding/pem"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func TestExplicitTLSRoots(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.StartTLS()
	defer server.Close()
	path := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	uri := "rediss://" + server.Listener.Addr().String()
	db, err := newDriverImpl(memory.DefaultAllocator).newDatabaseImpl(context.Background(), map[string]string{adbc.OptionKeyURI: uri, OptionStringTLSCAFile: path})
	if err != nil {
		t.Fatal(err)
	}
	got, err := db.GetOption(context.Background(), OptionStringTLSCAFile)
	if err != nil || got != path {
		t.Fatal(got, err)
	}
	cfg, err := db.clientConfig()
	if err != nil {
		t.Fatal(err)
	}
	for _, config := range []*tls.Config{cfg.single.TLSConfig, cfg.cluster.TLSConfig} {
		conn, err := tls.Dial("tcp", server.Listener.Addr().String(), config)
		if err != nil {
			t.Fatal(err)
		}
		conn.Close()
		wrong := config.Clone()
		wrong.ServerName = "wrong.invalid"
		if conn, err := tls.Dial("tcp", server.Listener.Addr().String(), wrong); err == nil {
			conn.Close()
			t.Fatal("accepted wrong hostname")
		}
	}
	// Clearing the option restores native trust rather than retaining prior roots.
	if err := db.SetOption(context.Background(), OptionStringTLSCAFile, ""); err != nil {
		t.Fatal(err)
	}
	cfg, err = db.clientConfig()
	if err != nil || cfg.single.TLSConfig.RootCAs != nil || cfg.cluster.TLSConfig.RootCAs != nil {
		t.Fatal("did not restore default roots", err)
	}
	for _, uri := range []string{"redis://localhost:6379", "rediss://localhost:6379?skip_verify=true"} {
		if _, err := (&databaseImpl{uri: uri, tlsCAFile: path}).clientConfig(); !isStatus(err, adbc.StatusInvalidArgument) {
			t.Fatal(uri, err)
		}
	}
	for _, path := range []string{filepath.Join(t.TempDir(), "missing"), t.TempDir()} {
		if _, err := (&databaseImpl{uri: uri, tlsCAFile: path}).clientConfig(); !isStatus(err, adbc.StatusInvalidArgument) {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(path, []byte("not a certificate"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = (&databaseImpl{uri: uri, tlsCAFile: path}).clientConfig()
	if !isStatus(err, adbc.StatusInvalidArgument) || strings.Contains(err.Error(), "not a certificate") {
		t.Fatal(err)
	}
}
