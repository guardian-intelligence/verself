package billing

import (
	"context"
	"time"
)

// noopMeteringWriter is a no-op implementation of MeteringWriter for tests
// that don't exercise ClickHouse metering paths.
type noopMeteringWriter struct{}

func (noopMeteringWriter) InsertMeteringRow(_ context.Context, _ MeteringRow) error {
	return nil
}

// noopMeteringQuerier is a no-op implementation of MeteringQuerier for tests
// that don't exercise ClickHouse metering paths. Returns zero values.
type noopMeteringQuerier struct{}

func (noopMeteringQuerier) SumDimension(_ context.Context, _ OrgID, _ string, _ string, _ time.Time) (float64, error) {
	return 0, nil
}
