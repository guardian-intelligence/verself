package workload

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestResolverHappyPath(t *testing.T) {
	srv := newFakeNomad(t, map[string][]nomadServiceEntry{
		"iam-service-internal-https": {
			{Address: "127.0.0.1", Port: 4441},
			{Address: "127.0.0.1", Port: 4442},
		},
	})
	defer srv.Close()

	r, err := NewResolver(srv.URL, "")
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	endpoints, err := r.Resolve(context.Background(), "iam-service-internal-https")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(endpoints) != 2 {
		t.Fatalf("got %d endpoints, want 2", len(endpoints))
	}
}

func TestResolverEmptyCatalogIsError(t *testing.T) {
	srv := newFakeNomad(t, map[string][]nomadServiceEntry{
		"iam-service-internal-https": {},
	})
	defer srv.Close()

	r, err := NewResolver(srv.URL, "")
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	if _, err := r.Resolve(context.Background(), "iam-service-internal-https"); err == nil {
		t.Fatal("expected error on empty catalog, got nil")
	}
}

func TestResolverCacheHonoursTTL(t *testing.T) {
	calls := atomic.Int64{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode([]nomadServiceEntry{{Address: "127.0.0.1", Port: 4441}})
	}))
	defer srv.Close()

	now := time.Unix(1000, 0)
	clock := &mockClock{now: now}
	r, err := NewResolver(srv.URL, "", WithResolverClock(clock.Now))
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}

	if _, err := r.Resolve(context.Background(), "x"); err != nil {
		t.Fatalf("first resolve: %v", err)
	}
	if _, err := r.Resolve(context.Background(), "x"); err != nil {
		t.Fatalf("second resolve: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("expected 1 fetch within TTL, got %d", calls.Load())
	}
	clock.now = now.Add(2 * time.Second)
	if _, err := r.Resolve(context.Background(), "x"); err != nil {
		t.Fatalf("post-ttl resolve: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected 2 fetches after TTL, got %d", calls.Load())
	}
}

func TestResolverEvictionFiltersAndExpires(t *testing.T) {
	srv := newFakeNomad(t, map[string][]nomadServiceEntry{
		"x": {
			{Address: "10.0.0.1", Port: 1000},
			{Address: "10.0.0.2", Port: 2000},
		},
	})
	defer srv.Close()

	now := time.Unix(1000, 0)
	clock := &mockClock{now: now}
	r, err := NewResolver(srv.URL, "", WithResolverClock(clock.Now))
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}

	if _, err := r.Resolve(context.Background(), "x"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	r.MarkUnhealthy("x", "10.0.0.1:1000")
	endpoints, err := r.Resolve(context.Background(), "x")
	if err != nil {
		t.Fatalf("post-evict: %v", err)
	}
	if len(endpoints) != 1 || endpoints[0].Address != "10.0.0.2" {
		t.Fatalf("eviction did not filter, got %+v", endpoints)
	}

	// Past the eviction window: the previously evicted endpoint comes back.
	// Move clock past TTL so we re-fetch and re-evaluate the eviction window.
	clock.now = now.Add(10 * time.Second)
	endpoints, err = r.Resolve(context.Background(), "x")
	if err != nil {
		t.Fatalf("post-window: %v", err)
	}
	if len(endpoints) != 2 {
		t.Fatalf("expected eviction to expire, got %+v", endpoints)
	}
}

func TestResolverAllEvictedFailsLoud(t *testing.T) {
	srv := newFakeNomad(t, map[string][]nomadServiceEntry{
		"x": {{Address: "10.0.0.1", Port: 1000}},
	})
	defer srv.Close()

	r, err := NewResolver(srv.URL, "")
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	if _, err := r.Resolve(context.Background(), "x"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	r.MarkUnhealthy("x", "10.0.0.1:1000")
	_, err = r.Resolve(context.Background(), "x")
	if err == nil {
		t.Fatal("expected error when all endpoints evicted")
	}
	if !strings.Contains(err.Error(), "no healthy endpoints") {
		t.Fatalf("expected no-healthy-endpoints error, got %v", err)
	}
}

func TestResolverServesStaleOnFetchFailure(t *testing.T) {
	calls := atomic.Int64{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			_ = json.NewEncoder(w).Encode([]nomadServiceEntry{{Address: "10.0.0.1", Port: 1000}})
			return
		}
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	now := time.Unix(1000, 0)
	clock := &mockClock{now: now}
	r, err := NewResolver(srv.URL, "", WithResolverClock(clock.Now))
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	if _, err := r.Resolve(context.Background(), "x"); err != nil {
		t.Fatalf("seed fetch: %v", err)
	}
	clock.now = now.Add(5 * time.Second)
	endpoints, err := r.Resolve(context.Background(), "x")
	if err != nil {
		t.Fatalf("expected stale fallback to succeed, got %v", err)
	}
	if len(endpoints) != 1 || endpoints[0].Address != "10.0.0.1" {
		t.Fatalf("stale fallback returned %+v", endpoints)
	}
}

func TestResolverNomadErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	r, err := NewResolver(srv.URL, "")
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	_, err = r.Resolve(context.Background(), "x")
	if err == nil {
		t.Fatal("expected error when nomad returns 500")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("expected status code in error, got %v", err)
	}
}

func TestResolverTokenHeader(t *testing.T) {
	gotToken := atomic.Value{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken.Store(r.Header.Get("X-Nomad-Token"))
		_ = json.NewEncoder(w).Encode([]nomadServiceEntry{{Address: "127.0.0.1", Port: 4444}})
	}))
	defer srv.Close()
	r, err := NewResolver(srv.URL, "secret-token")
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	if _, err := r.Resolve(context.Background(), "x"); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got := gotToken.Load(); got != "secret-token" {
		t.Fatalf("token header not propagated, got %v", got)
	}
}

func TestPickIsRandomOverMultiple(t *testing.T) {
	endpoints := []Endpoint{
		{Address: "10.0.0.1", Port: 1},
		{Address: "10.0.0.2", Port: 2},
		{Address: "10.0.0.3", Port: 3},
	}
	seen := map[string]int{}
	for range 200 {
		picked, err := Pick(endpoints)
		if err != nil {
			t.Fatalf("Pick: %v", err)
		}
		seen[picked.Address]++
	}
	if len(seen) != 3 {
		t.Fatalf("expected all three endpoints to be picked at least once, got %v", seen)
	}
}

func TestPickEmptyIsError(t *testing.T) {
	if _, err := Pick(nil); err == nil {
		t.Fatal("expected error on empty slice")
	}
}

func TestNewResolverRejectsBlankURL(t *testing.T) {
	if _, err := NewResolver("", ""); err == nil {
		t.Fatal("expected error on empty base URL")
	}
}

func TestInternalURLAndCatalogConvention(t *testing.T) {
	if got, want := CatalogNameForService(ServiceIAM), "iam-service-internal-https"; got != want {
		t.Fatalf("catalog name = %q, want %q", got, want)
	}
	if got, want := InternalURL(ServiceIAM), "https://iam-service-internal-https"; got != want {
		t.Fatalf("internal url = %q, want %q", got, want)
	}
}

type mockClock struct {
	now time.Time
}

func (c *mockClock) Now() time.Time { return c.now }

func newFakeNomad(t *testing.T, services map[string][]nomadServiceEntry) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/v1/service/") {
			http.NotFound(w, r)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/v1/service/")
		entries, ok := services[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(entries); err != nil {
			t.Errorf("encode: %v", err)
		}
	}))
}

func TestResolverFetchTimeoutPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()
	r, err := NewResolver(srv.URL, "")
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = r.Resolve(ctx, "x")
	if err == nil {
		t.Fatal("expected error on timeout")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "deadline") && !strings.Contains(err.Error(), "Client.Timeout") {
		t.Fatalf("expected deadline error, got %v", err)
	}
}
