package secrets

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	billingclient "github.com/forge-metal/billing-service/client"
)

const (
	KindSecret   = "secret"
	KindVariable = "variable"

	ScopeOrg         = "org"
	ScopeSource      = "source"
	ScopeEnvironment = "environment"
	ScopeBranch      = "branch"
)

var (
	ErrInvalidArgument = errors.New("invalid argument")
	ErrForbidden       = errors.New("forbidden")
	ErrNotFound        = errors.New("not found")
	ErrConflict        = errors.New("conflict")
	ErrStore           = errors.New("store unavailable")
	ErrCrypto          = errors.New("crypto unavailable")
)

var tracer = otel.Tracer("secrets-service/internal/secrets")

type Service struct {
	Store          *BaoStore
	Billing        *billingclient.ServiceClient
	Logger         *slog.Logger
	ServiceVersion string
	Environment    string
}

type Principal struct {
	OrgID                  string
	Subject                string
	Email                  string
	Type                   string
	CredentialID           string
	CredentialName         string
	CredentialFingerprint  string
	ActorOwnerID           string
	ActorOwnerDisplay      string
	AuthMethod             string
	OpenBaoRole            string
	UseServiceAccountToken bool
	JWTID                  string
	TokenExpiresAt         time.Time
}

type Scope struct {
	Level    string
	SourceID string
	EnvID    string
	Branch   string
}

type SecretRecord struct {
	SecretID       string
	OrgID          string
	Kind           string
	Name           string
	Scope          Scope
	CurrentVersion uint64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type SecretValue struct {
	Record SecretRecord
	Value  string
}

type PutSecretRequest struct {
	Kind  string
	Name  string
	Scope Scope
	Value string
}

type TransitKey struct {
	KeyID          string
	OrgID          string
	Name           string
	CurrentVersion uint64
	CreatedAt      time.Time
	UpdatedAt      time.Time
	PublicKey      string
}

type TransitCiphertext struct {
	KeyName    string
	Version    uint64
	Ciphertext string
}

func (s *Service) Validate() error {
	if s == nil {
		return fmt.Errorf("%w: service is nil", ErrStore)
	}
	if s.Store == nil {
		return fmt.Errorf("%w: openbao store is nil", ErrStore)
	}
	if s.Logger == nil {
		s.Logger = slog.Default()
	}
	return nil
}

func (s *Service) Ready(ctx context.Context) error {
	ctx, span := tracer.Start(ctx, "secrets.ready")
	defer span.End()
	if err := s.Validate(); err != nil {
		return err
	}
	return s.Store.Ready(ctx)
}

func (s *Service) PutSecret(ctx context.Context, principal Principal, req PutSecretRequest) (SecretRecord, error) {
	ctx, span := tracer.Start(ctx, "secrets.secret.put")
	defer span.End()
	if err := s.Validate(); err != nil {
		return SecretRecord{}, err
	}
	record, err := s.Store.PutSecret(ctx, principal, req)
	if err != nil {
		return SecretRecord{}, err
	}
	span.SetAttributes(
		attribute.String("forge_metal.org_id", principal.OrgID),
		attribute.String("forge_metal.secret_kind", record.Kind),
		attribute.String("forge_metal.secret_id", record.SecretID),
		attribute.Int64("forge_metal.secret_version", int64(record.CurrentVersion)),
	)
	return record, nil
}

func (s *Service) ReadSecret(ctx context.Context, principal Principal, kind, name string, scope Scope) (SecretValue, error) {
	ctx, span := tracer.Start(ctx, "secrets.secret.read")
	defer span.End()
	if err := s.Validate(); err != nil {
		return SecretValue{}, err
	}
	value, err := s.Store.ReadSecret(ctx, principal, kind, name, scope)
	if err != nil {
		return SecretValue{}, err
	}
	span.SetAttributes(
		attribute.String("forge_metal.org_id", principal.OrgID),
		attribute.String("forge_metal.secret_kind", value.Record.Kind),
		attribute.String("forge_metal.secret_id", value.Record.SecretID),
		attribute.Int64("forge_metal.secret_version", int64(value.Record.CurrentVersion)),
	)
	return value, nil
}

func (s *Service) GetSecretMetadata(ctx context.Context, principal Principal, kind, name string, scope Scope) (SecretRecord, error) {
	if err := s.Validate(); err != nil {
		return SecretRecord{}, err
	}
	return s.Store.GetSecretMetadata(ctx, principal, kind, name, scope)
}

func (s *Service) ListSecrets(ctx context.Context, principal Principal, kind string, limit int) ([]SecretRecord, error) {
	ctx, span := tracer.Start(ctx, "secrets.secret.list")
	defer span.End()
	if err := s.Validate(); err != nil {
		return nil, err
	}
	records, err := s.Store.ListSecrets(ctx, principal, kind, limit)
	if err != nil {
		return nil, err
	}
	span.SetAttributes(attribute.String("forge_metal.org_id", principal.OrgID), attribute.Int("forge_metal.secret_count", len(records)))
	return records, nil
}

func (s *Service) DeleteSecret(ctx context.Context, principal Principal, kind, name string, scope Scope) (SecretRecord, error) {
	ctx, span := tracer.Start(ctx, "secrets.secret.delete")
	defer span.End()
	if err := s.Validate(); err != nil {
		return SecretRecord{}, err
	}
	record, err := s.Store.DeleteSecret(ctx, principal, kind, name, scope)
	if err != nil {
		return SecretRecord{}, err
	}
	span.SetAttributes(attribute.String("forge_metal.secret_id", record.SecretID))
	return record, nil
}

func (s *Service) CreateTransitKey(ctx context.Context, principal Principal, name string) (TransitKey, error) {
	ctx, span := tracer.Start(ctx, "secrets.transit.key.create")
	defer span.End()
	if err := s.Validate(); err != nil {
		return TransitKey{}, err
	}
	key, err := s.Store.CreateTransitKey(ctx, principal, name)
	if err != nil {
		return TransitKey{}, err
	}
	span.SetAttributes(attribute.String("forge_metal.key_id", key.KeyID), attribute.String("forge_metal.org_id", principal.OrgID))
	return key, nil
}

func (s *Service) GetTransitKey(ctx context.Context, principal Principal, name string) (TransitKey, error) {
	if err := s.Validate(); err != nil {
		return TransitKey{}, err
	}
	return s.Store.GetTransitKey(ctx, principal, name)
}

func (s *Service) RotateTransitKey(ctx context.Context, principal Principal, name string) (TransitKey, error) {
	ctx, span := tracer.Start(ctx, "secrets.transit.key.rotate")
	defer span.End()
	if err := s.Validate(); err != nil {
		return TransitKey{}, err
	}
	key, err := s.Store.RotateTransitKey(ctx, principal, name)
	if err != nil {
		return TransitKey{}, err
	}
	span.SetAttributes(attribute.String("forge_metal.key_id", key.KeyID), attribute.Int64("forge_metal.key_version", int64(key.CurrentVersion)))
	return key, nil
}

func (s *Service) TransitEncrypt(ctx context.Context, principal Principal, name string, plaintext []byte) (TransitCiphertext, error) {
	ctx, span := tracer.Start(ctx, "secrets.transit.encrypt")
	defer span.End()
	if err := s.Validate(); err != nil {
		return TransitCiphertext{}, err
	}
	ciphertext, key, err := s.Store.TransitEncrypt(ctx, principal, name, plaintext)
	if err != nil {
		return TransitCiphertext{}, err
	}
	span.SetAttributes(attribute.String("forge_metal.key_id", key.KeyID), attribute.Int64("forge_metal.key_version", int64(ciphertext.Version)))
	return ciphertext, nil
}

func (s *Service) TransitDecrypt(ctx context.Context, principal Principal, name, encoded string) ([]byte, TransitKey, error) {
	ctx, span := tracer.Start(ctx, "secrets.transit.decrypt")
	defer span.End()
	if err := s.Validate(); err != nil {
		return nil, TransitKey{}, err
	}
	plaintext, key, version, err := s.Store.TransitDecrypt(ctx, principal, name, encoded)
	if err != nil {
		return nil, TransitKey{}, err
	}
	span.SetAttributes(attribute.String("forge_metal.key_id", key.KeyID), attribute.Int64("forge_metal.key_version", int64(version)))
	return plaintext, key, nil
}

func (s *Service) TransitSign(ctx context.Context, principal Principal, name string, message []byte) (string, TransitKey, error) {
	ctx, span := tracer.Start(ctx, "secrets.transit.sign")
	defer span.End()
	if err := s.Validate(); err != nil {
		return "", TransitKey{}, err
	}
	signature, key, version, err := s.Store.TransitSign(ctx, principal, name, message)
	if err != nil {
		return "", TransitKey{}, err
	}
	span.SetAttributes(attribute.String("forge_metal.key_id", key.KeyID), attribute.Int64("forge_metal.key_version", int64(version)))
	return signature, key, nil
}

func (s *Service) TransitVerify(ctx context.Context, principal Principal, name string, message []byte, signature string) (bool, TransitKey, error) {
	ctx, span := tracer.Start(ctx, "secrets.transit.verify")
	defer span.End()
	if err := s.Validate(); err != nil {
		return false, TransitKey{}, err
	}
	valid, key, err := s.Store.TransitVerify(ctx, principal, name, message, signature)
	if err != nil {
		return false, TransitKey{}, err
	}
	span.SetAttributes(attribute.String("forge_metal.key_id", key.KeyID), attribute.Bool("forge_metal.signature_valid", valid))
	return valid, key, nil
}

func validatePrincipal(principal Principal) error {
	if strings.TrimSpace(principal.OrgID) == "" || strings.TrimSpace(principal.Subject) == "" {
		return fmt.Errorf("%w: principal org and subject are required", ErrInvalidArgument)
	}
	return nil
}

func validateSecretInput(req PutSecretRequest) error {
	if req.Kind != KindSecret && req.Kind != KindVariable {
		return fmt.Errorf("%w: kind must be secret or variable", ErrInvalidArgument)
	}
	if req.Name == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidArgument)
	}
	if err := validateResourceName(req.Name); err != nil {
		return err
	}
	if len(req.Value) > 64<<10 {
		return fmt.Errorf("%w: secret value exceeds 64KiB", ErrInvalidArgument)
	}
	return validateScope(req.Scope)
}

func validateScope(scope Scope) error {
	switch scope.Level {
	case ScopeOrg:
		if scope.SourceID != "" || scope.EnvID != "" || scope.Branch != "" {
			return fmt.Errorf("%w: org scope cannot include source, env, or branch", ErrInvalidArgument)
		}
	case ScopeSource:
		if scope.SourceID == "" || scope.EnvID != "" || scope.Branch != "" {
			return fmt.Errorf("%w: source scope requires source only", ErrInvalidArgument)
		}
	case ScopeEnvironment:
		if scope.SourceID == "" || scope.EnvID == "" || scope.Branch != "" {
			return fmt.Errorf("%w: environment scope requires source and env only", ErrInvalidArgument)
		}
	case ScopeBranch:
		if scope.SourceID == "" || scope.EnvID == "" || scope.Branch == "" {
			return fmt.Errorf("%w: branch scope requires source, env, and branch", ErrInvalidArgument)
		}
	default:
		return fmt.Errorf("%w: unsupported scope level", ErrInvalidArgument)
	}
	return nil
}

func normalizeKind(kind string) string {
	kind = strings.TrimSpace(strings.ToLower(kind))
	if kind == "" {
		return KindSecret
	}
	return kind
}

func normalizeName(name string) string {
	return strings.TrimSpace(name)
}

func normalizeScope(scope Scope) Scope {
	scope.Level = strings.TrimSpace(strings.ToLower(scope.Level))
	if scope.Level == "" {
		scope.Level = ScopeOrg
	}
	scope.SourceID = strings.TrimSpace(scope.SourceID)
	scope.EnvID = strings.TrimSpace(scope.EnvID)
	scope.Branch = strings.TrimSpace(scope.Branch)
	return scope
}

func resolutionCandidates(scope Scope) ([]Scope, error) {
	scope = normalizeScope(scope)
	if err := validateScope(scope); err != nil {
		return nil, err
	}
	candidates := make([]Scope, 0, 4)
	if scope.Level == ScopeBranch {
		candidates = append(candidates, scope)
	}
	if scope.Level == ScopeBranch || scope.Level == ScopeEnvironment {
		candidates = append(candidates, Scope{Level: ScopeEnvironment, SourceID: scope.SourceID, EnvID: scope.EnvID})
	}
	if scope.Level == ScopeBranch || scope.Level == ScopeEnvironment || scope.Level == ScopeSource {
		candidates = append(candidates, Scope{Level: ScopeSource, SourceID: scope.SourceID})
	}
	candidates = append(candidates, Scope{Level: ScopeOrg})
	return candidates, nil
}

func hashBranch(branch string) string {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return ""
	}
	return sha256Hex([]byte(branch))
}

func SecretPathHash(orgID, kind, name string, scope Scope) string {
	scope = normalizeScope(scope)
	return sha256Hex([]byte(strings.Join([]string{
		strings.TrimSpace(orgID),
		normalizeKind(kind),
		normalizeName(name),
		scope.Level,
		scope.SourceID,
		scope.EnvID,
		hashBranch(scope.Branch),
	}, "\x00")))
}

func sha256Hex(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
