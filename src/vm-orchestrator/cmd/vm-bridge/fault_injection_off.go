//go:build !verself_fault_injection

package main

// parseBridgeFaultMode for production builds: ignores the fault-injection
// env var unconditionally. The verification code paths that drive
// bridgeFaultResultSeqZero are compiled out so a stray VERSELF_VM_BRIDGE_FAULT
// in a customer workload's Env cannot manipulate the bridge's protocol
// envelopes.
func parseBridgeFaultMode(_ string) (bridgeFaultMode, error) {
	return bridgeFaultNone, nil
}
