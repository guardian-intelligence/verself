package api

import "context"

type recordingProviderSessionRevoker struct {
	sessionIDs []string
	err        error
}

func (r *recordingProviderSessionRevoker) DeleteSession(_ context.Context, sessionID string) error {
	r.sessionIDs = append(r.sessionIDs, sessionID)
	return r.err
}
