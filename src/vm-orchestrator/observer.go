package vmorchestrator

import "github.com/forge-metal/vm-orchestrator/vmproto"

type RunObserver interface {
	OnGuestLogChunk(jobID string, chunk string)
	OnGuestEvent(jobID string, event vmproto.GuestEvent)
	OnGuestPhaseStart(jobID string, phase string)
	OnGuestPhaseEnd(jobID string, phase PhaseResult)
	OnTelemetryEvent(event TelemetryEvent)
}

type noopRunObserver struct{}

func (noopRunObserver) OnGuestLogChunk(string, string)          {}
func (noopRunObserver) OnGuestEvent(string, vmproto.GuestEvent) {}
func (noopRunObserver) OnGuestPhaseStart(string, string)        {}
func (noopRunObserver) OnGuestPhaseEnd(string, PhaseResult)     {}
func (noopRunObserver) OnTelemetryEvent(TelemetryEvent)         {}

func normalizeRunObserver(observer RunObserver) RunObserver {
	if observer == nil {
		return noopRunObserver{}
	}
	return observer
}
