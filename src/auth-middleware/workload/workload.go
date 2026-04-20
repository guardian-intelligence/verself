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
	"github.com/spiffe/go-spiffe/v2/svid/x509svid"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
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

func ParseID(raw string) (spiffeid.ID, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return spiffeid.ID{}, errors.New("spiffe id is required")
	}
	id, err := spiffeid.FromString(raw)
	if err != nil {
		return spiffeid.ID{}, fmt.Errorf("parse spiffe id %q: %w", raw, err)
	}
	return id, nil
}

func MTLSClient(source *workloadapi.X509Source, expectedServerID spiffeid.ID, base http.RoundTripper) (*http.Client, error) {
	if source == nil {
		return nil, errors.New("spiffe x509 source is required")
	}
	if expectedServerID.IsZero() {
		return nil, errors.New("expected server spiffe id is required")
	}
	if base == nil {
		base = http.DefaultTransport
	}
	transport, err := cloneTransport(base)
	if err != nil {
		return nil, err
	}
	transport.TLSClientConfig = tlsconfig.MTLSClientConfig(source, source, tlsconfig.AuthorizeID(expectedServerID))
	return &http.Client{
		Transport: &clientSpanTransport{
			next:             transport,
			expectedServerID: expectedServerID.String(),
			source:           source,
		},
		Timeout: 3 * time.Second,
	}, nil
}

func MTLSServerConfig(source *workloadapi.X509Source, expectedClientID spiffeid.ID) (*tls.Config, error) {
	if source == nil {
		return nil, errors.New("spiffe x509 source is required")
	}
	if expectedClientID.IsZero() {
		return nil, errors.New("expected client spiffe id is required")
	}
	return tlsconfig.MTLSServerConfig(source, source, tlsconfig.AuthorizeID(expectedClientID)), nil
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

func ServerPeerMiddleware(expectedClientID spiffeid.ID, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, span := tracer.Start(r.Context(), "auth.spiffe.mtls.server")
		defer span.End()
		span.SetAttributes(attribute.String("spiffe.expected_client_id", expectedClientID.String()))
		peerID, ok := PeerIDFromRequest(r)
		if !ok {
			err := errors.New("missing SPIFFE peer certificate")
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		span.SetAttributes(attribute.String("spiffe.peer_id", peerID.String()))
		if peerID != expectedClientID {
			err := fmt.Errorf("unexpected SPIFFE peer %s", peerID.String())
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		r = r.WithContext(ContextWithPeerID(ctx, peerID))
		next.ServeHTTP(w, r)
	})
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
