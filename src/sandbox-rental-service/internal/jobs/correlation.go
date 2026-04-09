package jobs

import "context"

type correlationIDKey struct{}

// WithCorrelationID threads the browser-scoped request correlation key through
// submission so the service can persist the pre-execution join handle.
func WithCorrelationID(ctx context.Context, correlationID string) context.Context {
	if correlationID == "" {
		return ctx
	}
	return context.WithValue(ctx, correlationIDKey{}, correlationID)
}

func CorrelationIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	correlationID, _ := ctx.Value(correlationIDKey{}).(string)
	return correlationID
}
