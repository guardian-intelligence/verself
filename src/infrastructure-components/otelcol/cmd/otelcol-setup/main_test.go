package main

import "testing"

func TestHasFieldsLine(t *testing.T) {
	body := []byte(`
# MAPNAME SYSTEM-USERNAME PG-USERNAME
verself_services      postgres            postgres
verself_services      otelcol             otelcol
`)
	if !hasFieldsLine(body, []string{"verself_services", "otelcol", "otelcol"}) {
		t.Fatal("expected otelcol peer map line")
	}
	if hasFieldsLine(body, []string{"verself_services", "otelcol", "postgres"}) {
		t.Fatal("unexpected mismatched peer map line")
	}
}
