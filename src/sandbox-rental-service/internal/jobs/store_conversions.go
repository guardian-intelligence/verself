package jobs

import (
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/verself/sandbox-rental-service/internal/store"
)

func (s *Service) storeQueries() *store.Queries {
	return store.New(s.PGX)
}

func dbOrgID(orgID uint64) int64 {
	return mustInt64FromUint64(orgID, "org id")
}

func orgIDFromDB(orgID int64) uint64 {
	if orgID <= 0 {
		return 0
	}
	return uint64(orgID) // #nosec G115 -- orgID is checked as positive above.
}

func pgTime(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}

func pgOptionalTime(value time.Time) pgtype.Timestamptz {
	if value.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgTime(value)
}

func timeFromPG(value pgtype.Timestamptz) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return value.Time.UTC()
}

func timePtrFromPG(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	t := value.Time.UTC()
	return &t
}

func uuidPtrFromZero(value uuid.UUID) *uuid.UUID {
	if value == uuid.Nil {
		return nil
	}
	return &value
}
