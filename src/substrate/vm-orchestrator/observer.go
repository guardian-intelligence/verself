package vmorchestrator

type LeaseObserver interface {
	OnGuestCheckpoint(leaseID string, event CheckpointEvent)
	OnTelemetryEvent(event TelemetryEvent)
}

type noopLeaseObserver struct{}

func (noopLeaseObserver) OnGuestCheckpoint(string, CheckpointEvent) {}
func (noopLeaseObserver) OnTelemetryEvent(TelemetryEvent)           {}

func normalizeLeaseObserver(observer LeaseObserver) LeaseObserver {
	if observer == nil {
		return noopLeaseObserver{}
	}
	return observer
}
