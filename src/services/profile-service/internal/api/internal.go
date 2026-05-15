package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/verself/profile-service/internal/internalcontractapi"
	"github.com/verself/profile-service/internal/profile"
	profilecatalog "github.com/verself/profile-service/internal/routecatalog"
	runtimecatalog "github.com/verself/service-runtime/routecatalog"
	workloadauth "github.com/verself/service-runtime/workload"
)

type dataRightsInput = internalcontractapi.ProfileDataRightsInput

type dataRightsStatusInput = internalcontractapi.ProfileDataRightsStatusInput

type dataRightsOutput struct {
	Body internalcontractapi.ProfileDataRightsOutputBody
}

func (o *dataRightsOutput) auditDetails() auditDetails {
	return auditDetails{
		artifactSHA256: firstArtifactHash(o.Body.Manifest.Artifacts),
		artifactBytes:  artifactBytes(o.Body.Manifest.Artifacts),
	}
}

func RegisterInternalRoutes(api huma.API, svc *profile.Service) {
	op, policy := internalProfileContract(profilecatalog.Internal.MustOperation("profile-org-export"), "Export organization-local profile data")
	registerProfileRoute(api, nil, op, policy, orgExport(svc))
	op, policy = internalProfileContract(profilecatalog.Internal.MustOperation("profile-subject-export"), "Export subject-local profile data")
	registerProfileRoute(api, nil, op, policy, subjectExport(svc))
	op, policy = internalProfileContract(profilecatalog.Internal.MustOperation("profile-subject-erasure"), "Erase subject-local profile data")
	registerProfileRoute(api, nil, op, policy, subjectErasure(svc))
	op, policy = internalProfileContract(profilecatalog.Internal.MustOperation("profile-data-rights-status"), "Get profile data-rights request status")
	registerProfileRoute(api, nil, op, policy, dataRightsStatus(svc))
}

func internalProfileContract(desc runtimecatalog.Operation, summary string) (huma.Operation, profileOperationPolicy) {
	op := huma.Operation{
		OperationID:   desc.OperationID,
		Method:        desc.Method,
		Path:          desc.Path,
		Summary:       summary,
		DefaultStatus: desc.DefaultStatus,
		Errors:        desc.ProblemStatuses(),
	}
	return op, profileOperationPolicy{OperationPolicy: desc.OperationPolicy(), Internal: true}
}

func orgExport(svc *profile.Service) func(context.Context, *dataRightsInput) (*dataRightsOutput, error) {
	return func(ctx context.Context, input *dataRightsInput) (*dataRightsOutput, error) {
		if err := requireGovernancePeer(ctx); err != nil {
			return nil, err
		}
		request, err := dataRightsRequest(input.Body)
		if err != nil {
			return nil, badRequest(ctx, "data-rights-request-invalid", "data-rights request is invalid", err)
		}
		manifest, err := svc.OrgExport(ctx, request)
		if err != nil {
			return nil, profileError(ctx, err)
		}
		return &dataRightsOutput{Body: dataRightsOutputBody(manifest)}, nil
	}
}

func subjectExport(svc *profile.Service) func(context.Context, *dataRightsInput) (*dataRightsOutput, error) {
	return func(ctx context.Context, input *dataRightsInput) (*dataRightsOutput, error) {
		if err := requireGovernancePeer(ctx); err != nil {
			return nil, err
		}
		request, err := dataRightsRequest(input.Body)
		if err != nil {
			return nil, badRequest(ctx, "data-rights-request-invalid", "data-rights request is invalid", err)
		}
		manifest, err := svc.SubjectExport(ctx, request)
		if err != nil {
			return nil, profileError(ctx, err)
		}
		return &dataRightsOutput{Body: dataRightsOutputBody(manifest)}, nil
	}
}

func subjectErasure(svc *profile.Service) func(context.Context, *dataRightsInput) (*dataRightsOutput, error) {
	return func(ctx context.Context, input *dataRightsInput) (*dataRightsOutput, error) {
		if err := requireGovernancePeer(ctx); err != nil {
			return nil, err
		}
		request, err := dataRightsRequest(input.Body)
		if err != nil {
			return nil, badRequest(ctx, "data-rights-request-invalid", "data-rights request is invalid", err)
		}
		manifest, err := svc.SubjectErasure(ctx, request)
		if err != nil {
			return nil, profileError(ctx, err)
		}
		return &dataRightsOutput{Body: dataRightsOutputBody(manifest)}, nil
	}
}

func dataRightsStatus(svc *profile.Service) func(context.Context, *dataRightsStatusInput) (*dataRightsOutput, error) {
	return func(ctx context.Context, input *dataRightsStatusInput) (*dataRightsOutput, error) {
		if err := requireGovernancePeer(ctx); err != nil {
			return nil, err
		}
		manifest, err := svc.DataRightsStatus(ctx, strings.TrimSpace(string(input.RequestID)))
		if err != nil {
			return nil, profileError(ctx, err)
		}
		return &dataRightsOutput{Body: dataRightsOutputBody(manifest)}, nil
	}
}

func requireGovernancePeer(ctx context.Context) error {
	if _, ok := workloadauth.PeerIDFromContext(ctx); !ok {
		return problem(ctx, http.StatusUnauthorized, "missing-workload-identity", "missing SPIFFE peer identity", nil)
	}
	return nil
}

func dataRightsRequest(body internalcontractapi.ProfileDataRightsInputBody) (profile.DataRightsRequest, error) {
	requestedAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(body.RequestedAt))
	if err != nil {
		return profile.DataRightsRequest{}, err
	}
	return profile.DataRightsRequest{
		RequestID:   string(body.RequestID),
		RequestedAt: requestedAt.UTC(),
		RequestedBy: string(body.RequestedBy),
		Traceparent: contractString(body.Traceparent),
		OrgID:       contractString(body.OrgID),
		SubjectID:   contractString(body.SubjectID),
	}, nil
}

func dataRightsOutputBody(manifest profile.DataRightsManifest) internalcontractapi.ProfileDataRightsOutputBody {
	return internalcontractapi.ProfileDataRightsOutputBody{
		Manifest: internalcontractapi.ProfileDataRightsManifest{
			RequestID:          internalcontractapi.DataRightsRequestID(manifest.RequestID),
			RequestType:        internalcontractapi.DataRightsRequestType(manifest.RequestType),
			Status:             internalcontractapi.DataRightsStatus(manifest.Status),
			OrgID:              optionalContractString[internalcontractapi.OrgID](manifest.OrgID),
			SubjectID:          optionalContractString[internalcontractapi.SubjectID](manifest.SubjectID),
			Artifacts:          artifactContracts(manifest.Artifacts),
			ErasureActions:     erasureActionContracts(manifest.ErasureActions),
			RetainedCategories: retainedCategoryContracts(manifest.RetainedCategories),
			RecordCounts:       recordCountsContract(manifest.RecordCounts),
			CompletedAt:        formatContractTime(manifest.CompletedAt),
		},
	}
}

func artifactContracts(artifacts []profile.DataRightsArtifact) internalcontractapi.ProfileArtifacts {
	out := make(internalcontractapi.ProfileArtifacts, 0, len(artifacts))
	for _, artifact := range artifacts {
		out = append(out, internalcontractapi.ProfileArtifact{
			Path:        internalcontractapi.ArtifactPath(artifact.Path),
			ContentType: internalcontractapi.MediaType(artifact.ContentType),
			Rows:        internalcontractapi.DecimalUint64(artifact.Rows),
			Bytes:       internalcontractapi.DecimalUint64(artifact.Bytes),
			SHA256:      internalcontractapi.SHA256Hex(artifact.SHA256),
		})
	}
	return out
}

func erasureActionContracts(actions []profile.DataRightsErasureAction) internalcontractapi.ProfileErasureActions {
	out := make(internalcontractapi.ProfileErasureActions, 0, len(actions))
	for _, action := range actions {
		out = append(out, internalcontractapi.ProfileDataRightsErasureAction{
			Name:        internalcontractapi.DataRightsActionName(action.Name),
			Rows:        internalcontractapi.DecimalUint64(action.Rows),
			Description: optionalContractString[internalcontractapi.DataRightsActionDescription](action.Description),
		})
	}
	return out
}

func retainedCategoryContracts(categories []profile.DataRightsRetainedCategory) internalcontractapi.ProfileRetainedCategories {
	out := make(internalcontractapi.ProfileRetainedCategories, 0, len(categories))
	for _, category := range categories {
		out = append(out, internalcontractapi.ProfileDataRightsRetainedCategory{
			Category: internalcontractapi.DataRightsCategory(category.Category),
			Reason:   internalcontractapi.DataRightsReason(category.Reason),
		})
	}
	return out
}

func recordCountsContract(counts map[string]string) map[string]any {
	out := make(map[string]any, len(counts))
	for key, value := range counts {
		if strings.TrimSpace(key) != "" {
			out[key] = value
		}
	}
	return out
}

func firstArtifactHash(artifacts internalcontractapi.ProfileArtifacts) string {
	if len(artifacts) == 0 {
		return ""
	}
	return string(artifacts[0].SHA256)
}

func artifactBytes(artifacts internalcontractapi.ProfileArtifacts) uint64 {
	var total uint64
	for _, artifact := range artifacts {
		bytes, err := strconv.ParseUint(string(artifact.Bytes), 10, 64)
		if err == nil {
			total += bytes
		}
	}
	return total
}
