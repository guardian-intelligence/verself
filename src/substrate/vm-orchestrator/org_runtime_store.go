package vmorchestrator

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var errOrgRuntimeNotReady = errors.New("org runtime not ready")

type orgRuntimeImageState struct {
	ImageRef       string `json:"image_ref"`
	SourceSnapshot string `json:"source_snapshot"`
	SourceDigest   string `json:"source_digest"`
	OrgSnapshot    string `json:"org_snapshot"`
}

type orgRuntimeSnapshot struct {
	OrgID      string
	QuotaBytes uint64
	Images     []orgRuntimeImageState
	ReadyAt    time.Time
	VerifiedAt time.Time
}

func (s *hostStateStore) ensureOrgRuntimeSchema(ctx context.Context) error {
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS org_runtime_state (
				org_id TEXT PRIMARY KEY,
				quota_bytes INTEGER NOT NULL,
				images_json TEXT NOT NULL DEFAULT '[]',
				ready_at_unix_nano INTEGER NOT NULL,
				verified_at_unix_nano INTEGER NOT NULL,
				updated_at_unix_nano INTEGER NOT NULL
			)`,
	} {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("apply org runtime state schema: %w", err)
		}
	}
	return nil
}

func (s *hostStateStore) upsertOrgRuntime(ctx context.Context, snapshot orgRuntimeSnapshot) error {
	if strings.TrimSpace(snapshot.OrgID) == "" {
		return fmt.Errorf("org runtime org id is required")
	}
	quotaBytes, err := int64FromUint64(snapshot.QuotaBytes, "org runtime quota bytes")
	if err != nil {
		return err
	}
	readyAt := snapshot.ReadyAt
	if readyAt.IsZero() {
		readyAt = time.Now().UTC()
	}
	verifiedAt := snapshot.VerifiedAt
	if verifiedAt.IsZero() {
		verifiedAt = readyAt
	}
	hostStateWriteMu.Lock()
	defer hostStateWriteMu.Unlock()
	prior, err := s.getOrgRuntime(ctx, snapshot.OrgID)
	if err != nil && !errors.Is(err, errOrgRuntimeNotReady) {
		return err
	}
	if err == nil {
		snapshot.Images = mergeOrgRuntimeImageState(prior.Images, snapshot.Images)
	} else {
		snapshot.Images = mergeOrgRuntimeImageState(nil, snapshot.Images)
	}
	imagesJSON, err := json.Marshal(snapshot.Images)
	if err != nil {
		return fmt.Errorf("marshal org runtime images: %w", err)
	}
	now := time.Now().UTC().UnixNano()
	_, err = s.db.ExecContext(ctx, `INSERT INTO org_runtime_state (
		org_id, quota_bytes, images_json, ready_at_unix_nano, verified_at_unix_nano, updated_at_unix_nano
	) VALUES (?, ?, ?, ?, ?, ?)
	ON CONFLICT(org_id) DO UPDATE SET
		quota_bytes = excluded.quota_bytes,
		images_json = excluded.images_json,
		ready_at_unix_nano = excluded.ready_at_unix_nano,
		verified_at_unix_nano = excluded.verified_at_unix_nano,
		updated_at_unix_nano = excluded.updated_at_unix_nano`,
		snapshot.OrgID,
		quotaBytes,
		string(imagesJSON),
		readyAt.UnixNano(),
		verifiedAt.UnixNano(),
		now,
	)
	if err != nil {
		return fmt.Errorf("upsert org runtime %s: %w", snapshot.OrgID, err)
	}
	return nil
}

func mergeOrgRuntimeImageState(existing, refreshed []orgRuntimeImageState) []orgRuntimeImageState {
	byRef := make(map[string]orgRuntimeImageState, len(existing)+len(refreshed))
	for _, image := range existing {
		ref := strings.TrimSpace(image.ImageRef)
		if ref == "" {
			continue
		}
		image.ImageRef = ref
		byRef[ref] = image
	}
	for _, image := range refreshed {
		ref := strings.TrimSpace(image.ImageRef)
		if ref == "" {
			continue
		}
		image.ImageRef = ref
		byRef[ref] = image
	}
	out := make([]orgRuntimeImageState, 0, len(byRef))
	for _, image := range byRef {
		out = append(out, image)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ImageRef < out[j].ImageRef
	})
	return out
}

func (s *hostStateStore) getOrgRuntime(ctx context.Context, orgID string) (orgRuntimeSnapshot, error) {
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return orgRuntimeSnapshot{}, errOrgRuntimeNotReady
	}
	row := s.db.QueryRowContext(ctx, `SELECT quota_bytes, images_json, ready_at_unix_nano, verified_at_unix_nano FROM org_runtime_state WHERE org_id = ?`, orgID)
	var (
		quotaBytes                          int64
		imagesJSON                          string
		readyAtUnixNano, verifiedAtUnixNano int64
	)
	if err := row.Scan(&quotaBytes, &imagesJSON, &readyAtUnixNano, &verifiedAtUnixNano); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return orgRuntimeSnapshot{}, errOrgRuntimeNotReady
		}
		return orgRuntimeSnapshot{}, fmt.Errorf("query org runtime %s: %w", orgID, err)
	}
	quota, err := uint64FromNonNegativeInt64(quotaBytes, "org runtime quota bytes")
	if err != nil {
		return orgRuntimeSnapshot{}, err
	}
	var images []orgRuntimeImageState
	if strings.TrimSpace(imagesJSON) != "" {
		if err := json.Unmarshal([]byte(imagesJSON), &images); err != nil {
			return orgRuntimeSnapshot{}, fmt.Errorf("decode org runtime images %s: %w", orgID, err)
		}
	}
	return orgRuntimeSnapshot{
		OrgID:      orgID,
		QuotaBytes: quota,
		Images:     images,
		ReadyAt:    unixNanoTime(readyAtUnixNano),
		VerifiedAt: unixNanoTime(verifiedAtUnixNano),
	}, nil
}
