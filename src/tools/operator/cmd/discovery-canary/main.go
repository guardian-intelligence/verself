// Command discovery-canary drives steady traffic billing -> iam-service via
// the runtime catalog resolver and reports any non-2xx-or-4xx outcome.
// It must run as the billing user on a Verself node so the SPIRE workload
// API issues an SVID for spiffe://<td>/svc/billing-service.
//
// Usage:
//
//	discovery-canary --duration=180s --rps=10 --slug=platform [--format=json]
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	iamclient "github.com/verself/iam-service/client"
	verselfotel "github.com/verself/observability/otel"
	auth "github.com/verself/service-runtime/auth"
	runtimeiam "github.com/verself/service-runtime/iam"
	workloadauth "github.com/verself/service-runtime/workload"
)

const (
	serviceName    = "discovery-canary"
	serviceVersion = "1.0.0"
)

type result struct {
	StartedAt        time.Time     `json:"started_at"`
	CompletedAt      time.Time     `json:"completed_at"`
	TargetRPS        int           `json:"target_rps"`
	Duration         time.Duration `json:"duration"`
	TotalRequests    int64         `json:"total_requests"`
	SuccessRequests  int64         `json:"success_requests"`
	FailureRequests  int64         `json:"failure_requests"`
	NetworkErrors    int64         `json:"network_errors"`
	StatusCounts     map[int]int64 `json:"status_counts"`
	AuthzChecks      int64         `json:"authz_checks"`
	AuthzAllowed     int64         `json:"authz_allowed"`
	AuthzDenied      int64         `json:"authz_denied"`
	AuthzErrors      int64         `json:"authz_errors"`
	ZeroDowntime     bool          `json:"zero_downtime"`
	LatencyP50Millis int64         `json:"latency_p50_ms"`
	LatencyP95Millis int64         `json:"latency_p95_ms"`
	LatencyP99Millis int64         `json:"latency_p99_ms"`
	LatencyMaxMillis int64         `json:"latency_max_ms"`
	FirstError       string        `json:"first_error,omitempty"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "discovery-canary: "+err.Error())
		os.Exit(1)
	}
}

func run() error {
	duration := flag.Duration("duration", 180*time.Second, "How long to drive traffic.")
	rps := flag.Int("rps", 10, "Target requests per second across all workers.")
	concurrency := flag.Int("concurrency", 4, "Number of parallel workers.")
	timeout := flag.Duration("timeout", 5*time.Second, "Per-request timeout.")
	slugFlag := flag.String("slug", "platform", "Org slug to resolve. 404 still counts as a successful round-trip.")
	format := flag.String("format", "json", "Output format: json | table.")
	spiffeSocket := flag.String("spiffe-socket", "unix:///run/spire-agent/sockets/agent.sock", "SPIFFE workload API socket address.")
	authzProbe := flag.Bool("authz-probe", true, "Also call the generated IAM operation authorizer with a typed policy.")
	authzOrgID := flag.String("authz-org-id", "1", "Fallback organization ID used for the authorization probe.")
	authzSubject := flag.String("authz-subject", "discovery-canary", "Subject ID used for the authorization probe.")
	authzPermission := flag.String("authz-permission", "iam:organization:read", "Known IAM permission used for the authorization probe.")
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	otelShutdown, _, err := verselfotel.Init(ctx, verselfotel.Config{ServiceName: serviceName, ServiceVersion: serviceVersion})
	if err != nil {
		return fmt.Errorf("otel init: %w", err)
	}
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		_ = otelShutdown(shutdownCtx)
	}()

	source, err := workloadauth.Source(ctx, *spiffeSocket)
	if err != nil {
		return fmt.Errorf("spiffe source: %w", err)
	}
	defer func() { _ = source.Close() }()
	httpClient, err := workloadauth.MTLSClientForService(source, workloadauth.ServiceIAM, nil)
	if err != nil {
		return fmt.Errorf("iam mtls client: %w", err)
	}
	iam, err := iamclient.NewClient(workloadauth.InternalURL(workloadauth.ServiceIAM), iamclient.WithHTTPClient(httpClient))
	if err != nil {
		return fmt.Errorf("iam client: %w", err)
	}
	authorizer := iamclient.NewAuthorizer(iam)
	slug := strings.TrimSpace(*slugFlag)

	startedAt := time.Now()
	deadline := startedAt.Add(*duration)

	tickInterval := time.Second / time.Duration(*rps)
	if tickInterval <= 0 {
		tickInterval = time.Millisecond
	}

	var (
		total        atomic.Int64
		success      atomic.Int64
		failure      atomic.Int64
		netErrors    atomic.Int64
		authzTotal   atomic.Int64
		authzAllowed atomic.Int64
		authzDenied  atomic.Int64
		authzErrors  atomic.Int64
		latencies    = newLatencyRecorder()
		statuses     = newStatusRecorder()
		firstErr     atomic.Value
	)

	work := make(chan struct{}, *concurrency*2)
	var wg sync.WaitGroup
	for range *concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range work {
				perReqCtx, perReqCancel := context.WithTimeout(ctx, *timeout)
				start := time.Now()
				resolveSlug := iamclient.OrgSlug(slug)
				resolveRequest := iamclient.ResolveOrganizationRequest{
					Body: iamclient.ResolveOrganizationInputBody{Slug: &resolveSlug},
				}
				resp, err := iam.ResolveOrganization(perReqCtx, resolveRequest)
				elapsed := time.Since(start)
				perReqCancel()
				latencies.add(elapsed)
				total.Add(1)
				if err != nil {
					failure.Add(1)
					netErrors.Add(1)
					firstErr.CompareAndSwap(nil, "request: "+err.Error())
					continue
				}
				statuses.add(resp.StatusCode)
				if resp.StatusCode >= 500 {
					failure.Add(1)
					firstErr.CompareAndSwap(nil, fmt.Sprintf("HTTP %d", resp.StatusCode))
					continue
				}
				success.Add(1)
				if *authzProbe {
					probeOrgID := authorizationProbeOrgID(*authzOrgID, resp)
					authzCtx, authzCancel := context.WithTimeout(ctx, *timeout)
					decision, err := authorizer.AuthorizeOperation(authzCtx, &auth.Identity{
						Subject: *authzSubject,
						OrgID:   probeOrgID,
					}, runtimeiam.OperationPolicy{
						Permission: runtimeiam.Permission(*authzPermission),
					})
					authzCancel()
					authzTotal.Add(1)
					if err != nil {
						authzErrors.Add(1)
						firstErr.CompareAndSwap(nil, "authorize: "+err.Error())
						continue
					}
					if decision.Allowed {
						authzAllowed.Add(1)
					} else {
						authzDenied.Add(1)
					}
				}
			}
		}()
	}

	deadlineCtx, cancelDeadline := context.WithDeadline(ctx, deadline)
	defer cancelDeadline()
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
				// queue full; skip the tick rather than backpressure the producer.
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
	r := result{
		StartedAt:        startedAt,
		CompletedAt:      completedAt,
		TargetRPS:        *rps,
		Duration:         completedAt.Sub(startedAt).Round(time.Millisecond),
		TotalRequests:    total.Load(),
		SuccessRequests:  success.Load(),
		FailureRequests:  failure.Load(),
		NetworkErrors:    netErrors.Load(),
		StatusCounts:     statuses.snapshot(),
		AuthzChecks:      authzTotal.Load(),
		AuthzAllowed:     authzAllowed.Load(),
		AuthzDenied:      authzDenied.Load(),
		AuthzErrors:      authzErrors.Load(),
		ZeroDowntime:     failure.Load() == 0 && total.Load() > 0 && authzErrors.Load() == 0,
		LatencyP50Millis: p50.Milliseconds(),
		LatencyP95Millis: p95.Milliseconds(),
		LatencyP99Millis: p99.Milliseconds(),
		LatencyMaxMillis: max.Milliseconds(),
		FirstError:       first,
	}

	switch *format {
	case "table":
		if err := printTable(r); err != nil {
			return err
		}
	default:
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(r); err != nil {
			return err
		}
	}
	if !r.ZeroDowntime {
		os.Exit(2)
	}
	return nil
}

func authorizationProbeOrgID(fallback string, resp *iamclient.ResolveOrganizationResponse) string {
	if resp != nil && resp.Result != nil {
		if orgID := strings.TrimSpace(string(resp.Result.Organization.OrgID)); orgID != "" {
			return orgID
		}
	}
	return strings.TrimSpace(fallback)
}

func printTable(r result) error {
	w := os.Stdout
	if _, err := fmt.Fprintln(w, "discovery-canary"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  duration        %s\n", r.Duration); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  target rps      %d\n", r.TargetRPS); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  total           %d\n", r.TotalRequests); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  success         %d\n", r.SuccessRequests); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  failure (5xx)   %d\n", r.FailureRequests-r.NetworkErrors); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  network errors  %d\n", r.NetworkErrors); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  authz checks    %d allowed=%d denied=%d errors=%d\n", r.AuthzChecks, r.AuthzAllowed, r.AuthzDenied, r.AuthzErrors); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  zero-downtime   %v\n", r.ZeroDowntime); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  latency p50/p95/p99/max  %dms / %dms / %dms / %dms\n",
		r.LatencyP50Millis, r.LatencyP95Millis, r.LatencyP99Millis, r.LatencyMaxMillis); err != nil {
		return err
	}
	if len(r.StatusCounts) > 0 {
		codes := make([]int, 0, len(r.StatusCounts))
		for c := range r.StatusCounts {
			codes = append(codes, c)
		}
		sort.Ints(codes)
		if _, err := fmt.Fprintf(w, "  status counts   "); err != nil {
			return err
		}
		for i, c := range codes {
			if i > 0 {
				if _, err := fmt.Fprint(w, " "); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprintf(w, "%d=%d", c, r.StatusCounts[c]); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
	}
	if r.FirstError != "" {
		if _, err := fmt.Fprintf(w, "  first error     %s\n", r.FirstError); err != nil {
			return err
		}
	}
	return nil
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
