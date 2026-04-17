package vmorchestrator

import "testing"

func TestTelemetryFaultProfileFromConfig(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		profile, err := telemetryFaultProfileFromConfig(Config{})
		if err != nil {
			t.Fatalf("telemetryFaultProfileFromConfig returned error: %v", err)
		}
		if profile != nil {
			t.Fatalf("empty profile = %#v, want nil", profile)
		}
	})

	t.Run("gap", func(t *testing.T) {
		profile, err := telemetryFaultProfileFromConfig(Config{TelemetryFaultProfile: "gap_once@7"})
		if err != nil {
			t.Fatalf("telemetryFaultProfileFromConfig returned error: %v", err)
		}
		if profile == nil || profile.kind != telemetryFaultProfileKindGapOnce || profile.targetSeq != 7 || profile.seqDelta != 1 {
			t.Fatalf("gap profile = %#v", profile)
		}
	})

	t.Run("regression", func(t *testing.T) {
		profile, err := telemetryFaultProfileFromConfig(Config{TelemetryFaultProfile: "regression_once@7"})
		if err != nil {
			t.Fatalf("telemetryFaultProfileFromConfig returned error: %v", err)
		}
		if profile == nil || profile.kind != telemetryFaultProfileKindRegressionOnce || profile.targetSeq != 7 || profile.seqDelta != -1 {
			t.Fatalf("regression profile = %#v", profile)
		}
	})

	t.Run("invalid", func(t *testing.T) {
		if _, err := telemetryFaultProfileFromConfig(Config{TelemetryFaultProfile: "gap_once@nope"}); err == nil {
			t.Fatal("expected invalid profile to fail")
		}
	})
}

func TestTelemetryGapFaultProducesOneDiagnostic(t *testing.T) {
	profile, err := telemetryFaultProfileFromConfig(Config{TelemetryFaultProfile: "gap_once@2"})
	if err != nil {
		t.Fatalf("telemetryFaultProfileFromConfig returned error: %v", err)
	}

	diagnostics := runFaultProfileValidation(t, profile, 4)
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostic count = %d, want 1: %#v", len(diagnostics), diagnostics)
	}
	got := diagnostics[0]
	if got.Kind != TelemetryDiagnosticKindGap || got.ExpectedSeq != 2 || got.ObservedSeq != 3 || got.MissingSamples != 1 {
		t.Fatalf("diagnostic = %#v", got)
	}
}

func TestTelemetryRegressionFaultProducesOneDiagnostic(t *testing.T) {
	profile, err := telemetryFaultProfileFromConfig(Config{TelemetryFaultProfile: "regression_once@2"})
	if err != nil {
		t.Fatalf("telemetryFaultProfileFromConfig returned error: %v", err)
	}

	diagnostics := runFaultProfileValidation(t, profile, 4)
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostic count = %d, want 1: %#v", len(diagnostics), diagnostics)
	}
	got := diagnostics[0]
	if got.Kind != TelemetryDiagnosticKindRegression || got.ExpectedSeq != 2 || got.ObservedSeq != 1 || got.MissingSamples != 0 {
		t.Fatalf("diagnostic = %#v", got)
	}
}

func runFaultProfileValidation(t *testing.T, profile *telemetryFaultProfile, samples uint32) []TelemetryDiagnostic {
	t.Helper()

	validator := telemetryStreamValidator{}
	emit, diagnostic, err := validator.validate(TelemetryEvent{Hello: &TelemetryHello{Seq: 0}})
	if err != nil {
		t.Fatalf("hello validation returned error: %v", err)
	}
	if !emit || diagnostic != nil {
		t.Fatalf("hello emit=%t diagnostic=%#v", emit, diagnostic)
	}

	diagnostics := []TelemetryDiagnostic{}
	for seq := uint32(1); seq <= samples; seq++ {
		event := TelemetryEvent{Sample: &TelemetrySample{Seq: seq}}
		injectTelemetryFault(profile, &event)
		_, diagnostic, err := validator.validate(event)
		if err != nil {
			t.Fatalf("sample %d validation returned error: %v", seq, err)
		}
		if diagnostic != nil {
			diagnostics = append(diagnostics, *diagnostic)
		}
	}
	return diagnostics
}
