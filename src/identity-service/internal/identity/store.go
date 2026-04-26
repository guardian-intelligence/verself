package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	chdriver "github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	identitystore "github.com/verself/identity-service/internal/store"
)

type SQLStore struct {
	PG *pgxpool.Pool
	CH chdriver.Conn
}

func (s SQLStore) q() *identitystore.Queries {
	return identitystore.New(s.PG)
}

func (s SQLStore) GetMemberCapabilities(ctx context.Context, orgID, actor string) (MemberCapabilitiesDocument, error) {
	if s.PG == nil {
		return MemberCapabilitiesDocument{}, ErrStoreUnavailable
	}
	row, err := s.q().GetMemberCapabilities(ctx, identitystore.GetMemberCapabilitiesParams{OrgID: orgID})
	if errors.Is(err, pgx.ErrNoRows) {
		return DefaultMemberCapabilitiesDocument(orgID, actor, time.Now().UTC()), nil
	}
	if err != nil {
		return MemberCapabilitiesDocument{}, fmt.Errorf("get identity member capabilities: %w", err)
	}
	return memberCapabilitiesFromRow(row)
}

func (s SQLStore) PutMemberCapabilities(ctx context.Context, doc MemberCapabilitiesDocument) (MemberCapabilitiesDocument, error) {
	if s.PG == nil {
		return MemberCapabilitiesDocument{}, ErrStoreUnavailable
	}
	if err := ValidateMemberCapabilities(doc); err != nil {
		return MemberCapabilitiesDocument{}, err
	}
	var (
		row identitystore.IdentityMemberCapability
		err error
	)
	if doc.Version == 0 {
		row, err = s.q().InsertMemberCapabilities(ctx, identitystore.InsertMemberCapabilitiesParams{
			OrgID:       doc.OrgID,
			EnabledKeys: append([]string(nil), doc.EnabledKeys...),
			UpdatedBy:   doc.UpdatedBy,
		})
	} else {
		row, err = s.q().UpdateMemberCapabilities(ctx, identitystore.UpdateMemberCapabilitiesParams{
			OrgID:       doc.OrgID,
			EnabledKeys: append([]string(nil), doc.EnabledKeys...),
			UpdatedBy:   doc.UpdatedBy,
			Version:     doc.Version,
		})
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return MemberCapabilitiesDocument{}, fmt.Errorf("%w: stale version for org %s", ErrCapabilitiesConflict, doc.OrgID)
	}
	if err != nil {
		return MemberCapabilitiesDocument{}, fmt.Errorf("put identity member capabilities: %w", err)
	}
	return memberCapabilitiesFromRow(row)
}

func memberCapabilitiesFromRow(row identitystore.IdentityMemberCapability) (MemberCapabilitiesDocument, error) {
	updatedAt, err := requiredTime(row.UpdatedAt, "identity_member_capabilities.updated_at")
	if err != nil {
		return MemberCapabilitiesDocument{}, err
	}
	return MemberCapabilitiesDocument{
		OrgID:       row.OrgID,
		Version:     row.Version,
		EnabledKeys: append([]string(nil), row.EnabledKeys...),
		UpdatedAt:   updatedAt,
		UpdatedBy:   row.UpdatedBy,
	}, nil
}

func (s SQLStore) CreateAPICredential(ctx context.Context, credential APICredential, secret APICredentialSecret) (APICredential, error) {
	if s.PG == nil {
		return APICredential{}, ErrStoreUnavailable
	}
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return APICredential{}, fmt.Errorf("begin create api credential: %w", err)
	}
	defer rollback(ctx, tx)
	q := identitystore.New(tx)
	if err := q.InsertAPICredential(ctx, identitystore.InsertAPICredentialParams{
		CredentialID:         credential.CredentialID,
		OrgID:                credential.OrgID,
		SubjectID:            credential.SubjectID,
		ClientID:             credential.ClientID,
		DisplayName:          credential.DisplayName,
		AuthMethod:           string(credential.AuthMethod),
		Status:               string(credential.Status),
		PolicyVersionAtIssue: credential.PolicyVersionAtIssue,
		CreatedAt:            timestamptz(credential.CreatedAt),
		CreatedBy:            credential.CreatedBy,
		ExpiresAt:            nullableTimestamptz(credential.ExpiresAt),
	}); err != nil {
		return APICredential{}, fmt.Errorf("insert api credential: %w", err)
	}
	for _, permission := range credential.Permissions {
		if err := q.InsertAPICredentialPermission(ctx, identitystore.InsertAPICredentialPermissionParams{
			CredentialID: credential.CredentialID,
			Permission:   permission,
			CreatedAt:    timestamptz(credential.CreatedAt),
		}); err != nil {
			return APICredential{}, fmt.Errorf("insert api credential permission: %w", err)
		}
	}
	if err := insertAPICredentialSecret(ctx, q, secret); err != nil {
		return APICredential{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return APICredential{}, fmt.Errorf("commit create api credential: %w", err)
	}
	return s.GetAPICredential(ctx, credential.OrgID, credential.CredentialID)
}

func (s SQLStore) ListAPICredentials(ctx context.Context, orgID string) ([]APICredential, error) {
	if s.PG == nil {
		return nil, ErrStoreUnavailable
	}
	rows, err := s.q().ListAPICredentials(ctx, identitystore.ListAPICredentialsParams{OrgID: orgID})
	if err != nil {
		return nil, fmt.Errorf("list api credentials: %w", err)
	}
	credentials := make([]APICredential, 0, len(rows))
	for _, row := range rows {
		credential, err := apiCredentialFromListRow(row)
		if err != nil {
			return nil, err
		}
		credential.Permissions, err = s.apiCredentialPermissions(ctx, credential.CredentialID)
		if err != nil {
			return nil, err
		}
		credentials = append(credentials, credential)
	}
	return credentials, nil
}

func (s SQLStore) GetAPICredential(ctx context.Context, orgID, credentialID string) (APICredential, error) {
	if s.PG == nil {
		return APICredential{}, ErrStoreUnavailable
	}
	row, err := s.q().GetAPICredential(ctx, identitystore.GetAPICredentialParams{OrgID: orgID, CredentialID: credentialID})
	if errors.Is(err, pgx.ErrNoRows) {
		return APICredential{}, ErrAPICredentialMissing
	}
	if err != nil {
		return APICredential{}, err
	}
	credential, err := apiCredentialFromGetRow(row)
	if err != nil {
		return APICredential{}, err
	}
	credential.Permissions, err = s.apiCredentialPermissions(ctx, credential.CredentialID)
	if err != nil {
		return APICredential{}, err
	}
	return credential, nil
}

func (s SQLStore) ActiveAPICredentialSecrets(ctx context.Context, orgID, credentialID string) ([]APICredentialSecret, error) {
	if s.PG == nil {
		return nil, ErrStoreUnavailable
	}
	rows, err := s.q().ListActiveAPICredentialSecrets(ctx, identitystore.ListActiveAPICredentialSecretsParams{
		OrgID:        orgID,
		CredentialID: credentialID,
	})
	if err != nil {
		return nil, fmt.Errorf("list active api credential secrets: %w", err)
	}
	secrets := make([]APICredentialSecret, 0, len(rows))
	for _, row := range rows {
		secret, err := apiCredentialSecretFromRow(row)
		if err != nil {
			return nil, err
		}
		secrets = append(secrets, secret)
	}
	return secrets, nil
}

func (s SQLStore) AddAPICredentialSecret(ctx context.Context, orgID, credentialID, actor string, secret APICredentialSecret) (APICredential, error) {
	if s.PG == nil {
		return APICredential{}, ErrStoreUnavailable
	}
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return APICredential{}, fmt.Errorf("begin add api credential secret: %w", err)
	}
	defer rollback(ctx, tx)
	q := identitystore.New(tx)
	if err := q.RevokeActiveAPICredentialSecrets(ctx, identitystore.RevokeActiveAPICredentialSecretsParams{
		OrgID:        orgID,
		CredentialID: credentialID,
		RevokedBy:    textParam(actor),
		RevokedAt:    timestamptz(secret.CreatedAt),
	}); err != nil {
		return APICredential{}, fmt.Errorf("revoke previous api credential secrets: %w", err)
	}
	if err := insertAPICredentialSecret(ctx, q, secret); err != nil {
		return APICredential{}, err
	}
	count, err := q.UpdateAPICredentialAfterRoll(ctx, identitystore.UpdateAPICredentialAfterRollParams{
		OrgID:        orgID,
		CredentialID: credentialID,
		AuthMethod:   string(secret.AuthMethod),
		UpdatedAt:    timestamptz(secret.CreatedAt),
	})
	if err != nil {
		return APICredential{}, fmt.Errorf("update api credential after roll: %w", err)
	}
	if count == 0 {
		return APICredential{}, ErrAPICredentialMissing
	}
	if err := tx.Commit(ctx); err != nil {
		return APICredential{}, fmt.Errorf("commit add api credential secret: %w", err)
	}
	return s.GetAPICredential(ctx, orgID, credentialID)
}

func (s SQLStore) RevokeAPICredential(ctx context.Context, orgID, credentialID, actor string, now time.Time) (APICredential, error) {
	if s.PG == nil {
		return APICredential{}, ErrStoreUnavailable
	}
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return APICredential{}, fmt.Errorf("begin revoke api credential: %w", err)
	}
	defer rollback(ctx, tx)
	q := identitystore.New(tx)
	if err := q.RevokeAPICredentialSecrets(ctx, identitystore.RevokeAPICredentialSecretsParams{
		OrgID:        orgID,
		CredentialID: credentialID,
		RevokedBy:    textParam(actor),
		RevokedAt:    timestamptz(now),
	}); err != nil {
		return APICredential{}, fmt.Errorf("revoke api credential secrets: %w", err)
	}
	count, err := q.RevokeAPICredential(ctx, identitystore.RevokeAPICredentialParams{
		OrgID:        orgID,
		CredentialID: credentialID,
		RevokedBy:    textParam(actor),
		RevokedAt:    timestamptz(now),
	})
	if err != nil {
		return APICredential{}, fmt.Errorf("revoke api credential: %w", err)
	}
	if count == 0 {
		return APICredential{}, ErrAPICredentialMissing
	}
	if err := tx.Commit(ctx); err != nil {
		return APICredential{}, fmt.Errorf("commit revoke api credential: %w", err)
	}
	return s.GetAPICredential(ctx, orgID, credentialID)
}

func (s SQLStore) ResolveAPICredentialClaims(ctx context.Context, subjectID string, usedAt time.Time) (ResolveAPICredentialClaimsResult, error) {
	if s.PG == nil {
		return ResolveAPICredentialClaimsResult{}, ErrStoreUnavailable
	}
	row, err := s.q().ResolveAPICredentialClaims(ctx, identitystore.ResolveAPICredentialClaimsParams{
		SubjectID: subjectID,
		UsedAt:    timestamptz(usedAt),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ResolveAPICredentialClaimsResult{}, ErrAPICredentialMissing
	}
	if err != nil {
		return ResolveAPICredentialClaimsResult{}, fmt.Errorf("resolve api credential claims: %w", err)
	}
	result := ResolveAPICredentialClaimsResult{
		CredentialID: row.CredentialID,
		OrgID:        row.OrgID,
		DisplayName:  row.DisplayName,
		AuthMethod:   APICredentialAuthMethod(row.AuthMethod),
		Fingerprint:  row.Fingerprint,
		OwnerID:      row.CreatedBy,
		OwnerDisplay: row.CreatedBy,
	}
	permissions, err := s.apiCredentialPermissions(ctx, result.CredentialID)
	if err != nil {
		return ResolveAPICredentialClaimsResult{}, err
	}
	result.Permissions = permissions
	result.OpenBaoRoles = OpenBaoRolesForPermissions(permissions)
	if err := s.q().RecordAPICredentialUse(ctx, identitystore.RecordAPICredentialUseParams{
		CredentialID: result.CredentialID,
		UsedAt:       timestamptz(usedAt),
	}); err != nil {
		return ResolveAPICredentialClaimsResult{}, fmt.Errorf("record api credential use: %w", err)
	}
	return result, nil
}

func apiCredentialFromGetRow(row identitystore.GetAPICredentialRow) (APICredential, error) {
	return apiCredentialFromFields(
		row.CredentialID,
		row.OrgID,
		row.SubjectID,
		row.ClientID,
		row.DisplayName,
		row.Status,
		row.AuthMethod,
		row.Fingerprint,
		row.PolicyVersionAtIssue,
		row.CreatedAt,
		row.CreatedBy,
		row.UpdatedAt,
		row.ExpiresAt,
		row.RevokedAt,
		row.RevokedBy,
		row.LastUsedAt,
	)
}

func apiCredentialFromListRow(row identitystore.ListAPICredentialsRow) (APICredential, error) {
	return apiCredentialFromFields(
		row.CredentialID,
		row.OrgID,
		row.SubjectID,
		row.ClientID,
		row.DisplayName,
		row.Status,
		row.AuthMethod,
		row.Fingerprint,
		row.PolicyVersionAtIssue,
		row.CreatedAt,
		row.CreatedBy,
		row.UpdatedAt,
		row.ExpiresAt,
		row.RevokedAt,
		row.RevokedBy,
		row.LastUsedAt,
	)
}

func apiCredentialFromFields(credentialID, orgID, subjectID, clientID, displayName, status, method, fingerprint string, policyVersionAtIssue int32, createdAt pgtype.Timestamptz, createdBy string, updatedAt, expiresAt, revokedAt pgtype.Timestamptz, revokedBy string, lastUsedAt pgtype.Timestamptz) (APICredential, error) {
	created, err := requiredTime(createdAt, "identity_api_credentials.created_at")
	if err != nil {
		return APICredential{}, err
	}
	updated, err := requiredTime(updatedAt, "identity_api_credentials.updated_at")
	if err != nil {
		return APICredential{}, err
	}
	return APICredential{
		CredentialID:         credentialID,
		OrgID:                orgID,
		SubjectID:            subjectID,
		ClientID:             clientID,
		DisplayName:          displayName,
		Status:               APICredentialStatus(status),
		AuthMethod:           APICredentialAuthMethod(method),
		Fingerprint:          fingerprint,
		Permissions:          nil,
		PolicyVersionAtIssue: policyVersionAtIssue,
		CreatedAt:            created,
		CreatedBy:            createdBy,
		UpdatedAt:            updated,
		ExpiresAt:            nullableTime(expiresAt),
		RevokedAt:            nullableTime(revokedAt),
		RevokedBy:            revokedBy,
		LastUsedAt:           nullableTime(lastUsedAt),
	}, nil
}

func (s SQLStore) apiCredentialPermissions(ctx context.Context, credentialID string) ([]string, error) {
	permissions, err := s.q().ListAPICredentialPermissions(ctx, identitystore.ListAPICredentialPermissionsParams{CredentialID: credentialID})
	if err != nil {
		return nil, fmt.Errorf("list api credential permissions: %w", err)
	}
	return append([]string(nil), permissions...), nil
}

func insertAPICredentialSecret(ctx context.Context, q *identitystore.Queries, secret APICredentialSecret) error {
	if err := q.InsertAPICredentialSecret(ctx, identitystore.InsertAPICredentialSecretParams{
		SecretID:      secret.SecretID,
		CredentialID:  secret.CredentialID,
		AuthMethod:    string(secret.AuthMethod),
		ProviderKeyID: secret.ProviderKeyID,
		Fingerprint:   secret.Fingerprint,
		SecretHash:    append([]byte(nil), secret.SecretHash...),
		HashAlgorithm: secret.HashAlgorithm,
		CreatedAt:     timestamptz(secret.CreatedAt),
		CreatedBy:     secret.CreatedBy,
		ExpiresAt:     nullableTimestamptz(secret.ExpiresAt),
		RevokedAt:     nullableTimestamptz(secret.RevokedAt),
		RevokedBy:     nullableStringParam(secret.RevokedBy),
	}); err != nil {
		return fmt.Errorf("insert api credential secret: %w", err)
	}
	return nil
}

func apiCredentialSecretFromRow(row identitystore.ListActiveAPICredentialSecretsRow) (APICredentialSecret, error) {
	createdAt, err := requiredTime(row.CreatedAt, "identity_api_credential_secrets.created_at")
	if err != nil {
		return APICredentialSecret{}, err
	}
	return APICredentialSecret{
		SecretID:      row.SecretID,
		CredentialID:  row.CredentialID,
		AuthMethod:    APICredentialAuthMethod(row.AuthMethod),
		ProviderKeyID: row.ProviderKeyID,
		Fingerprint:   row.Fingerprint,
		SecretHash:    append([]byte(nil), row.SecretHash...),
		HashAlgorithm: row.HashAlgorithm,
		CreatedAt:     createdAt,
		CreatedBy:     row.CreatedBy,
		ExpiresAt:     nullableTime(row.ExpiresAt),
		RevokedAt:     nullableTime(row.RevokedAt),
		RevokedBy:     row.RevokedBy,
	}, nil
}

func timestamptz(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}

func nullableTimestamptz(value *time.Time) pgtype.Timestamptz {
	if value == nil {
		return pgtype.Timestamptz{}
	}
	return timestamptz(*value)
}

func requiredTime(value pgtype.Timestamptz, field string) (time.Time, error) {
	if !value.Valid {
		return time.Time{}, fmt.Errorf("%w: %s was null", ErrStoreUnavailable, field)
	}
	return value.Time.UTC(), nil
}

func nullableTime(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	instant := value.Time.UTC()
	return &instant
}

func textParam(value string) pgtype.Text {
	return pgtype.Text{String: value, Valid: true}
}

func nullableStringParam(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func rollback(ctx context.Context, tx pgx.Tx) {
	if tx != nil {
		_ = tx.Rollback(ctx)
	}
}
