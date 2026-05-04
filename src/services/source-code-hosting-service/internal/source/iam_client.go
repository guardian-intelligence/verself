package source

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	iamclient "github.com/verself/iam-service/internalclient"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

var identityTracer = otel.Tracer("source-code-hosting-service/identity")

type IAMClient struct {
	Client *iamclient.ClientWithResponses
}

func NewIAMClient(baseURL string, httpClient iamclient.HttpRequestDoer) (IAMClient, error) {
	client, err := iamclient.NewClientWithResponses(strings.TrimRight(baseURL, "/"), iamclient.WithHTTPClient(httpClient))
	if err != nil {
		return IAMClient{}, err
	}
	return IAMClient{Client: client}, nil
}

func (c IAMClient) ResolveSourceOrganization(ctx context.Context, slug string) (_ OrganizationReference, err error) {
	return c.resolve(ctx, 0, slug)
}

func (c IAMClient) ResolveSourceOrganizationID(ctx context.Context, orgID uint64) (_ OrganizationReference, err error) {
	return c.resolve(ctx, orgID, "")
}

func (c IAMClient) resolve(ctx context.Context, orgID uint64, slug string) (_ OrganizationReference, err error) {
	ctx, span := identityTracer.Start(ctx, "source.identity.organization.resolve")
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()
	if c.Client == nil {
		return OrganizationReference{}, ErrStoreUnavailable
	}
	slug = NormalizeSlug(slug)
	if orgID == 0 && slug == "" {
		return OrganizationReference{}, ErrInvalid
	}
	span.SetAttributes(attribute.Int64("verself.org_id", int64FromUint64(orgID, "org id")), attribute.String("source.org_slug.requested", slug))
	body := iamclient.ResolveOrganizationJSONRequestBody{
		RequireActive: true,
	}
	if orgID != 0 {
		body.OrgId = stringPtr(strconv.FormatUint(orgID, 10))
	}
	if slug != "" {
		body.Slug = stringPtr(slug)
	}
	resp, err := c.Client.ResolveOrganizationWithResponse(ctx, body)
	if err != nil {
		return OrganizationReference{}, fmt.Errorf("%w: resolve organization: %v", ErrStoreUnavailable, err)
	}
	if resp.JSON200 == nil {
		status := 0
		body := ""
		if resp.HTTPResponse != nil {
			status = resp.HTTPResponse.StatusCode
			body = strings.TrimSpace(string(resp.Body))
		}
		switch status {
		case http.StatusNotFound:
			return OrganizationReference{}, ErrNotFound
		case http.StatusBadRequest, http.StatusConflict:
			return OrganizationReference{}, ErrInvalid
		default:
			return OrganizationReference{}, fmt.Errorf("%w: resolve organization unexpected status %d: %s", ErrStoreUnavailable, status, body)
		}
	}
	org := resp.JSON200.Organization
	resolvedOrgID, err := strconv.ParseUint(strings.TrimSpace(org.OrgId), 10, 64)
	if err != nil || resolvedOrgID == 0 {
		return OrganizationReference{}, fmt.Errorf("%w: parse organization id: %v", ErrStoreUnavailable, err)
	}
	ref := OrganizationReference{
		OrgID:          resolvedOrgID,
		Slug:           strings.TrimSpace(org.Slug),
		DisplayName:    strings.TrimSpace(org.DisplayName),
		RedirectedFrom: trimOptionalString(org.RedirectedFrom),
	}
	if ref.OrgID == 0 || ref.Slug == "" {
		return OrganizationReference{}, ErrStoreUnavailable
	}
	span.SetAttributes(
		attribute.Int64("verself.org_id", int64FromUint64(ref.OrgID, "org id")),
		attribute.String("source.org_slug", ref.Slug),
		attribute.String("source.org_slug.redirected_from", ref.RedirectedFrom),
	)
	return ref, nil
}

func stringPtr(value string) *string {
	return &value
}

func trimOptionalString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
