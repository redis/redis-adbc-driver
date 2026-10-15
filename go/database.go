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

package redis

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/adbc-drivers/driverbase-go/driverbase"
	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

type databaseImpl struct {
	foreignIndexes bool
	driverbase.DatabaseImplBase

	uri          string
	address      string
	username     string
	password     string
	db           int
	schema       string
	pushdown     string
	queryWorkers int
	workerBudget *workerBudget
	cluster      string
	rekey        bool
	// stream is adbc.redis.stream_results and adbc.redis.stream_batch_rows.
	stream streamSettings
	// timeouts are adbc.redis.read_timeout and write_timeout (see
	// timeout.go).
	timeouts timeoutSettings
	// timeZone is adbc.redis.time_zone (text "" if not set).
	timeZone paramValue
}

// clientConfig is what the driver makes its clients from (see newClient):
// go-redis's options for both kinds of client, from the URI (or the address
// and database) and the credentials.
type clientConfig struct {
	diagnostics atomic.Pointer[executionDiagnostics]
	// single are a single-endpoint client's options.
	single *goredis.Options
	// cluster are an OSS Cluster API client's: the same, the URI's address
	// as the first seed node, and the URI's cluster parameters.
	cluster *goredis.ClusterOptions
	// clusterParams names the cluster parameters that the URI sets.
	clusterParams []string
}

// uriClusterParams are the URI parameters that go-redis parses for a
// cluster client only (ParseClusterURL), not for a single endpoint
// (ParseURL). The driver takes them when it connects to a cluster, except
// that it refuses to read from replicas (read_only, route_by_latency or
// route_randomly true): a replica may not have the driver's latest writes
// yet (a table just created, rows just written).
var uriClusterParams = []string{"addr", "max_redirects", "dialer_retries", "dialer_retry_timeout", "failing_timeout_seconds",
	"read_only", "route_by_latency", "route_randomly"}

// clientConfig returns the clients' options: from the URI, or the address
// and database, and the credentials.
func (d *databaseImpl) clientConfig() (*clientConfig, error) {
	opts := &goredis.Options{Addr: d.address, DB: d.db}
	var extra *goredis.ClusterOptions
	var params []string
	if d.uri != "" {
		var err error
		if opts, extra, params, err = parseURI(d.uri); err != nil {
			return nil, errorf(adbc.StatusInvalidArgument, "invalid URI: %v", err)
		}
	}
	if d.username != "" {
		opts.Username = d.username
	}
	if d.password != "" {
		opts.Password = d.password
	}
	// go-redis uses RESP3 by default. Set it explicitly so that the same
	// protocol reaches a cluster's node clients, while preserving a URI's
	// explicit protocol=2 compatibility setting (or protocol=3).
	if opts.Protocol == 0 {
		opts.Protocol = 3
	}
	opts.DisableIdentity = true
	cfg := newClientConfig(opts)
	cfg.clusterParams = params
	if extra != nil {
		cfg.cluster.Addrs = append(cfg.cluster.Addrs, extra.Addrs[1:]...)
		cfg.cluster.MaxRedirects = extra.MaxRedirects
		cfg.cluster.DialerRetries = extra.DialerRetries
		cfg.cluster.DialerRetryTimeout = extra.DialerRetryTimeout
		cfg.cluster.FailingTimeoutSeconds = extra.FailingTimeoutSeconds
	}
	return cfg, nil
}

// parseURI parses a URI with go-redis: its single-endpoint options, and
// (nil if the URI has none) its cluster parameters, which it names.
func parseURI(uri string) (opts *goredis.Options, cluster *goredis.ClusterOptions, params []string, err error) {
	u, err := url.Parse(uri)
	if err != nil {
		return nil, nil, nil, err
	}
	q := u.Query()
	clusterQuery := url.Values{}
	for _, name := range uriClusterParams {
		if q.Has(name) {
			clusterQuery[name] = q[name]
			params = append(params, name)
			q.Del(name)
		}
	}
	if len(params) == 0 {
		opts, err = goredis.ParseURL(uri)
		return opts, nil, nil, err
	}
	u.RawQuery = q.Encode()
	if opts, err = goredis.ParseURL(u.String()); err != nil {
		return nil, nil, nil, err
	}
	// ParseClusterURL's first seed is the URI's address.
	if cluster, err = goredis.ParseClusterURL("redis://seed?" + clusterQuery.Encode()); err != nil {
		return nil, nil, nil, err
	}
	for _, p := range []struct {
		name string
		on   bool
	}{{"read_only", cluster.ReadOnly}, {"route_by_latency", cluster.RouteByLatency}, {"route_randomly", cluster.RouteRandomly}} {
		if p.on {
			return nil, nil, nil, fmt.Errorf("%s is not supported: the driver reads from primaries only, so that it sees its own writes", p.name)
		}
	}
	return opts, cluster, params, nil
}

// newClientConfig returns the config of clients with the given options.
func newClientConfig(opts *goredis.Options) *clientConfig {
	return &clientConfig{single: opts, cluster: clusterOptions(opts)}
}

// clusterOptions returns a cluster client's options with the same settings
// as a single-endpoint client's, and its address as the seed node. go-redis
// gives every node's client the settings (ClusterOptions.clientOptions).
// Some mean something else on a cluster: pool_size and max_active_conns are
// per node, and max_retries is a node client's, within each of the cluster
// client's attempts (by default a node's client doesn't retry).
func clusterOptions(o *goredis.Options) *goredis.ClusterOptions {
	return &goredis.ClusterOptions{
		Addrs:      []string{o.Addr},
		ClientName: o.ClientName,
		Dialer:     o.Dialer,
		OnConnect:  o.OnConnect,

		Protocol:                     o.Protocol,
		Username:                     o.Username,
		Password:                     o.Password,
		CredentialsProvider:          o.CredentialsProvider,
		CredentialsProviderContext:   o.CredentialsProviderContext,
		StreamingCredentialsProvider: o.StreamingCredentialsProvider,

		MaxRetries:      o.MaxRetries,
		MinRetryBackoff: o.MinRetryBackoff,
		MaxRetryBackoff: o.MaxRetryBackoff,

		DialTimeout:           o.DialTimeout,
		DialerRetries:         o.DialerRetries,
		DialerRetryTimeout:    o.DialerRetryTimeout,
		DialerRetryBackoff:    o.DialerRetryBackoff,
		ReadTimeout:           o.ReadTimeout,
		WriteTimeout:          o.WriteTimeout,
		ContextTimeoutEnabled: o.ContextTimeoutEnabled,

		MaxConcurrentDials:    o.MaxConcurrentDials,
		PoolFIFO:              o.PoolFIFO,
		PoolSize:              o.PoolSize,
		PoolTimeout:           o.PoolTimeout,
		MinIdleConns:          o.MinIdleConns,
		MaxIdleConns:          o.MaxIdleConns,
		MaxActiveConns:        o.MaxActiveConns,
		ConnMaxIdleTime:       o.ConnMaxIdleTime,
		ConnMaxLifetime:       o.ConnMaxLifetime,
		ConnMaxLifetimeJitter: o.ConnMaxLifetimeJitter,

		ReadBufferSize:          o.ReadBufferSize,
		WriteBufferSize:         o.WriteBufferSize,
		PipelineReadBufferSize:  o.PipelineReadBufferSize,
		PipelineWriteBufferSize: o.PipelineWriteBufferSize,
		PipelinePoolSize:        o.PipelinePoolSize,
		AutoPipelineOptions:     o.AutoPipelineOptions,

		TLSConfig: o.TLSConfig,

		DisableIndentity:          o.DisableIndentity,
		DisableIdentity:           o.DisableIdentity,
		IdentitySuffix:            o.IdentitySuffix,
		UnstableResp3:             o.UnstableResp3,
		PushNotificationProcessor: o.PushNotificationProcessor,
		FailingTimeoutSeconds:     o.FailingTimeoutSeconds,
		MaintNotificationsConfig:  o.MaintNotificationsConfig,
	}
}

// dial connects to Redis as Open does, without checking the server or
// writing anything.
func (d *databaseImpl) dial(ctx context.Context) (client goredis.UniversalClient, cfg *clientConfig, t timeouts, writeFollows bool, err error) {
	if cfg, err = d.clientConfig(); err != nil {
		return nil, nil, t, false, err
	}
	t, writeFollows = d.timeouts.resolve(cfg.single)
	client, err = d.connect(ctx, cfg, t)
	return client, cfg, t, writeFollows, err
}

func (d *databaseImpl) Open(ctx context.Context) (adbc.ConnectionWithContext, error) {
	client, cfg, t, writeFollows, err := d.dial(ctx)
	if err != nil {
		return nil, err
	}
	st := &store{client: client}
	if err := st.searchDo(ctx, "", "FT._LIST").Err(); err != nil {
		_ = client.Close()
		return nil, errorf(adbc.StatusNotImplemented, "the Redis server does not provide the search (RediSearch) module: %v", err)
	}
	if err := st.refuseFlex(ctx); err != nil {
		_ = client.Close()
		return nil, err
	}
	if err := st.migrateLegacyMetadata(ctx); err != nil {
		_ = client.Close()
		return nil, err
	}
	if err := st.ensureRegistry(ctx); err != nil {
		_ = client.Close()
		return nil, err
	}
	// Finish removing dropped columns' fields left over by earlier connections.
	if err := st.resumeCleanups(ctx); err != nil {
		_ = client.Close()
		return nil, err
	}
	// Roll back or finish renames that were moving rows when their
	// connection went away, before any statement runs (see rekey.go).
	if err := st.recoverDead(ctx); err != nil {
		_ = client.Close()
		return nil, err
	}
	// Drop temporary objects of connections that exited without closing.
	st.sweepTemp(ctx)
	st.foreignIndexes = d.foreignIndexes
	conn := &connectionImpl{
		ConnectionImplBase: driverbase.NewConnectionImplBase(&d.DatabaseImplBase),
		store:              st,
		sess:               d.newSession(),
		pushdown:           d.pushdown,
		queryWorkers:       d.queryWorkers,
		workerBudget:       d.workerBudget,
		rekey:              d.rekey,
		stream:             d.stream,
		clientCfg:          cfg,
		timeouts:           t,
		writeFollows:       writeFollows,
	}
	if cfg.single.Username != "" {
		conn.sess.user = cfg.single.Username
	}
	return driverbase.NewConnectionBuilder(conn).
		WithCurrentNamespacer(conn).
		WithDriverInfoPreparer(conn).
		WithTableTypeLister(conn).
		WithDbObjectsEnumerator(conn).
		Connection(), nil
}

// flexProbeIndex is an index name the driver never creates.
const flexProbeIndex = "adbc:{meta}:probe"

// refuseFlex fails on Redis Flex (RAM + flash) databases, before anything
// is written. Search on Flex has no FT.AGGREGATE and no NUMERIC fields (the
// Redis Cloud Pro Preview, and Redis Software 8.2 with Search 8.6, which
// also lacks SORTABLE), and every table needs them. Asking for a missing
// index tells Flex apart: other servers answer "No such index", so a Flex
// release that adds FT.AGGREGATE passes.
func (s *store) refuseFlex(ctx context.Context) error {
	err := s.searchDo(ctx, flexProbeIndex, "FT.AGGREGATE", flexProbeIndex, "*", "LIMIT", 0, 0).Err()
	if err != nil && strings.Contains(err.Error(), "not supported in Redis Flex") {
		return errorf(adbc.StatusNotImplemented, "Redis Flex databases are not supported: their search has no "+
			"FT.AGGREGATE and no NUMERIC or SORTABLE fields, which the driver needs (%v)", err)
	}
	return nil
}

// connect opens a single-endpoint or OSS Cluster API client, depending on
// the cluster option and (for "auto") what the server reports.
func (d *databaseImpl) connect(ctx context.Context, cfg *clientConfig, t timeouts) (goredis.UniversalClient, error) {
	if d.cluster == "false" && len(cfg.clusterParams) > 0 {
		return nil, clusterParamsError(cfg.clusterParams, OptionStringCluster+" is false")
	}
	useCluster := d.cluster == "true"
	if d.cluster != "true" {
		single := newClient(cfg, false, t)
		if err := single.Ping(ctx).Err(); err != nil {
			_ = single.Close()
			return nil, errorf(adbc.StatusIO, "failed to connect to Redis: %v", err)
		}
		if d.cluster == "false" {
			return single, nil
		}
		// Proxied databases (Redis Cloud, Redis Software) report
		// cluster_enabled:0 or reject the section; both mean single endpoint.
		info, err := single.Info(ctx, "cluster").Result()
		if err != nil || !strings.Contains(info, "cluster_enabled:1") {
			if len(cfg.clusterParams) > 0 {
				_ = single.Close()
				return nil, clusterParamsError(cfg.clusterParams, "the server is a single endpoint")
			}
			return single, nil
		}
		_ = single.Close()
		useCluster = true
	}
	if !useCluster {
		return nil, errorf(adbc.StatusInternal, "unreachable")
	}
	if cfg.single.DB != 0 {
		return nil, errorf(adbc.StatusInvalidArgument, "Redis Cluster only supports database 0 (got %d)", cfg.single.DB)
	}
	cluster := newClient(cfg, true, t)
	if err := cluster.Ping(ctx).Err(); err != nil {
		_ = cluster.Close()
		return nil, errorf(adbc.StatusIO, "failed to connect to Redis Cluster: %v", err)
	}
	return cluster, nil
}

// clusterParamsError is the error for a URI with cluster parameters when
// the client is a single endpoint's.
func clusterParamsError(params []string, why string) error {
	verb := "applies"
	if len(params) > 1 {
		verb = "apply"
	}
	return errorf(adbc.StatusInvalidArgument, "invalid URI: %s %s only to a Redis Cluster (OSS Cluster API), and %s",
		strings.Join(params, ", "), verb, why)
}

func (d *databaseImpl) Close(ctx context.Context) error { return nil }

func (d *databaseImpl) GetOption(ctx context.Context, key string) (string, error) {
	if key == OptionStringForeignIndexes {
		return strconv.FormatBool(d.foreignIndexes), nil
	}
	switch key {
	case adbc.OptionKeyURI:
		return d.uri, nil
	case adbc.OptionKeyUsername:
		return d.username, nil
	case OptionStringAddress:
		return d.address, nil
	case OptionIntDB:
		return strconv.Itoa(d.db), nil
	case OptionStringDefaultSchema:
		return d.schema, nil
	case OptionStringAggregatePushdown:
		return d.pushdown, nil
	case OptionStringQueryWorkerBudget, OptionIntQueryWorkersActive, OptionIntQueryWorkersQueued, OptionStringQueryWorkerTuning:
		return d.workerBudget.option(key), nil
	case OptionStringQueryWorkers:
		return strconv.Itoa(effectiveQueryWorkers(d.queryWorkers)), nil
	case OptionStringCluster:
		return d.cluster, nil
	case OptionStringRenameRekey:
		return strconv.FormatBool(d.rekey), nil
	case OptionStringStreamResults, OptionIntStreamBatchRows:
		return d.stream.get(key), nil
	case OptionStringReadTimeout, OptionStringWriteTimeout:
		// The timeout a connection opened now gets.
		opts := &goredis.Options{}
		if cfg, err := d.clientConfig(); err == nil {
			opts = cfg.single
		}
		t, _ := d.timeouts.resolve(opts)
		if key == OptionStringReadTimeout {
			return formatTimeout(t.read), nil
		}
		return formatTimeout(t.write), nil
	case OptionStringTimeZone:
		if d.timeZone.text == "" {
			return utcParam.text, nil
		}
		return d.timeZone.text, nil
	}
	return d.DatabaseImplBase.GetOption(ctx, key)
}

func (d *databaseImpl) SetOption(ctx context.Context, key, value string) error {
	switch key {
	case OptionStringForeignIndexes:
		v, err := parseForeignIndexes(value)
		if err != nil {
			return err
		}
		d.foreignIndexes = v
	case adbc.OptionKeyURI:
		d.uri = value
	case adbc.OptionKeyUsername:
		d.username = value
	case adbc.OptionKeyPassword:
		d.password = value
	case OptionStringAddress:
		d.address = value
	case OptionIntDB:
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 {
			return errorf(adbc.StatusInvalidArgument, "invalid %s: %q", key, value)
		}
		d.db = n
	case OptionStringDefaultSchema:
		if value == "" {
			return errorf(adbc.StatusInvalidArgument, "%s must not be empty", key)
		}
		if reservedSchema(value) {
			return errorf(adbc.StatusInvalidArgument, "%s: schema name %q is reserved for temporary tables and views", key, value)
		}
		d.schema = value
	case OptionStringQueryWorkerBudget:
		return d.workerBudget.configure(value)
	case OptionStringQueryWorkers:
		if value == "auto" {
			d.queryWorkers = 0
			return nil
		}
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > 32 {
			return errorf(adbc.StatusInvalidArgument, "invalid %s %q (want auto or 1 through 32)", key, value)
		}
		d.queryWorkers = n
	case OptionStringAggregatePushdown:
		if err := validatePushdown(value); err != nil {
			return err
		}
		d.pushdown = value
	case OptionStringCluster:
		switch value {
		case "auto", "true", "false":
			d.cluster = value
		default:
			return errorf(adbc.StatusInvalidArgument, "invalid %s %q (want auto, true or false)", key, value)
		}
	case OptionStringRenameRekey:
		rekey, err := parseRenameRekey(value)
		if err != nil {
			return err
		}
		d.rekey = rekey
	case OptionStringStreamResults, OptionIntStreamBatchRows:
		return d.stream.set(key, value)
	case OptionStringReadTimeout, OptionStringWriteTimeout:
		t, err := parseTimeout(key, value)
		if err != nil {
			return err
		}
		if key == OptionStringReadTimeout {
			d.timeouts.read = &t
		} else {
			d.timeouts.write = &t
		}
	case OptionStringTimeZone:
		tz, err := timeZoneParam(value)
		if err != nil {
			return err
		}
		d.timeZone = tz
	default:
		return d.DatabaseImplBase.SetOption(ctx, key, value)
	}
	return nil
}

// newSession is a new connection's settings.
func (d *databaseImpl) newSession() *session {
	s := newSession(d.schema)
	if d.timeZone.text != "" {
		s.defaultZone = d.timeZone
	}
	return s
}

func (d *databaseImpl) SetOptions(ctx context.Context, options map[string]string) error {
	for k, v := range options {
		if err := d.SetOption(ctx, k, v); err != nil {
			return err
		}
	}
	return nil
}
