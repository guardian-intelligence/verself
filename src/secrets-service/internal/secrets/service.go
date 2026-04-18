package secrets

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
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
	PG             *pgxpool.Pool
	Codec          *EnvelopeCodec
	Logger         *slog.Logger
	ServiceVersion string
	Environment    string
}

type Principal struct {
	OrgID                 string
	Subject               string
	Email                 string
	Type                  string
	CredentialID          string
	CredentialName        string
	CredentialFingerprint string
	ActorOwnerID          string
	ActorOwnerDisplay     string
	AuthMethod            string
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
}

type TransitCiphertext struct {
	KeyName    string
	Version    uint64
	Ciphertext string
}

func NewEnvelopeCodec(key []byte) (*EnvelopeCodec, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("%w: envelope key must be 32 bytes", ErrCrypto)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCrypto, err)
	}
	aead, err := cipher.NewGCMWithRandomNonce(block)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCrypto, err)
	}
	return &EnvelopeCodec{aead: aead}, nil
}

type EnvelopeCodec struct {
	aead cipher.AEAD
}

func (c *EnvelopeCodec) Seal(plaintext []byte, aad string) ([]byte, []byte, error) {
	if c == nil || c.aead == nil {
		return nil, nil, fmt.Errorf("%w: envelope codec is nil", ErrCrypto)
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, fmt.Errorf("%w: random nonce: %v", ErrCrypto, err)
	}
	return c.aead.Seal(nil, nonce, plaintext, []byte(aad)), nonce, nil
}

func (c *EnvelopeCodec) Open(ciphertext, nonce []byte, aad string) ([]byte, error) {
	if c == nil || c.aead == nil {
		return nil, fmt.Errorf("%w: envelope codec is nil", ErrCrypto)
	}
	plaintext, err := c.aead.Open(nil, nonce, ciphertext, []byte(aad))
	if err != nil {
		return nil, fmt.Errorf("%w: decrypt failed", ErrCrypto)
	}
	return plaintext, nil
}

func DecodeEnvelopeKey(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, fmt.Errorf("%w: empty envelope key", ErrCrypto)
	}
	if decoded, err := hex.DecodeString(value); err == nil && len(decoded) == 32 {
		return decoded, nil
	}
	if decoded, err := base64.StdEncoding.DecodeString(value); err == nil && len(decoded) == 32 {
		return decoded, nil
	}
	if len(value) == 32 {
		return []byte(value), nil
	}
	return nil, fmt.Errorf("%w: envelope key must decode to 32 bytes", ErrCrypto)
}

func (s *Service) Validate() error {
	if s == nil {
		return fmt.Errorf("%w: service is nil", ErrStore)
	}
	if s.PG == nil {
		return fmt.Errorf("%w: postgres is nil", ErrStore)
	}
	if s.Codec == nil {
		return fmt.Errorf("%w: envelope codec is nil", ErrCrypto)
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
	if err := s.PG.Ping(ctx); err != nil {
		return fmt.Errorf("%w: postgres ping: %v", ErrStore, err)
	}
	return nil
}

func (s *Service) PutSecret(ctx context.Context, principal Principal, req PutSecretRequest) (SecretRecord, error) {
	ctx, span := tracer.Start(ctx, "secrets.secret.put")
	defer span.End()
	if err := s.Validate(); err != nil {
		return SecretRecord{}, err
	}
	req.Kind = normalizeKind(req.Kind)
	req.Name = normalizeName(req.Name)
	req.Scope = normalizeScope(req.Scope)
	if err := validatePrincipal(principal); err != nil {
		return SecretRecord{}, err
	}
	if err := validateSecretInput(req); err != nil {
		return SecretRecord{}, err
	}
	branchHash := hashBranch(req.Scope.Branch)
	valueHash := sha256Hex([]byte(req.Value))
	secretID := uuid.New()
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return SecretRecord{}, fmt.Errorf("%w: begin put secret: %v", ErrStore, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	row := tx.QueryRow(ctx, `
		INSERT INTO secret_resources (
			secret_id, org_id, kind, name, scope_level, source_id, env_id, branch_hash,
			branch_display, current_version, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 1, $10)
		ON CONFLICT (org_id, kind, scope_level, source_id, env_id, branch_hash, name)
		WHERE deleted_at IS NULL
		DO UPDATE SET
			current_version = secret_resources.current_version + 1,
			updated_at = now()
		RETURNING secret_id, current_version
	`, secretID, principal.OrgID, req.Kind, req.Name, req.Scope.Level, req.Scope.SourceID, req.Scope.EnvID, branchHash, req.Scope.Branch, principal.Subject)
	var storedID uuid.UUID
	var version uint64
	if err := row.Scan(&storedID, &version); err != nil {
		return SecretRecord{}, fmt.Errorf("%w: upsert secret resource: %v", ErrStore, err)
	}
	aad := secretAAD(principal.OrgID, storedID.String(), version)
	ciphertext, nonce, err := s.Codec.Seal([]byte(req.Value), aad)
	if err != nil {
		return SecretRecord{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO secret_versions (secret_id, version, ciphertext, nonce, value_hash, created_by)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, storedID, version, ciphertext, nonce, valueHash, principal.Subject); err != nil {
		return SecretRecord{}, fmt.Errorf("%w: insert secret version: %v", ErrStore, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return SecretRecord{}, fmt.Errorf("%w: commit put secret: %v", ErrStore, err)
	}
	record, err := s.GetSecretMetadata(ctx, principal, req.Kind, req.Name, req.Scope)
	if err != nil {
		return SecretRecord{}, err
	}
	span.SetAttributes(
		attribute.String("forge_metal.org_id", principal.OrgID),
		attribute.String("forge_metal.secret_kind", req.Kind),
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
	if err := validatePrincipal(principal); err != nil {
		return SecretValue{}, err
	}
	kind = normalizeKind(kind)
	name = normalizeName(name)
	scope = normalizeScope(scope)
	if kind == "" || name == "" {
		return SecretValue{}, fmt.Errorf("%w: kind and name are required", ErrInvalidArgument)
	}
	candidates, err := resolutionCandidates(scope)
	if err != nil {
		return SecretValue{}, err
	}
	for _, candidate := range candidates {
		value, err := s.readExactSecret(ctx, principal, kind, name, candidate)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return SecretValue{}, err
		}
		span.SetAttributes(
			attribute.String("forge_metal.org_id", principal.OrgID),
			attribute.String("forge_metal.secret_kind", kind),
			attribute.String("forge_metal.secret_id", value.Record.SecretID),
			attribute.Int64("forge_metal.secret_version", int64(value.Record.CurrentVersion)),
		)
		return value, nil
	}
	return SecretValue{}, fmt.Errorf("%w: secret not found", ErrNotFound)
}

func (s *Service) readExactSecret(ctx context.Context, principal Principal, kind, name string, scope Scope) (SecretValue, error) {
	branchHash := hashBranch(scope.Branch)
	row := s.PG.QueryRow(ctx, `
		SELECT r.secret_id, r.org_id, r.kind, r.name, r.scope_level, r.source_id, r.env_id,
		       r.branch_display, r.current_version, r.created_at, r.updated_at,
		       v.ciphertext, v.nonce
		FROM secret_resources r
		JOIN secret_versions v ON v.secret_id = r.secret_id AND v.version = r.current_version
		WHERE r.org_id = $1
		  AND r.kind = $2
		  AND r.name = $3
		  AND r.scope_level = $4
		  AND r.source_id = $5
		  AND r.env_id = $6
		  AND r.branch_hash = $7
		  AND r.deleted_at IS NULL
		  AND v.destroyed_at IS NULL
	`, principal.OrgID, kind, name, scope.Level, scope.SourceID, scope.EnvID, branchHash)
	var record SecretRecord
	var ciphertext []byte
	var nonce []byte
	if err := row.Scan(&record.SecretID, &record.OrgID, &record.Kind, &record.Name, &record.Scope.Level, &record.Scope.SourceID, &record.Scope.EnvID, &record.Scope.Branch, &record.CurrentVersion, &record.CreatedAt, &record.UpdatedAt, &ciphertext, &nonce); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SecretValue{}, fmt.Errorf("%w: secret not found", ErrNotFound)
		}
		return SecretValue{}, fmt.Errorf("%w: read secret: %v", ErrStore, err)
	}
	plaintext, err := s.Codec.Open(ciphertext, nonce, secretAAD(principal.OrgID, record.SecretID, record.CurrentVersion))
	if err != nil {
		return SecretValue{}, err
	}
	return SecretValue{Record: record, Value: string(plaintext)}, nil
}

func (s *Service) GetSecretMetadata(ctx context.Context, principal Principal, kind, name string, scope Scope) (SecretRecord, error) {
	if err := s.Validate(); err != nil {
		return SecretRecord{}, err
	}
	if err := validatePrincipal(principal); err != nil {
		return SecretRecord{}, err
	}
	kind = normalizeKind(kind)
	name = normalizeName(name)
	scope = normalizeScope(scope)
	if err := validateScope(scope); err != nil {
		return SecretRecord{}, err
	}
	row := s.PG.QueryRow(ctx, `
		SELECT secret_id, org_id, kind, name, scope_level, source_id, env_id,
		       branch_display, current_version, created_at, updated_at
		FROM secret_resources
		WHERE org_id = $1
		  AND kind = $2
		  AND name = $3
		  AND scope_level = $4
		  AND source_id = $5
		  AND env_id = $6
		  AND branch_hash = $7
		  AND deleted_at IS NULL
	`, principal.OrgID, kind, name, scope.Level, scope.SourceID, scope.EnvID, hashBranch(scope.Branch))
	record, err := scanSecretRecord(row)
	if err != nil {
		return SecretRecord{}, err
	}
	return record, nil
}

func (s *Service) ListSecrets(ctx context.Context, principal Principal, kind string, limit int) ([]SecretRecord, error) {
	ctx, span := tracer.Start(ctx, "secrets.secret.list")
	defer span.End()
	if err := s.Validate(); err != nil {
		return nil, err
	}
	if err := validatePrincipal(principal); err != nil {
		return nil, err
	}
	kind = normalizeKind(kind)
	if kind == "" {
		kind = KindSecret
	}
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := s.PG.Query(ctx, `
		SELECT secret_id, org_id, kind, name, scope_level, source_id, env_id,
		       branch_display, current_version, created_at, updated_at
		FROM secret_resources
		WHERE org_id = $1 AND kind = $2 AND deleted_at IS NULL
		ORDER BY updated_at DESC, secret_id DESC
		LIMIT $3
	`, principal.OrgID, kind, limit)
	if err != nil {
		return nil, fmt.Errorf("%w: list secrets: %v", ErrStore, err)
	}
	defer rows.Close()
	records := []SecretRecord{}
	for rows.Next() {
		record, err := scanSecretRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: list secret rows: %v", ErrStore, err)
	}
	span.SetAttributes(attribute.String("forge_metal.org_id", principal.OrgID), attribute.Int("forge_metal.secret_count", len(records)))
	return records, nil
}

func (s *Service) DeleteSecret(ctx context.Context, principal Principal, kind, name string, scope Scope) (SecretRecord, error) {
	ctx, span := tracer.Start(ctx, "secrets.secret.delete")
	defer span.End()
	record, err := s.GetSecretMetadata(ctx, principal, kind, name, scope)
	if err != nil {
		return SecretRecord{}, err
	}
	tag, err := s.PG.Exec(ctx, `
		UPDATE secret_resources
		SET deleted_at = now(), updated_at = now()
		WHERE secret_id = $1 AND org_id = $2 AND deleted_at IS NULL
	`, record.SecretID, principal.OrgID)
	if err != nil {
		return SecretRecord{}, fmt.Errorf("%w: delete secret: %v", ErrStore, err)
	}
	if tag.RowsAffected() != 1 {
		return SecretRecord{}, fmt.Errorf("%w: secret not found", ErrNotFound)
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
	if err := validatePrincipal(principal); err != nil {
		return TransitKey{}, err
	}
	name = normalizeName(name)
	if name == "" {
		return TransitKey{}, fmt.Errorf("%w: key name is required", ErrInvalidArgument)
	}
	keyID := uuid.New()
	rawKey := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, rawKey); err != nil {
		return TransitKey{}, fmt.Errorf("%w: random transit key: %v", ErrCrypto, err)
	}
	wrapped, nonce, err := s.Codec.Seal(rawKey, transitAAD(principal.OrgID, keyID.String(), 1))
	if err != nil {
		return TransitKey{}, err
	}
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return TransitKey{}, fmt.Errorf("%w: begin transit key: %v", ErrStore, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
		INSERT INTO transit_keys (key_id, org_id, name, current_version, created_by)
		VALUES ($1, $2, $3, 1, $4)
	`, keyID, principal.OrgID, name, principal.Subject); err != nil {
		return TransitKey{}, fmt.Errorf("%w: insert transit key: %v", ErrStore, err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO transit_key_versions (key_id, version, wrapped_key, nonce, created_by)
		VALUES ($1, 1, $2, $3, $4)
	`, keyID, wrapped, nonce, principal.Subject); err != nil {
		return TransitKey{}, fmt.Errorf("%w: insert transit key version: %v", ErrStore, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return TransitKey{}, fmt.Errorf("%w: commit transit key: %v", ErrStore, err)
	}
	key, err := s.GetTransitKey(ctx, principal, name)
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
	if err := validatePrincipal(principal); err != nil {
		return TransitKey{}, err
	}
	name = normalizeName(name)
	row := s.PG.QueryRow(ctx, `
		SELECT key_id, org_id, name, current_version, created_at, updated_at
		FROM transit_keys
		WHERE org_id = $1 AND name = $2 AND deleted_at IS NULL
	`, principal.OrgID, name)
	var key TransitKey
	if err := row.Scan(&key.KeyID, &key.OrgID, &key.Name, &key.CurrentVersion, &key.CreatedAt, &key.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return TransitKey{}, fmt.Errorf("%w: transit key not found", ErrNotFound)
		}
		return TransitKey{}, fmt.Errorf("%w: get transit key: %v", ErrStore, err)
	}
	return key, nil
}

func (s *Service) RotateTransitKey(ctx context.Context, principal Principal, name string) (TransitKey, error) {
	ctx, span := tracer.Start(ctx, "secrets.transit.key.rotate")
	defer span.End()
	key, err := s.GetTransitKey(ctx, principal, name)
	if err != nil {
		return TransitKey{}, err
	}
	nextVersion := key.CurrentVersion + 1
	rawKey := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, rawKey); err != nil {
		return TransitKey{}, fmt.Errorf("%w: random transit key: %v", ErrCrypto, err)
	}
	wrapped, nonce, err := s.Codec.Seal(rawKey, transitAAD(principal.OrgID, key.KeyID, nextVersion))
	if err != nil {
		return TransitKey{}, err
	}
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return TransitKey{}, fmt.Errorf("%w: begin rotate transit key: %v", ErrStore, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
		INSERT INTO transit_key_versions (key_id, version, wrapped_key, nonce, created_by)
		VALUES ($1, $2, $3, $4, $5)
	`, key.KeyID, nextVersion, wrapped, nonce, principal.Subject); err != nil {
		return TransitKey{}, fmt.Errorf("%w: insert transit key version: %v", ErrStore, err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE transit_keys
		SET current_version = $2, updated_at = now()
		WHERE key_id = $1 AND org_id = $3
	`, key.KeyID, nextVersion, principal.OrgID); err != nil {
		return TransitKey{}, fmt.Errorf("%w: update transit key: %v", ErrStore, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return TransitKey{}, fmt.Errorf("%w: commit rotate transit key: %v", ErrStore, err)
	}
	rotated, err := s.GetTransitKey(ctx, principal, name)
	if err != nil {
		return TransitKey{}, err
	}
	span.SetAttributes(attribute.String("forge_metal.key_id", rotated.KeyID), attribute.Int64("forge_metal.key_version", int64(rotated.CurrentVersion)))
	return rotated, nil
}

func (s *Service) TransitEncrypt(ctx context.Context, principal Principal, name string, plaintext []byte) (TransitCiphertext, error) {
	ctx, span := tracer.Start(ctx, "secrets.transit.encrypt")
	defer span.End()
	key, rawKey, err := s.loadTransitKey(ctx, principal, name, 0)
	if err != nil {
		return TransitCiphertext{}, err
	}
	block, err := aes.NewCipher(rawKey)
	if err != nil {
		return TransitCiphertext{}, fmt.Errorf("%w: transit cipher: %v", ErrCrypto, err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return TransitCiphertext{}, fmt.Errorf("%w: transit gcm: %v", ErrCrypto, err)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return TransitCiphertext{}, fmt.Errorf("%w: random transit nonce: %v", ErrCrypto, err)
	}
	ciphertext := aead.Seal(nil, nonce, plaintext, []byte(transitPayloadAAD(principal.OrgID, key.KeyID, key.CurrentVersion)))
	raw := append(nonce, ciphertext...)
	span.SetAttributes(attribute.String("forge_metal.key_id", key.KeyID), attribute.Int64("forge_metal.key_version", int64(key.CurrentVersion)))
	return TransitCiphertext{
		KeyName:    key.Name,
		Version:    key.CurrentVersion,
		Ciphertext: fmt.Sprintf("fmtransit:v1:%d:%s", key.CurrentVersion, base64.StdEncoding.EncodeToString(raw)),
	}, nil
}

func (s *Service) TransitDecrypt(ctx context.Context, principal Principal, name, encoded string) ([]byte, TransitKey, error) {
	ctx, span := tracer.Start(ctx, "secrets.transit.decrypt")
	defer span.End()
	version, payload, err := parseTransitCiphertext(encoded)
	if err != nil {
		return nil, TransitKey{}, err
	}
	key, rawKey, err := s.loadTransitKey(ctx, principal, name, version)
	if err != nil {
		return nil, TransitKey{}, err
	}
	block, err := aes.NewCipher(rawKey)
	if err != nil {
		return nil, TransitKey{}, fmt.Errorf("%w: transit cipher: %v", ErrCrypto, err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, TransitKey{}, fmt.Errorf("%w: transit gcm: %v", ErrCrypto, err)
	}
	if len(payload) < aead.NonceSize() {
		return nil, TransitKey{}, fmt.Errorf("%w: malformed ciphertext", ErrInvalidArgument)
	}
	nonce := payload[:aead.NonceSize()]
	body := payload[aead.NonceSize():]
	plaintext, err := aead.Open(nil, nonce, body, []byte(transitPayloadAAD(principal.OrgID, key.KeyID, version)))
	if err != nil {
		return nil, TransitKey{}, fmt.Errorf("%w: transit decrypt failed", ErrCrypto)
	}
	span.SetAttributes(attribute.String("forge_metal.key_id", key.KeyID), attribute.Int64("forge_metal.key_version", int64(version)))
	return plaintext, key, nil
}

func (s *Service) TransitSign(ctx context.Context, principal Principal, name string, message []byte) (string, TransitKey, error) {
	ctx, span := tracer.Start(ctx, "secrets.transit.sign")
	defer span.End()
	key, rawKey, err := s.loadTransitKey(ctx, principal, name, 0)
	if err != nil {
		return "", TransitKey{}, err
	}
	mac := hmac.New(sha256.New, rawKey)
	mac.Write([]byte(transitPayloadAAD(principal.OrgID, key.KeyID, key.CurrentVersion)))
	mac.Write(message)
	signature := fmt.Sprintf("fmsig:v1:%d:%s", key.CurrentVersion, base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	span.SetAttributes(attribute.String("forge_metal.key_id", key.KeyID), attribute.Int64("forge_metal.key_version", int64(key.CurrentVersion)))
	return signature, key, nil
}

func (s *Service) TransitVerify(ctx context.Context, principal Principal, name string, message []byte, signature string) (bool, TransitKey, error) {
	ctx, span := tracer.Start(ctx, "secrets.transit.verify")
	defer span.End()
	version, sig, err := parseTransitSignature(signature)
	if err != nil {
		return false, TransitKey{}, err
	}
	key, rawKey, err := s.loadTransitKey(ctx, principal, name, version)
	if err != nil {
		return false, TransitKey{}, err
	}
	mac := hmac.New(sha256.New, rawKey)
	mac.Write([]byte(transitPayloadAAD(principal.OrgID, key.KeyID, version)))
	mac.Write(message)
	expected := mac.Sum(nil)
	ok := hmac.Equal(expected, sig)
	span.SetAttributes(attribute.String("forge_metal.key_id", key.KeyID), attribute.Bool("forge_metal.signature_valid", ok))
	return ok, key, nil
}

func (s *Service) loadTransitKey(ctx context.Context, principal Principal, name string, version uint64) (TransitKey, []byte, error) {
	key, err := s.GetTransitKey(ctx, principal, name)
	if err != nil {
		return TransitKey{}, nil, err
	}
	if version == 0 {
		version = key.CurrentVersion
	}
	row := s.PG.QueryRow(ctx, `
		SELECT wrapped_key, nonce
		FROM transit_key_versions
		WHERE key_id = $1 AND version = $2
	`, key.KeyID, version)
	var wrapped []byte
	var nonce []byte
	if err := row.Scan(&wrapped, &nonce); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return TransitKey{}, nil, fmt.Errorf("%w: transit key version not found", ErrNotFound)
		}
		return TransitKey{}, nil, fmt.Errorf("%w: load transit key: %v", ErrStore, err)
	}
	raw, err := s.Codec.Open(wrapped, nonce, transitAAD(principal.OrgID, key.KeyID, version))
	if err != nil {
		return TransitKey{}, nil, err
	}
	return key, raw, nil
}

func scanSecretRecord(row pgx.Row) (SecretRecord, error) {
	var record SecretRecord
	if err := row.Scan(&record.SecretID, &record.OrgID, &record.Kind, &record.Name, &record.Scope.Level, &record.Scope.SourceID, &record.Scope.EnvID, &record.Scope.Branch, &record.CurrentVersion, &record.CreatedAt, &record.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SecretRecord{}, fmt.Errorf("%w: secret not found", ErrNotFound)
		}
		return SecretRecord{}, fmt.Errorf("%w: scan secret: %v", ErrStore, err)
	}
	return record, nil
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

func secretAAD(orgID, secretID string, version uint64) string {
	return fmt.Sprintf("forge-metal:secret:%s:%s:%d", orgID, secretID, version)
}

func transitAAD(orgID, keyID string, version uint64) string {
	return fmt.Sprintf("forge-metal:transit-key:%s:%s:%d", orgID, keyID, version)
}

func transitPayloadAAD(orgID, keyID string, version uint64) string {
	return fmt.Sprintf("forge-metal:transit-payload:%s:%s:%d", orgID, keyID, version)
}

func parseTransitCiphertext(encoded string) (uint64, []byte, error) {
	parts := strings.Split(strings.TrimSpace(encoded), ":")
	if len(parts) != 4 || parts[0] != "fmtransit" || parts[1] != "v1" {
		return 0, nil, fmt.Errorf("%w: malformed transit ciphertext", ErrInvalidArgument)
	}
	var version uint64
	if _, err := fmt.Sscanf(parts[2], "%d", &version); err != nil || version == 0 {
		return 0, nil, fmt.Errorf("%w: malformed transit version", ErrInvalidArgument)
	}
	payload, err := base64.StdEncoding.DecodeString(parts[3])
	if err != nil {
		return 0, nil, fmt.Errorf("%w: malformed transit payload", ErrInvalidArgument)
	}
	return version, payload, nil
}

func parseTransitSignature(encoded string) (uint64, []byte, error) {
	parts := strings.Split(strings.TrimSpace(encoded), ":")
	if len(parts) != 4 || parts[0] != "fmsig" || parts[1] != "v1" {
		return 0, nil, fmt.Errorf("%w: malformed transit signature", ErrInvalidArgument)
	}
	var version uint64
	if _, err := fmt.Sscanf(parts[2], "%d", &version); err != nil || version == 0 {
		return 0, nil, fmt.Errorf("%w: malformed transit signature version", ErrInvalidArgument)
	}
	payload, err := base64.StdEncoding.DecodeString(parts[3])
	if err != nil {
		return 0, nil, fmt.Errorf("%w: malformed transit signature payload", ErrInvalidArgument)
	}
	return version, payload, nil
}
