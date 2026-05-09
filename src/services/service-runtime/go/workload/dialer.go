package workload

import (
	"context"
	"fmt"
	"net"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// newResolvingDialContext returns a DialContext that resolves catalog hostnames
// through the supplied Resolver and falls through to base.DialContext for any
// other host. A host is treated as a catalog name when it ends in
// InternalServiceCatalogSuffix.
//
// Connection pooling caveat: net/http pools by URL host, not dialed address.
// During an upstream rolling deploy the pool will eventually contain
// connections to allocs that have since stopped, and those will fail on next
// use; the transport will re-dial through this DialContext, mark the dead
// endpoint evicted, and pool a new connection to a healthy alloc. That brief
// window of connection churn is the whole zero-downtime story — there is no
// per-instance pooling to keep correct.
func newResolvingDialContext(resolver *Resolver, base *net.Dialer) func(context.Context, string, string) (net.Conn, error) {
	if resolver == nil {
		panic("workload: resolving dialer requires a non-nil resolver")
	}
	if base == nil {
		base = &net.Dialer{}
	}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return base.DialContext(ctx, network, addr)
		}
		if !strings.HasSuffix(host, InternalServiceCatalogSuffix) {
			return base.DialContext(ctx, network, addr)
		}
		ctx, span := tracer.Start(ctx, "service.discovery.dial")
		defer span.End()
		span.SetAttributes(
			attribute.String("service.name", host),
			attribute.String("network", network),
			attribute.String("requested.port", port),
		)
		endpoints, err := resolver.Resolve(ctx, host)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return nil, err
		}
		picked, err := Pick(endpoints)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return nil, fmt.Errorf("pick endpoint for %q: %w", host, err)
		}
		dialAddr := picked.hostPort()
		span.SetAttributes(attribute.String("selected.address", dialAddr))
		conn, err := base.DialContext(ctx, network, dialAddr)
		if err != nil {
			resolver.MarkUnhealthy(host, dialAddr)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return nil, fmt.Errorf("dial %s for service %q: %w", dialAddr, host, err)
		}
		return conn, nil
	}
}
