package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCertificateValidBeyondRejectsExpiredCertificate(t *testing.T) {
	path := writeTestCertificate(t, time.Now().Add(-time.Hour))
	if valid, err := certificateValidBeyond(path, time.Now()); valid || err == nil {
		t.Fatalf("certificateValidBeyond = %v, %v; want invalid expired cert", valid, err)
	}
}

func TestCertificateValidBeyondAcceptsFreshCertificate(t *testing.T) {
	path := writeTestCertificate(t, time.Now().Add(time.Hour))
	valid, err := certificateValidBeyond(path, time.Now().Add(5*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !valid {
		t.Fatal("expected fresh certificate to be valid")
	}
}

func TestWaitForMonitorProbeRetriesTransientFailures(t *testing.T) {
	attempts := 0
	err := waitForMonitorProbe(100*time.Millisecond, time.Millisecond, func() error {
		attempts++
		if attempts < 3 {
			return errors.New("certificate rotation window")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 3 {
		t.Fatalf("probe attempts = %d; want 3", attempts)
	}
}

func TestWaitForMonitorProbeTimesOutOnSustainedFailure(t *testing.T) {
	err := waitForMonitorProbe(5*time.Millisecond, time.Millisecond, func() error {
		return errors.New("clickhouse unavailable")
	})
	if err == nil {
		t.Fatal("expected sustained probe failure to time out")
	}
}

func TestUpsertConditionsReplacesExistingType(t *testing.T) {
	conditions := upsertConditions(
		[]condition{
			conditionFalse("ClickHouseMonitorHealthy", "ProbeFailed", "old", "clickhouse"),
			conditionTrue("ClickHouseRuntimeInstalled", "RuntimeReady", "runtime installed", "clickhouse"),
		},
		conditionTrue("ClickHouseMonitorHealthy", "ProbeSucceeded", "new", "clickhouse"),
	)
	if len(conditions) != 2 {
		t.Fatalf("condition count = %d; want 2", len(conditions))
	}
	if conditions[0].Status != "True" || conditions[0].Reason != "ProbeSucceeded" || conditions[0].Message != "new" {
		t.Fatalf("updated condition = %+v", conditions[0])
	}
	if conditions[1].Type != "ClickHouseRuntimeInstalled" {
		t.Fatalf("preserved condition = %+v", conditions[1])
	}
}

func writeTestCertificate(t *testing.T, notAfter time.Time) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "cert.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
