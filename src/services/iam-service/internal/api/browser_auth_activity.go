package api

import (
	"context"
	"net/http"

	governanceinternalclient "github.com/verself/governance-service/internalclient"
)

func browserAuthExternalRoute(r *http.Request) string {
	if r == nil || r.URL == nil {
		return "/api/v1/auth"
	}
	return "/api/v1/auth" + r.URL.Path
}

func sendBrowserAuthActivity(ctx context.Context, user browserUser, accountHandle, operation, eventCode, action, permission, method, route string, status uint16) {
	traceID, spanID := traceIDsFromContext(ctx)
	info := browserRequestInfoFromContext(ctx)
	orgID := firstNonEmpty(stringValue(user.SelectedOrgID), stringValue(user.HomeOrgID), stringValue(user.OrgID))
	sendGovernanceAPIActivity(ctx, governanceAPIActivity{
		OrgID:                 orgID,
		APIService:            "iam-service",
		APIOperation:          operation,
		APIEventCode:          eventCode,
		APIAction:             action,
		ActorType:             "user",
		ActorUID:              user.Sub,
		ActorName:             firstNonEmpty(stringValue(user.Name), stringValue(user.PreferredUsername), user.Sub),
		ActorEmail:            stringValue(user.Email),
		Permission:            permission,
		ResourceType:          "browser_account",
		ResourceUID:           accountHandle,
		ResourceName:          accountHandle,
		HTTPMethod:            method,
		HTTPRoute:             route,
		HTTPUserAgent:         info.UserAgent,
		HTTPClientIP:          info.Attribution.ClientIP,
		HTTPStatus:            status,
		AuthorizationDecision: governanceinternalclient.AuthorizationDecisionAllowed,
		Status:                governanceinternalclient.APIActivityStatusSuccess,
		TraceUID:              traceID,
		SpanUID:               spanID,
		Unmapped: compactAPIActivityUnmapped(map[string]any{
			"verself.request.client_ip_source":  info.Attribution.ClientIPSource,
			"verself.request.client_ip_trusted": info.Attribution.Trusted,
			"verself.request.client_country":    info.Attribution.ClientCountry,
			"verself.request.client_region":     info.Attribution.ClientRegion,
			"verself.request.client_city":       info.Attribution.ClientCity,
			"verself.auth.device_label":         parseBrowserDevice(info.UserAgent).Label,
		}),
	})
}
