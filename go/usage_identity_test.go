// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type usageIdentityTransport func(*http.Request) (*http.Response, error)

func (f usageIdentityTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestUsageECSAutomaticWorkloadIdentity(t *testing.T) {
	metadata := map[string]string{"Cluster": "arn:aws:ecs:us-east-1:123456789012:cluster/analytics", "TaskARN": "arn:aws:ecs:us-east-1:123456789012:task/analytics/container-one", "ServiceName": "reporting"}
	endpoint := "http://169.254.170.2/v4/container-one"
	calls := 0
	d := usageIdentityDiscoverer{getenv: func(key string) string {
		if key == "ECS_CONTAINER_METADATA_URI_V4" {
			return endpoint
		}
		return ""
	}, transport: usageIdentityTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.URL.Path != "/v4/container-one/task" {
			t.Errorf("unexpected metadata path %s", req.URL.Path)
		}
		data, _ := json.Marshal(metadata)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(data))), Header: make(http.Header)}, nil
	})}
	a := d.ecs(context.Background())
	if a == nil || a.Provider != "ecs_service" || len(a.Key) != 64 {
		t.Fatalf("missing automatic identity: %+v", a)
	}
	metadata["TaskARN"] = "arn:aws:ecs:us-east-1:123456789012:task/analytics/replacement"
	metadata["Cluster"] = "analytics"
	b := d.ecs(context.Background())
	if b == nil || a.Key != b.Key {
		t.Fatal("replacement container or cluster ARN formatting changed deployment identity")
	}
	metadata["ServiceName"] = "other-service"
	if c := d.ecs(context.Background()); c == nil || c.Key == a.Key {
		t.Fatal("different services were conflated")
	}
	delete(metadata, "ServiceName")
	if d.ecs(context.Background()) != nil {
		t.Fatal("task family/cluster alone must not identify a service")
	}
	before := calls
	endpoint = "https://external.example/metadata"
	if d.ecs(context.Background()) != nil || calls != before {
		t.Fatal("metadata URL escaped the local ECS endpoint")
	}
	raw, _ := json.Marshal(a)
	for _, private := range []string{"123456789012", "reporting", "analytics", "container-one"} {
		if strings.Contains(string(raw), private) {
			t.Fatalf("raw platform metadata exported: %s", private)
		}
	}
}

func TestUsageKubernetesControllerAcrossRollouts(t *testing.T) {
	pod := "pod-one"
	deny := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer private-token" {
			t.Error("missing service-account authentication")
		}
		if deny {
			w.WriteHeader(403)
			return
		}
		var object any
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v1/namespaces/team/pods/"):
			replica := "rs-one"
			if strings.HasSuffix(r.URL.Path, "pod-two") {
				replica = "rs-two"
			}
			object = map[string]any{"metadata": map[string]any{"ownerReferences": []any{map[string]any{"kind": "ReplicaSet", "name": replica, "uid": "uid-" + replica, "controller": true}}}}
		case strings.HasPrefix(r.URL.Path, "/apis/apps/v1/namespaces/team/replicasets/"):
			replica := strings.TrimPrefix(r.URL.Path, "/apis/apps/v1/namespaces/team/replicasets/")
			object = map[string]any{"metadata": map[string]any{"uid": "uid-" + replica, "ownerReferences": []any{map[string]any{"kind": "Deployment", "name": "private-app", "uid": "controller-uid", "controller": true}}}}
		default:
			t.Errorf("unexpected API request %s", r.URL.Path)
			w.WriteHeader(404)
			return
		}
		json.NewEncoder(w).Encode(object)
	}))
	defer server.Close()
	address, _ := url.Parse(server.URL)
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	d := usageIdentityDiscoverer{getenv: func(key string) string {
		switch key {
		case "KUBERNETES_SERVICE_HOST":
			return address.Hostname()
		case "KUBERNETES_SERVICE_PORT_HTTPS":
			return address.Port()
		case "HOSTNAME":
			return pod
		}
		return ""
	}, readFile: func(path string) ([]byte, error) {
		switch {
		case strings.HasSuffix(path, "/namespace"):
			return []byte("team"), nil
		case strings.HasSuffix(path, "/token"):
			return []byte("private-token"), nil
		default:
			return ca, nil
		}
	}}
	first := d.kubernetes(context.Background())
	pod = "pod-two"
	second := d.kubernetes(context.Background())
	if first == nil || second == nil || first.Key != second.Key {
		t.Fatal("ReplicaSet rollout split the deployment")
	}
	raw, _ := json.Marshal(first)
	for _, private := range []string{"private-token", "private-app", "controller-uid", "team"} {
		if strings.Contains(string(raw), private) {
			t.Fatal("platform metadata escaped into report")
		}
	}
	deny = true
	if d.kubernetes(context.Background()) != nil {
		t.Fatal("RBAC denial must fall back without inventing an identity")
	}
}

func TestUsageMetadataDeadlineAndRedirect(t *testing.T) {
	d := usageIdentityDiscoverer{transport: usageIdentityTransport(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	var value any
	if usageMetadataJSON(ctx, d.client(nil), "http://169.254.170.2/task", "", &value) {
		t.Fatal("timed-out discovery succeeded")
	}
	calls := 0
	d.transport = usageIdentityTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://external.example"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	if usageMetadataJSON(context.Background(), d.client(nil), "http://169.254.170.2/task", "private-token", &value) || calls != 1 {
		t.Fatal("redirected metadata credentials")
	}
}
