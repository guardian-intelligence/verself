package requestmeta

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const (
	HeaderClientIP       = "X-Verself-Client-IP"
	HeaderEdgePeerIP     = "X-Verself-Edge-Peer-IP"
	HeaderClientIPSource = "X-Verself-Client-IP-Source"
	HeaderClientCountry  = "X-Verself-Client-Country"
	HeaderClientRegion   = "X-Verself-Client-Region"
	HeaderClientCity     = "X-Verself-Client-City"

	SourceDirect     = "direct"
	SourceCloudflare = "cloudflare"
	SourceRemoteAddr = "remote_addr"
)

var bypassOnlyHeaders = []string{
	"CF-Connecting-IP",
	"CF-Connecting-IPv6",
	"CF-Pseudo-IPv4",
	"True-Client-IP",
	"CF-IPCountry",
	"CF-Region",
	"CF-City",
	"Forwarded",
	"X-Client-IP",
	"X-Cluster-Client-IP",
	"X-Original-Forwarded-For",
}

var legacyProxyHeaders = []string{
	"X-Forwarded-For",
	"X-Real-IP",
}

type contextKey struct{}

type Attribution struct {
	ClientIP        string
	EdgePeerIP      string
	ClientIPSource  string
	ClientCountry   string
	ClientRegion    string
	ClientCity      string
	Trusted         bool
	RejectedHeaders []string
	RemoteAddr      string
}

func FromHTTP(r *http.Request) Attribution {
	if r == nil {
		return Attribution{}
	}
	return FromValues(r.Header.Get, r.RemoteAddr)
}

func FromValues(header func(string) string, remoteAddr string) Attribution {
	if header == nil {
		header = func(string) string { return "" }
	}
	remoteIP := normalizeIP(remoteAddr)
	clientIP := normalizeIP(header(HeaderClientIP))
	edgePeerIP := normalizeIP(header(HeaderEdgePeerIP))
	source := strings.TrimSpace(header(HeaderClientIPSource))
	clientCountry := normalizeLabel(header(HeaderClientCountry), 2)
	clientRegion := normalizeLabel(header(HeaderClientRegion), 96)
	clientCity := normalizeLabel(header(HeaderClientCity), 96)
	rejected := rejectedHeaders(header, clientIP == "")

	if edgePeerIP == "" {
		edgePeerIP = remoteIP
	}
	if clientIP != "" {
		if source == "" {
			source = SourceDirect
		}
		return Attribution{
			ClientIP:        clientIP,
			EdgePeerIP:      edgePeerIP,
			ClientIPSource:  source,
			ClientCountry:   clientCountry,
			ClientRegion:    clientRegion,
			ClientCity:      clientCity,
			Trusted:         true,
			RejectedHeaders: rejected,
			RemoteAddr:      strings.TrimSpace(remoteAddr),
		}
	}
	if strings.TrimSpace(header(HeaderClientIP)) != "" {
		rejected = appendIfMissing(rejected, HeaderClientIP)
	}
	return Attribution{
		ClientIP:        remoteIP,
		EdgePeerIP:      edgePeerIP,
		ClientIPSource:  SourceRemoteAddr,
		ClientCountry:   "",
		ClientRegion:    "",
		ClientCity:      "",
		Trusted:         false,
		RejectedHeaders: rejected,
		RemoteAddr:      strings.TrimSpace(remoteAddr),
	}
}

func WithAttribution(ctx context.Context, attribution Attribution) context.Context {
	return context.WithValue(ctx, contextKey{}, attribution)
}

func FromContext(ctx context.Context) (Attribution, bool) {
	if ctx == nil {
		return Attribution{}, false
	}
	attribution, ok := ctx.Value(contextKey{}).(Attribution)
	return attribution, ok
}

func AnnotateSpan(ctx context.Context, attribution Attribution) {
	span := trace.SpanFromContext(ctx)
	if !span.SpanContext().IsValid() {
		return
	}
	span.SetAttributes(attribution.SpanAttributes()...)
}

func (a Attribution) SpanAttributes() []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String("verself.request.client_ip", a.ClientIP),
		attribute.String("verself.request.edge_peer_ip", a.EdgePeerIP),
		attribute.String("verself.request.client_ip_source", a.ClientIPSource),
		attribute.Bool("verself.request.client_ip_trusted", a.Trusted),
	}
	if a.ClientCountry != "" {
		attrs = append(attrs, attribute.String("verself.request.client_country", a.ClientCountry))
	}
	if a.ClientRegion != "" {
		attrs = append(attrs, attribute.String("verself.request.client_region", a.ClientRegion))
	}
	if a.ClientCity != "" {
		attrs = append(attrs, attribute.String("verself.request.client_city", a.ClientCity))
	}
	if len(a.RejectedHeaders) > 0 {
		attrs = append(attrs, attribute.StringSlice("verself.request.rejected_ip_headers", a.RejectedHeaders))
	}
	return attrs
}

func rejectedHeaders(header func(string) string, includeLegacy bool) []string {
	names := make([]string, 0, len(bypassOnlyHeaders)+len(legacyProxyHeaders))
	for _, name := range bypassOnlyHeaders {
		if strings.TrimSpace(header(name)) != "" {
			names = append(names, name)
		}
	}
	if includeLegacy {
		for _, name := range legacyProxyHeaders {
			if strings.TrimSpace(header(name)) != "" {
				names = append(names, name)
			}
		}
	}
	return names
}

func appendIfMissing(values []string, candidate string) []string {
	for _, value := range values {
		if value == candidate {
			return values
		}
	}
	return append(values, candidate)
}

func normalizeIP(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "-" {
		return ""
	}
	if host, _, err := net.SplitHostPort(raw); err == nil {
		raw = host
	}
	raw = strings.Trim(raw, "[]")
	addr, err := netip.ParseAddr(raw)
	if err != nil {
		return ""
	}
	addr = addr.Unmap().WithZone("")
	return addr.String()
}

func normalizeLabel(raw string, maxLength int) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || maxLength <= 0 {
		return ""
	}
	var builder strings.Builder
	builder.Grow(min(len(raw), maxLength))
	for _, r := range raw {
		if r < 0x20 || r == 0x7f {
			continue
		}
		builder.WriteRune(r)
		if builder.Len() >= maxLength {
			break
		}
	}
	return strings.TrimSpace(builder.String())
}
