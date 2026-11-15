// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	goredis "github.com/redis/go-redis/v9"
)

func writerTestDomain(n byte) (d [32]byte) { d[0] = n; return }
func writerTestReady(t *testing.T, turn *snapshotWriterTurn, ready bool) {
	t.Helper()
	select {
	case <-turn.ready:
		if !ready {
			t.Fatal("queued turn passed its predecessor")
		}
	default:
		if ready {
			t.Fatal("head turn was not handed off")
		}
	}
}
func writerTestEmpty(t *testing.T, r *snapshotWriterRegistry) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.requests != 0 || len(r.queues) != 0 {
		t.Fatal("registry retained released requests", r.requests, len(r.queues))
	}
}
func TestSnapshotWriterHandoffFIFOAndIsolation(t *testing.T) {
	var r snapshotWriterRegistry
	join := func(domain byte, target string) *snapshotWriterTurn {
		t.Helper()
		turn, err := r.join(context.Background(), writerTestDomain(domain), target)
		if err != nil || turn == nil {
			t.Fatal("join failed", err)
		}
		return turn
	}
	a, b, c := join(1, "t"), join(1, "t"), join(1, "t")
	otherTarget, otherIdentity := join(1, "other"), join(2, "t")
	writerTestReady(t, a, true)
	writerTestReady(t, b, false)
	writerTestReady(t, c, false)
	writerTestReady(t, otherTarget, true)
	writerTestReady(t, otherIdentity, true)
	a.release()
	writerTestReady(t, b, true)
	writerTestReady(t, c, false)
	a.release()
	writerTestReady(t, c, false)
	b.release()
	writerTestReady(t, c, true)
	c.release()
	otherTarget.release()
	otherIdentity.release()
	writerTestEmpty(t, &r)
}
func TestSnapshotWriterHandoffBoundedOverflow(t *testing.T) {
	r := snapshotWriterRegistry{targetLimit: 1, requestLimit: 2}
	a, _ := r.join(context.Background(), writerTestDomain(1), "t")
	b, _ := r.join(context.Background(), writerTestDomain(1), "t")
	for _, target := range []string{"t", "other"} {
		if turn, err := r.join(context.Background(), writerTestDomain(1), target); err != nil || turn != nil {
			t.Fatal("overflow did not retain native fallback", err)
		}
	}
	b.release()
	if turn, err := r.join(context.Background(), writerTestDomain(1), "other"); err != nil || turn != nil {
		t.Fatal("target overflow not bounded", err)
	}
	a.release()
	writerTestEmpty(t, &r)
	if turn, err := r.join(context.Background(), [32]byte{}, "t"); err != nil || turn != nil {
		t.Fatal("unrepresented identity did not bypass", err)
	}
}
func TestSnapshotWriterHandoffCancellationAndPinCleanup(t *testing.T) {
	for i := 0; i < 100; i++ {
		var r snapshotWriterRegistry
		ctx, cancel := context.WithCancel(context.Background())
		a, _ := r.join(ctx, writerTestDomain(1), "t")
		b, _ := r.join(context.Background(), writerTestDomain(1), "t")
		done := make(chan struct{})
		close(done)
		pin := &snapshotPin{writerTurn: a, stop: make(chan struct{}), done: done}
		var wg sync.WaitGroup
		wg.Add(3)
		go func() { defer wg.Done(); cancel() }()
		go func() { defer wg.Done(); pin.release() }()
		go func() { defer wg.Done(); a.release() }()
		wg.Wait()
		if err := b.wait(context.Background()); err != nil {
			t.Fatal(err)
		}
		b.release()
		writerTestEmpty(t, &r)
	}
	var r snapshotWriterRegistry
	a, _ := r.join(context.Background(), writerTestDomain(1), "t")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if turn, err := r.join(ctx, writerTestDomain(1), "t"); err == nil || turn != nil {
		t.Fatal("already-canceled request admitted")
	}
	waitCtx, stop := context.WithCancel(context.Background())
	b, _ := r.join(waitCtx, writerTestDomain(1), "t")
	c, _ := r.join(context.Background(), writerTestDomain(1), "t")
	stop()
	if err := b.wait(waitCtx); err == nil {
		t.Fatal("queued cancellation lost")
	}
	writerTestReady(t, c, false)
	a.release()
	writerTestReady(t, c, true)
	c.release()
	writerTestEmpty(t, &r)
}
func TestSnapshotWriterDomainIdentity(t *testing.T) {
	makeConfig := func() *clientConfig {
		return newClientConfig(&goredis.Options{Addr: "unused.invalid:1", Username: "user", Password: "secret", Protocol: 3})
	}
	a, b := makeConfig(), makeConfig()
	da, db := snapshotWriterDomain(a, ""), snapshotWriterDomain(b, "")
	if da == ([32]byte{}) || da != db {
		t.Fatal("independent DB config identities do not coordinate")
	}
	for _, change := range []func(*clientConfig){func(c *clientConfig) { c.single.Username = "other" }, func(c *clientConfig) { c.single.Password = "other" }, func(c *clientConfig) { c.single.DB = 1 }, func(c *clientConfig) { c.single.Addr = "different.invalid:1" }} {
		c := makeConfig()
		change(c)
		if snapshotWriterDomain(c, "") == da {
			t.Fatal("routing/principal identity conflated")
		}
	}
	c := makeConfig()
	c.single.Dialer = func(context.Context, string, string) (net.Conn, error) { panic("must not dial") }
	if snapshotWriterDomain(c, "") != ([32]byte{}) {
		t.Fatal("custom dialer identity retained")
	}
	c = makeConfig()
	c.single.CredentialsProvider = func() (string, string) { return "x", "y" }
	if snapshotWriterDomain(c, "") != ([32]byte{}) {
		t.Fatal("dynamic credentials retained")
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	makeTLS := func() *clientConfig {
		c := makeConfig()
		c.single.TLSConfig = &tls.Config{ServerName: "localhost", MinVersion: tls.VersionTLS12}
		if err := configureTLSRoots(c.single, path); err != nil {
			t.Fatal(err)
		}
		return c
	}
	ca, cb := makeTLS(), makeTLS()
	dca, dcb := snapshotWriterDomain(ca, path), snapshotWriterDomain(cb, path)
	if dca == ([32]byte{}) || dca != dcb || dca == da {
		t.Fatal("representable static TLS configs do not coordinate")
	}
	if snapshotWriterDomain(ca, "") != ([32]byte{}) {
		t.Fatal("unrepresented custom root pool retained")
	}
	cb.single.TLSConfig.RootCAs = x509.NewCertPool()
	if snapshotWriterDomain(cb, path) != ([32]byte{}) {
		t.Fatal("different trust roots conflated")
	}
	customTLS := map[string]func(*tls.Config){
		"certificates": func(c *tls.Config) { c.Certificates = []tls.Certificate{{}} },
		"get_client_certificate": func(c *tls.Config) {
			c.GetClientCertificate = func(*tls.CertificateRequestInfo) (*tls.Certificate, error) { panic("must not invoke") }
		},
		"get_certificate": func(c *tls.Config) {
			c.GetCertificate = func(*tls.ClientHelloInfo) (*tls.Certificate, error) { panic("must not invoke") }
		},
		"get_config": func(c *tls.Config) {
			c.GetConfigForClient = func(*tls.ClientHelloInfo) (*tls.Config, error) { panic("must not invoke") }
		},
		"verify_peer": func(c *tls.Config) {
			c.VerifyPeerCertificate = func([][]byte, [][]*x509.Certificate) error { panic("must not invoke") }
		},
		"verify_connection": func(c *tls.Config) { c.VerifyConnection = func(tls.ConnectionState) error { panic("must not invoke") } },
		"insecure":          func(c *tls.Config) { c.InsecureSkipVerify = true },
		"session_cache":     func(c *tls.Config) { c.ClientSessionCache = tls.NewLRUClientSessionCache(1) },
		"next_protocols":    func(c *tls.Config) { c.NextProtos = []string{"example"} },
		"cipher_suites":     func(c *tls.Config) { c.CipherSuites = []uint16{tls.TLS_AES_128_GCM_SHA256} },
		"curves":            func(c *tls.Config) { c.CurvePreferences = []tls.CurveID{tls.X25519} },
		"max_version":       func(c *tls.Config) { c.MaxVersion = tls.VersionTLS13 },
		"tickets_disabled":  func(c *tls.Config) { c.SessionTicketsDisabled = true },
		"random":            func(c *tls.Config) { c.Rand = strings.NewReader("not used") },
		"clock":             func(c *tls.Config) { c.Time = func() time.Time { panic("must not invoke") } },
		"keylog":            func(c *tls.Config) { c.KeyLogWriter = io.Discard },
	}
	for name, change := range customTLS {
		t.Run(name, func(t *testing.T) {
			c := makeTLS()
			change(c.single.TLSConfig)
			if snapshotWriterDomain(c, path) != ([32]byte{}) {
				t.Fatal("custom TLS behavior retained")
			}
		})
	}

	// Cancellation must not keep queue state if registered while already canceled.
	var r snapshotWriterRegistry
	for i := 0; i < 100; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		turn, err := r.join(ctx, da, "t")
		if err != nil {
			t.Fatal(err)
		}
		cancel()
		turn.release()
	}
	writerTestEmpty(t, &r)
}

func waitWriterRegistryRequests(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		snapshotWriteTurns.mu.Lock()
		got := snapshotWriteTurns.requests
		snapshotWriteTurns.mu.Unlock()
		if got == n {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("writer registry did not reach expected request count", n)
}

func TestSnapshotWriterHandoffOption(t *testing.T) {
	ctx := context.Background()
	d, err := newDriverImpl(memory.DefaultAllocator).newDatabaseImpl(ctx, testUsageOptions(map[string]string{adbc.OptionKeyURI: "redis://unused.invalid:1/0?protocol=3"}))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close(ctx)
	value, err := d.GetOption(ctx, OptionStringWriterAdmissionQueue)
	if err != nil || value != "false" {
		t.Fatal("queue must default false", value, err)
	}
	cfg, err := d.clientConfig()
	if err != nil || cfg.writerDomain != ([32]byte{}) {
		t.Fatal("default unexpectedly fingerprinted identity", err)
	}
	for _, value := range []string{"TRUE", "1", "", "yes"} {
		if err := d.SetOption(ctx, OptionStringWriterAdmissionQueue, value); !isStatus(err, adbc.StatusInvalidArgument) {
			t.Fatal("invalid queue option accepted", value, err)
		}
	}
	if err := d.SetOption(ctx, OptionStringWriterAdmissionQueue, "true"); err != nil {
		t.Fatal(err)
	}
	cfg, err = d.clientConfig()
	if err != nil || cfg.writerDomain == ([32]byte{}) {
		t.Fatal("enabled static identity not fingerprinted", err)
	}
	value, err = d.GetOption(ctx, OptionStringWriterAdmissionQueue)
	if err != nil || value != "true" {
		t.Fatal(value, err)
	}
	prior := cfg.writerDomain
	if err := d.SetOption(ctx, OptionStringWriterAdmissionQueue, "false"); err != nil {
		t.Fatal(err)
	}
	fresh, err := d.clientConfig()
	if err != nil || fresh.writerDomain != ([32]byte{}) || cfg.writerDomain != prior {
		t.Fatal("option changed an existing config or failed to disable future configs", err)
	}
}
