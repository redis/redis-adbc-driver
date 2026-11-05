// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// A runtime ID spans database handles, but never survives a process restart.
var usageRuntimeInstanceID = uuid.NewString()

type usageWorkloadIdentity struct {
	Provider string `json:"provider"`
	Key      string `json:"key"`
}

var usagePlatformDiscovery struct {
	once   sync.Once
	ready  chan struct{}
	result *usageWorkloadIdentity
}

func usageDiscoverPlatform() <-chan struct{} {
	usagePlatformDiscovery.once.Do(func() {
		usagePlatformDiscovery.ready = make(chan struct{})
		go func() {
			defer close(usagePlatformDiscovery.ready)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			usagePlatformDiscovery.result = (usageIdentityDiscoverer{getenv: os.Getenv, readFile: os.ReadFile}).discover(ctx)
		}()
	})
	return usagePlatformDiscovery.ready
}

type usageIdentityDiscoverer struct {
	getenv   func(string) string
	readFile func(string) ([]byte, error)
	// Tests can replace the transport without contacting metadata services.
	transport http.RoundTripper
}

func usageWorkloadDigest(provider string, parts ...string) *usageWorkloadIdentity {
	raw, _ := json.Marshal(append([]string{"v1", provider}, parts...))
	digest := sha256.Sum256(raw)
	return &usageWorkloadIdentity{Provider: provider, Key: hex.EncodeToString(digest[:])}
}

func (d usageIdentityDiscoverer) client(roots *x509.CertPool) *http.Client {
	transport := d.transport
	if transport == nil {
		// Never forward metadata traffic or service-account credentials through a proxy.
		transport = &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}, DisableKeepAlives: true}
	}
	return &http.Client{Transport: transport, Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func usageMetadataJSON(ctx context.Context, client *http.Client, endpoint, token string, out any) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := client.Do(req)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return false
	}
	return json.NewDecoder(io.LimitReader(response.Body, 256*1024)).Decode(out) == nil
}

func (d usageIdentityDiscoverer) discover(ctx context.Context) *usageWorkloadIdentity {
	if identity := d.ecs(ctx); identity != nil {
		return identity
	}
	return d.kubernetes(ctx)
}

func (d usageIdentityDiscoverer) ecs(ctx context.Context) *usageWorkloadIdentity {
	endpoint := d.getenv("ECS_CONTAINER_METADATA_URI_V4")
	if endpoint == "" {
		return nil
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "http" || u.Hostname() != "169.254.170.2" || (u.Port() != "" && u.Port() != "80") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil
	}
	var task struct{ Cluster, TaskARN, ServiceName string }
	if !usageMetadataJSON(ctx, d.client(nil), strings.TrimRight(endpoint, "/")+"/task", "", &task) {
		return nil
	}
	arn := strings.SplitN(task.TaskARN, ":", 6)
	// Family alone can be shared by unrelated services. Never merge on it.
	if len(arn) != 6 || arn[0] != "arn" || arn[2] != "ecs" || arn[3] == "" || len(arn[4]) != 12 || task.Cluster == "" || task.ServiceName == "" {
		return nil
	}
	for _, digit := range arn[4] {
		if digit < '0' || digit > '9' {
			return nil
		}
	}
	cluster := task.Cluster
	if strings.HasPrefix(cluster, "arn:") {
		parts := strings.SplitN(cluster, ":", 6)
		if len(parts) != 6 || parts[1] != arn[1] || parts[3] != arn[3] || parts[4] != arn[4] || !strings.HasPrefix(parts[5], "cluster/") {
			return nil
		}
		cluster = strings.TrimPrefix(parts[5], "cluster/")
	}
	return usageWorkloadDigest("ecs_service", arn[1], arn[3], arn[4], cluster, task.ServiceName)
}

type usageKubeObject struct {
	Metadata struct {
		UID             string `json:"uid"`
		OwnerReferences []struct {
			Kind       string `json:"kind"`
			Name       string `json:"name"`
			UID        string `json:"uid"`
			Controller bool   `json:"controller"`
		} `json:"ownerReferences"`
	} `json:"metadata"`
}

func (d usageIdentityDiscoverer) kubernetes(ctx context.Context) *usageWorkloadIdentity {
	host := d.getenv("KUBERNETES_SERVICE_HOST")
	if net.ParseIP(host) == nil {
		return nil
	}
	port := d.getenv("KUBERNETES_SERVICE_PORT_HTTPS")
	if port == "" {
		port = "443"
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return nil
	}
	const directory = "/var/run/secrets/kubernetes.io/serviceaccount/"
	namespace, err := d.readFile(directory + "namespace")
	if err != nil {
		return nil
	}
	token, err := d.readFile(directory + "token")
	if err != nil || len(token) > 65536 {
		return nil
	}
	ca, err := d.readFile(directory + "ca.crt")
	if err != nil {
		return nil
	}
	ns := strings.TrimSpace(string(namespace))
	bearer := strings.TrimSpace(string(token))
	pod := d.getenv("HOSTNAME")
	if ns == "" || len(ns) > 253 || pod == "" || len(pod) > 253 || bearer == "" {
		return nil
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return nil
	}
	client := d.client(roots)
	base := "https://" + net.JoinHostPort(host, port)
	var object usageKubeObject
	if !usageMetadataJSON(ctx, client, base+"/api/v1/namespaces/"+url.PathEscape(ns)+"/pods/"+url.PathEscape(pod), bearer, &object) {
		return nil
	}
	// Follow rollout-specific ReplicaSets/Jobs to their stable owning controller.
	for depth := 0; depth < 2; depth++ {
		found := false
		for _, owner := range object.Metadata.OwnerReferences {
			if !owner.Controller || owner.UID == "" || owner.Name == "" {
				continue
			}
			switch owner.Kind {
			case "Deployment", "StatefulSet", "DaemonSet", "CronJob":
				return usageWorkloadDigest("kubernetes_controller", ns, owner.Kind, owner.UID)
			case "ReplicaSet", "Job":
				if depth != 0 {
					return nil
				}
				resource := "/apis/apps/v1/namespaces/" + url.PathEscape(ns) + "/replicasets/"
				if owner.Kind == "Job" {
					resource = "/apis/batch/v1/namespaces/" + url.PathEscape(ns) + "/jobs/"
				}
				var parent usageKubeObject
				if !usageMetadataJSON(ctx, client, base+resource+url.PathEscape(owner.Name), bearer, &parent) || parent.Metadata.UID != owner.UID {
					return nil
				}
				object = parent
				found = true
			default:
				return nil
			}
			break
		}
		if !found {
			return nil
		}
	}
	return nil
}
