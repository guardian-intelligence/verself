package secrets

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
)

const (
	KindOpaqueCredential = "opaque_credential"

	CredentialStatusActive  = "active"
	CredentialStatusRevoked = "revoked"

	credentialTokenPrefix = "fmoc"
	credentialHMACKey     = "fm-opaque-credential-hmac"
)

type OpaqueCredential struct {
	CredentialID   string
	OrgID          string
	Kind           string
	Subject        string
	DisplayName    string
	Status         string
	TokenPrefix    string
	Scopes         []string
	Metadata       map[string]string
	CurrentVersion uint64
	ExpiresAt      time.Time
	LastUsedAt     *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
	RevokedAt      *time.Time
}

type OpaqueCredentialMaterial struct {
	Credential OpaqueCredential
	Token      string
}

type CreateOpaqueCredentialRequest struct {
	Kind        string
	Subject     string
	DisplayName string
	Scopes      []string
	Metadata    map[string]string
	ExpiresAt   time.Time
}

type RollOpaqueCredentialRequest struct {
	CredentialID string
	ExpiresAt    time.Time
}

type VerifyOpaqueCredentialRequest struct {
	Kind           string
	Token          string
	RequiredScopes []string
}

type VerifyOpaqueCredentialResult struct {
	Active       bool
	DenialReason string
	Credential   OpaqueCredential
}

type credentialDocument struct {
	CredentialID   string
	OrgID          string
	Kind           string
	Subject        string
	DisplayName    string
	Status         string
	TokenPrefix    string
	TokenHMAC      string
	Scopes         []string
	Metadata       map[string]string
	CurrentVersion uint64
	ExpiresAt      time.Time
	LastUsedAt     *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
	RevokedAt      *time.Time
}

func (s *Service) CreateOpaqueCredential(ctx context.Context, principal Principal, req CreateOpaqueCredentialRequest) (OpaqueCredentialMaterial, error) {
	ctx, span := tracer.Start(ctx, "secrets.credential.create")
	defer span.End()
	if err := s.Validate(); err != nil {
		return OpaqueCredentialMaterial{}, err
	}
	material, err := s.Store.CreateOpaqueCredential(ctx, principal, req)
	if err != nil {
		return OpaqueCredentialMaterial{}, err
	}
	span.SetAttributes(
		attribute.String("forge_metal.org_id", principal.OrgID),
		attribute.String("forge_metal.credential_id", material.Credential.CredentialID),
		attribute.String("forge_metal.credential_kind", material.Credential.Kind),
	)
	return material, nil
}

func (s *Service) GetOpaqueCredential(ctx context.Context, principal Principal, credentialID string) (OpaqueCredential, error) {
	ctx, span := tracer.Start(ctx, "secrets.credential.read")
	defer span.End()
	if err := s.Validate(); err != nil {
		return OpaqueCredential{}, err
	}
	credential, err := s.Store.GetOpaqueCredential(ctx, principal, credentialID)
	if err != nil {
		return OpaqueCredential{}, err
	}
	span.SetAttributes(
		attribute.String("forge_metal.org_id", principal.OrgID),
		attribute.String("forge_metal.credential_id", credential.CredentialID),
		attribute.String("forge_metal.credential_kind", credential.Kind),
	)
	return credential, nil
}

func (s *Service) ListOpaqueCredentials(ctx context.Context, principal Principal, kind string, limit int) ([]OpaqueCredential, error) {
	ctx, span := tracer.Start(ctx, "secrets.credential.list")
	defer span.End()
	if err := s.Validate(); err != nil {
		return nil, err
	}
	credentials, err := s.Store.ListOpaqueCredentials(ctx, principal, kind, limit)
	if err != nil {
		return nil, err
	}
	span.SetAttributes(
		attribute.String("forge_metal.org_id", principal.OrgID),
		attribute.String("forge_metal.credential_kind", strings.TrimSpace(kind)),
		attribute.Int("forge_metal.credential_count", len(credentials)),
	)
	return credentials, nil
}

func (s *Service) RollOpaqueCredential(ctx context.Context, principal Principal, req RollOpaqueCredentialRequest) (OpaqueCredentialMaterial, error) {
	ctx, span := tracer.Start(ctx, "secrets.credential.roll")
	defer span.End()
	if err := s.Validate(); err != nil {
		return OpaqueCredentialMaterial{}, err
	}
	material, err := s.Store.RollOpaqueCredential(ctx, principal, req)
	if err != nil {
		return OpaqueCredentialMaterial{}, err
	}
	span.SetAttributes(
		attribute.String("forge_metal.org_id", principal.OrgID),
		attribute.String("forge_metal.credential_id", material.Credential.CredentialID),
		attribute.String("forge_metal.credential_kind", material.Credential.Kind),
	)
	return material, nil
}

func (s *Service) RevokeOpaqueCredential(ctx context.Context, principal Principal, credentialID string) (OpaqueCredential, error) {
	ctx, span := tracer.Start(ctx, "secrets.credential.revoke")
	defer span.End()
	if err := s.Validate(); err != nil {
		return OpaqueCredential{}, err
	}
	credential, err := s.Store.RevokeOpaqueCredential(ctx, principal, credentialID)
	if err != nil {
		return OpaqueCredential{}, err
	}
	span.SetAttributes(
		attribute.String("forge_metal.org_id", principal.OrgID),
		attribute.String("forge_metal.credential_id", credential.CredentialID),
		attribute.String("forge_metal.credential_kind", credential.Kind),
	)
	return credential, nil
}

func (s *Service) VerifyOpaqueCredential(ctx context.Context, principal Principal, req VerifyOpaqueCredentialRequest) (VerifyOpaqueCredentialResult, error) {
	ctx, span := tracer.Start(ctx, "secrets.credential.verify")
	defer span.End()
	if err := s.Validate(); err != nil {
		return VerifyOpaqueCredentialResult{}, err
	}
	result, err := s.Store.VerifyOpaqueCredential(ctx, principal, req)
	if err != nil {
		return VerifyOpaqueCredentialResult{}, err
	}
	span.SetAttributes(
		attribute.String("forge_metal.org_id", principal.OrgID),
		attribute.String("forge_metal.credential_id", result.Credential.CredentialID),
		attribute.String("forge_metal.credential_kind", result.Credential.Kind),
		attribute.Bool("forge_metal.credential_active", result.Active),
	)
	return result, nil
}

func (s *BaoStore) CreateOpaqueCredential(ctx context.Context, principal Principal, req CreateOpaqueCredentialRequest) (OpaqueCredentialMaterial, error) {
	if err := validatePrincipal(principal); err != nil {
		return OpaqueCredentialMaterial{}, err
	}
	req, err := normalizeCreateOpaqueCredential(req, principal)
	if err != nil {
		return OpaqueCredentialMaterial{}, err
	}
	entry, err := s.token(ctx, principal)
	if err != nil {
		return OpaqueCredentialMaterial{}, err
	}
	if err := s.ensureCredentialHMACKey(ctx, entry, principal.OrgID); err != nil {
		return OpaqueCredentialMaterial{}, err
	}
	credentialID := uuid.NewString()
	token, tokenPrefix, err := newOpaqueCredentialToken(credentialID)
	if err != nil {
		return OpaqueCredentialMaterial{}, err
	}
	tokenHMAC, hmacVersion, err := s.credentialHMAC(ctx, entry, principal.OrgID, token)
	if err != nil {
		return OpaqueCredentialMaterial{}, err
	}
	now := time.Now().UTC()
	doc := credentialDocument{
		CredentialID:   credentialID,
		OrgID:          principal.OrgID,
		Kind:           req.Kind,
		Subject:        req.Subject,
		DisplayName:    req.DisplayName,
		Status:         CredentialStatusActive,
		TokenPrefix:    tokenPrefix,
		TokenHMAC:      tokenHMAC,
		Scopes:         req.Scopes,
		Metadata:       req.Metadata,
		CurrentVersion: hmacVersion,
		ExpiresAt:      req.ExpiresAt,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	record, err := s.writeCredentialDocument(ctx, entry, principal.OrgID, doc)
	if err != nil {
		return OpaqueCredentialMaterial{}, err
	}
	return OpaqueCredentialMaterial{Credential: record, Token: token}, nil
}

func (s *BaoStore) GetOpaqueCredential(ctx context.Context, principal Principal, credentialID string) (OpaqueCredential, error) {
	if err := validatePrincipal(principal); err != nil {
		return OpaqueCredential{}, err
	}
	credentialID = strings.TrimSpace(credentialID)
	if _, err := uuid.Parse(credentialID); err != nil {
		return OpaqueCredential{}, fmt.Errorf("%w: credential_id must be a UUID", ErrInvalidArgument)
	}
	entry, err := s.token(ctx, principal)
	if err != nil {
		return OpaqueCredential{}, err
	}
	doc, err := s.readCredentialDocument(ctx, entry, principal.OrgID, credentialID)
	if err != nil {
		return OpaqueCredential{}, err
	}
	return doc.record(), nil
}

func (s *BaoStore) ListOpaqueCredentials(ctx context.Context, principal Principal, kind string, limit int) ([]OpaqueCredential, error) {
	if err := validatePrincipal(principal); err != nil {
		return nil, err
	}
	kind = normalizeCredentialKind(kind)
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	entry, err := s.token(ctx, principal)
	if err != nil {
		return nil, err
	}
	mount := s.kvMount(principal.OrgID)
	recordOpenBaoAuditInfo(ctx, mount, "", entry.accessorHash, 0)
	paths, err := s.listSecretPaths(ctx, entry, mount, []string{KindOpaqueCredential, ScopeOrg})
	if err != nil {
		return nil, err
	}
	credentials := make([]OpaqueCredential, 0, len(paths))
	for _, path := range paths {
		segments := splitPath(path)
		if len(segments) != 3 || segments[0] != KindOpaqueCredential || segments[1] != ScopeOrg {
			continue
		}
		doc, err := s.readCredentialDocument(ctx, entry, principal.OrgID, segments[2])
		if err != nil {
			return nil, err
		}
		if kind != "" && doc.Kind != kind {
			continue
		}
		credentials = append(credentials, doc.record())
	}
	sort.Slice(credentials, func(i, j int) bool {
		if credentials[i].UpdatedAt.Equal(credentials[j].UpdatedAt) {
			return credentials[i].CredentialID > credentials[j].CredentialID
		}
		return credentials[i].UpdatedAt.After(credentials[j].UpdatedAt)
	})
	if len(credentials) > limit {
		credentials = credentials[:limit]
	}
	return credentials, nil
}

func (s *BaoStore) RollOpaqueCredential(ctx context.Context, principal Principal, req RollOpaqueCredentialRequest) (OpaqueCredentialMaterial, error) {
	if err := validatePrincipal(principal); err != nil {
		return OpaqueCredentialMaterial{}, err
	}
	req.CredentialID = strings.TrimSpace(req.CredentialID)
	if _, err := uuid.Parse(req.CredentialID); err != nil {
		return OpaqueCredentialMaterial{}, fmt.Errorf("%w: credential_id must be a UUID", ErrInvalidArgument)
	}
	entry, err := s.token(ctx, principal)
	if err != nil {
		return OpaqueCredentialMaterial{}, err
	}
	if err := s.ensureCredentialHMACKey(ctx, entry, principal.OrgID); err != nil {
		return OpaqueCredentialMaterial{}, err
	}
	doc, err := s.readCredentialDocument(ctx, entry, principal.OrgID, req.CredentialID)
	if err != nil {
		return OpaqueCredentialMaterial{}, err
	}
	if doc.Status != CredentialStatusActive {
		return OpaqueCredentialMaterial{}, fmt.Errorf("%w: credential is not active", ErrConflict)
	}
	token, tokenPrefix, err := newOpaqueCredentialToken(doc.CredentialID)
	if err != nil {
		return OpaqueCredentialMaterial{}, err
	}
	tokenHMAC, hmacVersion, err := s.credentialHMAC(ctx, entry, principal.OrgID, token)
	if err != nil {
		return OpaqueCredentialMaterial{}, err
	}
	now := time.Now().UTC()
	doc.TokenPrefix = tokenPrefix
	doc.TokenHMAC = tokenHMAC
	doc.CurrentVersion = hmacVersion
	doc.UpdatedAt = now
	if !req.ExpiresAt.IsZero() {
		doc.ExpiresAt = req.ExpiresAt.UTC()
	}
	record, err := s.writeCredentialDocument(ctx, entry, principal.OrgID, doc)
	if err != nil {
		return OpaqueCredentialMaterial{}, err
	}
	return OpaqueCredentialMaterial{Credential: record, Token: token}, nil
}

func (s *BaoStore) RevokeOpaqueCredential(ctx context.Context, principal Principal, credentialID string) (OpaqueCredential, error) {
	if err := validatePrincipal(principal); err != nil {
		return OpaqueCredential{}, err
	}
	credentialID = strings.TrimSpace(credentialID)
	if _, err := uuid.Parse(credentialID); err != nil {
		return OpaqueCredential{}, fmt.Errorf("%w: credential_id must be a UUID", ErrInvalidArgument)
	}
	entry, err := s.token(ctx, principal)
	if err != nil {
		return OpaqueCredential{}, err
	}
	doc, err := s.readCredentialDocument(ctx, entry, principal.OrgID, credentialID)
	if err != nil {
		return OpaqueCredential{}, err
	}
	now := time.Now().UTC()
	doc.Status = CredentialStatusRevoked
	doc.UpdatedAt = now
	doc.RevokedAt = &now
	return s.writeCredentialDocument(ctx, entry, principal.OrgID, doc)
}

func (s *BaoStore) VerifyOpaqueCredential(ctx context.Context, principal Principal, req VerifyOpaqueCredentialRequest) (VerifyOpaqueCredentialResult, error) {
	if err := validatePrincipal(principal); err != nil {
		return VerifyOpaqueCredentialResult{}, err
	}
	req.Kind = normalizeCredentialKind(req.Kind)
	req.Token = strings.TrimSpace(req.Token)
	if req.Kind == "" || req.Token == "" {
		return VerifyOpaqueCredentialResult{DenialReason: "invalid_request"}, nil
	}
	requiredScopes, err := normalizeCredentialScopes(req.RequiredScopes)
	if err != nil {
		return VerifyOpaqueCredentialResult{}, err
	}
	credentialID, err := credentialIDFromOpaqueToken(req.Token)
	if err != nil {
		return VerifyOpaqueCredentialResult{DenialReason: "malformed_token"}, nil
	}
	entry, err := s.token(ctx, principal)
	if err != nil {
		return VerifyOpaqueCredentialResult{}, err
	}
	doc, err := s.readCredentialDocument(ctx, entry, principal.OrgID, credentialID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return VerifyOpaqueCredentialResult{DenialReason: "not_found"}, nil
		}
		return VerifyOpaqueCredentialResult{}, err
	}
	result := VerifyOpaqueCredentialResult{Credential: doc.record()}
	now := time.Now().UTC()
	switch {
	case doc.Kind != req.Kind:
		result.DenialReason = "kind_mismatch"
		return result, nil
	case doc.Status != CredentialStatusActive:
		result.DenialReason = "inactive"
		return result, nil
	case !doc.ExpiresAt.IsZero() && !doc.ExpiresAt.After(now):
		result.DenialReason = "expired"
		return result, nil
	case !credentialHasScopes(doc.Scopes, requiredScopes):
		result.DenialReason = "scope_denied"
		return result, nil
	}
	valid, err := s.credentialVerifyHMAC(ctx, entry, principal.OrgID, req.Token, doc.TokenHMAC)
	if err != nil {
		return VerifyOpaqueCredentialResult{}, err
	}
	if !valid {
		result.DenialReason = "hmac_mismatch"
		return result, nil
	}
	doc.LastUsedAt = &now
	doc.UpdatedAt = now
	updated, err := s.writeCredentialDocument(ctx, entry, principal.OrgID, doc)
	if err != nil {
		return VerifyOpaqueCredentialResult{}, err
	}
	result.Active = true
	result.Credential = updated
	return result, nil
}

func (s *BaoStore) ensureCredentialHMACKey(ctx context.Context, entry baoTokenEntry, orgID string) error {
	mount := s.transitMount(orgID)
	if _, _, err := s.doBao(ctx, "secrets.bao.transit.metadata_read", http.MethodGet, mount, "keys", []string{credentialHMACKey}, entry, nil, http.StatusOK); err == nil {
		return nil
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	// OpenBao requires an explicit HMAC key size; omitting it fails at runtime on 2.3.x.
	_, _, err := s.doBao(ctx, "secrets.bao.transit.create", http.MethodPost, mount, "keys", []string{credentialHMACKey}, entry, map[string]any{"type": "hmac", "key_size": 32}, http.StatusNoContent, http.StatusOK)
	return err
}

func (s *BaoStore) credentialHMAC(ctx context.Context, entry baoTokenEntry, orgID string, token string) (string, uint64, error) {
	mount := s.transitMount(orgID)
	response, _, err := s.doBao(ctx, "secrets.bao.transit.hmac", http.MethodPost, mount, "hmac", []string{credentialHMACKey, "sha2-256"}, entry, map[string]any{
		"input": base64.StdEncoding.EncodeToString([]byte(token)),
	}, http.StatusOK)
	if err != nil {
		return "", 0, err
	}
	value := stringFrom(response.Data, "hmac")
	version := versionFromVaultValue(value)
	recordOpenBaoAuditInfo(ctx, mount, "", entry.accessorHash, version)
	if value == "" || version == 0 {
		return "", 0, fmt.Errorf("%w: openbao hmac response missing digest", ErrStore)
	}
	return value, version, nil
}

func (s *BaoStore) credentialVerifyHMAC(ctx context.Context, entry baoTokenEntry, orgID string, token string, tokenHMAC string) (bool, error) {
	mount := s.transitMount(orgID)
	response, _, err := s.doBao(ctx, "secrets.bao.transit.verify_hmac", http.MethodPost, mount, "verify", []string{credentialHMACKey, "sha2-256"}, entry, map[string]any{
		"input": base64.StdEncoding.EncodeToString([]byte(token)),
		"hmac":  tokenHMAC,
	}, http.StatusOK)
	if err != nil {
		return false, err
	}
	recordOpenBaoAuditInfo(ctx, mount, "", entry.accessorHash, versionFromVaultValue(tokenHMAC))
	return boolFrom(response.Data, "valid"), nil
}

func (s *BaoStore) writeCredentialDocument(ctx context.Context, entry baoTokenEntry, orgID string, doc credentialDocument) (OpaqueCredential, error) {
	document := map[string]any{
		"credential_id":   doc.CredentialID,
		"org_id":          doc.OrgID,
		"kind":            doc.Kind,
		"subject":         doc.Subject,
		"display_name":    doc.DisplayName,
		"status":          doc.Status,
		"token_prefix":    doc.TokenPrefix,
		"token_hmac":      doc.TokenHMAC,
		"scopes":          doc.Scopes,
		"metadata":        doc.Metadata,
		"current_version": doc.CurrentVersion,
		"expires_at":      formatOptionalTime(doc.ExpiresAt),
		"created_at":      doc.CreatedAt.Format(time.RFC3339Nano),
		"updated_at":      doc.UpdatedAt.Format(time.RFC3339Nano),
		"last_used_at":    formatOptionalTimePtr(doc.LastUsedAt),
		"revoked_at":      formatOptionalTimePtr(doc.RevokedAt),
	}
	mount := s.kvMount(orgID)
	response, _, err := s.doBao(ctx, "secrets.bao.kv.put", http.MethodPost, mount, "data", credentialPath(doc.CredentialID), entry, map[string]any{"data": document}, http.StatusOK, http.StatusNoContent)
	if err != nil {
		return OpaqueCredential{}, err
	}
	version := uint64From(responseDataMap(response.Data, "data"), "version")
	if version == 0 {
		version = uint64From(response.Data, "version")
	}
	if version > 0 {
		doc.CurrentVersion = version
	}
	return doc.record(), nil
}

func (s *BaoStore) readCredentialDocument(ctx context.Context, entry baoTokenEntry, orgID string, credentialID string) (credentialDocument, error) {
	mount := s.kvMount(orgID)
	recordOpenBaoAuditInfo(ctx, mount, "", entry.accessorHash, 0)
	response, _, err := s.doBao(ctx, "secrets.bao.kv.get", http.MethodGet, mount, "data", credentialPath(credentialID), entry, nil, http.StatusOK)
	if err != nil {
		return credentialDocument{}, err
	}
	return credentialDocumentFromKV(orgID, responseDataMap(response.Data, "data"), responseDataMap(response.Data, "metadata"))
}

func normalizeCreateOpaqueCredential(req CreateOpaqueCredentialRequest, principal Principal) (CreateOpaqueCredentialRequest, error) {
	req.Kind = normalizeCredentialKind(req.Kind)
	req.Subject = strings.TrimSpace(req.Subject)
	if req.Subject == "" {
		req.Subject = strings.TrimSpace(principal.Subject)
	}
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	if req.DisplayName == "" {
		req.DisplayName = req.Kind
	}
	if req.Kind == "" || req.Subject == "" {
		return CreateOpaqueCredentialRequest{}, fmt.Errorf("%w: credential kind and subject are required", ErrInvalidArgument)
	}
	if err := validateResourceName(req.Kind); err != nil {
		return CreateOpaqueCredentialRequest{}, err
	}
	if len(req.DisplayName) > 128 || len(req.Subject) > 255 {
		return CreateOpaqueCredentialRequest{}, fmt.Errorf("%w: credential display name or subject is too long", ErrInvalidArgument)
	}
	scopes, err := normalizeCredentialScopes(req.Scopes)
	if err != nil {
		return CreateOpaqueCredentialRequest{}, err
	}
	req.Scopes = scopes
	metadata, err := normalizeCredentialMetadata(req.Metadata)
	if err != nil {
		return CreateOpaqueCredentialRequest{}, err
	}
	req.Metadata = metadata
	if req.ExpiresAt.IsZero() {
		req.ExpiresAt = time.Now().UTC().Add(30 * 24 * time.Hour)
	}
	if !req.ExpiresAt.After(time.Now().UTC().Add(time.Minute)) {
		return CreateOpaqueCredentialRequest{}, fmt.Errorf("%w: credential expiry must be in the future", ErrInvalidArgument)
	}
	return req, nil
}

func normalizeCredentialKind(kind string) string {
	return strings.TrimSpace(strings.ToLower(kind))
}

func normalizeCredentialScopes(scopes []string) ([]string, error) {
	if len(scopes) == 0 {
		return nil, fmt.Errorf("%w: at least one credential scope is required", ErrInvalidArgument)
	}
	out := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if scope == "" || len(scope) > 128 || strings.ContainsAny(scope, "\x00\r\n\t") {
			return nil, fmt.Errorf("%w: invalid credential scope", ErrInvalidArgument)
		}
		out = append(out, scope)
	}
	sort.Strings(out)
	return compactCredentialStrings(out), nil
}

func normalizeCredentialMetadata(metadata map[string]string) (map[string]string, error) {
	if len(metadata) == 0 {
		return map[string]string{}, nil
	}
	out := make(map[string]string, len(metadata))
	for key, value := range metadata {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || len(key) > 128 || len(value) > 1024 || strings.ContainsAny(key, "\x00\r\n\t") {
			return nil, fmt.Errorf("%w: invalid credential metadata", ErrInvalidArgument)
		}
		out[key] = value
	}
	return out, nil
}

func compactCredentialStrings(values []string) []string {
	out := values[:0]
	var previous string
	for _, value := range values {
		if value == "" || value == previous {
			continue
		}
		out = append(out, value)
		previous = value
	}
	return out
}

func credentialHasScopes(actual []string, required []string) bool {
	if len(required) == 0 {
		return true
	}
	set := make(map[string]struct{}, len(actual))
	for _, scope := range actual {
		set[scope] = struct{}{}
	}
	for _, scope := range required {
		if _, ok := set[scope]; !ok {
			return false
		}
	}
	return true
}

func newOpaqueCredentialToken(credentialID string) (string, string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("%w: generate credential material: %v", ErrCrypto, err)
	}
	token := strings.Join([]string{credentialTokenPrefix, credentialID, base64.RawURLEncoding.EncodeToString(raw)}, "_")
	return token, credentialTokenPrefix + "_" + credentialID[:8], nil
}

func credentialIDFromOpaqueToken(token string) (string, error) {
	parts := strings.SplitN(strings.TrimSpace(token), "_", 3)
	if len(parts) != 3 || parts[0] != credentialTokenPrefix || parts[1] == "" || parts[2] == "" {
		return "", fmt.Errorf("%w: malformed credential token", ErrInvalidArgument)
	}
	if _, err := uuid.Parse(parts[1]); err != nil {
		return "", fmt.Errorf("%w: malformed credential id", ErrInvalidArgument)
	}
	return parts[1], nil
}

func credentialPath(credentialID string) []string {
	return []string{KindOpaqueCredential, ScopeOrg, strings.TrimSpace(credentialID)}
}

func credentialDocumentFromKV(orgID string, data, metadata map[string]any) (credentialDocument, error) {
	doc := credentialDocument{
		CredentialID:   stringFrom(data, "credential_id"),
		OrgID:          firstNonEmpty(stringFrom(data, "org_id"), orgID),
		Kind:           normalizeCredentialKind(stringFrom(data, "kind")),
		Subject:        stringFrom(data, "subject"),
		DisplayName:    stringFrom(data, "display_name"),
		Status:         stringFrom(data, "status"),
		TokenPrefix:    stringFrom(data, "token_prefix"),
		TokenHMAC:      stringFrom(data, "token_hmac"),
		Scopes:         stringListFromAny(data["scopes"]),
		Metadata:       stringMapFromAny(data["metadata"]),
		CurrentVersion: uint64From(metadata, "version"),
		ExpiresAt:      parseTime(stringFrom(data, "expires_at")),
		CreatedAt:      parseTime(firstNonEmpty(stringFrom(data, "created_at"), stringFrom(metadata, "created_time"))),
		UpdatedAt:      parseTime(firstNonEmpty(stringFrom(data, "updated_at"), stringFrom(metadata, "updated_time"))),
	}
	if doc.CurrentVersion == 0 {
		doc.CurrentVersion = uint64From(data, "current_version")
	}
	if parsed := parseTime(stringFrom(data, "last_used_at")); !parsed.IsZero() {
		doc.LastUsedAt = &parsed
	}
	if parsed := parseTime(stringFrom(data, "revoked_at")); !parsed.IsZero() {
		doc.RevokedAt = &parsed
	}
	if doc.CredentialID == "" || doc.OrgID == "" || doc.Kind == "" || doc.Subject == "" || doc.Status == "" || doc.TokenHMAC == "" {
		return credentialDocument{}, fmt.Errorf("%w: malformed openbao credential document", ErrStore)
	}
	if doc.CreatedAt.IsZero() {
		doc.CreatedAt = time.Now().UTC()
	}
	if doc.UpdatedAt.IsZero() {
		doc.UpdatedAt = doc.CreatedAt
	}
	if doc.Metadata == nil {
		doc.Metadata = map[string]string{}
	}
	return doc, nil
}

func (d credentialDocument) record() OpaqueCredential {
	credential := OpaqueCredential{
		CredentialID:   d.CredentialID,
		OrgID:          d.OrgID,
		Kind:           d.Kind,
		Subject:        d.Subject,
		DisplayName:    d.DisplayName,
		Status:         d.Status,
		TokenPrefix:    d.TokenPrefix,
		Scopes:         append([]string(nil), d.Scopes...),
		Metadata:       copyStringMap(d.Metadata),
		CurrentVersion: d.CurrentVersion,
		ExpiresAt:      d.ExpiresAt,
		LastUsedAt:     d.LastUsedAt,
		CreatedAt:      d.CreatedAt,
		UpdatedAt:      d.UpdatedAt,
		RevokedAt:      d.RevokedAt,
	}
	return credential
}

func stringListFromAny(value any) []string {
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				out = append(out, strings.TrimSpace(text))
			}
		}
		return out
	default:
		return nil
	}
}

func stringMapFromAny(value any) map[string]string {
	switch typed := value.(type) {
	case map[string]string:
		return copyStringMap(typed)
	case map[string]any:
		out := make(map[string]string, len(typed))
		for key, value := range typed {
			if text, ok := value.(string); ok {
				out[key] = text
			}
		}
		return out
	default:
		return nil
	}
}

func copyStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func formatOptionalTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func formatOptionalTimePtr(value *time.Time) string {
	if value == nil {
		return ""
	}
	return formatOptionalTime(*value)
}
