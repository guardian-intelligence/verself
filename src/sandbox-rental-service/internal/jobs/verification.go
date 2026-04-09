package jobs

import "context"

type verificationRunIDKey struct{}

// WithVerificationRunID threads a caller-supplied correlation key through the
// service without making it part of the durable execution schema.
func WithVerificationRunID(ctx context.Context, runID string) context.Context {
	if runID == "" {
		return ctx
	}
	return context.WithValue(ctx, verificationRunIDKey{}, runID)
}

func VerificationRunIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	runID, _ := ctx.Value(verificationRunIDKey{}).(string)
	return runID
}
