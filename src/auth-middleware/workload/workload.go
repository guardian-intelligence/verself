package workload

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/spiffetls/tlsconfig"
	"github.com/spiffe/go-spiffe/v2/svid/jwtsvid"
	"github.com/spiffe/go-spiffe/v2/svid/x509svid"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	EndpointSocketEnv = workloadapi.SocketEnv
	sourceInitTimeout = 5 * time.Second
)

var tracer = otel.Tracer("github.com/forge-metal/auth-middleware/workload")

type peerIDContextKey struct{}

// Source returns a Workload API X.509 source. The call blocks until SPIRE
// delivers the initial SVID and bundle, matching go-spiffe's source contract.
func Source(ctx context.Context, socket string) (*workloadapi.X509Source, error) {
	socket = strings.TrimSpace(socket)
	if socket == "" {
		socket = strings.TrimSpace(os.Getenv(EndpointSocketEnv))
	}
	ctx, span := tracer.Start(ctx, "auth.spiffe.source.init")
	defer span.End()
	span.SetAttributes(attribute.String("spiffe.endpoint_socket", socket))
	if socket == "" {
		err := fmt.Errorf("%s is required", EndpointSocketEnv)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	initCtx, cancel := context.WithTimeout(ctx, sourceInitTimeout)
	defer cancel()
	source, err := workloadapi.NewX509Source(initCtx, workloadapi.WithClientOptions(workloadapi.WithAddr(socket)))
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	if svid, err := source.GetX509SVID(); err == nil && svid != nil {
		span.SetAttributes(attribute.String("spiffe.id", svid.ID.String()))
	}
	return source, nil
}

// JWTSource returns a Workload API JWT source for repo-owned workload auth
// against relying parties such as OpenBao.
func JWTSource(ctx context.Context, socket string) (*workloadapi.JWTSource, error) {
	socket = strings.TrimSpace(socket)
	if socket == "" {
		socket = strings.TrimSpace(os.Getenv(EndpointSocketEnv))
	}
	ctx, span := tracer.Start(ctx, "auth.spiffe.jwt_source.init")
	defer span.End()
	span.SetAttributes(attribute.String("spiffe.endpoint_socket", socket))
	if socket == "" {
		err := fmt.Errorf("%s is required", EndpointSocketEnv)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	initCtx, cancel := context.WithTimeout(ctx, sourceInitTimeout)
	defer cancel()
	source, err := workloadapi.NewJWTSource(initCtx, workloadapi.WithClientOptions(workloadapi.WithAddr(socket)))
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	return source, nil
}

func FetchJWTSVID(ctx context.Context, source *workloadapi.JWTSource, audience string, subject spiffeid.ID) (string, time.Time, spiffeid.ID, error) {
	if source == nil {
		return "", time.Time{}, spiffeid.ID{}, errors.New("spiffe jwt source is required")
	}
	audience = strings.TrimSpace(audience)
	if audience == "" {
		return "", time.Time{}, spiffeid.ID{}, errors.New("jwt-svid audience is required")
	}
	ctx, span := tracer.Start(ctx, "auth.spiffe.jwt_svid.fetch")
	defer span.End()
	span.SetAttributes(attribute.String("jwt.audience", audience))
	params := jwtsvid.Params{Audience: audience}
	if !subject.IsZero() {
		params.Subject = subject
		span.SetAttributes(attribute.String("spiffe.subject", subject.String()))
	}
	svid, err := source.FetchJWTSVID(ctx, params)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", time.Time{}, spiffeid.ID{}, err
	}
	span.SetAttributes(
		attribute.String("spiffe.id", svid.ID.String()),
		attribute.Int64("jwt.expires_in_ms", time.Until(svid.Expiry).Milliseconds()),
	)
	return svid.Marshal(), svid.Expiry, svid.ID, nil
}

// PeerIDForSource derives the SPIFFE ID for a peer service inside the caller's
// trust domain. The trust domain is read from the caller's own SVID so no env
// var is required.
func PeerIDForSource(source *workloadapi.X509Source, service string) (spiffeid.ID, error) {
	td, err := currentTrustDomain(source)
	if err != nil {
		return spiffeid.ID{}, err
	}
	return peerIDForService(td, service)
}

// PeerIDsForSource resolves a list of peer services, de-duplicating on the
// way. Useful for constructing mTLS allowlists that must stay in sync across
// a server TLS config and a runtime authorization middleware.
func PeerIDsForSource(source *workloadapi.X509Source, services ...string) ([]spiffeid.ID, error) {
	if len(services) == 0 {
		return nil, errors.New("at least one peer service is required")
	}
	td, err := currentTrustDomain(source)
	if err != nil {
		return nil, err
	}
	ids := make([]spiffeid.ID, 0, len(services))
	seen := make(map[spiffeid.ID]struct{}, len(services))
	for _, service := range services {
		id, err := peerIDForService(td, service)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids, nil
}

// CurrentIDForService asserts that the caller's own SVID matches the expected
// service identity. Call once at startup to fail loud if a workload has been
// launched under the wrong SPIRE registration entry.
func CurrentIDForService(source *workloadapi.X509Source, service string) (spiffeid.ID, error) {
	id, err := currentID(source)
	if err != nil {
		return spiffeid.ID{}, err
	}
	expected, err := peerIDForService(id.TrustDomain(), service)
	if err != nil {
		return spiffeid.ID{}, err
	}
	if id != expected {
		return spiffeid.ID{}, fmt.Errorf("current spiffe id %s does not match expected service id %s", id, expected)
	}
	return id, nil
}

// MTLSClientForService returns an HTTP client that presents the caller's SVID
// and authorizes the peer by expected SPIFFE ID. Pass a nil base transport to
// start from http.DefaultTransport.
func MTLSClientForService(source *workloadapi.X509Source, service string, base http.RoundTripper) (*http.Client, error) {
	id, err := PeerIDForSource(source, service)
	if err != nil {
		return nil, err
	}
	if base == nil {
		base = http.DefaultTransport
	}
	transport, err := cloneTransport(base)
	if err != nil {
		return nil, err
	}
	transport.TLSClientConfig = tlsconfig.MTLSClientConfig(source, source, tlsconfig.AuthorizeID(id))
	return &http.Client{
		// otelhttp injects trace context on the repo-wide SPIFFE mTLS path so
		// downstream service spans and audit rows stay joinable in ClickHouse.
		Transport: otelhttp.NewTransport(&clientSpanTransport{
			next:             transport,
			expectedServerID: id.String(),
			source:           source,
		}),
		Timeout: 5 * time.Second,
	}, nil
}

// MTLSServerConfigForAny builds a server TLS config that authorizes any peer
// in the provided allowlist. Use together with ServerPeerAllowlistMiddleware so
// the TLS handshake allowlist and the per-request authorization stay aligned.
func MTLSServerConfigForAny(source *workloadapi.X509Source, expectedClientIDs ...spiffeid.ID) (*tls.Config, error) {
	if source == nil {
		return nil, errors.New("spiffe x509 source is required")
	}
	ids, err := nonZeroIDs(expectedClientIDs)
	if err != nil {
		return nil, err
	}
	return tlsconfig.MTLSServerConfig(source, source, tlsconfig.AuthorizeOneOf(ids...)), nil
}

// ServerPeerAllowlistMiddleware authorizes each request against the allowlist
// and stashes the peer identity on the request context. Returns an error at
// construction if the allowlist is empty or contains a zero ID, so a
// misconfigured server refuses to start rather than silently rejecting every
// request with 401.
func ServerPeerAllowlistMiddleware(expectedClientIDs []spiffeid.ID, next http.Handler) (http.Handler, error) {
	ids, err := nonZeroIDs(expectedClientIDs)
	if err != nil {
		return nil, err
	}
	expected := make(map[spiffeid.ID]struct{}, len(ids))
	expectedStrings := make([]string, 0, len(ids))
	for _, id := range ids {
		expected[id] = struct{}{}
		expectedStrings = append(expectedStrings, id.String())
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, span := tracer.Start(r.Context(), "auth.spiffe.mtls.server")
		defer span.End()
		span.SetAttributes(attribute.StringSlice("spiffe.expected_client_ids", expectedStrings))
		peerID, ok := PeerIDFromRequest(r)
		if !ok {
			err := errors.New("missing SPIFFE peer certificate")
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		span.SetAttributes(attribute.String("spiffe.peer_id", peerID.String()))
		if _, ok := expected[peerID]; !ok {
			err := fmt.Errorf("unexpected SPIFFE peer %s", peerID.String())
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		r = r.WithContext(ContextWithPeerID(ctx, peerID))
		next.ServeHTTP(w, r)
	}), nil
}

func PeerIDFromRequest(r *http.Request) (spiffeid.ID, bool) {
	if r == nil || r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return spiffeid.ID{}, false
	}
	id, err := x509svid.IDFromCert(r.TLS.PeerCertificates[0])
	if err != nil {
		return spiffeid.ID{}, false
	}
	return id, true
}

func ContextWithPeerID(ctx context.Context, id spiffeid.ID) context.Context {
	if id.IsZero() {
		return ctx
	}
	return context.WithValue(ctx, peerIDContextKey{}, id)
}

func PeerIDFromContext(ctx context.Context) (spiffeid.ID, bool) {
	id, ok := ctx.Value(peerIDContextKey{}).(spiffeid.ID)
	return id, ok && !id.IsZero()
}

func peerIDForService(td spiffeid.TrustDomain, service string) (spiffeid.ID, error) {
	if td.IsZero() {
		return spiffeid.ID{}, errors.New("spiffe trust domain is required")
	}
	service = strings.TrimSpace(service)
	if service == "" {
		return spiffeid.ID{}, errors.New("spiffe service name is required")
	}
	id, err := spiffeid.FromSegments(td, "svc", service)
	if err != nil {
		return spiffeid.ID{}, fmt.Errorf("derive spiffe id for service %q: %w", service, err)
	}
	return id, nil
}

func currentID(source *workloadapi.X509Source) (spiffeid.ID, error) {
	if source == nil {
		return spiffeid.ID{}, errors.New("spiffe x509 source is required")
	}
	svid, err := source.GetX509SVID()
	if err != nil {
		return spiffeid.ID{}, fmt.Errorf("load current x509-svid: %w", err)
	}
	if svid == nil || svid.ID.IsZero() {
		return spiffeid.ID{}, errors.New("current x509-svid is required")
	}
	return svid.ID, nil
}

func currentTrustDomain(source *workloadapi.X509Source) (spiffeid.TrustDomain, error) {
	id, err := currentID(source)
	if err != nil {
		return spiffeid.TrustDomain{}, err
	}
	return id.TrustDomain(), nil
}

type clientSpanTransport struct {
	next             http.RoundTripper
	expectedServerID string
	source           *workloadapi.X509Source
}

func (t *clientSpanTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx, span := tracer.Start(req.Context(), "auth.spiffe.mtls.client", trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()
	span.SetAttributes(attribute.String("spiffe.expected_server_id", t.expectedServerID))
	if t.source != nil {
		if svid, err := t.source.GetX509SVID(); err == nil && svid != nil {
			span.SetAttributes(attribute.String("spiffe.local_id", svid.ID.String()))
		}
	}
	resp, err := t.next.RoundTrip(req.WithContext(ctx))
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	span.SetAttributes(attribute.Int("http.response.status_code", resp.StatusCode))
	if resp.StatusCode >= 400 {
		span.SetStatus(codes.Error, http.StatusText(resp.StatusCode))
	}
	return resp, nil
}

func cloneTransport(base http.RoundTripper) (*http.Transport, error) {
	if base == nil {
		base = http.DefaultTransport
	}
	if transport, ok := base.(*http.Transport); ok {
		return transport.Clone(), nil
	}
	return nil, fmt.Errorf("spiffe mTLS requires *http.Transport, got %T", base)
}

func nonZeroIDs(ids []spiffeid.ID) ([]spiffeid.ID, error) {
	if len(ids) == 0 {
		return nil, errors.New("at least one expected spiffe id is required")
	}
	out := make([]spiffeid.ID, 0, len(ids))
	for _, id := range ids {
		if id.IsZero() {
			return nil, errors.New("expected spiffe id is required")
		}
		out = append(out, id)
	}
	return out, nil
}
