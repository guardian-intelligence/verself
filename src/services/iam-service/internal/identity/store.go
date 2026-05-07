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

	identitystore "github.com/verself/iam-service/internal/store"
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
		row identitystore.IamMemberCapability
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

func memberCapabilitiesFromRow(row identitystore.IamMemberCapability) (MemberCapabilitiesDocument, error) {
	updatedAt, err := requiredTime(row.UpdatedAt, "iam_member_capabilities.updated_at")
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

func (s SQLStore) CreateServiceAccount(ctx context.Context, account ServiceAccount, credential APICredential, secret APICredentialSecret) (ServiceAccount, APICredential, error) {
	if s.PG == nil {
		return ServiceAccount{}, APICredential{}, ErrStoreUnavailable
	}
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ServiceAccount{}, APICredential{}, fmt.Errorf("begin create service account: %w", err)
	}
	defer rollback(ctx, tx)
	q := identitystore.New(tx)
	if err := q.InsertServiceAccount(ctx, identitystore.InsertServiceAccountParams{
		ServiceAccountID: account.ServiceAccountID,
		OrgID:            account.OrgID,
		SubjectID:        account.SubjectID,
		ClientID:         account.ClientID,
		DisplayName:      account.DisplayName,
		Description:      account.Description,
		Status:           string(account.Status),
		CreatedAt:        timestamptz(account.CreatedAt),
		CreatedBy:        account.CreatedBy,
	}); err != nil {
		return ServiceAccount{}, APICredential{}, fmt.Errorf("insert service account: %w", err)
	}
	if err := insertAPICredential(ctx, q, credential); err != nil {
		return ServiceAccount{}, APICredential{}, err
	}
	for _, permission := range credential.Permissions {
		if err := q.InsertAPICredentialPermission(ctx, identitystore.InsertAPICredentialPermissionParams{
			CredentialID: credential.CredentialID,
			Permission:   permission,
			CreatedAt:    timestamptz(credential.CreatedAt),
		}); err != nil {
			return ServiceAccount{}, APICredential{}, fmt.Errorf("insert api credential permission: %w", err)
		}
	}
	if err := insertAPICredentialSecret(ctx, q, secret); err != nil {
		return ServiceAccount{}, APICredential{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ServiceAccount{}, APICredential{}, fmt.Errorf("commit create service account: %w", err)
	}
	createdAccount, err := s.GetServiceAccount(ctx, account.OrgID, account.ServiceAccountID)
	if err != nil {
		return ServiceAccount{}, APICredential{}, err
	}
	createdCredential, err := s.GetAPICredential(ctx, credential.OrgID, credential.CredentialID)
	if err != nil {
		return ServiceAccount{}, APICredential{}, err
	}
	return createdAccount, createdCredential, nil
}

func (s SQLStore) ListServiceAccounts(ctx context.Context, orgID string) ([]ServiceAccount, error) {
	if s.PG == nil {
		return nil, ErrStoreUnavailable
	}
	rows, err := s.q().ListServiceAccounts(ctx, identitystore.ListServiceAccountsParams{OrgID: orgID})
	if err != nil {
		return nil, fmt.Errorf("list service accounts: %w", err)
	}
	accounts := make([]ServiceAccount, 0, len(rows))
	for _, row := range rows {
		account, err := serviceAccountFromListRow(row)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}
	return accounts, nil
}

func (s SQLStore) GetServiceAccount(ctx context.Context, orgID, serviceAccountID string) (ServiceAccount, error) {
	if s.PG == nil {
		return ServiceAccount{}, ErrStoreUnavailable
	}
	row, err := s.q().GetServiceAccount(ctx, identitystore.GetServiceAccountParams{OrgID: orgID, ServiceAccountID: serviceAccountID})
	if errors.Is(err, pgx.ErrNoRows) {
		return ServiceAccount{}, ErrAPICredentialMissing
	}
	if err != nil {
		return ServiceAccount{}, fmt.Errorf("get service account: %w", err)
	}
	return serviceAccountFromGetRow(row)
}

func (s SQLStore) DisableServiceAccount(ctx context.Context, orgID, serviceAccountID, actor string, now time.Time) (ServiceAccount, []APICredential, error) {
	if s.PG == nil {
		return ServiceAccount{}, nil, ErrStoreUnavailable
	}
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ServiceAccount{}, nil, fmt.Errorf("begin disable service account: %w", err)
	}
	defer rollback(ctx, tx)
	q := identitystore.New(tx)
	if err := q.RevokeServiceAccountCredentialSecrets(ctx, identitystore.RevokeServiceAccountCredentialSecretsParams{
		OrgID:            orgID,
		ServiceAccountID: serviceAccountID,
		RevokedBy:        textParam(actor),
		RevokedAt:        timestamptz(now),
	}); err != nil {
		return ServiceAccount{}, nil, fmt.Errorf("revoke service account credential secrets: %w", err)
	}
	if err := q.RevokeServiceAccountCredentials(ctx, identitystore.RevokeServiceAccountCredentialsParams{
		OrgID:            orgID,
		ServiceAccountID: serviceAccountID,
		RevokedBy:        textParam(actor),
		RevokedAt:        timestamptz(now),
	}); err != nil {
		return ServiceAccount{}, nil, fmt.Errorf("revoke service account credentials: %w", err)
	}
	count, err := q.DisableServiceAccount(ctx, identitystore.DisableServiceAccountParams{
		OrgID:            orgID,
		ServiceAccountID: serviceAccountID,
		DisabledBy:       textParam(actor),
		DisabledAt:       timestamptz(now),
	})
	if err != nil {
		return ServiceAccount{}, nil, fmt.Errorf("disable service account: %w", err)
	}
	if count == 0 {
		return ServiceAccount{}, nil, ErrAPICredentialMissing
	}
	if err := tx.Commit(ctx); err != nil {
		return ServiceAccount{}, nil, fmt.Errorf("commit disable service account: %w", err)
	}
	account, err := s.GetServiceAccount(ctx, orgID, serviceAccountID)
	if err != nil {
		return ServiceAccount{}, nil, err
	}
	credentials, err := s.ListAPICredentials(ctx, orgID)
	if err != nil {
		return ServiceAccount{}, nil, err
	}
	accountCredentials := make([]APICredential, 0, len(credentials))
	for _, credential := range credentials {
		if credential.ServiceAccountID == serviceAccountID {
			accountCredentials = append(accountCredentials, credential)
		}
	}
	return account, accountCredentials, nil
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
	if err := insertAPICredential(ctx, q, credential); err != nil {
		return APICredential{}, err
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
		CredentialID:       row.CredentialID,
		ServiceAccountID:   row.ServiceAccountID,
		OrgID:              row.OrgID,
		DisplayName:        row.DisplayName,
		ServiceAccountName: row.ServiceAccountDisplayName,
		AuthMethod:         APICredentialAuthMethod(row.AuthMethod),
		Fingerprint:        row.Fingerprint,
		OwnerID:            row.CreatedBy,
		OwnerDisplay:       row.CreatedBy,
	}
	if err := s.q().RecordAPICredentialUse(ctx, identitystore.RecordAPICredentialUseParams{
		CredentialID: result.CredentialID,
		UsedAt:       timestamptz(usedAt),
	}); err != nil {
		return ResolveAPICredentialClaimsResult{}, fmt.Errorf("record api credential use: %w", err)
	}
	if err := s.q().RecordServiceAccountUse(ctx, identitystore.RecordServiceAccountUseParams{
		ServiceAccountID: result.ServiceAccountID,
		UsedAt:           timestamptz(usedAt),
	}); err != nil {
		return ResolveAPICredentialClaimsResult{}, fmt.Errorf("record service account use: %w", err)
	}
	return result, nil
}

func apiCredentialFromGetRow(row identitystore.GetAPICredentialRow) (APICredential, error) {
	return apiCredentialFromFields(
		row.CredentialID,
		row.ServiceAccountID,
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
		row.ServiceAccountID,
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

func apiCredentialFromFields(credentialID, serviceAccountID, orgID, subjectID, clientID, displayName, status, method, fingerprint string, policyVersionAtIssue int32, createdAt pgtype.Timestamptz, createdBy string, updatedAt, expiresAt, revokedAt pgtype.Timestamptz, revokedBy string, lastUsedAt pgtype.Timestamptz) (APICredential, error) {
	created, err := requiredTime(createdAt, "iam_api_credentials.created_at")
	if err != nil {
		return APICredential{}, err
	}
	updated, err := requiredTime(updatedAt, "iam_api_credentials.updated_at")
	if err != nil {
		return APICredential{}, err
	}
	return APICredential{
		CredentialID:         credentialID,
		ServiceAccountID:     serviceAccountID,
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

func serviceAccountFromGetRow(row identitystore.GetServiceAccountRow) (ServiceAccount, error) {
	return serviceAccountFromFields(
		row.ServiceAccountID,
		row.OrgID,
		row.SubjectID,
		row.ClientID,
		row.DisplayName,
		row.Description,
		row.Status,
		row.CreatedAt,
		row.CreatedBy,
		row.UpdatedAt,
		row.DisabledAt,
		row.DisabledBy,
		row.LastUsedAt,
	)
}

func serviceAccountFromListRow(row identitystore.ListServiceAccountsRow) (ServiceAccount, error) {
	return serviceAccountFromFields(
		row.ServiceAccountID,
		row.OrgID,
		row.SubjectID,
		row.ClientID,
		row.DisplayName,
		row.Description,
		row.Status,
		row.CreatedAt,
		row.CreatedBy,
		row.UpdatedAt,
		row.DisabledAt,
		row.DisabledBy,
		row.LastUsedAt,
	)
}

func serviceAccountFromFields(serviceAccountID, orgID, subjectID, clientID, displayName, description, status string, createdAt pgtype.Timestamptz, createdBy string, updatedAt, disabledAt pgtype.Timestamptz, disabledBy string, lastUsedAt pgtype.Timestamptz) (ServiceAccount, error) {
	created, err := requiredTime(createdAt, "iam_service_accounts.created_at")
	if err != nil {
		return ServiceAccount{}, err
	}
	updated, err := requiredTime(updatedAt, "iam_service_accounts.updated_at")
	if err != nil {
		return ServiceAccount{}, err
	}
	return ServiceAccount{
		ServiceAccountID: serviceAccountID,
		OrgID:            orgID,
		SubjectID:        subjectID,
		ClientID:         clientID,
		DisplayName:      displayName,
		Description:      description,
		Status:           ServiceAccountStatus(status),
		Permissions:      nil,
		CreatedAt:        created,
		CreatedBy:        createdBy,
		UpdatedAt:        updated,
		DisabledAt:       nullableTime(disabledAt),
		DisabledBy:       disabledBy,
		LastUsedAt:       nullableTime(lastUsedAt),
	}, nil
}

func (s SQLStore) apiCredentialPermissions(ctx context.Context, credentialID string) ([]string, error) {
	permissions, err := s.q().ListAPICredentialPermissions(ctx, identitystore.ListAPICredentialPermissionsParams{CredentialID: credentialID})
	if err != nil {
		return nil, fmt.Errorf("list api credential permissions: %w", err)
	}
	return append([]string(nil), permissions...), nil
}

func insertAPICredential(ctx context.Context, q *identitystore.Queries, credential APICredential) error {
	if err := q.InsertAPICredential(ctx, identitystore.InsertAPICredentialParams{
		CredentialID:         credential.CredentialID,
		ServiceAccountID:     credential.ServiceAccountID,
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
		return fmt.Errorf("insert api credential: %w", err)
	}
	return nil
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
	createdAt, err := requiredTime(row.CreatedAt, "iam_api_credential_secrets.created_at")
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
