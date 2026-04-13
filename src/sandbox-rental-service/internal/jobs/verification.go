package jobs

import "context"

type verificationRunIDKey struct{}

// WithVerificationRunID threads a caller-supplied evidence key through submit
// and River worker contexts so ClickHouse rows can be joined to live proofs.
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
