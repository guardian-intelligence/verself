package workload

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// InternalServiceCatalogSuffix is the convention every internal mTLS endpoint
// follows when registering with Nomad's native service catalog. The catalog
// name for a service is its SPIFFE short name plus this suffix.
const InternalServiceCatalogSuffix = "-internal-https"

// Tunables for the runtime catalog resolver. Cache TTL is short so a deploy's
// catalog change propagates inside the same second a caller picks up its next
// request; the eviction window keeps the resolver from re-selecting a known-
// failed endpoint while Nomad is still updating.
const (
	resolverCacheTTL          = 1500 * time.Millisecond
	resolverEvictionWindow    = 5 * time.Second
	resolverHTTPTimeout       = 250 * time.Millisecond
	defaultNomadAgentHTTPAddr = "http://127.0.0.1:4646"
)

// Endpoint is one healthy registration of a Nomad service.
type Endpoint struct {
	Address string
	Port    int
	Tags    []string
}

func (e Endpoint) hostPort() string {
	return net.JoinHostPort(e.Address, strconv.Itoa(e.Port))
}

// Resolver looks up healthy registrations for a Nomad service via the local
// Nomad HTTP agent. It is safe for concurrent use.
type Resolver struct {
	baseURL    string
	token      string
	httpClient *http.Client
	now        func() time.Time

	mu       sync.Mutex
	cache    map[string]cachedSet
	evicted  map[string]map[string]time.Time
}

type cachedSet struct {
	endpoints []Endpoint
	fetchedAt time.Time
}

// ResolverOption customizes a Resolver. Options exist primarily so tests can
// substitute the HTTP transport and clock without exporting fields.
type ResolverOption func(*Resolver)

// WithResolverHTTPClient overrides the resolver's HTTP client. Used in tests
// to point at an httptest server.
func WithResolverHTTPClient(client *http.Client) ResolverOption {
	return func(r *Resolver) {
		if client != nil {
			r.httpClient = client
		}
	}
}

// WithResolverClock overrides the clock used for cache and eviction freshness.
func WithResolverClock(now func() time.Time) ResolverOption {
	return func(r *Resolver) {
		if now != nil {
			r.now = now
		}
	}
}

// NewResolver constructs a Resolver targeting the given Nomad HTTP base URL
// (e.g. http://127.0.0.1:4646 or http+unix:///run/nomad/nomad.sock). Pass an
// empty token when ACLs are not enabled or the agent is reached over an
// authenticated unix socket.
func NewResolver(baseURL, token string, opts ...ResolverOption) (*Resolver, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return nil, errors.New("nomad base url is required")
	}
	r := &Resolver{
		baseURL:    strings.TrimRight(baseURL, "/"),
		token:      strings.TrimSpace(token),
		httpClient: &http.Client{Timeout: resolverHTTPTimeout},
		now:        time.Now,
		cache:      make(map[string]cachedSet),
		evicted:    make(map[string]map[string]time.Time),
	}
	for _, opt := range opts {
		opt(r)
	}
	return r, nil
}

// NewResolverFromEnv builds a resolver from NOMAD_ADDR and NOMAD_TOKEN, falling
// back to the local agent at 127.0.0.1:4646 when NOMAD_ADDR is unset. Nomad
// injects both env vars into allocations under workload identity.
func NewResolverFromEnv(opts ...ResolverOption) (*Resolver, error) {
	addr := strings.TrimSpace(os.Getenv("NOMAD_ADDR"))
	if addr == "" {
		addr = defaultNomadAgentHTTPAddr
	}
	return NewResolver(addr, os.Getenv("NOMAD_TOKEN"), opts...)
}

var (
	defaultResolverOnce sync.Once
	defaultResolverInst *Resolver
	defaultResolverErr  error
)

// DefaultResolver returns the process-global resolver. It is initialized once
// from NOMAD_ADDR/NOMAD_TOKEN; a misconfigured environment fails every caller
// loud rather than producing silently broken clients.
func DefaultResolver() (*Resolver, error) {
	defaultResolverOnce.Do(func() {
		defaultResolverInst, defaultResolverErr = NewResolverFromEnv()
	})
	return defaultResolverInst, defaultResolverErr
}

// SetDefaultResolver injects a resolver as the process-global. Tests use this;
// production code should rely on the env-driven default.
func SetDefaultResolver(r *Resolver) {
	defaultResolverOnce.Do(func() {})
	defaultResolverInst = r
	defaultResolverErr = nil
}

type nomadServiceEntry struct {
	Address string   `json:"Address"`
	Port    int      `json:"Port"`
	Tags    []string `json:"Tags"`
}

// Resolve returns the cached or freshly-fetched healthy endpoint set for the
// catalog service name. An empty set is returned as a structured error so
// callers fail loud rather than dial a stale address.
func (r *Resolver) Resolve(ctx context.Context, catalogName string) ([]Endpoint, error) {
	if r == nil {
		return nil, errors.New("nomad resolver is nil")
	}
	catalogName = strings.TrimSpace(catalogName)
	if catalogName == "" {
		return nil, errors.New("nomad catalog name is required")
	}
	ctx, span := tracer.Start(ctx, "service.discovery.resolve")
	defer span.End()
	span.SetAttributes(attribute.String("service.name", catalogName))

	now := r.now()
	if endpoints, ok := r.takeCached(catalogName, now); ok {
		filtered := r.filterEvicted(catalogName, endpoints, now)
		span.SetAttributes(
			attribute.Bool("cache.hit", true),
			attribute.Int("candidates.total", len(endpoints)),
			attribute.Int("candidates.healthy", len(filtered)),
		)
		if len(filtered) == 0 {
			err := fmt.Errorf("nomad service %q has no healthy endpoints (all evicted)", catalogName)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return nil, err
		}
		return filtered, nil
	}

	endpoints, err := r.fetch(ctx, catalogName)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	r.storeCached(catalogName, endpoints, now)
	filtered := r.filterEvicted(catalogName, endpoints, now)
	span.SetAttributes(
		attribute.Bool("cache.hit", false),
		attribute.Int("candidates.total", len(endpoints)),
		attribute.Int("candidates.healthy", len(filtered)),
	)
	if len(filtered) == 0 {
		err := fmt.Errorf("nomad service %q has no healthy endpoints", catalogName)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	return filtered, nil
}

// MarkUnhealthy evicts a host:port from the resolver's selectable set for the
// eviction window. The next Resolve call will skip it; a subsequent fresh
// fetch from Nomad past the window will re-include it if still registered.
func (r *Resolver) MarkUnhealthy(catalogName, hostPort string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.evicted[catalogName] == nil {
		r.evicted[catalogName] = make(map[string]time.Time)
	}
	r.evicted[catalogName][hostPort] = r.now().Add(resolverEvictionWindow)
}

// Pick chooses one healthy endpoint at random. Random beats round-robin here:
// no shared counter, no thundering-herd alignment when many callers share the
// same resolver.
func Pick(endpoints []Endpoint) (Endpoint, error) {
	if len(endpoints) == 0 {
		return Endpoint{}, errors.New("no endpoints")
	}
	if len(endpoints) == 1 {
		return endpoints[0], nil
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(endpoints))))
	if err != nil {
		return Endpoint{}, err
	}
	return endpoints[n.Int64()], nil
}

func (r *Resolver) takeCached(catalogName string, now time.Time) ([]Endpoint, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.cache[catalogName]
	if !ok {
		return nil, false
	}
	if now.Sub(entry.fetchedAt) > resolverCacheTTL {
		return nil, false
	}
	clone := make([]Endpoint, len(entry.endpoints))
	copy(clone, entry.endpoints)
	return clone, true
}

func (r *Resolver) storeCached(catalogName string, endpoints []Endpoint, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	clone := make([]Endpoint, len(endpoints))
	copy(clone, endpoints)
	r.cache[catalogName] = cachedSet{endpoints: clone, fetchedAt: now}
}

func (r *Resolver) filterEvicted(catalogName string, endpoints []Endpoint, now time.Time) []Endpoint {
	r.mu.Lock()
	defer r.mu.Unlock()
	evictions := r.evicted[catalogName]
	if len(evictions) == 0 {
		return endpoints
	}
	for hp, until := range evictions {
		if now.After(until) {
			delete(evictions, hp)
		}
	}
	if len(evictions) == 0 {
		return endpoints
	}
	out := make([]Endpoint, 0, len(endpoints))
	for _, e := range endpoints {
		if _, blocked := evictions[e.hostPort()]; blocked {
			continue
		}
		out = append(out, e)
	}
	return out
}

func (r *Resolver) fetch(ctx context.Context, catalogName string) ([]Endpoint, error) {
	endpoint := r.baseURL + "/v1/service/" + url.PathEscape(catalogName)
	fetchCtx, cancel := context.WithTimeout(ctx, resolverHTTPTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build nomad services request: %w", err)
	}
	if r.token != "" {
		req.Header.Set("X-Nomad-Token", r.token)
	}
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("query nomad services for %q: %w", catalogName, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("nomad services for %q returned %d", catalogName, resp.StatusCode)
	}
	var entries []nomadServiceEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("decode nomad services response: %w", err)
	}
	out := make([]Endpoint, 0, len(entries))
	for _, e := range entries {
		if e.Address == "" || e.Port == 0 {
			continue
		}
		out = append(out, Endpoint{Address: e.Address, Port: e.Port, Tags: e.Tags})
	}
	return out, nil
}

// CatalogNameForService returns the Nomad service catalog name a given SPIFFE
// service short name registers under. Every internal mTLS endpoint follows the
// "<spiffe-name>-internal-https" convention.
func CatalogNameForService(service string) string {
	return service + InternalServiceCatalogSuffix
}

// InternalURL returns the synthetic base URL a caller passes to a generated
// HTTP client for an internal mTLS peer. The host is the catalog name; the
// resolving dialer in MTLSClientForService intercepts on that host and dials
// whichever healthy alloc Nomad currently advertises. SPIFFE authorizes the
// peer at the TLS layer, so the host header is cosmetic.
func InternalURL(service string) string {
	return "https://" + CatalogNameForService(service)
}
