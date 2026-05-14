package api

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/spiffe/go-spiffe/v2/workloadapi"
	governanceinternalclient "github.com/verself/governance-service/internalclient"
	"github.com/verself/object-storage-service/internal/objectstorage"
	workloadauth "github.com/verself/service-runtime/workload"
)

type apiActivitySinkConfig struct {
	Client *governanceinternalclient.Client
}

var configuredAPIActivitySink atomic.Pointer[apiActivitySinkConfig]

func ConfigureAPIActivitySink(url string, source *workloadapi.X509Source) {
	url = strings.TrimSpace(url)
	if url == "" || source == nil {
		return
	}
	httpClient, err := workloadauth.MTLSClientForService(source, workloadauth.ServiceGovernance, nil)
	if err != nil {
		slog.Default().Error("object-storage governance API Activity mtls client init failed", "error", err)
		return
	}
	sinkClient, err := governanceinternalclient.NewClient(url, governanceinternalclient.WithHTTPClient(httpClient))
	if err != nil {
		slog.Default().Error("object-storage governance API Activity client init failed", "error", err)
		return
	}
	configuredAPIActivitySink.Store(&apiActivitySinkConfig{
		Client: sinkClient,
	})
}

func SendGovernanceAPIActivity(ctx context.Context, record objectstorageAPIActivity) error {
	sink := configuredAPIActivitySink.Load()
	if sink == nil || strings.TrimSpace(record.OrgID) == "" {
		return nil
	}
	reqCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	resp, err := sink.Client.AppendAPIActivity(reqCtx, governanceinternalclient.AppendAPIActivityRequest{Body: governanceAPIActivityToContract(record)})
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		err := fmt.Errorf("governance API Activity rejected with status %d", resp.StatusCode)
		slog.Default().ErrorContext(ctx, "object-storage governance API Activity rejected", "status", resp.StatusCode)
		return err
	}
	return nil
}

type objectstorageAPIActivity = objectstorage.APIActivity

func governanceAPIActivityToContract(record objectstorageAPIActivity) governanceinternalclient.APIActivityRecord {
	httpStatus := record.HTTPStatus
	if httpStatus == 0 {
		httpStatus = 500
		if strings.EqualFold(record.Status, "Success") {
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
		HTTPRequest:           governanceinternalclient.APIActivityHTTPRequest{Method: "POST", Route: "/internal/" + record.APIOperation},
		HTTPResponse:          governanceinternalclient.APIActivityHTTPResponse{Code: httpStatus},
		AuthorizationDecision: governanceinternalclient.AuthorizationDecision(record.Decision),
		Status:                governanceinternalclient.APIActivityStatus(record.Status),
		StatusCode:            governanceinternalclient.ProblemStatusCode(firstNonEmpty(record.StatusCode, strconv.Itoa(int(httpStatus)))),
		TraceUID:              optionalStringTyped[governanceinternalclient.TraceID](record.TraceUID),
		Unmapped:              optionalMap(record.Unmapped),
	}
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
