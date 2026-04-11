package vmorchestrator

type RunObserver interface {
	OnGuestLogChunk(jobID string, chunk string)
	OnGuestPhaseStart(jobID string, phase string)
	OnGuestPhaseEnd(jobID string, phase PhaseResult)
	OnGuestCheckpoint(jobID string, event CheckpointEvent)
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
