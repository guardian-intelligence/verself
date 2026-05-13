package source

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	secretsinternalclient "github.com/verself/secrets-service/internalclient"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

var secretsCredentialTracer = otel.Tracer("source-code-hosting-service/secrets")

type SecretsCredentialClient struct {
	Client *secretsinternalclient.Client
}

func NewSecretsCredentialClient(baseURL string, httpClient secretsinternalclient.HTTPRequestDoer) (SecretsCredentialClient, error) {
	client, err := secretsinternalclient.NewClient(strings.TrimRight(baseURL, "/"), secretsinternalclient.WithHTTPClient(httpClient))
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
	scopes := credentialScopes(input.Scopes)
	expiresAt := time.Now().UTC().Add(time.Duration(input.ExpiresInSeconds) * time.Second)
	orgID := strconv.FormatUint(principal.OrgID, 10)
	metadata := secretsinternalclient.CredentialMetadata{
		"source": "source-code-hosting-service",
		"label":  input.Label,
	}
	displayName := secretsinternalclient.CredentialDisplayName(input.Label)
	expiresAtText := expiresAt.Format(time.RFC3339Nano)
	resp, err := c.Client.CreateInternalOpaqueCredential(ctx, secretsinternalclient.CreateInternalOpaqueCredentialRequest{Body: secretsinternalclient.InternalOpaqueCredentialInputBody{
		ActorID:     secretsinternalclient.SubjectId(principal.Subject),
		DisplayName: &displayName,
		ExpiresAt:   &expiresAtText,
		Kind:        secretsinternalclient.CredentialKind(GitCredentialKind),
		Metadata:    &metadata,
		OrgID:       secretsinternalclient.OrgId(orgID),
		Scopes:      scopes,
	}})
	if err != nil {
		return GitCredential{}, fmt.Errorf("%w: create secrets credential: %v", ErrStoreUnavailable, err)
	}
	if resp.Result == nil {
		return GitCredential{}, unexpectedSecretsStatus("create credential", resp.HTTPResponse, resp.Body)
	}
	credential, err := gitCredentialFromSecrets(principal, input.Label, resp.Result.Credential, resp.Result.Token)
	if err != nil {
		return GitCredential{}, err
	}
	span.SetAttributes(
		attribute.Int64("verself.org_id", int64FromUint64(principal.OrgID, "org id")),
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
	body := secretsinternalclient.VerifyInternalOpaqueCredentialInputBody{
		Kind:  secretsinternalclient.CredentialKind(GitCredentialKind),
		OrgID: secretsinternalclient.OrgId(orgText),
		Token: secretsinternalclient.SecretValue(strings.TrimSpace(token)),
	}
	if actorID != "" {
		converted := secretsinternalclient.SubjectId(actorID)
		body.ActorID = &converted
	}
	if len(requiredScopes) > 0 {
		scopes := credentialScopes(requiredScopes)
		body.RequiredScopes = &scopes
	}
	resp, err := c.Client.VerifyInternalOpaqueCredential(ctx, secretsinternalclient.VerifyInternalOpaqueCredentialRequest{Body: body})
	if err != nil {
		return GitCredential{}, false, fmt.Errorf("%w: verify secrets credential: %v", ErrStoreUnavailable, err)
	}
	if resp.Result == nil {
		return GitCredential{}, false, unexpectedSecretsStatus("verify credential", resp.HTTPResponse, resp.Body)
	}
	if !resp.Result.Active || resp.Result.Credential == nil {
		span.SetAttributes(attribute.Bool("secrets.credential_active", false))
		return GitCredential{}, false, nil
	}
	credential, err := gitCredentialFromSecrets(Principal{OrgID: orgID}, "", *resp.Result.Credential, secretsinternalclient.SecretValue(""))
	if err != nil {
		return GitCredential{}, false, err
	}
	span.SetAttributes(
		attribute.Int64("verself.org_id", int64FromUint64(orgID, "org id")),
		attribute.String("source.git_credential_id", credential.CredentialID.String()),
		attribute.Bool("secrets.credential_active", true),
	)
	return credential, true, nil
}

func gitCredentialFromSecrets(principal Principal, fallbackLabel string, wire secretsinternalclient.OpaqueCredentialDTO, token secretsinternalclient.SecretValue) (GitCredential, error) {
	credentialID, err := parseWireUUID(string(wire.CredentialID), "credential_id")
	if err != nil {
		return GitCredential{}, err
	}
	orgID := principal.OrgID
	if strings.TrimSpace(string(wire.OrgID)) != "" {
		parsed, err := strconv.ParseUint(strings.TrimSpace(string(wire.OrgID)), 10, 64)
		if err != nil || parsed == 0 {
			return GitCredential{}, ErrStoreUnavailable
		}
		orgID = parsed
	}
	if orgID == 0 {
		return GitCredential{}, ErrStoreUnavailable
	}
	metadata := map[string]string(wire.Metadata)
	if metadata == nil {
		metadata = map[string]string{}
	}
	label := firstNonEmpty(metadata["label"], strings.TrimSpace(wire.DisplayName), fallbackLabel, "git push")
	expiresAt, err := parseWireTime(wire.ExpiresAt, "expires_at")
	if err != nil {
		return GitCredential{}, err
	}
	createdAt, err := parseWireTime(wire.CreatedAt, "created_at")
	if err != nil {
		return GitCredential{}, err
	}
	if len(wire.Scopes) == 0 {
		return GitCredential{}, ErrStoreUnavailable
	}
	scopes, err := NormalizeGitCredentialScopes(stringsFromCredentialScopes(wire.Scopes))
	if err != nil {
		return GitCredential{}, err
	}
	return GitCredential{
		CredentialID: credentialID,
		OrgID:        orgID,
		ActorID:      firstNonEmpty(strings.TrimSpace(wire.Subject), principal.Subject),
		Label:        label,
		Username:     GitCredentialUsername,
		Token:        string(token),
		TokenPrefix:  strings.TrimSpace(wire.TokenPrefix),
		Scopes:       scopes,
		State:        firstNonEmpty(string(wire.Status), "active"),
		ExpiresAt:    expiresAt,
		CreatedAt:    createdAt,
	}, nil
}

func credentialScopes(values []string) secretsinternalclient.CredentialScopes {
	out := make(secretsinternalclient.CredentialScopes, 0, len(values))
	for _, value := range values {
		out = append(out, secretsinternalclient.CredentialScope(value))
	}
	return out
}

func stringsFromCredentialScopes(values secretsinternalclient.CredentialScopes) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, string(value))
	}
	return out
}

func parseWireUUID(value string, field string) (uuid.UUID, error) {
	if strings.TrimSpace(value) == "" {
		return uuid.Nil, fmt.Errorf("%w: secrets credential missing %s", ErrStoreUnavailable, field)
	}
	parsed, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: secrets credential invalid %s", ErrStoreUnavailable, field)
	}
	return parsed, nil
}

func parseWireTime(value string, field string) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return time.Time{}, fmt.Errorf("%w: secrets credential missing %s", ErrStoreUnavailable, field)
	}
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		parsed, err = time.Parse(time.RFC3339, strings.TrimSpace(value))
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
