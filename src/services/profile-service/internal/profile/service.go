package profile

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type Store interface {
	Ready(ctx context.Context) error
	Snapshot(ctx context.Context, principal Principal) (Snapshot, error)
	UpdateIdentity(ctx context.Context, principal Principal, input UpdateIdentityRequest, bearerToken string, writer IdentityWriter) (Snapshot, []string, error)
	PutPreferences(ctx context.Context, principal Principal, input PutPreferencesRequest) (Snapshot, []string, error)
	OrgExport(ctx context.Context, input DataRightsRequest) (DataRightsManifest, error)
	SubjectExport(ctx context.Context, input DataRightsRequest) (DataRightsManifest, error)
	SubjectErasure(ctx context.Context, input DataRightsRequest) (DataRightsManifest, error)
	DataRightsStatus(ctx context.Context, requestID string) (DataRightsManifest, error)
}

type Service struct {
	Store    Store
	Identity IdentityWriter
	Now      func() time.Time
}

func (s *Service) Ready(ctx context.Context) error {
	store, err := s.store()
	if err != nil {
		return err
	}
	return store.Ready(ctx)
}

func (s *Service) Snapshot(ctx context.Context, principal Principal) (Snapshot, error) {
	if err := ValidatePrincipal(principal); err != nil {
		return Snapshot{}, err
	}
	store, err := s.store()
	if err != nil {
		return Snapshot{}, err
	}
	return store.Snapshot(ctx, principal)
}

func (s *Service) UpdateIdentity(ctx context.Context, principal Principal, input UpdateIdentityRequest, bearerToken string) (Snapshot, []string, error) {
	if err := ValidatePrincipal(principal); err != nil {
		return Snapshot{}, nil, err
	}
	input, err := NormalizeIdentityInput(input)
	if err != nil {
		return Snapshot{}, nil, err
	}
	if s.Identity == nil {
		return Snapshot{}, nil, ErrIdentityUnavailable
	}
	if bearerToken == "" {
		return Snapshot{}, nil, fmt.Errorf("%w: bearer token is required for identity update", ErrInvalidInput)
	}
	store, err := s.store()
	if err != nil {
		return Snapshot{}, nil, err
	}
	return store.UpdateIdentity(ctx, principal, input, bearerToken, s.Identity)
}

func (s *Service) PutPreferences(ctx context.Context, principal Principal, input PutPreferencesRequest) (Snapshot, []string, error) {
	if err := ValidatePrincipal(principal); err != nil {
		return Snapshot{}, nil, err
	}
	input, err := NormalizePreferencesInput(input)
	if err != nil {
		return Snapshot{}, nil, err
	}
	store, err := s.store()
	if err != nil {
		return Snapshot{}, nil, err
	}
	return store.PutPreferences(ctx, principal, input)
}

func (s *Service) OrgExport(ctx context.Context, input DataRightsRequest) (DataRightsManifest, error) {
	if err := input.validate("org_export"); err != nil {
		return DataRightsManifest{}, err
	}
	store, err := s.store()
	if err != nil {
		return DataRightsManifest{}, err
	}
	return store.OrgExport(ctx, input)
}

func (s *Service) SubjectExport(ctx context.Context, input DataRightsRequest) (DataRightsManifest, error) {
	if err := input.validate("subject_export"); err != nil {
		return DataRightsManifest{}, err
	}
	store, err := s.store()
	if err != nil {
		return DataRightsManifest{}, err
	}
	return store.SubjectExport(ctx, input)
}

func (s *Service) SubjectErasure(ctx context.Context, input DataRightsRequest) (DataRightsManifest, error) {
	if err := input.validate("subject_erasure"); err != nil {
		return DataRightsManifest{}, err
	}
	store, err := s.store()
	if err != nil {
		return DataRightsManifest{}, err
	}
	return store.SubjectErasure(ctx, input)
}

func (s *Service) DataRightsStatus(ctx context.Context, requestID string) (DataRightsManifest, error) {
	if requestID == "" {
		return DataRightsManifest{}, fmt.Errorf("%w: request_id is required", ErrInvalidInput)
	}
	store, err := s.store()
	if err != nil {
		return DataRightsManifest{}, err
	}
	return store.DataRightsStatus(ctx, requestID)
}

func (s *Service) store() (Store, error) {
	if s.Store == nil {
		return nil, ErrStoreUnavailable
	}
	return s.Store, nil
}

func IsInvalid(err error) bool {
	return errors.Is(err, ErrInvalidInput)
}
