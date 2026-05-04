package secrets

import "context"

type openBaoAuditInfoContextKey struct{}

type OpenBaoAuditInfo struct {
	Mount        string
	RequestID    string
	AccessorHash string
	KeyVersion   uint64
}

func ContextWithOpenBaoAuditInfo(ctx context.Context) context.Context {
	if _, ok := OpenBaoAuditInfoFromContext(ctx); ok {
		return ctx
	}
	return context.WithValue(ctx, openBaoAuditInfoContextKey{}, &OpenBaoAuditInfo{})
}

func OpenBaoAuditInfoFromContext(ctx context.Context) (*OpenBaoAuditInfo, bool) {
	info, ok := ctx.Value(openBaoAuditInfoContextKey{}).(*OpenBaoAuditInfo)
	return info, ok && info != nil
}

func recordOpenBaoAuditInfo(ctx context.Context, mount, requestID, accessorHash string, keyVersion uint64) {
	info, ok := OpenBaoAuditInfoFromContext(ctx)
	if !ok {
		return
	}
	if mount != "" {
		info.Mount = mount
	}
	if requestID != "" {
		info.RequestID = requestID
	}
	if accessorHash != "" {
		info.AccessorHash = accessorHash
	}
	if keyVersion > 0 {
		info.KeyVersion = keyVersion
	}
}
