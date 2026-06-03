package api

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/verself/object-storage-service/internal/objectstorage"
	workloadauth "github.com/verself/service-runtime/workload"
)

type createObjectWriteSessionInput struct {
	Site           string `path:"site" required:"true"`
	IdempotencyKey string `header:"Idempotency-Key" required:"true"`
	Body           createObjectWriteSessionBody
}

type createObjectWriteSessionBody struct {
	Capability string               `json:"capability" required:"true"`
	Namespace  string               `json:"namespace" required:"true"`
	Objects    []objectWriteRequest `json:"objects" required:"true"`
	TTLSeconds int                  `json:"ttl_seconds" required:"true"`
}

type objectWriteRequest struct {
	Name      string `json:"name" required:"true"`
	SHA256    string `json:"sha256" required:"true"`
	SizeBytes int64  `json:"size_bytes" required:"true"`
}

type completeObjectWriteSessionInput struct {
	Site      string `path:"site" required:"true"`
	SessionID string `path:"session_id" required:"true"`
}

type objectWriteSessionOutput struct {
	Body objectWriteSessionBody
}

type objectWriteSessionBody struct {
	SessionID   string                     `json:"session_id" required:"true"`
	ExpiresAt   string                     `json:"expires_at,omitempty"`
	CompletedAt *string                    `json:"completed_at,omitempty"`
	Objects     []objectWriteSessionObject `json:"objects" required:"true"`
}

type objectWriteSessionObject struct {
	Name         string              `json:"name" required:"true"`
	Key          string              `json:"key" required:"true"`
	Action       string              `json:"action" required:"true"`
	Write        *s3CompatibleObject `json:"write,omitempty"`
	Read         *s3CompatibleObject `json:"read,omitempty"`
	GetterSource string              `json:"getter_source,omitempty"`
}

type s3CompatibleObject struct {
	Method    string            `json:"method" required:"true"`
	URL       string            `json:"url" required:"true"`
	Headers   map[string]string `json:"headers" required:"true"`
	ExpiresAt string            `json:"expires_at" required:"true"`
	SHA256    string            `json:"sha256,omitempty"`
}

func RegisterInternalRoutes(api huma.API, svc *objectstorage.Service) {
	huma.Register(api, huma.Operation{
		OperationID:   "create-object-write-session",
		Method:        http.MethodPost,
		Path:          "/internal/v1/object-storage/sites/{site}/write-sessions",
		Summary:       "Create object write session",
		DefaultStatus: http.StatusCreated,
		MaxBodyBytes:  65536,
		Errors:        []int{http.StatusBadRequest, http.StatusConflict, http.StatusServiceUnavailable},
		Extensions: map[string]any{
			"x-verself-contract": map[string]any{
				"shape_id":    "verself.objectstorage.v1#CreateObjectWriteSession",
				"identity":    "spiffe_mtls",
				"permission":  "object-storage:object-session:write",
				"audit_event": "object_storage.object_write_session.create",
				"resource":    "object_write_session",
				"action":      "create",
				"sdk_module":  "objectStorageInternal.writeSessions",
				"sdk_method":  "create",
			},
		},
	}, func(ctx context.Context, input *createObjectWriteSessionInput) (*objectWriteSessionOutput, error) {
		actor := internalActor(ctx)
		requests := make([]objectstorage.ObjectWriteRequest, 0, len(input.Body.Objects))
		for _, object := range input.Body.Objects {
			requests = append(requests, objectstorage.ObjectWriteRequest{
				Name:      object.Name,
				SHA256:    object.SHA256,
				SizeBytes: object.SizeBytes,
			})
		}
		session, err := svc.CreateObjectWriteSession(ctx, objectstorage.CreateObjectWriteSessionInput{
			IdempotencyKey: input.IdempotencyKey,
			Site:           input.Site,
			Capability:     input.Body.Capability,
			Namespace:      input.Body.Namespace,
			Objects:        requests,
			TTL:            time.Duration(input.Body.TTLSeconds) * time.Second,
			Actor:          actor,
		})
		if err != nil {
			return nil, toHumaError(ctx, "create object write session failed", err)
		}
		return objectWriteSessionOutputFromDomain(session), nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "complete-object-write-session",
		Method:        http.MethodPost,
		Path:          "/internal/v1/object-storage/sites/{site}/write-sessions/{session_id}/complete",
		Summary:       "Complete object write session",
		DefaultStatus: http.StatusOK,
		MaxBodyBytes:  1024,
		Errors:        []int{http.StatusBadRequest, http.StatusNotFound, http.StatusConflict, http.StatusServiceUnavailable},
		Extensions: map[string]any{
			"x-verself-contract": map[string]any{
				"shape_id":    "verself.objectstorage.v1#CompleteObjectWriteSession",
				"identity":    "spiffe_mtls",
				"permission":  "object-storage:object-session:write",
				"audit_event": "object_storage.object_write_session.complete",
				"resource":    "object_write_session",
				"action":      "verify",
				"sdk_module":  "objectStorageInternal.writeSessions",
				"sdk_method":  "complete",
			},
		},
	}, func(ctx context.Context, input *completeObjectWriteSessionInput) (*objectWriteSessionOutput, error) {
		session, err := svc.CompleteObjectWriteSession(ctx, input.Site, input.SessionID)
		if err != nil {
			return nil, toHumaError(ctx, "complete object write session failed", err)
		}
		return objectWriteSessionOutputFromDomain(session), nil
	})
}

func objectWriteSessionOutputFromDomain(session objectstorage.ObjectWriteSession) *objectWriteSessionOutput {
	body := objectWriteSessionBody{
		SessionID: session.SessionID,
		ExpiresAt: session.ExpiresAt.UTC().Format(time.RFC3339Nano),
		Objects:   make([]objectWriteSessionObject, 0, len(session.Objects)),
	}
	if session.CompletedAt != nil {
		completedAt := session.CompletedAt.UTC().Format(time.RFC3339Nano)
		body.CompletedAt = &completedAt
	}
	for _, object := range session.Objects {
		item := objectWriteSessionObject{
			Name:   object.Name,
			Key:    object.ObjectKey,
			Action: object.Action,
		}
		if object.Write != nil {
			item.Write = s3CompatibleObjectFromDomain(*object.Write)
		}
		if object.Read != nil {
			item.Read = s3CompatibleObjectFromDomain(*object.Read)
			item.GetterSource = object.Read.URL
		}
		body.Objects = append(body.Objects, item)
	}
	return &objectWriteSessionOutput{Body: body}
}

func s3CompatibleObjectFromDomain(object objectstorage.S3CompatibleObject) *s3CompatibleObject {
	return &s3CompatibleObject{
		Method:    object.Method,
		URL:       object.URL,
		Headers:   object.Headers,
		ExpiresAt: object.ExpiresAt.UTC().Format(time.RFC3339Nano),
		SHA256:    object.SHA256,
	}
}

func internalActor(ctx context.Context) string {
	if peerID, ok := workloadauth.PeerIDFromContext(ctx); ok {
		return "spiffe:" + peerID.String()
	}
	return "spiffe:unknown"
}
