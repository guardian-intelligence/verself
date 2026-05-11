package verself

import (
	"context"
	"fmt"
	"strings"
	"time"

	secretscore "github.com/verself/verself-go/internal/generated/secrets"
)

type SecretScopeLevel string

const (
	SecretScopeOrg         SecretScopeLevel = "org"
	SecretScopeSource      SecretScopeLevel = "source"
	SecretScopeEnvironment SecretScopeLevel = "environment"
	SecretScopeBranch      SecretScopeLevel = "branch"
)

type SecretScope struct {
	Level    SecretScopeLevel `json:"level"`
	SourceID string           `json:"source_id,omitempty"`
	EnvID    string           `json:"env_id,omitempty"`
	Branch   string           `json:"branch,omitempty"`
}

type Secret struct {
	SecretID       string      `json:"secret_id"`
	ResourceName   string      `json:"resourceName,omitempty"`
	Kind           string      `json:"kind"`
	Name           string      `json:"name"`
	Scope          SecretScope `json:"scope"`
	CurrentVersion string      `json:"current_version"`
	CreatedAt      time.Time   `json:"created_at"`
	UpdatedAt      time.Time   `json:"updated_at"`
}

type SecretValue struct {
	Secret
	Value string `json:"value"`
}

type SecretList struct {
	Secrets []Secret `json:"secrets"`
}

type ResolvedSecrets struct {
	Values []SecretValue `json:"values"`
}

type ListSecretsOptions struct {
	Limit int
}

type PutSecretInput struct {
	Scope          SecretScope
	Value          string
	IdempotencyKey string
}

type ResolveSecretsInput struct {
	Scope SecretScope
	Names []string
	Limit int
}

type DeleteSecretInput struct {
	Scope          SecretScope
	IdempotencyKey string
}

type SecretsClient struct {
	client *secretscore.ClientWithResponses
}

func (c *SecretsClient) Put(ctx context.Context, name string, input PutSecretInput) (Secret, error) {
	if c == nil || c.client == nil {
		return Secret{}, fmt.Errorf("verself sdk: secrets client is not initialized")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return Secret{}, fmt.Errorf("verself sdk: secret name is required")
	}
	key, err := mutationKey("secret", input.IdempotencyKey)
	if err != nil {
		return Secret{}, err
	}
	body := secretscore.PutSecretJSONRequestBody{
		Value: input.Value,
	}
	applyPutSecretScope(&body, input.Scope)
	response, err := c.client.PutSecretWithResponse(ctx, name, &secretscore.PutSecretParams{
		IdempotencyKey: key,
	}, body)
	if err != nil {
		return Secret{}, err
	}
	if response.JSON200 == nil {
		return Secret{}, secretsAPIError("put secret", response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	return secretFromGenerated(*response.JSON200), nil
}

func (c *SecretsClient) Resolve(ctx context.Context, input ResolveSecretsInput) (ResolvedSecrets, error) {
	if c == nil || c.client == nil {
		return ResolvedSecrets{}, fmt.Errorf("verself sdk: secrets client is not initialized")
	}
	body := secretscore.ResolveSecretsJSONRequestBody{}
	applyResolveSecretsScope(&body, input.Scope)
	names := compactStrings(input.Names)
	if len(names) > 0 {
		body.Names = &names
	}
	if input.Limit > 0 {
		limit := int64(input.Limit)
		body.Limit = &limit
	}
	response, err := c.client.ResolveSecretsWithResponse(ctx, body)
	if err != nil {
		return ResolvedSecrets{}, err
	}
	if response.JSON200 == nil {
		return ResolvedSecrets{}, secretsAPIError("resolve secrets", response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	return resolvedSecretsFromGenerated(*response.JSON200), nil
}

func (c *SecretsClient) Delete(ctx context.Context, name string, input DeleteSecretInput) (Secret, error) {
	if c == nil || c.client == nil {
		return Secret{}, fmt.Errorf("verself sdk: secrets client is not initialized")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return Secret{}, fmt.Errorf("verself sdk: secret name is required")
	}
	key, err := mutationKey("secret", input.IdempotencyKey)
	if err != nil {
		return Secret{}, err
	}
	params := &secretscore.DeleteSecretParams{IdempotencyKey: key}
	applyDeleteSecretScope(params, input.Scope)
	response, err := c.client.DeleteSecretWithResponse(ctx, name, params, secretscore.DeleteSecretJSONRequestBody{})
	if err != nil {
		return Secret{}, err
	}
	if response.JSON200 == nil {
		return Secret{}, secretsAPIError("delete secret", response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	return secretFromGenerated(*response.JSON200), nil
}

func (c *SecretsClient) List(ctx context.Context, options ListSecretsOptions) (SecretList, error) {
	if c == nil || c.client == nil {
		return SecretList{}, fmt.Errorf("verself sdk: secrets client is not initialized")
	}
	params := &secretscore.ListSecretsParams{}
	if options.Limit > 0 {
		limit := int64(options.Limit)
		params.Limit = &limit
	}
	response, err := c.client.ListSecretsWithResponse(ctx, params)
	if err != nil {
		return SecretList{}, err
	}
	if response.JSON200 == nil {
		return SecretList{}, secretsAPIError("list secrets", response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	out := SecretList{Secrets: make([]Secret, 0, len(response.JSON200.Secrets))}
	for _, secret := range response.JSON200.Secrets {
		out.Secrets = append(out.Secrets, secretFromGenerated(secret))
	}
	return out, nil
}

func applyPutSecretScope(body *secretscore.PutSecretBody, scope SecretScope) {
	level := strings.TrimSpace(string(scope.Level))
	if level != "" {
		value := secretscore.PutSecretBodyScopeLevel(level)
		body.ScopeLevel = &value
	}
	body.SourceId = trimPointer(scope.SourceID)
	body.EnvId = trimPointer(scope.EnvID)
	body.Branch = trimPointer(scope.Branch)
}

func applyResolveSecretsScope(body *secretscore.ResolveSecretsBody, scope SecretScope) {
	level := strings.TrimSpace(string(scope.Level))
	if level != "" {
		value := secretscore.ResolveSecretsBodyScopeLevel(level)
		body.ScopeLevel = &value
	}
	body.SourceId = trimPointer(scope.SourceID)
	body.EnvId = trimPointer(scope.EnvID)
	body.Branch = trimPointer(scope.Branch)
}

func applyDeleteSecretScope(params *secretscore.DeleteSecretParams, scope SecretScope) {
	level := strings.TrimSpace(string(scope.Level))
	if level != "" {
		value := secretscore.DeleteSecretParamsScopeLevel(level)
		params.ScopeLevel = &value
	}
	params.SourceId = trimPointer(scope.SourceID)
	params.EnvId = trimPointer(scope.EnvID)
	params.Branch = trimPointer(scope.Branch)
}

func trimPointer(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func secretFromGenerated(input secretscore.SecretDTO) Secret {
	return Secret{
		SecretID:     input.SecretId,
		ResourceName: stringFromPointer(input.ResourceName),
		Kind:         input.Kind,
		Name:         input.Name,
		Scope: SecretScope{
			Level:    SecretScopeLevel(input.ScopeLevel),
			SourceID: stringFromPointer(input.SourceId),
			EnvID:    stringFromPointer(input.EnvId),
			Branch:   stringFromPointer(input.Branch),
		},
		CurrentVersion: input.CurrentVersion,
		CreatedAt:      input.CreatedAt,
		UpdatedAt:      input.UpdatedAt,
	}
}

func secretValueFromGenerated(input secretscore.SecretValueDTO) SecretValue {
	return SecretValue{
		Secret: Secret{
			SecretID:     input.SecretId,
			ResourceName: stringFromPointer(input.ResourceName),
			Kind:         input.Kind,
			Name:         input.Name,
			Scope: SecretScope{
				Level:    SecretScopeLevel(input.ScopeLevel),
				SourceID: stringFromPointer(input.SourceId),
				EnvID:    stringFromPointer(input.EnvId),
				Branch:   stringFromPointer(input.Branch),
			},
			CurrentVersion: input.CurrentVersion,
			CreatedAt:      input.CreatedAt,
			UpdatedAt:      input.UpdatedAt,
		},
		Value: input.Value,
	}
}

func resolvedSecretsFromGenerated(input secretscore.ResolvedSecretsDTO) ResolvedSecrets {
	out := ResolvedSecrets{Values: make([]SecretValue, 0, len(input.Values))}
	for _, value := range input.Values {
		out.Values = append(out.Values, secretValueFromGenerated(value))
	}
	return out
}

func stringFromPointer(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func secretsAPIError(operation string, statusCode int, model *secretscore.ErrorModel, body []byte) error {
	var title *string
	var detail *string
	if model != nil {
		title = model.Title
		detail = model.Detail
	}
	return apiErrorFields("Secrets API", operation, statusCode, title, detail, body)
}
