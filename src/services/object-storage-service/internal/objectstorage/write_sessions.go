package objectstorage

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
)

const installationOrgID = "installation"

var (
	objectNamePattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
	objectNamespacePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
	siteNamePattern        = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
)

func (s *Service) CreateObjectWriteSession(ctx context.Context, input CreateObjectWriteSessionInput) (ObjectWriteSession, error) {
	ctx, span := tracer.Start(ctx, "object_storage.object_write_session.create")
	defer span.End()

	input = normalizeObjectWriteSessionInput(input)
	if err := validateObjectWriteSessionInput(input); err != nil {
		return ObjectWriteSession{}, invalidArgument(span, err.Error())
	}
	if input.Capability != ObjectStorageCapabilityDeploymentArtifacts {
		return ObjectWriteSession{}, invalidArgument(span, "only deployment_artifacts write sessions are implemented")
	}
	if s == nil || s.Store == nil {
		return ObjectWriteSession{}, fmt.Errorf("object-storage store unavailable")
	}
	transfer, ok := s.Provider.(ObjectTransferProvider)
	if !ok || transfer == nil {
		return ObjectWriteSession{}, fmt.Errorf("object-storage provider does not support object transfer sessions")
	}

	if existing, err := s.Store.WriteSessionByIdempotencyKey(ctx, input.Site, input.Capability, input.IdempotencyKey); err == nil {
		if err := requireSameWriteSession(existing, input); err != nil {
			return ObjectWriteSession{}, err
		}
		return s.attachTransferHandles(ctx, transfer, existing)
	} else if !errors.Is(err, ErrNotFound) {
		return ObjectWriteSession{}, err
	}

	bucket, err := s.ensureCapabilityBucket(ctx, input.Capability, input.Actor)
	if err != nil {
		return ObjectWriteSession{}, err
	}
	sessionID, err := NewObjectWriteSessionID()
	if err != nil {
		return ObjectWriteSession{}, err
	}
	now := time.Now().UTC()
	session := ObjectWriteSession{
		SessionID:      sessionID,
		Site:           input.Site,
		Capability:     input.Capability,
		Namespace:      input.Namespace,
		BucketID:       bucket.BucketID,
		IdempotencyKey: input.IdempotencyKey,
		Status:         ObjectWriteSessionStatusOpen,
		ExpiresAt:      now.Add(input.TTL),
		CreatedAt:      now,
		CreatedBy:      input.Actor,
		Objects:        make([]ObjectWriteSessionObject, 0, len(input.Objects)),
	}
	for _, requested := range sortedObjectWriteRequests(input.Objects) {
		objectKey := deploymentArtifactObjectKey(input.Site, input.Namespace, requested)
		action := ObjectWriteActionPut
		if err := transfer.VerifyObject(ctx, bucket.BucketName, objectKey, requested.SHA256, requested.SizeBytes); err == nil {
			action = ObjectWriteActionPresent
		} else if !errors.Is(err, ErrNotFound) {
			return ObjectWriteSession{}, err
		}
		session.Objects = append(session.Objects, ObjectWriteSessionObject{
			SessionID: session.SessionID,
			Name:      requested.Name,
			ObjectKey: objectKey,
			SHA256:    requested.SHA256,
			SizeBytes: requested.SizeBytes,
			Action:    action,
		})
	}
	if err := s.Store.CreateWriteSession(ctx, session); err != nil {
		if errors.Is(err, ErrConflict) {
			existing, lookupErr := s.Store.WriteSessionByIdempotencyKey(ctx, input.Site, input.Capability, input.IdempotencyKey)
			if lookupErr == nil {
				if sameErr := requireSameWriteSession(existing, input); sameErr != nil {
					return ObjectWriteSession{}, sameErr
				}
				return s.attachTransferHandles(ctx, transfer, existing)
			}
		}
		return ObjectWriteSession{}, err
	}
	return s.attachTransferHandles(ctx, transfer, session)
}

func (s *Service) CompleteObjectWriteSession(ctx context.Context, site, sessionID string) (ObjectWriteSession, error) {
	ctx, span := tracer.Start(ctx, "object_storage.object_write_session.complete")
	defer span.End()

	site = strings.TrimSpace(site)
	sessionID = strings.TrimSpace(sessionID)
	if !siteNamePattern.MatchString(site) || sessionID == "" {
		return ObjectWriteSession{}, invalidArgument(span, "site and session_id are required")
	}
	if s == nil || s.Store == nil {
		return ObjectWriteSession{}, fmt.Errorf("object-storage store unavailable")
	}
	transfer, ok := s.Provider.(ObjectTransferProvider)
	if !ok || transfer == nil {
		return ObjectWriteSession{}, fmt.Errorf("object-storage provider does not support object transfer sessions")
	}
	session, err := s.Store.WriteSessionByID(ctx, site, sessionID)
	if err != nil {
		return ObjectWriteSession{}, err
	}
	if session.Status == ObjectWriteSessionStatusCompleted {
		return s.attachTransferHandles(ctx, transfer, session)
	}
	now := time.Now().UTC()
	if !now.Before(session.ExpiresAt) {
		return ObjectWriteSession{}, fmt.Errorf("complete write session: %w", ErrConflict)
	}
	bucket, err := s.Store.BucketByID(ctx, session.BucketID)
	if err != nil {
		return ObjectWriteSession{}, err
	}
	for _, object := range session.Objects {
		if err := transfer.VerifyObject(ctx, bucket.BucketName, object.ObjectKey, object.SHA256, object.SizeBytes); err != nil {
			return ObjectWriteSession{}, fmt.Errorf("%s: %w", object.Name, err)
		}
	}
	completed, err := s.Store.CompleteWriteSession(ctx, site, sessionID, now)
	if errors.Is(err, ErrNotFound) {
		completed, err = s.Store.WriteSessionByID(ctx, site, sessionID)
	}
	if err != nil {
		return ObjectWriteSession{}, err
	}
	return s.attachTransferHandles(ctx, transfer, completed)
}

func (s *Service) ensureCapabilityBucket(ctx context.Context, capability, actor string) (Bucket, error) {
	switch capability {
	case ObjectStorageCapabilityDeploymentArtifacts:
	default:
		return Bucket{}, fmt.Errorf("unsupported object-storage capability %q", capability)
	}
	bucketName := normalizeBucketName(s.Config.DeploymentArtifactsBucket)
	if bucketName == "" {
		return Bucket{}, fmt.Errorf("OBJECT_STORAGE_DEPLOYMENT_ARTIFACTS_BUCKET is required")
	}
	bucket, err := s.Store.BucketByName(ctx, bucketName)
	if err == nil {
		return bucket, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Bucket{}, err
	}
	created, err := s.CreateBucket(ctx, CreateBucketInput{
		OrgID:      installationOrgID,
		BucketName: bucketName,
		Actor:      actor,
	})
	if err == nil {
		return created, nil
	}
	if errors.Is(err, ErrConflict) {
		return s.Store.BucketByName(ctx, bucketName)
	}
	return Bucket{}, err
}

func (s *Service) attachTransferHandles(ctx context.Context, transfer ObjectTransferProvider, session ObjectWriteSession) (ObjectWriteSession, error) {
	if session.Status == ObjectWriteSessionStatusOpen && !time.Now().UTC().Before(session.ExpiresAt) {
		return ObjectWriteSession{}, fmt.Errorf("write session expired: %w", ErrConflict)
	}
	bucket, err := s.Store.BucketByID(ctx, session.BucketID)
	if err != nil {
		return ObjectWriteSession{}, err
	}
	ttl := time.Until(session.ExpiresAt)
	if ttl <= 0 {
		ttl = time.Minute
	}
	for i := range session.Objects {
		object := &session.Objects[i]
		read, err := transfer.PresignObject(ctx, http.MethodGet, bucket.BucketName, object.ObjectKey, awsUnsignedPayload, ttl)
		if err != nil {
			return ObjectWriteSession{}, fmt.Errorf("%s: presign read object: %w", object.Name, err)
		}
		read.SHA256 = object.SHA256
		object.Read = &read
		if session.Status == ObjectWriteSessionStatusOpen && object.Action == ObjectWriteActionPut {
			write, err := transfer.PresignObject(ctx, http.MethodPut, bucket.BucketName, object.ObjectKey, object.SHA256, ttl)
			if err != nil {
				return ObjectWriteSession{}, fmt.Errorf("%s: presign write object: %w", object.Name, err)
			}
			object.Write = &write
		}
	}
	return session, nil
}

func normalizeObjectWriteSessionInput(input CreateObjectWriteSessionInput) CreateObjectWriteSessionInput {
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	input.Site = strings.TrimSpace(input.Site)
	input.Capability = strings.TrimSpace(input.Capability)
	input.Namespace = strings.Trim(strings.TrimSpace(input.Namespace), "/")
	input.Actor = strings.TrimSpace(input.Actor)
	for i := range input.Objects {
		input.Objects[i].Name = strings.Trim(strings.TrimSpace(input.Objects[i].Name), "/")
		input.Objects[i].SHA256 = strings.ToLower(strings.TrimSpace(input.Objects[i].SHA256))
	}
	return input
}

func validateObjectWriteSessionInput(input CreateObjectWriteSessionInput) error {
	if len(input.IdempotencyKey) < 8 || len(input.IdempotencyKey) > 128 {
		return fmt.Errorf("idempotency_key must be 8-128 characters")
	}
	if !siteNamePattern.MatchString(input.Site) {
		return fmt.Errorf("site must match ^[a-z][a-z0-9_-]*$")
	}
	if input.Capability == "" || input.Namespace == "" || input.Actor == "" {
		return fmt.Errorf("capability, namespace, and actor are required")
	}
	if !objectNamespacePattern.MatchString(input.Namespace) {
		return fmt.Errorf("namespace contains unsupported characters")
	}
	if input.TTL < time.Minute || input.TTL > 7*24*time.Hour {
		return fmt.Errorf("ttl must be between 60 and 604800 seconds")
	}
	if len(input.Objects) == 0 {
		return fmt.Errorf("at least one object is required")
	}
	seen := map[string]struct{}{}
	for _, object := range input.Objects {
		if !objectNamePattern.MatchString(object.Name) {
			return fmt.Errorf("object name %q contains unsupported characters", object.Name)
		}
		if _, ok := seen[object.Name]; ok {
			return fmt.Errorf("duplicate object name %q", object.Name)
		}
		seen[object.Name] = struct{}{}
		if !sigv4HexSHA256Pattern.MatchString(object.SHA256) {
			return fmt.Errorf("object %q sha256 must be lowercase hex", object.Name)
		}
		if object.SizeBytes <= 0 {
			return fmt.Errorf("object %q size_bytes must be positive", object.Name)
		}
	}
	return nil
}

func requireSameWriteSession(existing ObjectWriteSession, input CreateObjectWriteSessionInput) error {
	if existing.Namespace != input.Namespace {
		return fmt.Errorf("write session idempotency payload mismatch: %w", ErrConflict)
	}
	existingObjects := make(map[string]ObjectWriteSessionObject, len(existing.Objects))
	for _, object := range existing.Objects {
		existingObjects[object.Name] = object
	}
	if len(existingObjects) != len(input.Objects) {
		return fmt.Errorf("write session idempotency payload mismatch: %w", ErrConflict)
	}
	for _, object := range input.Objects {
		existingObject, ok := existingObjects[object.Name]
		if !ok || existingObject.SHA256 != object.SHA256 || existingObject.SizeBytes != object.SizeBytes {
			return fmt.Errorf("write session idempotency payload mismatch: %w", ErrConflict)
		}
	}
	return nil
}

func sortedObjectWriteRequests(objects []ObjectWriteRequest) []ObjectWriteRequest {
	out := append([]ObjectWriteRequest(nil), objects...)
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

func deploymentArtifactObjectKey(site, namespace string, object ObjectWriteRequest) string {
	return path.Join("deployment-artifacts", safeObjectKeySegment(site), safeObjectKeySegment(namespace), "sha256", object.SHA256, safeObjectKeySegment(object.Name)+".tar")
}

func safeObjectKeySegment(value string) string {
	value = strings.Trim(strings.TrimSpace(value), "/")
	if value == "" {
		return "object"
	}
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	out := b.String()
	if out == "." || out == ".." {
		return "_"
	}
	return out
}
