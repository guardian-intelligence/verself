package vmorchestrator

type LeaseObserver interface {
	OnTelemetryEvent(event TelemetryEvent)
}

type noopLeaseObserver struct{}

func (noopLeaseObserver) OnTelemetryEvent(TelemetryEvent) {}

func normalizeLeaseObserver(observer LeaseObserver) LeaseObserver {
	if observer == nil {
		return noopLeaseObserver{}
	}
	return observer
}
