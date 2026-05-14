package api

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/spiffe/go-spiffe/v2/workloadapi"
	governanceinternalclient "github.com/verself/governance-service/internalclient"
	workloadauth "github.com/verself/service-runtime/workload"
)

type auditSinkConfig struct {
	Client *governanceinternalclient.Client
}

var configuredAuditSink atomic.Pointer[auditSinkConfig]

func ConfigureAuditSink(url string, source *workloadapi.X509Source) {
	url = strings.TrimSpace(url)
	if url == "" || source == nil {
		return
	}
	httpClient, err := workloadauth.MTLSClientForService(source, workloadauth.ServiceGovernance, nil)
	if err != nil {
		slog.Default().Error("sandbox governance audit mtls client init failed", "error", err)
		return
	}
	sinkClient, err := governanceinternalclient.NewClient(url, governanceinternalclient.WithHTTPClient(httpClient))
	if err != nil {
		slog.Default().Error("sandbox governance audit client init failed", "error", err)
		return
	}
	configuredAuditSink.Store(&auditSinkConfig{
		Client: sinkClient,
	})
}

type governanceAPIActivity struct {
	OrgID                 string
	APIService            string
	APIOperation          string
	APIEventCode          string
	APIAction             string
	ActorType             string
	ActorUID              string
	CredentialUID         string
	Permission            string
	ResourceType          string
	ResourceUID           string
	HTTPMethod            string
	HTTPRoute             string
	HTTPStatus            uint16
	AuthorizationDecision governanceinternalclient.AuthorizationDecision
	Status                governanceinternalclient.APIActivityStatus
	StatusCode            string
	StatusDetail          string
	TraceUID              string
	Unmapped              map[string]any
}

func sendGovernanceAPIActivity(ctx context.Context, record governanceAPIActivity) {
	sink := configuredAuditSink.Load()
	if sink == nil || record.OrgID == "" {
		return
	}
	reqCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	resp, err := sink.Client.AppendAPIActivity(reqCtx, governanceinternalclient.AppendAPIActivityRequest{Body: governanceAPIActivityToContract(record)})
	if err != nil {
		slog.Default().ErrorContext(ctx, "sandbox governance audit send failed", "error", err)
		return
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slog.Default().ErrorContext(ctx, "sandbox governance audit rejected", "status", resp.StatusCode)
	}
}

func governanceAPIActivityToContract(record governanceAPIActivity) governanceinternalclient.APIActivityRecord {
	method := firstNonEmpty(record.HTTPMethod, "POST")
	route := firstNonEmpty(record.HTTPRoute, "/internal/"+record.APIOperation)
	httpStatus := record.HTTPStatus
	if httpStatus == 0 {
		httpStatus = 500
		if record.Status == governanceinternalclient.APIActivityStatusSuccess {
			httpStatus = 200
		}
	}
	return governanceinternalclient.APIActivityRecord{
		OrgID:         record.OrgID,
		APIService:    record.APIService,
		APIOperation:  record.APIOperation,
		APIEventCode:  record.APIEventCode,
		APIAction:     record.APIAction,
		ActorType:     record.ActorType,
		ActorUID:      record.ActorUID,
		CredentialUID: optionalStringTyped[governanceinternalclient.CredentialUID](record.CredentialUID),
		Permission:    record.Permission,
		Resources: governanceinternalclient.APIActivityResources{{
			Type: record.ResourceType,
			UID:  optionalStringTyped[governanceinternalclient.ResourceUID](record.ResourceUID),
		}},
		HTTPRequest:           governanceinternalclient.APIActivityHTTPRequest{Method: method, Route: route},
		HTTPResponse:          governanceinternalclient.APIActivityHTTPResponse{Code: httpStatus},
		AuthorizationDecision: record.AuthorizationDecision,
		Status:                record.Status,
		StatusCode:            governanceinternalclient.ProblemStatusCode(firstNonEmpty(record.StatusCode, strconv.Itoa(int(httpStatus)))),
		StatusDetail:          optionalString(record.StatusDetail),
		TraceUID:              optionalStringTyped[governanceinternalclient.TraceID](record.TraceUID),
		Unmapped:              optionalMap(record.Unmapped),
	}
}

func optionalString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func optionalStringTyped[T ~string](value string) *T {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	typed := T(value)
	return &typed
}

func optionalMap(value map[string]any) *map[string]any {
	if len(value) == 0 {
		return nil
	}
	return &value
}
