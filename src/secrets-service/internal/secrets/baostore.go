package secrets

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	workloadauth "github.com/forge-metal/auth-middleware/workload"
	"github.com/google/uuid"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
)

const (
	defaultKVPrefix      = "kv"
	defaultTransitPrefix = "transit"
	defaultJWTPrefix     = "jwt"

	openBaoRoleInjection = "secrets-injection"

	transitMetadataKind = "_platform"
	transitMetadataRoot = "transit"
)

type BaoStoreConfig struct {
	Address           string
	CACertPath        string
	KVMountPrefix     string
	TransitPrefix     string
	JWTAuthPrefix     string
	WorkloadJWT       WorkloadJWTConfig
	TokenCacheSkew    time.Duration
	HTTPClientTimeout time.Duration
}

type WorkloadJWTConfig struct {
	Source     *workloadapi.JWTSource
	Audience   string
	Subject    spiffeid.ID
	AuthPrefix string
}

type BaoStore struct {
	addr          *url.URL
	httpClient    *http.Client
	logger        *slog.Logger
	kvPrefix      string
	transitPrefix string
	jwtPrefix     string
	cacheSkew     time.Duration

	tokens                *baoTokenCache
	workloadJWT           *workloadapi.JWTSource
	workloadJWTAudience   string
	workloadJWTSubject    spiffeid.ID
	workloadJWTAuthPrefix string
}

type baoTokenCache struct {
	mu      sync.Mutex
	entries map[string]baoTokenEntry
	warned  bool
}

type baoTokenEntry struct {
	token        string
	accessorHash string
	expiresAt    time.Time
}

type baoResponse struct {
	RequestID     string         `json:"-"`
	Data          map[string]any `json:"data"`
	Auth          *baoAuth       `json:"auth"`
	Errors        []string       `json:"errors"`
	LeaseDuration int            `json:"lease_duration"`
	Renewable     bool           `json:"renewable"`
	Sealed        bool           `json:"sealed"`
	Standby       bool           `json:"standby"`
	Version       string         `json:"version"`
}

type baoAuth struct {
	ClientToken   string   `json:"client_token"`
	Accessor      string   `json:"accessor"`
	LeaseDuration int      `json:"lease_duration"`
	Renewable     bool     `json:"renewable"`
	Policies      []string `json:"policies"`
}

type secretDocument struct {
	SecretID       string
	OrgID          string
	Kind           string
	Name           string
	Scope          Scope
	Value          string
	CurrentVersion uint64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type transitMetadata struct {
	KeyID          string
	OrgID          string
	Name           string
	EncryptionKey  string
	SigningKey     string
	CurrentVersion uint64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func NewBaoStore(ctx context.Context, cfg BaoStoreConfig, logger *slog.Logger) (*BaoStore, error) {
	if logger == nil {
		logger = slog.Default()
	}
	cfg.Address = strings.TrimRight(strings.TrimSpace(cfg.Address), "/")
	if cfg.Address == "" {
		return nil, fmt.Errorf("%w: openbao address is required", ErrStore)
	}
	addr, err := url.Parse(cfg.Address)
	if err != nil {
		return nil, fmt.Errorf("%w: parse openbao address: %v", ErrStore, err)
	}
	if addr.Scheme != "https" && addr.Scheme != "http" {
		return nil, fmt.Errorf("%w: openbao address must be http or https", ErrStore)
	}
	transport, err := openBaoTransport(cfg.CACertPath)
	if err != nil {
		return nil, err
	}
	timeout := cfg.HTTPClientTimeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	skew := cfg.TokenCacheSkew
	if skew <= 0 {
		skew = 15 * time.Second
	}
	store := &BaoStore{
		addr:          addr,
		httpClient:    &http.Client{Transport: otelhttp.NewTransport(transport), Timeout: timeout},
		logger:        logger,
		kvPrefix:      firstNonEmpty(cfg.KVMountPrefix, defaultKVPrefix),
		transitPrefix: firstNonEmpty(cfg.TransitPrefix, defaultTransitPrefix),
		jwtPrefix:     firstNonEmpty(cfg.JWTAuthPrefix, defaultJWTPrefix),
		cacheSkew:     skew,
		tokens:        &baoTokenCache{entries: map[string]baoTokenEntry{}},
	}
	store.workloadJWT = cfg.WorkloadJWT.Source
	store.workloadJWTAudience = strings.TrimSpace(cfg.WorkloadJWT.Audience)
	store.workloadJWTSubject = cfg.WorkloadJWT.Subject
	store.workloadJWTAuthPrefix = firstNonEmpty(strings.TrimSpace(cfg.WorkloadJWT.AuthPrefix), "spiffe-jwt")
	if err := store.Ready(ctx); err != nil {
		return nil, err
	}
	return store, nil
}

func openBaoTransport(caPath string) (*http.Transport, error) {
	base := http.DefaultTransport.(*http.Transport).Clone()
	caPath = strings.TrimSpace(caPath)
	if caPath == "" {
		return base, nil
	}
	pem, err := os.ReadFile(caPath)
	if err != nil {
		return nil, fmt.Errorf("%w: read openbao CA cert: %v", ErrStore, err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("%w: parse openbao CA cert", ErrStore)
	}
	base.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	return base, nil
}

func (s *BaoStore) Ready(ctx context.Context) error {
	ctx, span := tracer.Start(ctx, "secrets.bao.ready")
	defer span.End()
	response, status, err := s.do(ctx, http.MethodGet, "sys/health", "", nil, nil, http.StatusOK, http.StatusTooManyRequests, http.StatusServiceUnavailable)
	if err != nil {
		return err
	}
	span.SetAttributes(
		attribute.Bool("bao.sealed", response.Sealed),
		attribute.Bool("bao.active", !response.Standby),
		attribute.String("bao.version", response.Version),
		attribute.Int("bao.http_status", status),
	)
	if response.Sealed {
		return fmt.Errorf("%w: openbao is sealed", ErrStore)
	}
	return nil
}

func (s *BaoStore) PutSecret(ctx context.Context, principal Principal, req PutSecretRequest) (SecretRecord, error) {
	req.Kind = normalizeKind(req.Kind)
	req.Name = normalizeName(req.Name)
	req.Scope = normalizeScope(req.Scope)
	if err := validatePrincipal(principal); err != nil {
		return SecretRecord{}, err
	}
	if err := validateSecretInput(req); err != nil {
		return SecretRecord{}, err
	}
	entry, err := s.token(ctx, principal)
	if err != nil {
		return SecretRecord{}, err
	}
	mount := s.kvMount(principal.OrgID)
	recordOpenBaoAuditInfo(ctx, mount, "", entry.accessorHash, 0)
	path := secretPath(req.Kind, req.Name, req.Scope)
	now := time.Now().UTC()
	secretID := uuid.NewString()
	createdAt := now
	if existing, err := s.readExactSecret(ctx, entry, principal, req.Kind, req.Name, req.Scope); err == nil {
		secretID = existing.Record.SecretID
		createdAt = existing.Record.CreatedAt
	} else if !errors.Is(err, ErrNotFound) {
		return SecretRecord{}, err
	}
	document := map[string]any{
		"secret_id":   secretID,
		"org_id":      principal.OrgID,
		"kind":        req.Kind,
		"name":        req.Name,
		"scope_level": req.Scope.Level,
		"source_id":   req.Scope.SourceID,
		"env_id":      req.Scope.EnvID,
		"branch":      req.Scope.Branch,
		"value":       req.Value,
		"created_at":  createdAt.Format(time.RFC3339Nano),
		"updated_at":  now.Format(time.RFC3339Nano),
	}
	response, _, err := s.doBao(ctx, "secrets.bao.kv.put", http.MethodPost, mount, "data", path, entry, map[string]any{"data": document}, http.StatusOK, http.StatusNoContent)
	if err != nil {
		return SecretRecord{}, err
	}
	version := uint64From(responseDataMap(response.Data, "data"), "version")
	if version == 0 {
		version = uint64From(response.Data, "version")
	}
	return SecretRecord{
		SecretID:       secretID,
		OrgID:          principal.OrgID,
		Kind:           req.Kind,
		Name:           req.Name,
		Scope:          req.Scope,
		CurrentVersion: version,
		CreatedAt:      createdAt,
		UpdatedAt:      now,
	}, nil
}

func (s *BaoStore) ReadSecret(ctx context.Context, principal Principal, kind, name string, scope Scope) (SecretValue, error) {
	if err := validatePrincipal(principal); err != nil {
		return SecretValue{}, err
	}
	kind = normalizeKind(kind)
	name = normalizeName(name)
	scope = normalizeScope(scope)
	if kind == "" || name == "" {
		return SecretValue{}, fmt.Errorf("%w: kind and name are required", ErrInvalidArgument)
	}
	if err := validateResourceName(name); err != nil {
		return SecretValue{}, err
	}
	candidates, err := resolutionCandidates(scope)
	if err != nil {
		return SecretValue{}, err
	}
	entry, err := s.token(ctx, principal)
	if err != nil {
		return SecretValue{}, err
	}
	recordOpenBaoAuditInfo(ctx, s.kvMount(principal.OrgID), "", entry.accessorHash, 0)
	for _, candidate := range candidates {
		value, err := s.readExactSecret(ctx, entry, principal, kind, name, candidate)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		return value, err
	}
	return SecretValue{}, fmt.Errorf("%w: secret not found", ErrNotFound)
}

func (s *BaoStore) GetSecretMetadata(ctx context.Context, principal Principal, kind, name string, scope Scope) (SecretRecord, error) {
	value, err := s.ReadSecret(ctx, principal, kind, name, scope)
	if err != nil {
		return SecretRecord{}, err
	}
	return value.Record, nil
}

func (s *BaoStore) ListSecrets(ctx context.Context, principal Principal, kind string, limit int) ([]SecretRecord, error) {
	if err := validatePrincipal(principal); err != nil {
		return nil, err
	}
	kind = normalizeKind(kind)
	if kind == "" {
		kind = KindSecret
	}
	if kind != KindSecret && kind != KindVariable {
		return nil, fmt.Errorf("%w: kind must be secret or variable", ErrInvalidArgument)
	}
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	entry, err := s.token(ctx, principal)
	if err != nil {
		return nil, err
	}
	mount := s.kvMount(principal.OrgID)
	recordOpenBaoAuditInfo(ctx, mount, "", entry.accessorHash, 0)
	paths, err := s.listSecretPaths(ctx, entry, mount, []string{kind})
	if err != nil {
		return nil, err
	}
	records := make([]SecretRecord, 0, len(paths))
	for _, path := range paths {
		segments := splitPath(path)
		if !isCanonicalSecretPath(kind, segments) {
			continue
		}
		value, err := s.readKVPath(ctx, entry, principal, segments)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		records = append(records, value.Record)
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].UpdatedAt.Equal(records[j].UpdatedAt) {
			return records[i].SecretID > records[j].SecretID
		}
		return records[i].UpdatedAt.After(records[j].UpdatedAt)
	})
	if len(records) > limit {
		records = records[:limit]
	}
	return records, nil
}

func (s *BaoStore) DeleteSecret(ctx context.Context, principal Principal, kind, name string, scope Scope) (SecretRecord, error) {
	if err := validatePrincipal(principal); err != nil {
		return SecretRecord{}, err
	}
	kind = normalizeKind(kind)
	name = normalizeName(name)
	scope = normalizeScope(scope)
	if err := validateResourceName(name); err != nil {
		return SecretRecord{}, err
	}
	if err := validateScope(scope); err != nil {
		return SecretRecord{}, err
	}
	entry, err := s.token(ctx, principal)
	if err != nil {
		return SecretRecord{}, err
	}
	value, err := s.readExactSecret(ctx, entry, principal, kind, name, scope)
	if err != nil {
		return SecretRecord{}, err
	}
	mount := s.kvMount(principal.OrgID)
	recordOpenBaoAuditInfo(ctx, mount, "", entry.accessorHash, 0)
	if _, _, err := s.doBao(ctx, "secrets.bao.kv.delete", http.MethodDelete, mount, "metadata", secretPath(kind, name, scope), entry, nil, http.StatusNoContent); err != nil {
		return SecretRecord{}, err
	}
	return value.Record, nil
}

func (s *BaoStore) CreateTransitKey(ctx context.Context, principal Principal, name string) (TransitKey, error) {
	if err := validatePrincipal(principal); err != nil {
		return TransitKey{}, err
	}
	name = normalizeName(name)
	if err := validateResourceName(name); err != nil {
		return TransitKey{}, err
	}
	entry, err := s.token(ctx, principal)
	if err != nil {
		return TransitKey{}, err
	}
	if _, err := s.readTransitMetadata(ctx, entry, principal, name); err == nil {
		return TransitKey{}, fmt.Errorf("%w: transit key already exists", ErrConflict)
	} else if !errors.Is(err, ErrNotFound) {
		return TransitKey{}, err
	}
	meta := newTransitMetadata(principal.OrgID, name)
	mount := s.transitMount(principal.OrgID)
	recordOpenBaoAuditInfo(ctx, mount, "", entry.accessorHash, 0)
	if _, _, err := s.doBao(ctx, "secrets.bao.transit.create", http.MethodPost, mount, "keys", []string{meta.EncryptionKey}, entry, map[string]any{"type": "aes256-gcm96"}, http.StatusNoContent, http.StatusOK); err != nil {
		return TransitKey{}, err
	}
	if _, _, err := s.doBao(ctx, "secrets.bao.transit.create", http.MethodPost, mount, "keys", []string{meta.SigningKey}, entry, map[string]any{"type": "ed25519"}, http.StatusNoContent, http.StatusOK); err != nil {
		return TransitKey{}, err
	}
	if err := s.writeTransitMetadata(ctx, entry, principal, meta); err != nil {
		return TransitKey{}, err
	}
	key, err := s.transitKeyFromMetadata(ctx, entry, principal, meta)
	if err != nil {
		return TransitKey{}, err
	}
	return key, nil
}

func (s *BaoStore) GetTransitKey(ctx context.Context, principal Principal, name string) (TransitKey, error) {
	if err := validatePrincipal(principal); err != nil {
		return TransitKey{}, err
	}
	name = normalizeName(name)
	if err := validateResourceName(name); err != nil {
		return TransitKey{}, err
	}
	entry, err := s.token(ctx, principal)
	if err != nil {
		return TransitKey{}, err
	}
	meta, err := s.readTransitMetadata(ctx, entry, principal, name)
	if err != nil {
		return TransitKey{}, err
	}
	return s.transitKeyFromMetadata(ctx, entry, principal, meta)
}

func (s *BaoStore) RotateTransitKey(ctx context.Context, principal Principal, name string) (TransitKey, error) {
	if err := validatePrincipal(principal); err != nil {
		return TransitKey{}, err
	}
	name = normalizeName(name)
	if err := validateResourceName(name); err != nil {
		return TransitKey{}, err
	}
	entry, err := s.token(ctx, principal)
	if err != nil {
		return TransitKey{}, err
	}
	meta, err := s.readTransitMetadata(ctx, entry, principal, name)
	if err != nil {
		return TransitKey{}, err
	}
	mount := s.transitMount(principal.OrgID)
	recordOpenBaoAuditInfo(ctx, mount, "", entry.accessorHash, 0)
	if _, _, err := s.doBao(ctx, "secrets.bao.transit.rotate", http.MethodPost, mount, "keys", []string{meta.EncryptionKey, "rotate"}, entry, nil, http.StatusNoContent, http.StatusOK); err != nil {
		return TransitKey{}, err
	}
	if _, _, err := s.doBao(ctx, "secrets.bao.transit.rotate", http.MethodPost, mount, "keys", []string{meta.SigningKey, "rotate"}, entry, nil, http.StatusNoContent, http.StatusOK); err != nil {
		return TransitKey{}, err
	}
	meta.CurrentVersion++
	meta.UpdatedAt = time.Now().UTC()
	if err := s.writeTransitMetadata(ctx, entry, principal, meta); err != nil {
		return TransitKey{}, err
	}
	return s.transitKeyFromMetadata(ctx, entry, principal, meta)
}

func (s *BaoStore) TransitEncrypt(ctx context.Context, principal Principal, name string, plaintext []byte) (TransitCiphertext, TransitKey, error) {
	key, entry, meta, err := s.transitOperationKey(ctx, principal, name)
	if err != nil {
		return TransitCiphertext{}, TransitKey{}, err
	}
	mount := s.transitMount(principal.OrgID)
	body := map[string]any{"plaintext": base64.StdEncoding.EncodeToString(plaintext)}
	response, _, err := s.doBao(ctx, "secrets.bao.transit.encrypt", http.MethodPost, mount, "encrypt", []string{meta.EncryptionKey}, entry, body, http.StatusOK)
	if err != nil {
		return TransitCiphertext{}, TransitKey{}, err
	}
	ciphertext := stringFrom(response.Data, "ciphertext")
	version := versionFromVaultValue(ciphertext)
	recordOpenBaoAuditInfo(ctx, mount, "", entry.accessorHash, version)
	if ciphertext == "" || version == 0 {
		return TransitCiphertext{}, TransitKey{}, fmt.Errorf("%w: openbao encrypt response missing ciphertext", ErrStore)
	}
	return TransitCiphertext{KeyName: key.Name, Version: version, Ciphertext: ciphertext}, key, nil
}

func (s *BaoStore) TransitDecrypt(ctx context.Context, principal Principal, name, encoded string) ([]byte, TransitKey, uint64, error) {
	key, entry, meta, err := s.transitOperationKey(ctx, principal, name)
	if err != nil {
		return nil, TransitKey{}, 0, err
	}
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return nil, TransitKey{}, 0, fmt.Errorf("%w: ciphertext is required", ErrInvalidArgument)
	}
	mount := s.transitMount(principal.OrgID)
	version := versionFromVaultValue(encoded)
	response, _, err := s.doBao(ctx, "secrets.bao.transit.decrypt", http.MethodPost, mount, "decrypt", []string{meta.EncryptionKey}, entry, map[string]any{"ciphertext": encoded}, http.StatusOK)
	if err != nil {
		return nil, TransitKey{}, 0, err
	}
	plaintext, err := base64.StdEncoding.DecodeString(stringFrom(response.Data, "plaintext"))
	if err != nil {
		return nil, TransitKey{}, 0, fmt.Errorf("%w: malformed openbao plaintext", ErrCrypto)
	}
	recordOpenBaoAuditInfo(ctx, mount, "", entry.accessorHash, version)
	return plaintext, key, version, nil
}

func (s *BaoStore) TransitSign(ctx context.Context, principal Principal, name string, message []byte) (string, TransitKey, uint64, error) {
	key, entry, meta, err := s.transitOperationKey(ctx, principal, name)
	if err != nil {
		return "", TransitKey{}, 0, err
	}
	mount := s.transitMount(principal.OrgID)
	response, _, err := s.doBao(ctx, "secrets.bao.transit.sign", http.MethodPost, mount, "sign", []string{meta.SigningKey}, entry, map[string]any{"input": base64.StdEncoding.EncodeToString(message)}, http.StatusOK)
	if err != nil {
		return "", TransitKey{}, 0, err
	}
	signature := stringFrom(response.Data, "signature")
	version := versionFromVaultValue(signature)
	recordOpenBaoAuditInfo(ctx, mount, "", entry.accessorHash, version)
	if signature == "" || version == 0 {
		return "", TransitKey{}, 0, fmt.Errorf("%w: openbao sign response missing signature", ErrStore)
	}
	return signature, key, version, nil
}

func (s *BaoStore) TransitVerify(ctx context.Context, principal Principal, name string, message []byte, signature string) (bool, TransitKey, error) {
	key, entry, meta, err := s.transitOperationKey(ctx, principal, name)
	if err != nil {
		return false, TransitKey{}, err
	}
	signature = strings.TrimSpace(signature)
	if signature == "" {
		return false, TransitKey{}, fmt.Errorf("%w: signature is required", ErrInvalidArgument)
	}
	mount := s.transitMount(principal.OrgID)
	version := versionFromVaultValue(signature)
	body := map[string]any{
		"input":     base64.StdEncoding.EncodeToString(message),
		"signature": signature,
	}
	response, _, err := s.doBao(ctx, "secrets.bao.transit.verify", http.MethodPost, mount, "verify", []string{meta.SigningKey}, entry, body, http.StatusOK)
	if err != nil {
		return false, TransitKey{}, err
	}
	recordOpenBaoAuditInfo(ctx, mount, "", entry.accessorHash, version)
	return boolFrom(response.Data, "valid"), key, nil
}

func (s *BaoStore) readExactSecret(ctx context.Context, entry baoTokenEntry, principal Principal, kind, name string, scope Scope) (SecretValue, error) {
	return s.readKVPath(ctx, entry, principal, secretPath(kind, name, scope))
}

func (s *BaoStore) readKVPath(ctx context.Context, entry baoTokenEntry, principal Principal, path []string) (SecretValue, error) {
	mount := s.kvMount(principal.OrgID)
	recordOpenBaoAuditInfo(ctx, mount, "", entry.accessorHash, 0)
	response, _, err := s.doBao(ctx, "secrets.bao.kv.get", http.MethodGet, mount, "data", path, entry, nil, http.StatusOK)
	if err != nil {
		return SecretValue{}, err
	}
	data := responseDataMap(response.Data, "data")
	metadata := responseDataMap(response.Data, "metadata")
	document, err := secretDocumentFromKV(principal.OrgID, data, metadata)
	if err != nil {
		return SecretValue{}, err
	}
	return SecretValue{
		Record: SecretRecord{
			SecretID:       document.SecretID,
			OrgID:          document.OrgID,
			Kind:           document.Kind,
			Name:           document.Name,
			Scope:          document.Scope,
			CurrentVersion: document.CurrentVersion,
			CreatedAt:      document.CreatedAt,
			UpdatedAt:      document.UpdatedAt,
		},
		Value: document.Value,
	}, nil
}

func (s *BaoStore) listSecretPaths(ctx context.Context, entry baoTokenEntry, mount string, prefix []string) ([]string, error) {
	response, _, err := s.doBao(ctx, "secrets.bao.kv.list", "LIST", mount, "metadata", prefix, entry, nil, http.StatusOK)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	keys := stringSliceFrom(responseDataMap(response.Data, ""), "keys")
	paths := []string{}
	for _, key := range keys {
		if strings.HasSuffix(key, "/") {
			childPrefix := append(append([]string{}, prefix...), strings.TrimSuffix(key, "/"))
			childPaths, err := s.listSecretPaths(ctx, entry, mount, childPrefix)
			if err != nil {
				return nil, err
			}
			paths = append(paths, childPaths...)
			continue
		}
		full := append(append([]string{}, prefix...), key)
		paths = append(paths, strings.Join(full, "/"))
	}
	return paths, nil
}

func (s *BaoStore) readTransitMetadata(ctx context.Context, entry baoTokenEntry, principal Principal, name string) (transitMetadata, error) {
	value, err := s.readKVPath(ctx, entry, principal, transitMetadataPath(name))
	if err != nil {
		return transitMetadata{}, err
	}
	return transitMetadata{
		KeyID:          value.Record.SecretID,
		OrgID:          value.Record.OrgID,
		Name:           value.Record.Name,
		EncryptionKey:  "fm-" + value.Record.SecretID[:32] + "-enc",
		SigningKey:     "fm-" + value.Record.SecretID[:32] + "-sig",
		CurrentVersion: value.Record.CurrentVersion,
		CreatedAt:      value.Record.CreatedAt,
		UpdatedAt:      value.Record.UpdatedAt,
	}, nil
}

func (s *BaoStore) writeTransitMetadata(ctx context.Context, entry baoTokenEntry, principal Principal, meta transitMetadata) error {
	path := transitMetadataPath(meta.Name)
	document := map[string]any{
		"secret_id":       meta.KeyID,
		"org_id":          meta.OrgID,
		"kind":            transitMetadataKind,
		"name":            meta.Name,
		"scope_level":     ScopeOrg,
		"source_id":       "",
		"env_id":          "",
		"branch":          "",
		"value":           "",
		"encryption_key":  meta.EncryptionKey,
		"signing_key":     meta.SigningKey,
		"current_version": meta.CurrentVersion,
		"created_at":      meta.CreatedAt.Format(time.RFC3339Nano),
		"updated_at":      meta.UpdatedAt.Format(time.RFC3339Nano),
	}
	_, _, err := s.doBao(ctx, "secrets.bao.kv.put", http.MethodPost, s.kvMount(principal.OrgID), "data", path, entry, map[string]any{"data": document}, http.StatusOK, http.StatusNoContent)
	return err
}

func (s *BaoStore) transitOperationKey(ctx context.Context, principal Principal, name string) (TransitKey, baoTokenEntry, transitMetadata, error) {
	if err := validatePrincipal(principal); err != nil {
		return TransitKey{}, baoTokenEntry{}, transitMetadata{}, err
	}
	name = normalizeName(name)
	if err := validateResourceName(name); err != nil {
		return TransitKey{}, baoTokenEntry{}, transitMetadata{}, err
	}
	entry, err := s.token(ctx, principal)
	if err != nil {
		return TransitKey{}, baoTokenEntry{}, transitMetadata{}, err
	}
	meta, err := s.readTransitMetadata(ctx, entry, principal, name)
	if err != nil {
		return TransitKey{}, baoTokenEntry{}, transitMetadata{}, err
	}
	key, err := s.transitKeyFromMetadata(ctx, entry, principal, meta)
	if err != nil {
		return TransitKey{}, baoTokenEntry{}, transitMetadata{}, err
	}
	return key, entry, meta, nil
}

func (s *BaoStore) transitKeyFromMetadata(ctx context.Context, entry baoTokenEntry, principal Principal, meta transitMetadata) (TransitKey, error) {
	publicKey, version, err := s.readSigningKey(ctx, entry, principal, meta.SigningKey)
	if err != nil {
		return TransitKey{}, err
	}
	if version > 0 {
		meta.CurrentVersion = version
	}
	return TransitKey{
		KeyID:          meta.KeyID,
		OrgID:          meta.OrgID,
		Name:           meta.Name,
		CurrentVersion: meta.CurrentVersion,
		CreatedAt:      meta.CreatedAt,
		UpdatedAt:      meta.UpdatedAt,
		PublicKey:      publicKey,
	}, nil
}

func (s *BaoStore) readSigningKey(ctx context.Context, entry baoTokenEntry, principal Principal, signingKey string) (string, uint64, error) {
	mount := s.transitMount(principal.OrgID)
	response, _, err := s.doBao(ctx, "secrets.bao.transit.metadata_read", http.MethodGet, mount, "keys", []string{signingKey}, entry, nil, http.StatusOK)
	if err != nil {
		return "", 0, err
	}
	keys := responseDataMap(response.Data, "keys")
	currentVersion := uint64(0)
	publicKey := ""
	for versionText, raw := range keys {
		version, _ := strconv.ParseUint(versionText, 10, 64)
		if version > currentVersion {
			currentVersion = version
		}
		if publicKey == "" {
			if value, ok := raw.(string); ok {
				publicKey = value
			}
			if nested, ok := raw.(map[string]any); ok {
				publicKey = stringFrom(nested, "public_key")
			}
		}
	}
	return publicKey, currentVersion, nil
}

func (s *BaoStore) token(ctx context.Context, principal Principal) (baoTokenEntry, error) {
	role := strings.TrimSpace(principal.OpenBaoRole)
	raw, ok := RawBearerTokenFromContext(ctx)
	authMount := s.jwtMount(principal.OrgID)
	spanName := "secrets.bao.jwt.login"
	if principal.UseWorkloadSVID {
		if principal.OrgID == "" {
			return baoTokenEntry{}, fmt.Errorf("%w: org id is required for workload OpenBao login", ErrForbidden)
		}
		role = openBaoRoleInjection + "-" + principal.OrgID
		if s.workloadJWT == nil || s.workloadJWTAudience == "" {
			return baoTokenEntry{}, fmt.Errorf("%w: workload JWT-SVID source is not configured", ErrStore)
		}
		token, expiresAt, subject, err := workloadauth.FetchJWTSVID(ctx, s.workloadJWT, s.workloadJWTAudience, s.workloadJWTSubject)
		if err != nil {
			return baoTokenEntry{}, err
		}
		var tokenOK bool
		raw, tokenOK = NewRawBearerToken(token)
		if !tokenOK {
			return baoTokenEntry{}, fmt.Errorf("%w: SPIRE returned an empty JWT-SVID", ErrStore)
		}
		principal.JWTID = subject.String()
		principal.TokenExpiresAt = expiresAt
		authMount = s.workloadJWTAuthPrefix
		spanName = "secrets.bao.jwt_svid.login"
		ok = true
	}
	if !ok {
		return baoTokenEntry{}, fmt.Errorf("%w: raw bearer token is required for openbao login", ErrForbidden)
	}
	if role == "" {
		return baoTokenEntry{}, fmt.Errorf("%w: openbao role is required", ErrForbidden)
	}
	namespace := principal.OrgID
	jwtID := strings.TrimSpace(principal.JWTID)
	tokenHash := ""
	if jwtID == "" {
		tokenHash = raw.SHA256()
		s.tokens.warnJWTIDMissing(s.logger)
	}
	key := strings.Join([]string{namespace, role, jwtID, tokenHash}, "\x00")
	now := time.Now()
	s.tokens.mu.Lock()
	if entry, ok := s.tokens.entries[key]; ok && entry.expiresAt.After(now.Add(s.cacheSkew)) {
		s.tokens.mu.Unlock()
		s.tokenCacheSpan(ctx, namespace, "hit", entry.expiresAt)
		recordOpenBaoAuditInfo(ctx, "", "", entry.accessorHash, 0)
		return entry, nil
	}
	s.tokens.mu.Unlock()
	s.tokenCacheSpan(ctx, namespace, "miss", time.Time{})
	entry, err := s.jwtLogin(ctx, principal, role, raw, authMount, spanName)
	if err != nil {
		return baoTokenEntry{}, err
	}
	if !principal.TokenExpiresAt.IsZero() && principal.TokenExpiresAt.Before(entry.expiresAt) {
		entry.expiresAt = principal.TokenExpiresAt.Add(-s.cacheSkew)
	}
	s.tokens.mu.Lock()
	s.tokens.entries[key] = entry
	s.tokens.mu.Unlock()
	recordOpenBaoAuditInfo(ctx, "", "", entry.accessorHash, 0)
	return entry, nil
}

func (c *baoTokenCache) warnJWTIDMissing(logger *slog.Logger) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.warned {
		return
	}
	c.warned = true
	if logger == nil {
		logger = slog.Default()
	}
	logger.Warn("Zitadel access token lacked jti; OpenBao token cache is using raw-token hash fallback")
}

func (s *BaoStore) tokenCacheSpan(ctx context.Context, namespace, outcome string, expiresAt time.Time) {
	_, span := tracer.Start(ctx, "secrets.bao.token.cache")
	defer span.End()
	ttl := int64(0)
	if !expiresAt.IsZero() {
		ttl = time.Until(expiresAt).Milliseconds()
	}
	span.SetAttributes(
		attribute.String("bao.namespace", namespace),
		attribute.String("forge_metal.cache_outcome", outcome),
		attribute.Int64("bao.ttl_remaining_ms", ttl),
	)
}

func (s *BaoStore) jwtLogin(ctx context.Context, principal Principal, role string, jwt RawBearerToken, authMount string, spanName string) (baoTokenEntry, error) {
	ctx, span := tracer.Start(ctx, spanName)
	defer span.End()
	body := map[string]any{
		"role": role,
		"jwt":  strings.TrimPrefix(jwt.AuthorizationHeader(), "Bearer "),
	}
	response, status, err := s.do(ctx, http.MethodPost, joinPath("auth", authMount, "login"), "", nil, body, http.StatusOK)
	if err != nil {
		span.SetAttributes(attribute.Int("bao.http_status", status))
		return baoTokenEntry{}, err
	}
	if response.Auth == nil || response.Auth.ClientToken == "" {
		return baoTokenEntry{}, fmt.Errorf("%w: openbao jwt login omitted client token", ErrStore)
	}
	lease := time.Duration(response.Auth.LeaseDuration) * time.Second
	if lease <= 0 {
		lease = 15 * time.Minute
	}
	accessorHash := sha256Hex([]byte(response.Auth.Accessor))
	entry := baoTokenEntry{
		token:        response.Auth.ClientToken,
		accessorHash: accessorHash,
		expiresAt:    time.Now().Add(lease).Add(-s.cacheSkew),
	}
	span.SetAttributes(
		attribute.String("bao.namespace", principal.OrgID),
		attribute.String("bao.auth_method", firstNonEmpty(strings.TrimPrefix(spanName, "secrets.bao."), "jwt")),
		attribute.String("bao.role", role),
		attribute.String("bao.request_id", response.RequestID),
		attribute.Int("bao.http_status", status),
		attribute.String("forge_metal.org_id", principal.OrgID),
		attribute.String("forge_metal.cache_outcome", "miss"),
	)
	recordOpenBaoAuditInfo(ctx, "", response.RequestID, accessorHash, 0)
	return entry, nil
}

func (s *BaoStore) doBao(ctx context.Context, spanName, method, mount, apiPrefix string, path []string, entry baoTokenEntry, body any, expected ...int) (baoResponse, int, error) {
	ctx, span := tracer.Start(ctx, spanName)
	defer span.End()
	response, status, err := s.do(ctx, method, joinPath(append([]string{mount, apiPrefix}, path...)...), entry.token, path, body, expected...)
	pathHash := ""
	if len(path) > 0 {
		pathHash = sha256Hex([]byte(strings.Join(path, "/")))
	}
	keyVersion := uint64(0)
	if strings.HasPrefix(spanName, "secrets.bao.transit.") {
		keyVersion = versionFromVaultValue(stringFrom(response.Data, "ciphertext"))
		if keyVersion == 0 {
			keyVersion = versionFromVaultValue(stringFrom(response.Data, "signature"))
		}
	}
	span.SetAttributes(
		attribute.String("bao.namespace", namespaceFromMount(mount)),
		attribute.String("bao.mount", mount),
		attribute.String("bao.path_hash", pathHash),
		attribute.String("bao.request_id", response.RequestID),
		attribute.Int("bao.http_status", status),
		attribute.String("bao.key_id", keyIDFromPath(path)),
		attribute.Int64("bao.key_version", int64(keyVersion)),
	)
	recordOpenBaoAuditInfo(ctx, mount, response.RequestID, entry.accessorHash, keyVersion)
	if err != nil {
		return baoResponse{}, status, err
	}
	return response, status, nil
}

func (s *BaoStore) do(ctx context.Context, method, path, token string, logicalPath []string, body any, expected ...int) (baoResponse, int, error) {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return baoResponse{}, 0, fmt.Errorf("%w: marshal openbao request: %v", ErrStore, err)
		}
		reader = bytes.NewReader(payload)
	}
	reqURL := *s.addr
	basePath := strings.Trim(strings.TrimSuffix(reqURL.Path, "/"), "/")
	apiPath := strings.Trim(path, "/")
	if basePath == "" {
		reqURL.Path = "/v1/" + apiPath
	} else {
		reqURL.Path = "/" + basePath + "/v1/" + apiPath
	}
	req, err := http.NewRequestWithContext(ctx, method, reqURL.String(), reader)
	if err != nil {
		return baoResponse{}, 0, fmt.Errorf("%w: create openbao request: %v", ErrStore, err)
	}
	clientRequestID := "client:" + uuid.NewString()
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Forge-Metal-Request-Id", clientRequestID)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("X-Vault-Token", token)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return baoResponse{}, 0, fmt.Errorf("%w: openbao %s %s: %v", ErrStore, method, sanitizedPath(path), err)
	}
	defer resp.Body.Close()
	rawBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	response := baoResponse{RequestID: firstNonEmpty(
		resp.Header.Get("X-Vault-Request-Id"),
		resp.Header.Get("X-OpenBao-Request-Id"),
	)}
	if len(rawBody) > 0 {
		_ = json.Unmarshal(rawBody, &response)
		if response.RequestID == "" {
			response.RequestID = stringFrom(response.Data, "request_id")
		}
	}
	if response.RequestID == "" {
		response.RequestID = clientRequestID
	}
	if len(expected) == 0 {
		expected = []int{http.StatusOK}
	}
	for _, status := range expected {
		if resp.StatusCode == status {
			return response, resp.StatusCode, nil
		}
	}
	return response, resp.StatusCode, mapBaoStatus(resp.StatusCode, method, path, response.Errors)
}

func mapBaoStatus(status int, method, path string, errs []string) error {
	detail := ""
	if len(errs) > 0 {
		detail = ": " + strings.Join(errs, "; ")
	}
	switch status {
	case http.StatusNotFound:
		return fmt.Errorf("%w: openbao path not found", ErrNotFound)
	case http.StatusForbidden, http.StatusUnauthorized:
		return fmt.Errorf("%w: openbao denied %s %s", ErrForbidden, method, sanitizedPath(path))
	case http.StatusBadRequest:
		return fmt.Errorf("%w: openbao rejected %s %s%s", ErrInvalidArgument, method, sanitizedPath(path), detail)
	default:
		return fmt.Errorf("%w: openbao status %d for %s %s%s", ErrStore, status, method, sanitizedPath(path), detail)
	}
}

func (s *BaoStore) kvMount(orgID string) string {
	return s.kvPrefix + "-" + strings.TrimSpace(orgID)
}

func (s *BaoStore) transitMount(orgID string) string {
	return s.transitPrefix + "-" + strings.TrimSpace(orgID)
}

func (s *BaoStore) jwtMount(orgID string) string {
	return s.jwtPrefix + "-" + strings.TrimSpace(orgID)
}

func secretPath(kind, name string, scope Scope) []string {
	kind = normalizeKind(kind)
	name = normalizeName(name)
	scope = normalizeScope(scope)
	switch scope.Level {
	case ScopeOrg:
		return []string{kind, ScopeOrg, name}
	case ScopeSource:
		return []string{kind, ScopeSource, scope.SourceID, name}
	case ScopeEnvironment:
		return []string{kind, ScopeEnvironment, scope.SourceID, scope.EnvID, name}
	case ScopeBranch:
		return []string{kind, ScopeBranch, scope.SourceID, scope.EnvID, hashBranch(scope.Branch), name}
	default:
		return []string{kind, ScopeOrg, name}
	}
}

func transitMetadataPath(name string) []string {
	return []string{transitMetadataKind, transitMetadataRoot, normalizeName(name)}
}

func isCanonicalSecretPath(kind string, path []string) bool {
	if len(path) < 2 || path[0] != kind {
		return false
	}
	switch path[1] {
	case ScopeOrg:
		return len(path) == 3
	case ScopeSource:
		return len(path) == 4
	case ScopeEnvironment:
		return len(path) == 5
	case ScopeBranch:
		return len(path) == 6
	default:
		return false
	}
}

func newTransitMetadata(orgID, name string) transitMetadata {
	now := time.Now().UTC()
	keyID := sha256Hex([]byte(strings.Join([]string{orgID, normalizeName(name)}, "\x00")))
	return transitMetadata{
		KeyID:          keyID,
		OrgID:          orgID,
		Name:           normalizeName(name),
		EncryptionKey:  "fm-" + keyID[:32] + "-enc",
		SigningKey:     "fm-" + keyID[:32] + "-sig",
		CurrentVersion: 1,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

func secretDocumentFromKV(orgID string, data, metadata map[string]any) (secretDocument, error) {
	createdAt := parseTime(firstNonEmpty(stringFrom(data, "created_at"), stringFrom(metadata, "created_time")))
	updatedAt := parseTime(firstNonEmpty(stringFrom(data, "updated_at"), stringFrom(metadata, "updated_time")))
	version := uint64From(metadata, "version")
	if version == 0 {
		version = uint64From(data, "current_version")
	}
	document := secretDocument{
		SecretID: firstNonEmpty(stringFrom(data, "secret_id"), sha256Hex([]byte(strings.Join([]string{
			orgID,
			stringFrom(data, "kind"),
			stringFrom(data, "name"),
			stringFrom(data, "scope_level"),
			stringFrom(data, "source_id"),
			stringFrom(data, "env_id"),
			hashBranch(stringFrom(data, "branch")),
		}, "\x00")))),
		OrgID:          firstNonEmpty(stringFrom(data, "org_id"), orgID),
		Kind:           normalizeKind(stringFrom(data, "kind")),
		Name:           normalizeName(stringFrom(data, "name")),
		Scope:          normalizeScope(Scope{Level: stringFrom(data, "scope_level"), SourceID: stringFrom(data, "source_id"), EnvID: stringFrom(data, "env_id"), Branch: stringFrom(data, "branch")}),
		Value:          stringFrom(data, "value"),
		CurrentVersion: version,
		CreatedAt:      createdAt,
		UpdatedAt:      updatedAt,
	}
	if document.Kind == "" || document.Name == "" || document.CurrentVersion == 0 {
		return secretDocument{}, fmt.Errorf("%w: malformed openbao secret document", ErrStore)
	}
	if document.CreatedAt.IsZero() {
		document.CreatedAt = time.Now().UTC()
	}
	if document.UpdatedAt.IsZero() {
		document.UpdatedAt = document.CreatedAt
	}
	return document, nil
}

func validateResourceName(name string) error {
	name = normalizeName(name)
	if name == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidArgument)
	}
	if strings.ContainsAny(name, "/\x00\r\n\t") {
		return fmt.Errorf("%w: name cannot contain path or control characters", ErrInvalidArgument)
	}
	return nil
}

func splitPath(path string) []string {
	if path == "" {
		return nil
	}
	return strings.Split(path, "/")
}

func joinPath(parts ...string) string {
	segments := make([]string, 0, len(parts))
	for _, part := range parts {
		for _, segment := range strings.Split(part, "/") {
			segment = strings.TrimSpace(segment)
			if segment == "" {
				continue
			}
			segments = append(segments, url.PathEscape(segment))
		}
	}
	return strings.Join(segments, "/")
}

func sanitizedPath(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) <= 2 {
		return path
	}
	return strings.Join(parts[:2], "/") + "/..."
}

func namespaceFromMount(mount string) string {
	if _, suffix, ok := strings.Cut(mount, "-"); ok {
		return suffix
	}
	return ""
}

func keyIDFromPath(path []string) string {
	if len(path) == 0 {
		return ""
	}
	return path[0]
}

func versionFromVaultValue(value string) uint64 {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) < 3 || parts[0] != "vault" || !strings.HasPrefix(parts[1], "v") {
		return 0
	}
	version, _ := strconv.ParseUint(strings.TrimPrefix(parts[1], "v"), 10, 64)
	return version
}

func responseDataMap(data map[string]any, key string) map[string]any {
	if key == "" {
		if data == nil {
			return map[string]any{}
		}
		return data
	}
	if nested, ok := data[key].(map[string]any); ok {
		return nested
	}
	return map[string]any{}
}

func stringFrom(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	switch value := values[key].(type) {
	case string:
		return strings.TrimSpace(value)
	case fmt.Stringer:
		return strings.TrimSpace(value.String())
	default:
		return ""
	}
}

func boolFrom(values map[string]any, key string) bool {
	if values == nil {
		return false
	}
	value, _ := values[key].(bool)
	return value
}

func uint64From(values map[string]any, key string) uint64 {
	if values == nil {
		return 0
	}
	switch value := values[key].(type) {
	case float64:
		if value < 0 || value > float64(math.MaxUint64) {
			return 0
		}
		return uint64(value)
	case int:
		if value < 0 {
			return 0
		}
		return uint64(value)
	case int64:
		if value < 0 {
			return 0
		}
		return uint64(value)
	case uint64:
		return value
	case json.Number:
		parsed, _ := strconv.ParseUint(value.String(), 10, 64)
		return parsed
	case string:
		parsed, _ := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
		return parsed
	default:
		return 0
	}
}

func stringSliceFrom(values map[string]any, key string) []string {
	raw, ok := values[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, value := range raw {
		if text, ok := value.(string); ok && text != "" {
			out = append(out, text)
		}
	}
	return out
}

func parseTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err == nil {
		return parsed.UTC()
	}
	parsed, err = time.Parse(time.RFC3339, value)
	if err == nil {
		return parsed.UTC()
	}
	return time.Time{}
}
