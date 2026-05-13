package source

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	iamclient "github.com/verself/iam-service/client"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

var identityTracer = otel.Tracer("source-code-hosting-service/identity")

type IAMClient struct {
	Client *iamclient.Client
}

func NewIAMClient(baseURL string, httpClient iamclient.HTTPRequestDoer) (IAMClient, error) {
	client, err := iamclient.NewClient(strings.TrimRight(baseURL, "/"), iamclient.WithHTTPClient(httpClient))
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
	body := iamclient.ResolveOrganizationInputBody{
		RequireActive: true,
	}
	if orgID != 0 {
		value := iamclient.ProviderOrgId(strconv.FormatUint(orgID, 10))
		body.OrgID = &value
	}
	if slug != "" {
		value := iamclient.OrgSlug(slug)
		body.Slug = &value
	}
	resp, err := c.Client.ResolveOrganization(ctx, iamclient.ResolveOrganizationRequest{Body: body})
	if err != nil {
		return OrganizationReference{}, fmt.Errorf("%w: resolve organization: %v", ErrStoreUnavailable, err)
	}
	if resp.Result == nil {
		status := resp.StatusCode
		body := ""
		if resp.HTTPResponse != nil {
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
	org := resp.Result.Organization
	resolvedOrgID, err := strconv.ParseUint(strings.TrimSpace(string(org.OrgID)), 10, 64)
	if err != nil || resolvedOrgID == 0 {
		return OrganizationReference{}, fmt.Errorf("%w: parse organization id: %v", ErrStoreUnavailable, err)
	}
	ref := OrganizationReference{
		OrgID:          resolvedOrgID,
		Slug:           strings.TrimSpace(string(org.Slug)),
		DisplayName:    strings.TrimSpace(string(org.DisplayName)),
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

func trimOptionalString(value *iamclient.OrgSlug) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(string(*value))
}
