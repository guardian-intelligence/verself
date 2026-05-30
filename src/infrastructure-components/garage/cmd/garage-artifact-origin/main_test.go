package main

import (
	"path/filepath"
	"testing"
)

func TestServerTLSReadyRequiresDesiredOriginHostname(t *testing.T) {
	dir := t.TempDir()
	caCertPath := filepath.Join(dir, "ca.pem")
	caKeyPath := filepath.Join(dir, "ca-key.pem")
	serverCertPath := filepath.Join(dir, "server.pem")
	serverKeyPath := filepath.Join(dir, "server-key.pem")
	if err := generateCA(caCertPath, caKeyPath); err != nil {
		t.Fatal(err)
	}
	oldOpts := options{OriginHostname: "artifacts.internal.verself.sh"}
	if err := generateServerCert(oldOpts, serverCertPath, serverKeyPath, caCertPath, caKeyPath); err != nil {
		t.Fatal(err)
	}
	newOpts := options{OriginHostname: "artifacts.internal.verself.local"}
	if serverTLSReady(newOpts, serverCertPath, serverKeyPath) {
		t.Fatal("serverTLSReady accepted a certificate for the previous artifact origin hostname")
	}
}

func TestServerTLSReadyAcceptsGeneratedMaterial(t *testing.T) {
	dir := t.TempDir()
	caCertPath := filepath.Join(dir, "ca.pem")
	caKeyPath := filepath.Join(dir, "ca-key.pem")
	serverCertPath := filepath.Join(dir, "server.pem")
	serverKeyPath := filepath.Join(dir, "server-key.pem")
	if err := generateCA(caCertPath, caKeyPath); err != nil {
		t.Fatal(err)
	}
	opts := options{OriginHostname: "artifacts.internal.verself.local"}
	if err := generateServerCert(opts, serverCertPath, serverKeyPath, caCertPath, caKeyPath); err != nil {
		t.Fatal(err)
	}
	if !serverTLSReady(opts, serverCertPath, serverKeyPath) {
		t.Fatal("serverTLSReady rejected generated artifact origin TLS material")
	}
}
