package main

import (
	"bytes"
	"testing"
)

func TestRenderDiscoveryHosts(t *testing.T) {
	got := string(renderDiscoveryHosts("example.test"))
	want := "127.0.0.1 localhost\n::1 localhost ip6-localhost ip6-loopback\n127.0.0.1 example.test\n"
	if got != want {
		t.Fatalf("hosts file:\n%s", got)
	}
}

func TestRenderZitadelConfig(t *testing.T) {
	got := renderZitadelConfig("gamma.example.test", siteSecrets{
		PostgresAdminPassword: "pg-admin",
		ZitadelDBPassword:     "db-password",
		ResendAPIKey:          "resend-key",
	})
	for _, want := range [][]byte{
		[]byte("ExternalDomain: gamma.example.test\n"),
		[]byte("Password: \"db-password\"\n"),
		[]byte("Password: \"pg-admin\"\n"),
		[]byte("Password: \"resend-key\"\n"),
		[]byte("From: \"noreply@notify.gamma.example.test\"\n"),
	} {
		if !bytes.Contains(got, want) {
			t.Fatalf("rendered config missing %q:\n%s", string(want), string(got))
		}
	}
}

func TestRenderZitadelSteps(t *testing.T) {
	got := renderZitadelSteps("gamma.example.test", siteSecrets{ZitadelAdminPassword: "admin-password"})
	for _, want := range [][]byte{
		[]byte("Address: \"anveio@gamma.example.test\"\n"),
		[]byte("Password: \"admin-password\"\n"),
	} {
		if !bytes.Contains(got, want) {
			t.Fatalf("rendered steps missing %q:\n%s", string(want), string(got))
		}
	}
}

func TestValidateZitadelBootstrapPassword(t *testing.T) {
	if err := validateZitadelBootstrapPassword("Gamma-1234567890"); err != nil {
		t.Fatalf("valid password rejected: %v", err)
	}
	for _, tc := range []struct {
		name  string
		value string
	}{
		{name: "short", value: "Gm-123"},
		{name: "hex", value: "abcdef0123456789"},
		{name: "no symbol", value: "Gamma1234567890"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateZitadelBootstrapPassword(tc.value); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
