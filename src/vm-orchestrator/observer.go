package vmorchestrator

type RunObserver interface {
	OnGuestLogChunk(runID string, chunk string)
	OnGuestPhaseStart(runID string, phase string)
	OnGuestPhaseEnd(runID string, phase PhaseResult)
	OnGuestCheckpoint(runID string, event CheckpointEvent)
	OnTelemetryEvent(event TelemetryEvent)
}

type noopRunObserver struct{}

func (noopRunObserver) OnGuestLogChunk(string, string)            {}
func (noopRunObserver) OnGuestPhaseStart(string, string)          {}
func (noopRunObserver) OnGuestPhaseEnd(string, PhaseResult)       {}
func (noopRunObserver) OnGuestCheckpoint(string, CheckpointEvent) {}
func (noopRunObserver) OnTelemetryEvent(TelemetryEvent)           {}

func normalizeRunObserver(observer RunObserver) RunObserver {
	if observer == nil {
		return noopRunObserver{}
	}
	return observer
}
