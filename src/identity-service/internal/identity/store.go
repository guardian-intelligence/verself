package identity

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type SQLStore struct {
	DB *sql.DB
}

type storedPolicyDocument struct {
	Roles []PolicyRole `json:"roles"`
}

func (s SQLStore) GetPolicy(ctx context.Context, orgID, actor string) (PolicyDocument, error) {
	if s.DB == nil {
		return PolicyDocument{}, ErrStoreUnavailable
	}
	var (
		raw       []byte
		version   int32
		updatedAt time.Time
		updatedBy string
	)
	err := s.DB.QueryRowContext(ctx, `
SELECT document, version, updated_at, updated_by
FROM identity_policy_documents
WHERE org_id = $1
`, orgID).Scan(&raw, &version, &updatedAt, &updatedBy)
	if errors.Is(err, sql.ErrNoRows) {
		policy := DefaultPolicy(orgID, actor)
		policy.UpdatedAt = time.Now().UTC()
		return policy, nil
	}
	if err != nil {
		return PolicyDocument{}, fmt.Errorf("get identity policy: %w", err)
	}
	var stored storedPolicyDocument
	if err := json.Unmarshal(raw, &stored); err != nil {
		return PolicyDocument{}, fmt.Errorf("decode identity policy: %w", err)
	}
	return PolicyDocument{
		OrgID:     orgID,
		Version:   version,
		Roles:     clonePolicyRoles(stored.Roles),
		UpdatedAt: updatedAt,
		UpdatedBy: updatedBy,
	}, nil
}

func (s SQLStore) PutPolicy(ctx context.Context, policy PolicyDocument) (PolicyDocument, error) {
	if s.DB == nil {
		return PolicyDocument{}, ErrStoreUnavailable
	}
	if err := ValidatePolicy(policy); err != nil {
		return PolicyDocument{}, err
	}
	raw, err := json.Marshal(storedPolicyDocument{Roles: clonePolicyRoles(policy.Roles)})
	if err != nil {
		return PolicyDocument{}, fmt.Errorf("encode identity policy: %w", err)
	}
	query := `
UPDATE identity_policy_documents
SET document = $2::jsonb,
    version = version + 1,
    updated_at = now(),
    updated_by = $3
WHERE org_id = $1 AND version = $4
RETURNING version, updated_at
`
	args := []any{policy.OrgID, string(raw), policy.UpdatedBy, policy.Version}
	if policy.Version == 0 {
		query = `
INSERT INTO identity_policy_documents (org_id, document, version, updated_at, updated_by)
VALUES ($1, $2::jsonb, 1, now(), $3)
ON CONFLICT (org_id) DO NOTHING
RETURNING version, updated_at
`
		args = []any{policy.OrgID, string(raw), policy.UpdatedBy}
	}
	err = s.DB.QueryRowContext(ctx, query, args...).Scan(&policy.Version, &policy.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return PolicyDocument{}, fmt.Errorf("%w: stale version for org %s", ErrPolicyConflict, policy.OrgID)
	}
	if err != nil {
		return PolicyDocument{}, fmt.Errorf("put identity policy: %w", err)
	}
	policy.Roles = clonePolicyRoles(policy.Roles)
	return policy, nil
}
