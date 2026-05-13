package workload

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestResolvingDialContextRoutesToCatalogAddress(t *testing.T) {
	upstreamCalls := atomic.Int64{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalls.Add(1)
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	upstreamHost, upstreamPort := mustHostPort(t, upstream.URL)

	nomad := newFakeNomadFromMap(t, map[string][]nomadServiceEntry{
		"foo-internal-https": {{Address: upstreamHost, Port: upstreamPort}},
	})
	defer nomad.Close()

	resolver, err := NewResolver(nomad.URL, "")
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}

	dial := newResolvingDialContext(resolver, &net.Dialer{})
	tr := &http.Transport{DialContext: dial}
	client := &http.Client{Transport: tr}

	resp, err := client.Get("http://foo-internal-https/")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if upstreamCalls.Load() != 1 {
		t.Fatalf("upstream call count = %d", upstreamCalls.Load())
	}
}

func TestResolvingDialContextEvictsAndRetriesOnDialFailure(t *testing.T) {
	// Live upstream we expect the second dial to succeed against.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()
	liveHost, livePort := mustHostPort(t, upstream.URL)

	// Closed listener: produces a dial error.
	deadLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	deadAddr := deadLn.Addr().(*net.TCPAddr)
	_ = deadLn.Close()

	nomad := newFakeNomadFromMap(t, map[string][]nomadServiceEntry{
		"bar-internal-https": {
			{Address: deadAddr.IP.String(), Port: deadAddr.Port},
			{Address: liveHost, Port: livePort},
		},
	})
	defer nomad.Close()

	resolver, err := NewResolver(nomad.URL, "")
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}

	dial := newResolvingDialContext(resolver, &net.Dialer{})
	tr := &http.Transport{DialContext: dial}
	client := &http.Client{Transport: tr}

	// Try up to a small number of attempts; on dial failure we evict and the
	// next attempt will see a smaller pool. After at most two attempts both
	// callers succeed because Pick is random and only one endpoint is dead.
	var lastErr error
	for range 3 {
		resp, err := client.Get("http://bar-internal-https/")
		if err == nil {
			_ = resp.Body.Close()
			lastErr = nil
			break
		}
		lastErr = err
	}
	if lastErr != nil {
		t.Fatalf("never recovered: %v", lastErr)
	}
}

func TestResolvingDialContextFailsLoudWhenCatalogEmpty(t *testing.T) {
	nomad := newFakeNomadFromMap(t, map[string][]nomadServiceEntry{
		"baz-internal-https": {},
	})
	defer nomad.Close()
	resolver, err := NewResolver(nomad.URL, "")
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	dial := newResolvingDialContext(resolver, &net.Dialer{})
	tr := &http.Transport{DialContext: dial}
	client := &http.Client{Transport: tr}
	_, err = client.Get("http://baz-internal-https/")
	if err == nil {
		t.Fatal("expected error from empty catalog")
	}
	if !strings.Contains(err.Error(), "no healthy endpoints") {
		t.Fatalf("expected no-healthy-endpoints error, got %v", err)
	}
}

func TestResolvingDialContextPassesThroughForNonCatalogHosts(t *testing.T) {
	// Loop a real local server that is not registered in any fake catalog.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()
	resolver, err := NewResolver("http://127.0.0.1:1", "")
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	dial := newResolvingDialContext(resolver, &net.Dialer{})
	tr := &http.Transport{DialContext: dial}
	client := &http.Client{Transport: tr}
	resp, err := client.Get(upstream.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestResolvingDialerWithTLSUpstream(t *testing.T) {
	// Confirms the dialer composes correctly with TLS — the URL's host
	// (catalog name) is what's used for the SNI/handshake from net/http's
	// default TLS path; here we use InsecureSkipVerify because the upstream
	// is httptest's self-signed cert. SPIFFE handles peer auth in production.
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	defer upstream.Close()
	host, port := mustHostPort(t, upstream.URL)

	nomad := newFakeNomadFromMap(t, map[string][]nomadServiceEntry{
		"qux-internal-https": {{Address: host, Port: port}},
	})
	defer nomad.Close()
	resolver, err := NewResolver(nomad.URL, "")
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}

	tr := &http.Transport{
		DialContext:     newResolvingDialContext(resolver, &net.Dialer{}),
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // test only
	}
	client := &http.Client{Transport: tr}
	resp, err := client.Get("https://qux-internal-https/")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func mustHostPort(t *testing.T, urlStr string) (string, int) {
	t.Helper()
	u := strings.TrimPrefix(strings.TrimPrefix(urlStr, "http://"), "https://")
	host, portStr, err := net.SplitHostPort(u)
	if err != nil {
		t.Fatalf("split host/port from %s: %v", urlStr, err)
	}
	var port int
	if _, err := fmt.Sscanf(portStr, "%d", &port); err != nil {
		t.Fatalf("parse port %q: %v", portStr, err)
	}
	return host, port
}

func newFakeNomadFromMap(t *testing.T, services map[string][]nomadServiceEntry) *httptest.Server {
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
