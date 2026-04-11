package api

import (
	"github.com/forge-metal/apiwire"
	"github.com/forge-metal/sandbox-rental-service/internal/jobs"
)

func webhookEndpointRecord(record jobs.WebhookEndpointRecord) apiwire.SandboxWebhookEndpointRecord {
	return apiwire.SandboxWebhookEndpointRecord{
		EndpointID:        record.EndpointID,
		IntegrationID:     record.IntegrationID,
		OrgID:             apiwire.Uint64(record.OrgID),
		Provider:          record.Provider,
		ProviderHost:      record.ProviderHost,
		Label:             record.Label,
		Active:            record.Active,
		CreatedAt:         record.CreatedAt,
		UpdatedAt:         record.UpdatedAt,
		LastDeliveryAt:    record.LastDeliveryAt,
		DeliveryCount:     record.DeliveryCount,
		SecretFingerprint: record.SecretFingerprint,
	}
}

func webhookEndpointRecords(records []jobs.WebhookEndpointRecord) []apiwire.SandboxWebhookEndpointRecord {
	out := make([]apiwire.SandboxWebhookEndpointRecord, 0, len(records))
	for _, record := range records {
		out = append(out, webhookEndpointRecord(record))
	}
	return out
}

func createWebhookEndpointResponse(result jobs.CreateWebhookEndpointResult, webhookURL string) apiwire.SandboxCreateWebhookEndpointResponse {
	record := result.Endpoint
	return apiwire.SandboxCreateWebhookEndpointResponse{
		EndpointID:        record.EndpointID,
		IntegrationID:     record.IntegrationID,
		WebhookURL:        webhookURL,
		Secret:            result.Secret,
		Provider:          record.Provider,
		ProviderHost:      record.ProviderHost,
		Label:             record.Label,
		Active:            record.Active,
		CreatedAt:         record.CreatedAt,
		UpdatedAt:         record.UpdatedAt,
		DeliveryCount:     record.DeliveryCount,
		SecretFingerprint: record.SecretFingerprint,
	}
}

func rotateWebhookEndpointSecretResponse(result jobs.RotateWebhookEndpointSecretResult) apiwire.SandboxRotateWebhookEndpointSecretResponse {
	return apiwire.SandboxRotateWebhookEndpointSecretResponse{
		EndpointID:         result.EndpointID,
		Secret:             result.Secret,
		SecretFingerprint:  result.SecretFingerprint,
		RotatedAt:          result.RotatedAt,
		PreviousRetiringAt: result.PreviousRetiringAt,
	}
}
