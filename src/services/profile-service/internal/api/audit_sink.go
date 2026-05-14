package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
		slog.Default().Error("profile governance audit mtls client init failed", "error", err)
		return
	}
	client, err := governanceinternalclient.NewClient(url, governanceinternalclient.WithHTTPClient(httpClient))
	if err != nil {
		slog.Default().Error("profile governance audit client init failed", "error", err)
		return
	}
	configuredAuditSink.Store(&auditSinkConfig{Client: client})
}

type governanceAPIActivity struct {
	OrgID                 string
	APIService            string
	APIOperation          string
	APIEventCode          string
	APIAction             string
	ActorType             string
	ActorUID              string
	Permission            string
	ResourceType          string
	ResourceUID           string
	HTTPStatus            uint16
	AuthorizationDecision governanceinternalclient.AuthorizationDecision
	Status                governanceinternalclient.APIActivityStatus
	StatusCode            string
	StatusDetail          string
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
		slog.Default().ErrorContext(ctx, "profile governance audit send failed", "error", err)
		return
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slog.Default().ErrorContext(ctx, "profile governance audit rejected", "status", resp.StatusCode)
	}
}

func governanceAPIActivityToContract(record governanceAPIActivity) governanceinternalclient.APIActivityRecord {
	httpStatus := record.HTTPStatus
	if httpStatus == 0 {
		httpStatus = 500
		if record.Status == governanceinternalclient.APIActivityStatusSuccess {
			httpStatus = 200
		}
	}
	return governanceinternalclient.APIActivityRecord{
		OrgID:        record.OrgID,
		APIService:   record.APIService,
		APIOperation: record.APIOperation,
		APIEventCode: record.APIEventCode,
		APIAction:    record.APIAction,
		ActorType:    record.ActorType,
		ActorUID:     record.ActorUID,
		Permission:   record.Permission,
		Resources: governanceinternalclient.APIActivityResources{{
			Type: record.ResourceType,
			UID:  optionalStringTyped[governanceinternalclient.ResourceUID](record.ResourceUID),
		}},
		HTTPRequest:           governanceinternalclient.APIActivityHTTPRequest{Method: "POST", Route: "/internal/" + record.APIOperation},
		HTTPResponse:          governanceinternalclient.APIActivityHTTPResponse{Code: httpStatus},
		AuthorizationDecision: record.AuthorizationDecision,
		Status:                record.Status,
		StatusCode:            governanceinternalclient.ProblemStatusCode(firstNonEmpty(record.StatusCode, strconv.Itoa(int(httpStatus)))),
		StatusDetail:          optionalString(record.StatusDetail),
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func hashTextForAudit(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
