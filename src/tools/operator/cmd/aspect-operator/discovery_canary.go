package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// service-discovery-canary drives traffic against billing's public API while
// the operator rolls iam-service. Billing's auth path goes:
//
//	HAProxy -> billing -> billing.auth.middleware -> IAM /internal/v1/authorize
//
// so any non-2xx-or-4xx outcome on this path during a deploy implicates the
// runtime catalog resolver. 4xx means the IAM round-trip completed (auth
// denied is success for our purposes); 5xx, EOF, and timeouts are the failures
// we care about.
type discoveryCanaryConfig struct {
	personaEnvPath string
	endpointPath  string
	duration      time.Duration
	rps           int
	concurrency   int
	timeout       time.Duration
	outputFormat  string
}

type discoveryCanaryResult struct {
	StartedAt        time.Time `json:"started_at"`
	CompletedAt      time.Time `json:"completed_at"`
	Endpoint         string    `json:"endpoint"`
	TargetRPS        int       `json:"target_rps"`
	TotalRequests    int64     `json:"total_requests"`
	SuccessRequests  int64     `json:"success_requests"`
	FailureRequests  int64     `json:"failure_requests"`
	StatusCounts     map[int]int64 `json:"status_counts"`
	NetworkErrors    int64     `json:"network_errors"`
	ZeroDowntime     bool      `json:"zero_downtime"`
	LatencyP50Millis int64     `json:"latency_p50_ms"`
	LatencyP95Millis int64     `json:"latency_p95_ms"`
	LatencyP99Millis int64     `json:"latency_p99_ms"`
	LatencyMaxMillis int64     `json:"latency_max_ms"`
	FirstError       string    `json:"first_error,omitempty"`
}

func cmdDiscoveryCanary(args []string) error {
	fs := flagSet("service-discovery-canary")
	cfg := discoveryCanaryConfig{}
	fs.StringVar(&cfg.personaEnvPath, "persona-env", "smoke-artifacts/personas/platform-admin.env", "Path to persona env file produced by `aspect persona assume`.")
	fs.StringVar(&cfg.endpointPath, "endpoint-path", "/api/v1/plans", "Billing API path to call. Any /api/v1/* path exercises the billing -> IAM authorize round-trip.")
	fs.DurationVar(&cfg.duration, "duration", 180*time.Second, "How long to drive traffic.")
	fs.IntVar(&cfg.rps, "rps", 10, "Target requests per second across all workers.")
	fs.IntVar(&cfg.concurrency, "concurrency", 4, "Number of parallel workers.")
	fs.DurationVar(&cfg.timeout, "timeout", 5*time.Second, "Per-request timeout.")
	fs.StringVar(&cfg.outputFormat, "format", "table", "Output format: table | json.")
	if err := fs.Parse(args); err != nil {
		return err
	}

	personaEnv, err := loadPersonaEnvFile(cfg.personaEnvPath)
	if err != nil {
		return fmt.Errorf("load persona env: %w", err)
	}
	domain := strings.TrimSpace(personaEnv["VERSELF_DOMAIN"])
	if domain == "" {
		return fmt.Errorf("persona env %s missing VERSELF_DOMAIN", cfg.personaEnvPath)
	}
	token := firstNonEmpty(
		personaEnv["IAM_SERVICE_ACCESS_TOKEN"],
		personaEnv["BILLING_ACCESS_TOKEN"],
		personaEnv["VERSELF_TOKEN"],
	)
	if token == "" {
		return fmt.Errorf("persona env %s missing access token (IAM_SERVICE_ACCESS_TOKEN | BILLING_ACCESS_TOKEN | VERSELF_TOKEN)", cfg.personaEnvPath)
	}
	endpoint := "https://billing.api." + domain + cfg.endpointPath

	ctx, cancel := context.WithTimeout(context.Background(), cfg.duration+5*time.Second)
	defer cancel()
	result := runDiscoveryCanary(ctx, cfg, endpoint, token)

	switch cfg.outputFormat {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(result); err != nil {
			return err
		}
	default:
		printDiscoveryCanaryTable(result)
	}
	if !result.ZeroDowntime {
		return exitError{code: 2}
	}
	return nil
}

func runDiscoveryCanary(ctx context.Context, cfg discoveryCanaryConfig, endpoint, token string) discoveryCanaryResult {
	startedAt := time.Now()
	tickInterval := time.Second / time.Duration(cfg.rps)
	if tickInterval <= 0 {
		tickInterval = time.Millisecond
	}
	deadlineCtx, deadlineCancel := context.WithDeadline(ctx, startedAt.Add(cfg.duration))
	defer deadlineCancel()

	httpClient := &http.Client{Timeout: cfg.timeout}
	work := make(chan struct{}, cfg.concurrency*2)
	var wg sync.WaitGroup
	var (
		total     atomic.Int64
		success   atomic.Int64
		failure   atomic.Int64
		netErrors atomic.Int64
		latencies = newLatencyRecorder()
		statuses  = newStatusRecorder()
		firstErr  atomic.Value
	)
	for range cfg.concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range work {
				start := time.Now()
				req, err := http.NewRequestWithContext(deadlineCtx, http.MethodGet, endpoint, nil)
				if err != nil {
					failure.Add(1)
					netErrors.Add(1)
					firstErr.CompareAndSwap(nil, "build request: "+err.Error())
					continue
				}
				req.Header.Set("Authorization", "Bearer "+token)
				req.Header.Set("Accept", "application/json")
				resp, err := httpClient.Do(req)
				elapsed := time.Since(start)
				latencies.add(elapsed)
				total.Add(1)
				if err != nil {
					failure.Add(1)
					netErrors.Add(1)
					firstErr.CompareAndSwap(nil, "request: "+err.Error())
					continue
				}
				_ = resp.Body.Close()
				statuses.add(resp.StatusCode)
				if resp.StatusCode >= 500 {
					failure.Add(1)
					firstErr.CompareAndSwap(nil, fmt.Sprintf("HTTP %d", resp.StatusCode))
					continue
				}
				success.Add(1)
			}
		}()
	}

	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
loop:
	for {
		select {
		case <-deadlineCtx.Done():
			break loop
		case <-ticker.C:
			select {
			case work <- struct{}{}:
			default:
				// queue full; skip this tick rather than backpressure the producer.
			}
		}
	}
	close(work)
	wg.Wait()

	completedAt := time.Now()
	first := ""
	if v := firstErr.Load(); v != nil {
		first, _ = v.(string)
	}
	p50, p95, p99, max := latencies.percentiles()
	totalCount := total.Load()
	failureCount := failure.Load()
	return discoveryCanaryResult{
		StartedAt:        startedAt,
		CompletedAt:      completedAt,
		Endpoint:         endpoint,
		TargetRPS:        cfg.rps,
		TotalRequests:    totalCount,
		SuccessRequests:  success.Load(),
		FailureRequests:  failureCount,
		StatusCounts:     statuses.snapshot(),
		NetworkErrors:    netErrors.Load(),
		ZeroDowntime:     failureCount == 0 && totalCount > 0,
		LatencyP50Millis: p50.Milliseconds(),
		LatencyP95Millis: p95.Milliseconds(),
		LatencyP99Millis: p99.Milliseconds(),
		LatencyMaxMillis: max.Milliseconds(),
		FirstError:       first,
	}
}

func printDiscoveryCanaryTable(r discoveryCanaryResult) {
	w := os.Stdout
	fmt.Fprintln(w, "service-discovery-canary")
	fmt.Fprintf(w, "  endpoint        %s\n", r.Endpoint)
	fmt.Fprintf(w, "  duration        %s\n", r.CompletedAt.Sub(r.StartedAt).Round(time.Millisecond))
	fmt.Fprintf(w, "  target rps      %d\n", r.TargetRPS)
	fmt.Fprintf(w, "  total           %d\n", r.TotalRequests)
	fmt.Fprintf(w, "  success         %d\n", r.SuccessRequests)
	fmt.Fprintf(w, "  failure (5xx)   %d\n", r.FailureRequests-r.NetworkErrors)
	fmt.Fprintf(w, "  network errors  %d\n", r.NetworkErrors)
	fmt.Fprintf(w, "  zero-downtime   %v\n", r.ZeroDowntime)
	fmt.Fprintf(w, "  latency p50/p95/p99/max  %dms / %dms / %dms / %dms\n",
		r.LatencyP50Millis, r.LatencyP95Millis, r.LatencyP99Millis, r.LatencyMaxMillis)
	if len(r.StatusCounts) > 0 {
		codes := make([]int, 0, len(r.StatusCounts))
		for c := range r.StatusCounts {
			codes = append(codes, c)
		}
		sort.Ints(codes)
		fmt.Fprintf(w, "  status counts   ")
		for i, c := range codes {
			if i > 0 {
				fmt.Fprint(w, " ")
			}
			fmt.Fprintf(w, "%d=%d", c, r.StatusCounts[c])
		}
		fmt.Fprintln(w)
	}
	if r.FirstError != "" {
		fmt.Fprintf(w, "  first error     %s\n", r.FirstError)
	}
}

func loadPersonaEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path) //nolint:gosec // operator-supplied path
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	out := map[string]string{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		value := strings.TrimSpace(line[eq+1:])
		value = strings.Trim(value, "\"'")
		out[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

type latencyRecorder struct {
	mu      sync.Mutex
	samples []time.Duration
}

func newLatencyRecorder() *latencyRecorder { return &latencyRecorder{} }

func (r *latencyRecorder) add(d time.Duration) {
	r.mu.Lock()
	r.samples = append(r.samples, d)
	r.mu.Unlock()
}

func (r *latencyRecorder) percentiles() (p50, p95, p99, max time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.samples) == 0 {
		return 0, 0, 0, 0
	}
	sorted := make([]time.Duration, len(r.samples))
	copy(sorted, r.samples)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	pick := func(p float64) time.Duration {
		idx := int(float64(len(sorted)-1) * p)
		return sorted[idx]
	}
	return pick(0.50), pick(0.95), pick(0.99), sorted[len(sorted)-1]
}

type statusRecorder struct {
	mu     sync.Mutex
	counts map[int]int64
}

func newStatusRecorder() *statusRecorder {
	return &statusRecorder{counts: map[int]int64{}}
}

func (s *statusRecorder) add(code int) {
	s.mu.Lock()
	s.counts[code]++
	s.mu.Unlock()
}

func (s *statusRecorder) snapshot() map[int]int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[int]int64, len(s.counts))
	for k, v := range s.counts {
		out[k] = v
	}
	return out
}

