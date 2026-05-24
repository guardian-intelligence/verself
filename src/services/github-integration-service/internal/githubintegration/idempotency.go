package githubintegration

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/verself/github-integration-service/internal/store"
)

type IdempotencyLock struct {
	tx          pgx.Tx
	queries     *store.Queries
	principal   Principal
	operation   string
	keyHash     string
	requestHash string
	createdAt   time.Time
	closed      bool
}

func (s *Service) BeginIdempotency(ctx context.Context, principal Principal, operation, idempotencyKey, requestHash string) (*IdempotencyLock, []byte, bool, error) {
	if err := validatePrincipal(principal); err != nil {
		return nil, nil, false, err
	}
	operation = strings.TrimSpace(operation)
	if operation == "" {
		return nil, nil, false, fmt.Errorf("%w: operation is required", ErrConfiguration)
	}
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" || requestHash == "" {
		return nil, nil, false, ErrIdempotencyMismatch
	}
	keyHash := sha256Hex([]byte(idempotencyKey))
	tx, err := s.cfg.PG.Begin(ctx)
	if err != nil {
		return nil, nil, false, err
	}
	rollback := func() {
		_ = tx.Rollback(ctx)
	}
	q := s.queries.WithTx(tx)
	if err := q.LockIdempotencyKey(ctx, store.LockIdempotencyKeyParams{LockKey: idempotencyLockKey(principal, operation, keyHash)}); err != nil {
		rollback()
		return nil, nil, false, err
	}
	row, err := q.GetIdempotencyRecord(ctx, store.GetIdempotencyRecordParams{
		OrgID:     principal.OrgID,
		ActorID:   principal.ActorID,
		Operation: operation,
		KeyHash:   keyHash,
	})
	if err == nil {
		rollback()
		if row.RequestHash != requestHash {
			return nil, nil, false, ErrIdempotencyMismatch
		}
		return nil, append([]byte(nil), row.ResultPayload...), true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		rollback()
		return nil, nil, false, err
	}
	return &IdempotencyLock{
		tx:          tx,
		queries:     q,
		principal:   principal,
		operation:   operation,
		keyHash:     keyHash,
		requestHash: requestHash,
		createdAt:   time.Now().UTC(),
	}, nil, false, nil
}

func (l *IdempotencyLock) Commit(ctx context.Context, resultPayload []byte) error {
	if l == nil || l.closed {
		return nil
	}
	if len(resultPayload) == 0 {
		resultPayload = []byte(`{}`)
	}
	if err := l.queries.InsertIdempotencyRecord(ctx, store.InsertIdempotencyRecordParams{
		OrgID:         l.principal.OrgID,
		ActorID:       l.principal.ActorID,
		Operation:     l.operation,
		KeyHash:       l.keyHash,
		RequestHash:   l.requestHash,
		ResultPayload: resultPayload,
		CreatedAt:     pgTime(l.createdAt),
	}); err != nil {
		_ = l.tx.Rollback(ctx)
		l.closed = true
		return err
	}
	if err := l.tx.Commit(ctx); err != nil {
		l.closed = true
		return err
	}
	l.closed = true
	return nil
}

func (l *IdempotencyLock) Rollback(ctx context.Context) {
	if l == nil || l.closed {
		return
	}
	_ = l.tx.Rollback(ctx)
	l.closed = true
}

func idempotencyLockKey(principal Principal, operation, keyHash string) string {
	return principal.OrgID + "|" + principal.ActorID + "|" + operation + "|" + keyHash
}
