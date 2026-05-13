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
	client *secretscore.Client
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
	body := secretscore.SecretMutationInputBody{
		Value: secretscore.SecretValue(input.Value),
	}
	applyPutSecretScope(&body, input.Scope)
	response, err := c.client.PutSecret(ctx, secretscore.PutSecretRequest{
		IdempotencyKey: key,
		Name:           secretscore.SecretName(name),
		Body:           body,
	})
	if err != nil {
		return Secret{}, err
	}
	if response.Result == nil {
		return Secret{}, secretsAPIError("put secret", response.StatusCode, response.Problem, response.Body)
	}
	return secretFromGenerated(*response.Result), nil
}

func (c *SecretsClient) Resolve(ctx context.Context, input ResolveSecretsInput) (ResolvedSecrets, error) {
	if c == nil || c.client == nil {
		return ResolvedSecrets{}, fmt.Errorf("verself sdk: secrets client is not initialized")
	}
	body := secretscore.ResolveSecretsInputBody{}
	applyResolveSecretsScope(&body, input.Scope)
	names := compactStrings(input.Names)
	if len(names) > 0 {
		converted := make(secretscore.SecretNames, 0, len(names))
		for _, name := range names {
			converted = append(converted, secretscore.SecretName(name))
		}
		body.Names = &converted
	}
	if input.Limit > 0 {
		limit := secretscore.SecretListLimit(input.Limit)
		body.Limit = &limit
	}
	response, err := c.client.ResolveSecrets(ctx, secretscore.ResolveSecretsRequest{Body: body})
	if err != nil {
		return ResolvedSecrets{}, err
	}
	if response.Result == nil {
		return ResolvedSecrets{}, secretsAPIError("resolve secrets", response.StatusCode, response.Problem, response.Body)
	}
	return resolvedSecretsFromGenerated(*response.Result), nil
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
	request := secretscore.DeleteSecretRequest{IdempotencyKey: key, Name: secretscore.SecretName(name)}
	applyDeleteSecretScope(&request, input.Scope)
	response, err := c.client.DeleteSecret(ctx, request)
	if err != nil {
		return Secret{}, err
	}
	if response.Result == nil {
		return Secret{}, secretsAPIError("delete secret", response.StatusCode, response.Problem, response.Body)
	}
	return secretFromGenerated(*response.Result), nil
}

func (c *SecretsClient) List(ctx context.Context, options ListSecretsOptions) (SecretList, error) {
	if c == nil || c.client == nil {
		return SecretList{}, fmt.Errorf("verself sdk: secrets client is not initialized")
	}
	request := secretscore.ListSecretsRequest{}
	if options.Limit > 0 {
		limit := secretscore.SecretListLimit(options.Limit)
		request.Limit = &limit
	}
	response, err := c.client.ListSecrets(ctx, request)
	if err != nil {
		return SecretList{}, err
	}
	if response.Result == nil {
		return SecretList{}, secretsAPIError("list secrets", response.StatusCode, response.Problem, response.Body)
	}
	out := SecretList{Secrets: make([]Secret, 0, len(response.Result.Secrets))}
	for _, secret := range response.Result.Secrets {
		out.Secrets = append(out.Secrets, secretFromGenerated(secret))
	}
	return out, nil
}

func applyPutSecretScope(body *secretscore.SecretMutationInputBody, scope SecretScope) {
	level := strings.TrimSpace(string(scope.Level))
	if level != "" {
		value := secretscore.ScopeLevel(level)
		body.ScopeLevel = &value
	}
	body.SourceID = trimAliasPointer[secretscore.SourceId](scope.SourceID)
	body.EnvID = trimAliasPointer[secretscore.EnvId](scope.EnvID)
	body.Branch = trimAliasPointer[secretscore.Branch](scope.Branch)
}

func applyResolveSecretsScope(body *secretscore.ResolveSecretsInputBody, scope SecretScope) {
	level := strings.TrimSpace(string(scope.Level))
	if level != "" {
		value := secretscore.ScopeLevel(level)
		body.ScopeLevel = &value
	}
	body.SourceID = trimAliasPointer[secretscore.SourceId](scope.SourceID)
	body.EnvID = trimAliasPointer[secretscore.EnvId](scope.EnvID)
	body.Branch = trimAliasPointer[secretscore.Branch](scope.Branch)
}

func applyDeleteSecretScope(request *secretscore.DeleteSecretRequest, scope SecretScope) {
	level := strings.TrimSpace(string(scope.Level))
	if level != "" {
		value := secretscore.ScopeLevel(level)
		request.ScopeLevel = &value
	}
	request.SourceID = trimAliasPointer[secretscore.SourceId](scope.SourceID)
	request.EnvID = trimAliasPointer[secretscore.EnvId](scope.EnvID)
	request.Branch = trimAliasPointer[secretscore.Branch](scope.Branch)
}

func trimAliasPointer[T ~string](value string) *T {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	converted := T(trimmed)
	return &converted
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
		SecretID:     input.SecretID,
		ResourceName: stringFromAliasPointer(input.ResourceName),
		Kind:         input.Kind,
		Name:         string(input.Name),
		Scope: SecretScope{
			Level:    SecretScopeLevel(input.ScopeLevel),
			SourceID: stringFromAliasPointer(input.SourceID),
			EnvID:    stringFromAliasPointer(input.EnvID),
			Branch:   stringFromAliasPointer(input.Branch),
		},
		CurrentVersion: string(input.CurrentVersion),
		CreatedAt:      parseSDKTime(input.CreatedAt),
		UpdatedAt:      parseSDKTime(input.UpdatedAt),
	}
}

func secretValueFromGenerated(input secretscore.SecretValueDTO) SecretValue {
	return SecretValue{
		Secret: Secret{
			SecretID:     input.SecretID,
			ResourceName: stringFromAliasPointer(input.ResourceName),
			Kind:         input.Kind,
			Name:         string(input.Name),
			Scope: SecretScope{
				Level:    SecretScopeLevel(input.ScopeLevel),
				SourceID: stringFromAliasPointer(input.SourceID),
				EnvID:    stringFromAliasPointer(input.EnvID),
				Branch:   stringFromAliasPointer(input.Branch),
			},
			CurrentVersion: string(input.CurrentVersion),
			CreatedAt:      parseSDKTime(input.CreatedAt),
			UpdatedAt:      parseSDKTime(input.UpdatedAt),
		},
		Value: string(input.Value),
	}
}

func resolvedSecretsFromGenerated(input secretscore.ResolvedSecretsDTO) ResolvedSecrets {
	out := ResolvedSecrets{Values: make([]SecretValue, 0, len(input.Values))}
	for _, value := range input.Values {
		out.Values = append(out.Values, secretValueFromGenerated(value))
	}
	return out
}

func stringFromAliasPointer[T ~string](value *T) string {
	if value == nil {
		return ""
	}
	return string(*value)
}

func stringFromPointer(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func parseSDKTime(value string) time.Time {
	if strings.TrimSpace(value) == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		parsed, err = time.Parse(time.RFC3339, strings.TrimSpace(value))
	}
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
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
