package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/verself/apiwire"
	workloadauth "github.com/verself/auth-middleware/workload"
	"github.com/verself/profile-service/internal/profile"
)

type dataRightsInput struct {
	Body apiwire.ProfileDataRightsRequest
}

type dataRightsStatusInput struct {
	RequestID string `path:"request_id" doc:"Data-rights request ID"`
}

type dataRightsOutput struct {
	Body apiwire.ProfileDataRightsManifest
}

func (o *dataRightsOutput) auditDetails() auditDetails {
	return auditDetails{
		artifactSHA256: firstArtifactHash(o.Body.Artifacts),
		artifactBytes:  artifactBytes(o.Body.Artifacts),
	}
}

func RegisterInternalRoutes(api huma.API, svc *profile.Service) {
	registerProfileRoute(api, huma.Operation{
		OperationID:   "profile-org-export",
		Method:        http.MethodPost,
		Path:          "/internal/v1/data-rights/org-export",
		Summary:       "Export organization-local profile data",
		DefaultStatus: http.StatusOK,
	}, operationPolicy{
		Permission:       permissionProfileDataRights,
		Resource:         "profile_data_rights",
		Action:           "export",
		OrgScope:         "request_org_id",
		RateLimitClass:   "internal_data_rights",
		AuditEvent:       "profile.data_rights.org_export",
		OperationDisplay: "export organization profile data",
		OperationType:    "export",
		RiskLevel:        "high",
		BodyLimitBytes:   bodyLimitDataRightsJSON,
		Internal:         true,
	}, orgExport(svc))

	registerProfileRoute(api, huma.Operation{
		OperationID:   "profile-subject-export",
		Method:        http.MethodPost,
		Path:          "/internal/v1/data-rights/subject-export",
		Summary:       "Export subject-local profile data",
		DefaultStatus: http.StatusOK,
	}, operationPolicy{
		Permission:       permissionProfileDataRights,
		Resource:         "profile_data_rights",
		Action:           "export",
		OrgScope:         "request_subject_id",
		RateLimitClass:   "internal_data_rights",
		AuditEvent:       "profile.data_rights.subject_export",
		OperationDisplay: "export subject profile data",
		OperationType:    "export",
		RiskLevel:        "high",
		BodyLimitBytes:   bodyLimitDataRightsJSON,
		Internal:         true,
	}, subjectExport(svc))

	registerProfileRoute(api, huma.Operation{
		OperationID:   "profile-subject-erasure",
		Method:        http.MethodPost,
		Path:          "/internal/v1/data-rights/subject-erasure",
		Summary:       "Erase subject-local profile data",
		DefaultStatus: http.StatusOK,
	}, operationPolicy{
		Permission:       permissionProfileDataRights,
		Resource:         "profile_data_rights",
		Action:           "erase",
		OrgScope:         "request_subject_id",
		RateLimitClass:   "internal_data_rights",
		AuditEvent:       "profile.data_rights.subject_erasure",
		OperationDisplay: "erase subject profile data",
		OperationType:    "delete",
		RiskLevel:        "high",
		BodyLimitBytes:   bodyLimitDataRightsJSON,
		Internal:         true,
	}, subjectErasure(svc))

	registerProfileRoute(api, huma.Operation{
		OperationID: "profile-data-rights-status",
		Method:      http.MethodGet,
		Path:        "/internal/v1/data-rights/requests/{request_id}",
		Summary:     "Get profile data-rights request status",
	}, operationPolicy{
		Permission:       permissionProfileDataRights,
		Resource:         "profile_data_rights",
		Action:           "read",
		OrgScope:         "request_id",
		RateLimitClass:   "internal_data_rights",
		AuditEvent:       "profile.data_rights.status",
		OperationDisplay: "get profile data-rights status",
		OperationType:    "read",
		RiskLevel:        "medium",
		Internal:         true,
	}, dataRightsStatus(svc))
}

func orgExport(svc *profile.Service) func(context.Context, *dataRightsInput) (*dataRightsOutput, error) {
	return func(ctx context.Context, input *dataRightsInput) (*dataRightsOutput, error) {
		if err := requireGovernancePeer(ctx); err != nil {
			return nil, err
		}
		manifest, err := svc.OrgExport(ctx, dataRightsRequest(input.Body))
		if err != nil {
			return nil, profileError(ctx, err)
		}
		return &dataRightsOutput{Body: dataRightsManifestDTO(manifest)}, nil
	}
}

func subjectExport(svc *profile.Service) func(context.Context, *dataRightsInput) (*dataRightsOutput, error) {
	return func(ctx context.Context, input *dataRightsInput) (*dataRightsOutput, error) {
		if err := requireGovernancePeer(ctx); err != nil {
			return nil, err
		}
		manifest, err := svc.SubjectExport(ctx, dataRightsRequest(input.Body))
		if err != nil {
			return nil, profileError(ctx, err)
		}
		return &dataRightsOutput{Body: dataRightsManifestDTO(manifest)}, nil
	}
}

func subjectErasure(svc *profile.Service) func(context.Context, *dataRightsInput) (*dataRightsOutput, error) {
	return func(ctx context.Context, input *dataRightsInput) (*dataRightsOutput, error) {
		if err := requireGovernancePeer(ctx); err != nil {
			return nil, err
		}
		manifest, err := svc.SubjectErasure(ctx, dataRightsRequest(input.Body))
		if err != nil {
			return nil, profileError(ctx, err)
		}
		return &dataRightsOutput{Body: dataRightsManifestDTO(manifest)}, nil
	}
}

func dataRightsStatus(svc *profile.Service) func(context.Context, *dataRightsStatusInput) (*dataRightsOutput, error) {
	return func(ctx context.Context, input *dataRightsStatusInput) (*dataRightsOutput, error) {
		if err := requireGovernancePeer(ctx); err != nil {
			return nil, err
		}
		manifest, err := svc.DataRightsStatus(ctx, strings.TrimSpace(input.RequestID))
		if err != nil {
			return nil, profileError(ctx, err)
		}
		return &dataRightsOutput{Body: dataRightsManifestDTO(manifest)}, nil
	}
}

func requireGovernancePeer(ctx context.Context) error {
	if _, ok := workloadauth.PeerIDFromContext(ctx); !ok {
		return problem(ctx, http.StatusUnauthorized, "missing-workload-identity", "missing SPIFFE peer identity", nil)
	}
	return nil
}

func dataRightsRequest(dto apiwire.ProfileDataRightsRequest) profile.DataRightsRequest {
	return profile.DataRightsRequest{
		RequestID:   dto.RequestID,
		RequestedAt: dto.RequestedAt,
		RequestedBy: dto.RequestedBy,
		Traceparent: dto.Traceparent,
		OrgID:       dto.OrgID,
		SubjectID:   dto.SubjectID,
	}
}

func dataRightsManifestDTO(manifest profile.DataRightsManifest) apiwire.ProfileDataRightsManifest {
	return apiwire.ProfileDataRightsManifest{
		RequestID:          manifest.RequestID,
		RequestType:        manifest.RequestType,
		Status:             manifest.Status,
		OrgID:              manifest.OrgID,
		SubjectID:          manifest.SubjectID,
		Artifacts:          artifactDTOs(manifest.Artifacts),
		ErasureActions:     erasureActionDTOs(manifest.ErasureActions),
		RetainedCategories: retainedCategoryDTOs(manifest.RetainedCategories),
		RecordCounts:       manifest.RecordCounts,
		CompletedAt:        manifest.CompletedAt,
	}
}

func artifactDTOs(artifacts []profile.DataRightsArtifact) []apiwire.ProfileDataRightsArtifact {
	out := make([]apiwire.ProfileDataRightsArtifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		out = append(out, apiwire.ProfileDataRightsArtifact{
			Path:        artifact.Path,
			ContentType: artifact.ContentType,
			Rows:        artifact.Rows,
			Bytes:       artifact.Bytes,
			SHA256:      artifact.SHA256,
		})
	}
	return out
}

func erasureActionDTOs(actions []profile.DataRightsErasureAction) []apiwire.ProfileDataRightsErasureAction {
	out := make([]apiwire.ProfileDataRightsErasureAction, 0, len(actions))
	for _, action := range actions {
		out = append(out, apiwire.ProfileDataRightsErasureAction{
			Name:        action.Name,
			Rows:        action.Rows,
			Description: action.Description,
		})
	}
	return out
}

func retainedCategoryDTOs(categories []profile.DataRightsRetainedCategory) []apiwire.ProfileDataRightsRetainedCategory {
	out := make([]apiwire.ProfileDataRightsRetainedCategory, 0, len(categories))
	for _, category := range categories {
		out = append(out, apiwire.ProfileDataRightsRetainedCategory{
			Category: category.Category,
			Reason:   category.Reason,
		})
	}
	return out
}

func firstArtifactHash(artifacts []apiwire.ProfileDataRightsArtifact) string {
	if len(artifacts) == 0 {
		return ""
	}
	return artifacts[0].SHA256
}
