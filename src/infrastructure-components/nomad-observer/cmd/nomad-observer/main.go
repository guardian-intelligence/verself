package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/hashicorp/nomad/api"
	verselfotel "github.com/verself/observability/otel"
	workloadauth "github.com/verself/service-runtime/workload"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	serviceName    = "nomad-observer"
	serviceVersion = "0.1.0"
	defaultNS      = "default"

	maxTailBytes      = 1024 * 1024
	dedupeMaxAge      = 24 * time.Hour
	heartbeatInterval = 60 * time.Second
	logCaptureTimeout = 5 * time.Second
)

var (
	bearerPattern      = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]+`)
	jwtPattern         = regexp.MustCompile(`\b[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b`)
	secretLinePattern  = regexp.MustCompile(`(?i)\b(authorization|proxy-authorization|cookie|set-cookie|token|jwt|access_token|id_token|refresh_token|client_secret|password|passwd|secret|api_key|private_key)\b\s*[:=]\s*([^\s]+)`)
	privateKeyPattern  = regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----`)
	terminalAllocState = map[string]struct{}{
		api.AllocClientStatusFailed: {},
		api.AllocClientStatusLost:   {},
	}
)

type config struct {
	site                  string
	nomadAddr             string
	namespace             string
	podmanMeasureSocket   string
	stderrTailBytes       int64
	stdoutTailBytes       int64
	captureWorkers        int
	captureQueueSize      int
	clickhouseAddr        string
	clickhouseUser        string
	clickhouseCACertPath  string
	spiffeSocket          string
	fleetSnapshotInterval time.Duration
}

type deployMeta struct {
	DeployRunKey   string
	DeploySHA      string
	SpecSHA256     string
	ArtifactSHA256 string
}

func (m deployMeta) empty() bool {
	return m.DeployRunKey == "" && m.DeploySHA == "" && m.SpecSHA256 == "" && m.ArtifactSHA256 == ""
}

type logCaptureRequest struct {
	Namespace    string
	AllocID      string
	JobID        string
	DeploymentID string
	EvalID       string
	TaskGroup    string
	Task         string
	Stream       string
	TailBytes    int64
	Meta         deployMeta
}

type jobMetaCache struct {
	mu     sync.Mutex
	values map[string]deployMeta
}

func newJobMetaCache() *jobMetaCache {
	return &jobMetaCache{values: make(map[string]deployMeta)}
}

func (c *jobMetaCache) get(namespace, jobID string) (deployMeta, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	meta, ok := c.values[jobCacheKey(namespace, jobID)]
	return meta, ok
}

func (c *jobMetaCache) put(namespace, jobID string, meta deployMeta) {
	if jobID == "" {
		return
	}
	c.mu.Lock()
	c.values[jobCacheKey(namespace, jobID)] = meta
	c.mu.Unlock()
}

func jobCacheKey(namespace, jobID string) string {
	if namespace == "" {
		namespace = defaultNS
	}
	return namespace + "\x00" + jobID
}

type captureDedupe struct {
	mu   sync.Mutex
	seen map[string]time.Time
}

func newCaptureDedupe() *captureDedupe {
	return &captureDedupe{seen: make(map[string]time.Time)}
}

func (d *captureDedupe) mark(req logCaptureRequest) bool {
	key := req.Namespace + "\x00" + req.AllocID + "\x00" + req.Task + "\x00" + req.Stream
	now := time.Now()
	d.mu.Lock()
	defer d.mu.Unlock()
	if firstSeen, ok := d.seen[key]; ok && now.Sub(firstSeen) < dedupeMaxAge {
		return false
	}
	if len(d.seen) > 4096 {
		for candidate, firstSeen := range d.seen {
			if now.Sub(firstSeen) > dedupeMaxAge {
				delete(d.seen, candidate)
			}
		}
	}
	d.seen[key] = now
	return true
}

type observerStats struct {
	events       atomic.Uint64
	failures     atomic.Uint64
	captures     atomic.Uint64
	captureDrops atomic.Uint64
	streamErrors atomic.Uint64
}

type observer struct {
	cfg     config
	client  *api.Client
	ch      clickhouse.Conn
	logger  *slog.Logger
	tracer  trace.Tracer
	meta    *jobMetaCache
	dedupe  *captureDedupe
	stats   observerStats
	capture chan logCaptureRequest
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if envString("NOMAD_OBSERVER_MODE", "observer") == "podman-measurer" {
		cfg, err := podmanMeasurerConfigFromEnv()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		if err := runPodmanMeasurer(ctx, cfg); err != nil && !errors.Is(err, context.Canceled) {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	cfg, err := configFromEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err := run(ctx, cfg); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg config) error {
	shutdown, logger, err := verselfotel.Init(ctx, verselfotel.Config{
		ServiceName:    serviceName,
		ServiceVersion: serviceVersion,
	})
	if err != nil {
		return fmt.Errorf("otel init: %w", err)
	}
	defer func() {
		flushCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_ = shutdown(flushCtx)
		cancel()
	}()
	logger = logger.With(
		slog.String("verself.supervisor", strings.TrimSpace(os.Getenv("VERSELF_SUPERVISOR"))),
	)
	slog.SetDefault(logger)

	nomadConfig := api.DefaultConfig()
	nomadConfig.Address = cfg.nomadAddr
	nomadConfig.HttpClient = nomadHTTPClient()
	nomadClient, err := api.NewClient(nomadConfig)
	if err != nil {
		return fmt.Errorf("nomad client: %w", err)
	}

	chConn, err := startFleetProjector(ctx, cfg, nomadClient, logger)
	if err != nil {
		logger.Warn("nomad-observer clickhouse sink disabled", slog.Any("error", err))
	}

	obs := &observer{
		cfg:     cfg,
		client:  nomadClient,
		ch:      chConn,
		logger:  logger,
		tracer:  otel.Tracer(serviceName),
		meta:    newJobMetaCache(),
		dedupe:  newCaptureDedupe(),
		capture: make(chan logCaptureRequest, cfg.captureQueueSize),
	}
	for i := 0; i < cfg.captureWorkers; i++ {
		go obs.captureWorker(ctx, i)
	}
	go obs.heartbeat(ctx)

	return obs.streamLoop(ctx)
}

func nomadHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxConnsPerHost = 16
	transport.MaxIdleConns = 16
	transport.MaxIdleConnsPerHost = 4
	return &http.Client{Transport: transport}
}

func startFleetProjector(ctx context.Context, cfg config, nomadClient *api.Client, logger *slog.Logger) (clickhouse.Conn, error) {
	if cfg.clickhouseCACertPath == "" {
		return nil, errors.New("VERSELF_CRED_CLICKHOUSE_CA_CERT must not be empty")
	}
	if cfg.spiffeSocket == "" {
		return nil, fmt.Errorf("%s must not be empty", workloadauth.EndpointSocketEnv)
	}
	spiffeSource, err := workloadauth.Source(ctx, cfg.spiffeSocket)
	if err != nil {
		return nil, fmt.Errorf("spiffe source: %w", err)
	}
	chTLS, err := workloadauth.TLSConfigWithX509SourceAndCABundle(ctx, spiffeSource, cfg.clickhouseCACertPath)
	if err != nil {
		_ = spiffeSource.Close()
		return nil, fmt.Errorf("clickhouse tls: %w", err)
	}
	chConn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{cfg.clickhouseAddr},
		Auth: clickhouse.Auth{Database: "verself", Username: cfg.clickhouseUser},
		TLS:  chTLS,
	})
	if err != nil {
		_ = spiffeSource.Close()
		return nil, fmt.Errorf("open clickhouse: %w", err)
	}
	pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
	err = chConn.Ping(pingCtx)
	pingCancel()
	if err != nil {
		_ = chConn.Close()
		_ = spiffeSource.Close()
		return nil, fmt.Errorf("ping clickhouse: %w", err)
	}
	region, err := nomadClient.Agent().Region()
	if err != nil {
		_ = chConn.Close()
		_ = spiffeSource.Close()
		return nil, fmt.Errorf("nomad region: %w", err)
	}
	go func() {
		<-ctx.Done()
		_ = chConn.Close()
		_ = spiffeSource.Close()
	}()
	go (&fleetProjector{
		nomad:    nomadClient,
		ch:       chConn,
		region:   region,
		interval: cfg.fleetSnapshotInterval,
		logger:   logger,
	}).run(ctx)
	return chConn, nil
}

func configFromEnv() (config, error) {
	stderrTailBytes, err := envInt64("NOMAD_OBSERVER_STDERR_TAIL_BYTES", 64*1024, 0, maxTailBytes)
	if err != nil {
		return config{}, err
	}
	stdoutTailBytes, err := envInt64("NOMAD_OBSERVER_STDOUT_TAIL_BYTES", 32*1024, 0, maxTailBytes)
	if err != nil {
		return config{}, err
	}
	captureWorkers, err := envInt("NOMAD_OBSERVER_CAPTURE_WORKERS", 4, 1, 32)
	if err != nil {
		return config{}, err
	}
	captureQueueSize, err := envInt("NOMAD_OBSERVER_CAPTURE_QUEUE_SIZE", 128, 1, 4096)
	if err != nil {
		return config{}, err
	}
	fleetIntervalSeconds, err := envInt("NOMAD_OBSERVER_FLEET_SNAPSHOT_INTERVAL_SECONDS", 30, 5, 3600)
	if err != nil {
		return config{}, err
	}
	cfg := config{
		site:                  envString("VERSELF_SITE", ""),
		nomadAddr:             envString("NOMAD_ADDR", "http://127.0.0.1:4646"),
		namespace:             envString("NOMAD_NAMESPACE", defaultNS),
		podmanMeasureSocket:   envString(podmanMeasureSocketEnv, ""),
		stderrTailBytes:       stderrTailBytes,
		stdoutTailBytes:       stdoutTailBytes,
		captureWorkers:        captureWorkers,
		captureQueueSize:      captureQueueSize,
		clickhouseAddr:        envString("VERSELF_CLICKHOUSE_ADDRESS", "127.0.0.1:9440"),
		clickhouseUser:        envString("VERSELF_CLICKHOUSE_USER", "nomad_observer"),
		clickhouseCACertPath:  envString("VERSELF_CRED_CLICKHOUSE_CA_CERT", ""),
		spiffeSocket:          envString(workloadauth.EndpointSocketEnv, ""),
		fleetSnapshotInterval: time.Duration(fleetIntervalSeconds) * time.Second,
	}
	if cfg.nomadAddr == "" {
		return config{}, errors.New("NOMAD_ADDR must not be empty")
	}
	if cfg.site == "" {
		return config{}, errors.New("VERSELF_SITE must not be empty")
	}
	if cfg.namespace == "" {
		cfg.namespace = defaultNS
	}
	return cfg, nil
}

func envString(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func envInt(key string, fallback, minValue, maxValue int) (int, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	if parsed < minValue {
		return 0, fmt.Errorf("%s must be >= %d", key, minValue)
	}
	if parsed > maxValue {
		return 0, fmt.Errorf("%s must be <= %d", key, maxValue)
	}
	return parsed, nil
}

func envInt64(key string, fallback, minValue, maxValue int64) (int64, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	if parsed < minValue {
		return 0, fmt.Errorf("%s must be >= %d", key, minValue)
	}
	if parsed > maxValue {
		return 0, fmt.Errorf("%s must be <= %d", key, maxValue)
	}
	return parsed, nil
}

func (o *observer) streamLoop(ctx context.Context) error {
	topics := map[api.Topic][]string{
		api.TopicDeployment: {},
		api.TopicEvaluation: {},
		api.TopicAllocation: {},
		api.TopicJob:        {},
		api.TopicNode:       {},
	}
	var nextIndex uint64
	backoff := time.Second
	for ctx.Err() == nil {
		streamBaseCtx, streamCancel := context.WithCancel(ctx)
		streamCtx, span := o.tracer.Start(streamBaseCtx, "nomad_observer.event_stream",
			trace.WithAttributes(
				attribute.String("nomad.namespace", o.cfg.namespace),
				attribute.Int64("nomad.stream.index", int64(nextIndex)),
			),
		)
		events, err := o.client.EventStream().Stream(streamCtx, topics, nextIndex, &api.QueryOptions{Namespace: o.cfg.namespace})
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			span.End()
			o.stats.streamErrors.Add(1)
			o.logger.WarnContext(streamCtx, "nomad.observer.stream_open_failed",
				slog.String("nomad.namespace", o.cfg.namespace),
				slog.Uint64("nomad.stream.index", nextIndex),
				slog.String("error", err.Error()),
			)
			streamCancel()
			if !sleepContext(ctx, backoff) {
				return ctx.Err()
			}
			backoff = growBackoff(backoff)
			continue
		}
		o.logger.InfoContext(streamCtx, "nomad.observer.stream_connected",
			slog.String("nomad.namespace", o.cfg.namespace),
			slog.Uint64("nomad.stream.index", nextIndex),
		)
		backoff = time.Second
		for batch := range events {
			if batch.Err != nil {
				o.stats.streamErrors.Add(1)
				span.RecordError(batch.Err)
				span.SetStatus(codes.Error, batch.Err.Error())
				o.logger.WarnContext(streamCtx, "nomad.observer.stream_error",
					slog.String("nomad.namespace", o.cfg.namespace),
					slog.Uint64("nomad.stream.index", nextIndex),
					slog.String("error", batch.Err.Error()),
				)
				break
			}
			if batch.Index >= nextIndex {
				nextIndex = batch.Index + 1
			}
			for i := range batch.Events {
				event := batch.Events[i]
				if event.Index >= nextIndex {
					nextIndex = event.Index + 1
				}
				o.stats.events.Add(1)
				o.handleEvent(streamCtx, event)
			}
		}
		span.End()
		streamCancel()
		if !sleepContext(ctx, backoff) {
			return ctx.Err()
		}
		backoff = growBackoff(backoff)
	}
	return ctx.Err()
}

func growBackoff(current time.Duration) time.Duration {
	current *= 2
	if current > 30*time.Second {
		return 30 * time.Second
	}
	return current
}

func sleepContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (o *observer) heartbeat(ctx context.Context) {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			o.logger.InfoContext(ctx, "nomad.observer.heartbeat",
				slog.Uint64("nomad.observer.events", o.stats.events.Load()),
				slog.Uint64("nomad.observer.failures", o.stats.failures.Load()),
				slog.Uint64("nomad.observer.log_captures", o.stats.captures.Load()),
				slog.Uint64("nomad.observer.capture_drops", o.stats.captureDrops.Load()),
				slog.Uint64("nomad.observer.stream_errors", o.stats.streamErrors.Load()),
			)
		}
	}
}

func (o *observer) handleEvent(ctx context.Context, event api.Event) {
	switch event.Topic {
	case api.TopicJob:
		o.handleJobEvent(ctx, event)
	case api.TopicDeployment:
		o.handleDeploymentEvent(ctx, event)
	case api.TopicEvaluation:
		o.handleEvaluationEvent(ctx, event)
	case api.TopicAllocation:
		o.handleAllocationEvent(ctx, event)
	case api.TopicNode:
		o.handleNodeEvent(ctx, event)
	default:
		o.logNomadEvent(ctx, slog.LevelInfo, "nomad.event", event, nil)
	}
}

func (o *observer) handleJobEvent(ctx context.Context, event api.Event) {
	job, deleted, err := event.DeregisteredJob()
	if err != nil {
		o.logDecodeError(ctx, event, err)
		return
	}
	attrs := eventAttrs(event)
	if job != nil {
		namespace := ptrValue(job.Namespace)
		jobID := ptrValue(job.ID)
		meta := metadataFromJob(job)
		o.meta.put(namespace, jobID, meta)
		attrs = append(attrs,
			slog.String("nomad.namespace", normalizeNamespace(namespace, o.cfg.namespace)),
			slog.String("nomad.job_id", jobID),
			slog.Bool("nomad.job.deleted", deleted),
			slog.String("nomad.job.status", ptrValue(job.Status)),
			slog.Uint64("nomad.job.version", ptrUint64(job.Version)),
			slog.Uint64("nomad.job.modify_index", ptrUint64(job.ModifyIndex)),
		)
		attrs = appendMetaAttrs(attrs, meta)
	} else {
		attrs = append(attrs, slog.Bool("nomad.job.deleted", deleted))
	}
	o.logAttrs(ctx, slog.LevelInfo, "nomad.job.event", attrs)
}

func (o *observer) handleDeploymentEvent(ctx context.Context, event api.Event) {
	deployment, err := event.Deployment()
	if err != nil {
		o.logDecodeError(ctx, event, err)
		return
	}
	attrs := eventAttrs(event)
	level := slog.LevelInfo
	if deployment != nil {
		meta, err := o.metadataForJob(ctx, deployment.Namespace, deployment.JobID)
		if err != nil {
			attrs = append(attrs, slog.String("nomad.metadata_error", err.Error()))
		}
		desired, placed, healthy, unhealthy := deploymentAllocCounts(deployment)
		attrs = append(attrs,
			slog.String("nomad.namespace", normalizeNamespace(deployment.Namespace, o.cfg.namespace)),
			slog.String("nomad.job_id", deployment.JobID),
			slog.String("nomad.deployment_id", deployment.ID),
			slog.String("nomad.deployment.status", deployment.Status),
			slog.String("nomad.deployment.status_description", deployment.StatusDescription),
			slog.Uint64("nomad.job.version", deployment.JobVersion),
			slog.Uint64("nomad.deployment.modify_index", deployment.ModifyIndex),
			slog.Int("nomad.deployment.desired_total", desired),
			slog.Int("nomad.deployment.placed_allocs", placed),
			slog.Int("nomad.deployment.healthy_allocs", healthy),
			slog.Int("nomad.deployment.unhealthy_allocs", unhealthy),
		)
		attrs = appendMetaAttrs(attrs, meta)
		if deploymentFailed(deployment.Status) {
			level = slog.LevelWarn
			o.stats.failures.Add(1)
		}
	}
	o.logAttrs(ctx, level, "nomad.deployment.event", attrs)
}

func (o *observer) handleEvaluationEvent(ctx context.Context, event api.Event) {
	eval, err := event.Evaluation()
	if err != nil {
		o.logDecodeError(ctx, event, err)
		return
	}
	attrs := eventAttrs(event)
	level := slog.LevelInfo
	if eval != nil {
		meta, err := o.metadataForJob(ctx, eval.Namespace, eval.JobID)
		if err != nil {
			attrs = append(attrs, slog.String("nomad.metadata_error", err.Error()))
		}
		attrs = append(attrs,
			slog.String("nomad.namespace", normalizeNamespace(eval.Namespace, o.cfg.namespace)),
			slog.String("nomad.eval_id", eval.ID),
			slog.String("nomad.job_id", eval.JobID),
			slog.String("nomad.deployment_id", eval.DeploymentID),
			slog.String("nomad.eval.status", eval.Status),
			slog.String("nomad.eval.status_description", eval.StatusDescription),
			slog.String("nomad.eval.triggered_by", eval.TriggeredBy),
			slog.Int("nomad.eval.queued_allocations", sumIntMap(eval.QueuedAllocations)),
			slog.Int("nomad.eval.failed_task_groups", len(eval.FailedTGAllocs)),
			slog.Uint64("nomad.eval.modify_index", eval.ModifyIndex),
		)
		attrs = appendMetaAttrs(attrs, meta)
		if evaluationFailed(eval.Status) {
			level = slog.LevelWarn
			o.stats.failures.Add(1)
		}
	}
	o.logAttrs(ctx, level, "nomad.evaluation.event", attrs)
}

func (o *observer) handleAllocationEvent(ctx context.Context, event api.Event) {
	alloc, err := event.Allocation()
	if err != nil {
		o.logDecodeError(ctx, event, err)
		return
	}
	attrs := eventAttrs(event)
	level := slog.LevelInfo
	if alloc != nil {
		namespace := normalizeNamespace(alloc.Namespace, o.cfg.namespace)
		meta := metadataFromAlloc(alloc)
		if meta.empty() {
			if cached, err := o.metadataForJob(ctx, namespace, alloc.JobID); err == nil {
				meta = cached
			} else {
				attrs = append(attrs, slog.String("nomad.metadata_error", err.Error()))
			}
		}
		task, taskState, taskEvent := latestTaskFailure(alloc.TaskStates)
		attrs = append(attrs,
			slog.String("nomad.namespace", namespace),
			slog.String("nomad.alloc_id", alloc.ID),
			slog.String("nomad.alloc_name", alloc.Name),
			slog.String("nomad.job_id", alloc.JobID),
			slog.String("nomad.eval_id", alloc.EvalID),
			slog.String("nomad.deployment_id", alloc.DeploymentID),
			slog.String("nomad.task_group", alloc.TaskGroup),
			slog.String("nomad.node_id", alloc.NodeID),
			slog.String("nomad.node_name", alloc.NodeName),
			slog.String("nomad.alloc.client_status", alloc.ClientStatus),
			slog.String("nomad.alloc.desired_status", alloc.DesiredStatus),
			slog.String("nomad.alloc.client_description", alloc.ClientDescription),
			slog.String("nomad.alloc.desired_description", alloc.DesiredDescription),
			slog.Uint64("nomad.alloc.modify_index", alloc.ModifyIndex),
		)
		if task != "" {
			attrs = append(attrs,
				slog.String("nomad.task", task),
				slog.String("nomad.task.state", taskState.State),
				slog.Bool("nomad.task.failed", taskState.Failed),
				slog.Uint64("nomad.task.restarts", taskState.Restarts),
			)
		}
		if taskEvent != nil {
			attrs = append(attrs,
				slog.String("nomad.task.event.type", taskEvent.Type),
				slog.String("nomad.task.event.message", firstNonEmpty(taskEvent.DisplayMessage, taskEvent.Message, taskEvent.DriverError, taskEvent.SetupError)),
				slog.Int("nomad.task.event.exit_code", taskEvent.ExitCode),
				slog.String("nomad.task.event.driver_error", taskEvent.DriverError),
			)
		}
		attrs = appendMetaAttrs(attrs, meta)
		o.recordWorkloadOCIEvidence(ctx, alloc, meta)
		if allocationFailed(alloc.ClientStatus) {
			level = slog.LevelWarn
			o.stats.failures.Add(1)
			o.enqueueLogCaptures(ctx, alloc, meta)
		}
	}
	o.logAttrs(ctx, level, "nomad.allocation.event", attrs)
}

func (o *observer) handleNodeEvent(ctx context.Context, event api.Event) {
	node, err := event.Node()
	if err != nil {
		o.logDecodeError(ctx, event, err)
		return
	}
	attrs := eventAttrs(event)
	level := slog.LevelInfo
	if node != nil {
		attrs = append(attrs,
			slog.String("nomad.node_id", node.ID),
			slog.String("nomad.node_name", node.Name),
			slog.String("nomad.node.datacenter", node.Datacenter),
			slog.String("nomad.node.status", node.Status),
			slog.String("nomad.node.status_description", node.StatusDescription),
			slog.String("nomad.node.eligibility", node.SchedulingEligibility),
			slog.Bool("nomad.node.drain", node.Drain),
			slog.Uint64("nomad.node.modify_index", node.ModifyIndex),
		)
		if strings.EqualFold(node.Status, "down") || strings.EqualFold(node.Status, "lost") {
			level = slog.LevelWarn
			o.stats.failures.Add(1)
		}
	}
	o.logAttrs(ctx, level, "nomad.node.event", attrs)
}

func (o *observer) logDecodeError(ctx context.Context, event api.Event, err error) {
	attrs := eventAttrs(event)
	attrs = append(attrs, slog.String("error", err.Error()))
	o.logAttrs(ctx, slog.LevelWarn, "nomad.event.decode_failed", attrs)
}

func (o *observer) logNomadEvent(ctx context.Context, level slog.Level, message string, event api.Event, attrs []slog.Attr) {
	o.logAttrs(ctx, level, message, append(eventAttrs(event), attrs...))
}

func (o *observer) logAttrs(ctx context.Context, level slog.Level, message string, attrs []slog.Attr) {
	attrs = append([]slog.Attr{slog.String("nomad.event_name", message)}, attrs...)
	o.logger.LogAttrs(ctx, level, message, attrs...)
}

func eventAttrs(event api.Event) []slog.Attr {
	return []slog.Attr{
		slog.String("nomad.topic", event.Topic.String()),
		slog.String("nomad.event.type", event.Type),
		slog.String("nomad.event.key", event.Key),
		slog.Uint64("nomad.event.index", event.Index),
	}
}

func appendMetaAttrs(attrs []slog.Attr, meta deployMeta) []slog.Attr {
	if meta.DeployRunKey != "" {
		attrs = append(attrs, slog.String("verself.deploy_run_key", meta.DeployRunKey))
	}
	if meta.DeploySHA != "" {
		attrs = append(attrs, slog.String("verself.deploy_sha", meta.DeploySHA))
	}
	if meta.SpecSHA256 != "" {
		attrs = append(attrs, slog.String("verself.deploy_spec_sha256", meta.SpecSHA256))
	}
	if meta.ArtifactSHA256 != "" {
		attrs = append(attrs, slog.String("verself.artifact_sha256", meta.ArtifactSHA256))
	}
	return attrs
}

func setMetaSpanAttributes(span trace.Span, meta deployMeta) {
	if meta.DeployRunKey != "" {
		span.SetAttributes(attribute.String("verself.deploy_run_key", meta.DeployRunKey))
	}
	if meta.DeploySHA != "" {
		span.SetAttributes(attribute.String("verself.deploy_sha", meta.DeploySHA))
	}
	if meta.SpecSHA256 != "" {
		span.SetAttributes(attribute.String("verself.deploy_spec_sha256", meta.SpecSHA256))
	}
	if meta.ArtifactSHA256 != "" {
		span.SetAttributes(attribute.String("verself.artifact_sha256", meta.ArtifactSHA256))
	}
}

func (o *observer) metadataForJob(ctx context.Context, namespace, jobID string) (deployMeta, error) {
	if jobID == "" {
		return deployMeta{}, nil
	}
	namespace = normalizeNamespace(namespace, o.cfg.namespace)
	if meta, ok := o.meta.get(namespace, jobID); ok {
		return meta, nil
	}
	job, _, err := o.client.Jobs().Info(jobID, (&api.QueryOptions{Namespace: namespace}).WithContext(ctx))
	if err != nil {
		return deployMeta{}, err
	}
	meta := metadataFromJob(job)
	o.meta.put(namespace, jobID, meta)
	return meta, nil
}

func metadataFromJob(job *api.Job) deployMeta {
	if job == nil {
		return deployMeta{}
	}
	return metadataFromMap(job.Meta)
}

func metadataFromAlloc(alloc *api.Allocation) deployMeta {
	if alloc == nil || alloc.Job == nil {
		return deployMeta{}
	}
	return metadataFromJob(alloc.Job)
}

func metadataFromMap(values map[string]string) deployMeta {
	return deployMeta{
		DeployRunKey:   values["deploy_run_key"],
		DeploySHA:      values["deploy_sha"],
		SpecSHA256:     values["spec_sha256"],
		ArtifactSHA256: values["artifact_sha256"],
	}
}

func (o *observer) enqueueLogCaptures(ctx context.Context, alloc *api.Allocation, meta deployMeta) {
	tasks := failedTaskNames(alloc.TaskStates)
	if len(tasks) == 0 {
		return
	}
	namespace := normalizeNamespace(alloc.Namespace, o.cfg.namespace)
	for _, task := range tasks {
		for _, stream := range []string{api.FSLogNameStderr, api.FSLogNameStdout} {
			tailBytes := o.cfg.stderrTailBytes
			if stream == api.FSLogNameStdout {
				tailBytes = o.cfg.stdoutTailBytes
			}
			if tailBytes <= 0 {
				continue
			}
			req := logCaptureRequest{
				Namespace:    namespace,
				AllocID:      alloc.ID,
				JobID:        alloc.JobID,
				DeploymentID: alloc.DeploymentID,
				EvalID:       alloc.EvalID,
				TaskGroup:    alloc.TaskGroup,
				Task:         task,
				Stream:       stream,
				TailBytes:    tailBytes,
				Meta:         meta,
			}
			if !o.dedupe.mark(req) {
				continue
			}
			select {
			case o.capture <- req:
			default:
				o.stats.captureDrops.Add(1)
				o.logger.LogAttrs(ctx, slog.LevelWarn, "nomad.alloc.log_capture_dropped",
					append(captureAttrs(req, 0), slog.String("nomad.event_name", "nomad.alloc.log_capture_dropped"))...)
			}
		}
	}
}

func (o *observer) captureWorker(ctx context.Context, workerID int) {
	for {
		select {
		case <-ctx.Done():
			return
		case req := <-o.capture:
			o.captureLogTail(ctx, workerID, req)
		}
	}
}

func (o *observer) captureLogTail(ctx context.Context, workerID int, req logCaptureRequest) {
	ctx, cancel := context.WithTimeout(ctx, logCaptureTimeout)
	defer cancel()
	ctx, span := o.tracer.Start(ctx, "nomad_observer.log_capture",
		trace.WithAttributes(
			attribute.String("nomad.namespace", req.Namespace),
			attribute.String("nomad.alloc_id", req.AllocID),
			attribute.String("nomad.job_id", req.JobID),
			attribute.String("nomad.deployment_id", req.DeploymentID),
			attribute.String("nomad.eval_id", req.EvalID),
			attribute.String("nomad.task_group", req.TaskGroup),
			attribute.String("nomad.task", req.Task),
			attribute.String("nomad.log.stream", req.Stream),
			attribute.Int64("nomad.log.tail_bytes", req.TailBytes),
			attribute.Int("nomad.observer.worker_id", workerID),
		),
	)
	defer span.End()

	alloc, _, err := o.client.Allocations().Info(req.AllocID, (&api.QueryOptions{Namespace: req.Namespace}).WithContext(ctx))
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		o.logger.LogAttrs(ctx, slog.LevelWarn, "nomad.alloc.log_capture_failed",
			append(captureAttrs(req, 0), slog.String("nomad.event_name", "nomad.alloc.log_capture_failed"), slog.String("error", err.Error()))...)
		return
	}
	if alloc != nil {
		if req.JobID == "" {
			req.JobID = alloc.JobID
		}
		if req.DeploymentID == "" {
			req.DeploymentID = alloc.DeploymentID
		}
		if req.EvalID == "" {
			req.EvalID = alloc.EvalID
		}
		if req.TaskGroup == "" {
			req.TaskGroup = alloc.TaskGroup
		}
		if req.Meta.empty() {
			req.Meta = metadataFromAlloc(alloc)
		}
		if req.Meta.empty() {
			if meta, err := o.metadataForJob(ctx, req.Namespace, req.JobID); err == nil {
				req.Meta = meta
			}
		}
	}
	setMetaSpanAttributes(span, req.Meta)

	body, err := o.readLogTail(ctx, alloc, req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		o.logger.LogAttrs(ctx, slog.LevelWarn, "nomad.alloc.log_capture_failed",
			append(captureAttrs(req, 0), slog.String("nomad.event_name", "nomad.alloc.log_capture_failed"), slog.String("error", err.Error()))...)
		return
	}
	span.SetAttributes(attribute.Int("nomad.log.captured_bytes", len(body)))
	if len(bytes.TrimSpace(body)) == 0 {
		o.logger.LogAttrs(ctx, slog.LevelInfo, "nomad.alloc.log_tail_empty",
			append(captureAttrs(req, 0), slog.String("nomad.event_name", "nomad.alloc.log_tail_empty"))...)
		return
	}
	redacted := redactLogTail(string(body))
	redacted = trimStringTail(redacted, req.TailBytes)
	o.stats.captures.Add(1)
	o.logger.LogAttrs(ctx, slog.LevelWarn, redacted,
		append(captureAttrs(req, len(redacted)), slog.String("nomad.event_name", "nomad.alloc."+req.Stream+"_tail"))...)
}

func (o *observer) readLogTail(ctx context.Context, alloc *api.Allocation, req logCaptureRequest) ([]byte, error) {
	if alloc == nil {
		return nil, errors.New("allocation not found")
	}
	cancel := make(chan struct{})
	defer close(cancel)
	frames, errCh := o.client.AllocFS().Logs(
		alloc,
		false,
		req.Task,
		req.Stream,
		api.OriginEnd,
		req.TailBytes,
		cancel,
		(&api.QueryOptions{Namespace: req.Namespace}).WithContext(ctx),
	)
	if frames == nil {
		select {
		case err := <-errCh:
			if err == nil {
				err = errors.New("log stream unavailable")
			}
			return nil, err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	var buf bytes.Buffer
	limit := int(req.TailBytes)
	for {
		select {
		case frame, ok := <-frames:
			if !ok {
				if limit > 0 && buf.Len() > limit {
					trimBuffer(&buf, limit)
				}
				return buf.Bytes(), nil
			}
			if frame == nil || len(frame.Data) == 0 {
				continue
			}
			_, _ = buf.Write(frame.Data)
			if limit > 0 && buf.Len() > limit*2 {
				trimBuffer(&buf, limit)
			}
		case err := <-errCh:
			if err != nil {
				return nil, err
			}
		case <-ctx.Done():
			if buf.Len() > 0 {
				if limit > 0 && buf.Len() > limit {
					trimBuffer(&buf, limit)
				}
				return buf.Bytes(), nil
			}
			return nil, ctx.Err()
		}
	}
}

func trimBuffer(buf *bytes.Buffer, limit int) {
	if limit <= 0 || buf.Len() <= limit {
		return
	}
	data := append([]byte(nil), buf.Bytes()[buf.Len()-limit:]...)
	buf.Reset()
	_, _ = buf.Write(data)
}

func trimStringTail(value string, maxBytes int64) string {
	if maxBytes <= 0 || int64(len(value)) <= maxBytes {
		return value
	}
	start := len(value) - int(maxBytes)
	for start < len(value) && !utf8.RuneStart(value[start]) {
		start++
	}
	return value[start:]
}

func captureAttrs(req logCaptureRequest, capturedBytes int) []slog.Attr {
	attrs := []slog.Attr{
		slog.String("nomad.namespace", req.Namespace),
		slog.String("nomad.alloc_id", req.AllocID),
		slog.String("nomad.job_id", req.JobID),
		slog.String("nomad.deployment_id", req.DeploymentID),
		slog.String("nomad.eval_id", req.EvalID),
		slog.String("nomad.task_group", req.TaskGroup),
		slog.String("nomad.task", req.Task),
		slog.String("nomad.log.stream", req.Stream),
		slog.Int64("nomad.log.tail_bytes", req.TailBytes),
		slog.Int("nomad.log.captured_bytes", capturedBytes),
	}
	return appendMetaAttrs(attrs, req.Meta)
}

func failedTaskNames(states map[string]*api.TaskState) []string {
	if len(states) == 0 {
		return nil
	}
	tasks := make([]string, 0, len(states))
	for name, state := range states {
		if state == nil {
			continue
		}
		if taskStateFailed(state) {
			tasks = append(tasks, name)
		}
	}
	if len(tasks) == 0 {
		for name := range states {
			tasks = append(tasks, name)
		}
	}
	sort.Strings(tasks)
	if len(tasks) > 8 {
		tasks = tasks[:8]
	}
	return tasks
}

func latestTaskFailure(states map[string]*api.TaskState) (string, api.TaskState, *api.TaskEvent) {
	if len(states) == 0 {
		return "", api.TaskState{}, nil
	}
	names := make([]string, 0, len(states))
	for name := range states {
		names = append(names, name)
	}
	sort.Strings(names)
	var selectedTask string
	var selectedState *api.TaskState
	var selectedEvent *api.TaskEvent
	var selectedTime int64
	selectedFailed := false
	for _, name := range names {
		state := states[name]
		if state == nil {
			continue
		}
		event := latestTaskEvent(state)
		eventTime := int64(0)
		if event != nil {
			eventTime = event.Time
		}
		failed := taskStateFailed(state)
		if selectedState == nil || (failed && !selectedFailed) || (failed == selectedFailed && eventTime > selectedTime) {
			selectedTask = name
			selectedState = state
			selectedEvent = event
			selectedTime = eventTime
			selectedFailed = failed
		}
	}
	if selectedState == nil {
		return "", api.TaskState{}, nil
	}
	return selectedTask, *selectedState, selectedEvent
}

func latestTaskEvent(state *api.TaskState) *api.TaskEvent {
	if state == nil || len(state.Events) == 0 {
		return nil
	}
	return state.Events[len(state.Events)-1]
}

func taskStateFailed(state *api.TaskState) bool {
	if state == nil {
		return false
	}
	if state.Failed || strings.EqualFold(state.State, "dead") {
		return true
	}
	event := latestTaskEvent(state)
	return event != nil && (event.FailsTask || event.ExitCode != 0 || event.DriverError != "" || event.SetupError != "" || event.ValidationError != "" || event.DownloadError != "")
}

func allocationFailed(status string) bool {
	_, ok := terminalAllocState[status]
	return ok
}

func deploymentFailed(status string) bool {
	switch status {
	case api.DeploymentStatusFailed, api.DeploymentStatusCancelled, api.DeploymentStatusBlocked:
		return true
	default:
		return false
	}
}

func evaluationFailed(status string) bool {
	switch status {
	case api.EvalStatusFailed, api.EvalStatusCancelled, api.EvalStatusBlocked:
		return true
	default:
		return false
	}
}

func deploymentAllocCounts(deployment *api.Deployment) (desired, placed, healthy, unhealthy int) {
	if deployment == nil {
		return 0, 0, 0, 0
	}
	for _, state := range deployment.TaskGroups {
		if state == nil {
			continue
		}
		desired += state.DesiredTotal
		placed += state.PlacedAllocs
		healthy += state.HealthyAllocs
		unhealthy += state.UnhealthyAllocs
	}
	return desired, placed, healthy, unhealthy
}

func sumIntMap(values map[string]int) int {
	total := 0
	for _, value := range values {
		total += value
	}
	return total
}

func ptrValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func ptrUint64(value *uint64) uint64 {
	if value == nil {
		return 0
	}
	return *value
}

func normalizeNamespace(namespace, fallback string) string {
	namespace = strings.TrimSpace(namespace)
	if namespace != "" {
		return namespace
	}
	fallback = strings.TrimSpace(fallback)
	if fallback != "" {
		return fallback
	}
	return defaultNS
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func redactLogTail(value string) string {
	if !utf8.ValidString(value) {
		value = strings.ToValidUTF8(value, "\uFFFD")
	}
	value = privateKeyPattern.ReplaceAllString(value, "[redacted:private-key]")
	value = bearerPattern.ReplaceAllString(value, "Bearer [redacted:jwt]")
	value = jwtPattern.ReplaceAllString(value, "[redacted:jwt]")
	value = secretLinePattern.ReplaceAllString(value, "$1=[redacted:secret]")
	return strings.TrimRight(value, "\r\n")
}
