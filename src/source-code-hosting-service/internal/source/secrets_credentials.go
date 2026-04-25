package source

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	secretsinternalclient "github.com/forge-metal/secrets-service/internalclient"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

var secretsCredentialTracer = otel.Tracer("source-code-hosting-service/secrets")

type SecretsCredentialClient struct {
	Client *secretsinternalclient.ClientWithResponses
}

func NewSecretsCredentialClient(baseURL string, httpClient secretsinternalclient.HttpRequestDoer) (SecretsCredentialClient, error) {
	client, err := secretsinternalclient.NewClientWithResponses(strings.TrimRight(baseURL, "/"), secretsinternalclient.WithHTTPClient(httpClient))
	if err != nil {
		return SecretsCredentialClient{}, err
	}
	return SecretsCredentialClient{Client: client}, nil
}

func (c SecretsCredentialClient) CreateSourceGitCredential(ctx context.Context, principal Principal, input CreateGitCredentialRequest) (_ GitCredential, err error) {
	ctx, span := secretsCredentialTracer.Start(ctx, "source.secrets.git_credential.create")
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()
	if c.Client == nil {
		return GitCredential{}, ErrStoreUnavailable
	}
	if err := ValidatePrincipal(principal); err != nil {
		return GitCredential{}, err
	}
	input, err = NormalizeCreateGitCredential(input)
	if err != nil {
		return GitCredential{}, err
	}
	expiresAt := time.Now().UTC().Add(time.Duration(input.ExpiresInSeconds) * time.Second)
	orgID := strconv.FormatUint(principal.OrgID, 10)
	metadata := map[string]string{
		"source":   "source-code-hosting-service",
		"org_path": OrgPath(principal.OrgID),
		"label":    input.Label,
	}
	displayName := input.Label
	expiresAtText := expiresAt.Format(time.RFC3339Nano)
	resp, err := c.Client.CreateInternalOpaqueCredentialWithResponse(ctx, secretsinternalclient.CreateInternalOpaqueCredentialJSONRequestBody{
		ActorId:     principal.Subject,
		DisplayName: &displayName,
		ExpiresAt:   &expiresAtText,
		Kind:        GitCredentialKind,
		Metadata:    &metadata,
		OrgId:       orgID,
		Scopes:      []string{"repo:read", "repo:write"},
	})
	if err != nil {
		return GitCredential{}, fmt.Errorf("%w: create secrets credential: %v", ErrStoreUnavailable, err)
	}
	if resp.JSON201 == nil {
		return GitCredential{}, unexpectedSecretsStatus("create credential", resp.HTTPResponse, resp.Body)
	}
	credential, err := gitCredentialFromSecrets(principal, input.Label, resp.JSON201.Credential, resp.JSON201.Token)
	if err != nil {
		return GitCredential{}, err
	}
	span.SetAttributes(
		attribute.Int64("forge_metal.org_id", int64(principal.OrgID)),
		attribute.String("source.git_credential_id", credential.CredentialID.String()),
		attribute.String("secrets.credential_kind", GitCredentialKind),
	)
	return credential, nil
}

func (c SecretsCredentialClient) VerifySourceGitCredential(ctx context.Context, orgID uint64, actorID string, token string, requiredScopes []string) (_ GitCredential, _ bool, err error) {
	ctx, span := secretsCredentialTracer.Start(ctx, "source.secrets.git_credential.verify")
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()
	if c.Client == nil {
		return GitCredential{}, false, ErrStoreUnavailable
	}
	orgText := strconv.FormatUint(orgID, 10)
	actorID = strings.TrimSpace(actorID)
	body := secretsinternalclient.VerifyInternalOpaqueCredentialJSONRequestBody{
		Kind:  GitCredentialKind,
		OrgId: orgText,
		Token: strings.TrimSpace(token),
	}
	if actorID != "" {
		body.ActorId = &actorID
	}
	if len(requiredScopes) > 0 {
		body.RequiredScopes = &requiredScopes
	}
	resp, err := c.Client.VerifyInternalOpaqueCredentialWithResponse(ctx, body)
	if err != nil {
		return GitCredential{}, false, fmt.Errorf("%w: verify secrets credential: %v", ErrStoreUnavailable, err)
	}
	if resp.JSON200 == nil {
		return GitCredential{}, false, unexpectedSecretsStatus("verify credential", resp.HTTPResponse, resp.Body)
	}
	if !resp.JSON200.Active || resp.JSON200.Credential == nil {
		span.SetAttributes(attribute.Bool("secrets.credential_active", false))
		return GitCredential{}, false, nil
	}
	credential, err := gitCredentialFromSecrets(Principal{OrgID: orgID}, "", *resp.JSON200.Credential, "")
	if err != nil {
		return GitCredential{}, false, err
	}
	span.SetAttributes(
		attribute.Int64("forge_metal.org_id", int64(orgID)),
		attribute.String("source.git_credential_id", credential.CredentialID.String()),
		attribute.Bool("secrets.credential_active", true),
	)
	return credential, true, nil
}

func gitCredentialFromSecrets(principal Principal, fallbackLabel string, wire secretsinternalclient.OpaqueCredentialWire, token string) (GitCredential, error) {
	credentialID, err := parseWireUUID(wire.CredentialId, "credential_id")
	if err != nil {
		return GitCredential{}, err
	}
	orgID := principal.OrgID
	if wire.OrgId != nil && strings.TrimSpace(*wire.OrgId) != "" {
		parsed, err := strconv.ParseUint(strings.TrimSpace(*wire.OrgId), 10, 64)
		if err != nil || parsed == 0 {
			return GitCredential{}, ErrStoreUnavailable
		}
		orgID = parsed
	}
	if orgID == 0 {
		return GitCredential{}, ErrStoreUnavailable
	}
	metadata := map[string]string{}
	if wire.Metadata != nil {
		metadata = *wire.Metadata
	}
	label := firstNonEmpty(metadata["label"], stringPtrValue(wire.DisplayName), fallbackLabel, "git push")
	expiresAt, err := parseWireTime(wire.ExpiresAt, "expires_at")
	if err != nil {
		return GitCredential{}, err
	}
	createdAt, err := parseWireTime(wire.CreatedAt, "created_at")
	if err != nil {
		return GitCredential{}, err
	}
	scopes := []string{"repo:read", "repo:write"}
	if wire.Scopes != nil && len(*wire.Scopes) > 0 {
		scopes = append([]string(nil), (*wire.Scopes)...)
	}
	return GitCredential{
		CredentialID: credentialID,
		OrgID:        orgID,
		OrgPath:      firstNonEmpty(metadata["org_path"], OrgPath(orgID)),
		ActorID:      firstNonEmpty(stringPtrValue(wire.Subject), principal.Subject),
		Label:        label,
		Username:     GitCredentialUsername,
		Token:        token,
		TokenPrefix:  stringPtrValue(wire.TokenPrefix),
		Scopes:       scopes,
		State:        firstNonEmpty(stringPtrValue(wire.Status), "active"),
		ExpiresAt:    expiresAt,
		CreatedAt:    createdAt,
	}, nil
}

func parseWireUUID(value *string, field string) (uuid.UUID, error) {
	if value == nil || strings.TrimSpace(*value) == "" {
		return uuid.Nil, fmt.Errorf("%w: secrets credential missing %s", ErrStoreUnavailable, field)
	}
	parsed, err := uuid.Parse(strings.TrimSpace(*value))
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: secrets credential invalid %s", ErrStoreUnavailable, field)
	}
	return parsed, nil
}

func parseWireTime(value *string, field string) (time.Time, error) {
	if value == nil || strings.TrimSpace(*value) == "" {
		return time.Time{}, fmt.Errorf("%w: secrets credential missing %s", ErrStoreUnavailable, field)
	}
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(*value))
	if err != nil {
		parsed, err = time.Parse(time.RFC3339, strings.TrimSpace(*value))
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: secrets credential invalid %s", ErrStoreUnavailable, field)
	}
	return parsed.UTC(), nil
}

func unexpectedSecretsStatus(operation string, response *http.Response, body []byte) error {
	status := 0
	if response != nil {
		status = response.StatusCode
	}
	return fmt.Errorf("%w: secrets %s returned status %d: %s", ErrStoreUnavailable, operation, status, strings.TrimSpace(string(body)))
}

func stringPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
