package main

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ProtonMail/go-crypto/openpgp"
)

type fakeOpenBaoClient struct {
	status        baoStatus
	statuses      []baoStatus
	rootToken     string
	unsealShares  []string
	snapshot      []byte
	revokedTokens []string
	restored      bool
	revokeErr     error
}

func (f *fakeOpenBaoClient) Status(context.Context) (baoStatus, error) {
	if len(f.statuses) > 0 {
		status := f.statuses[0]
		f.statuses = f.statuses[1:]
		f.status = status
		return status, nil
	}
	return f.status, nil
}

func (f *fakeOpenBaoClient) Init(_ context.Context, opts initOptions) (initResponse, error) {
	f.status.Initialized = true
	f.status.Sealed = true
	shares := f.unsealShares
	if len(shares) == 0 {
		for i := 0; i < opts.KeyShares; i++ {
			shares = append(shares, fmt.Sprintf("generated-share-%d", i))
		}
		f.unsealShares = shares
	}
	return initResponse{
		RootToken:     f.rootToken,
		UnsealKeysB64: shares,
	}, nil
}

func (f *fakeOpenBaoClient) Unseal(_ context.Context, share string) (baoStatus, error) {
	for _, expected := range f.unsealShares {
		if share == expected {
			f.status.Sealed = false
			return f.status, nil
		}
	}
	f.status.Progress++
	return f.status, nil
}

func (f *fakeOpenBaoClient) RestoreSnapshot(context.Context, string, string) error {
	f.restored = true
	f.status.Initialized = true
	f.status.Sealed = true
	return nil
}

func (f *fakeOpenBaoClient) SaveSnapshot(context.Context, string) ([]byte, error) {
	return f.snapshot, nil
}

func (f *fakeOpenBaoClient) RevokeSelf(_ context.Context, token string) error {
	f.revokedTokens = append(f.revokedTokens, token)
	return f.revokeErr
}

func TestFreshInitWritesEncryptedMaterialAndRevokesInitialRootToken(t *testing.T) {
	cfg := testConfig(t)
	cfg.pgpKeys = writeTestPGPRecipientFiles(t, 3)
	cfg.initOutputPath = filepath.Join(t.TempDir(), "init-material.json")
	client := &fakeOpenBaoClient{
		status:       baoStatus{Initialized: false, Sealed: true},
		rootToken:    "root-token-secret",
		unsealShares: []string{"share-a", "share-b", "share-c"},
	}

	rep := recoverOnce(context.Background(), cfg, client, strings.NewReader(""))

	assertCondition(t, rep, "OpenBaoInitialized", "True", "FreshInitComplete")
	assertCondition(t, rep, "OpenBaoUnsealed", "True", "UnsealComplete")
	assertCondition(t, rep, "OpenBaoTransientTokenRevoked", "True", "Revoked")
	assertCondition(t, rep, "OpenBaoRecoveryComplete", "True", "Recovered")
	if got := client.revokedTokens; len(got) != 1 || got[0] != "root-token-secret" {
		t.Fatalf("revoked tokens = %#v", got)
	}
	body, err := os.ReadFile(cfg.initOutputPath)
	if err != nil {
		t.Fatal(err)
	}
	assertDoesNotContain(t, body, "root-token-secret")
	assertDoesNotContain(t, body, "share-a")
}

func TestInitializedUnsealedCompletesWithoutOperatorMaterial(t *testing.T) {
	cfg := testConfig(t)
	client := &fakeOpenBaoClient{
		status: baoStatus{Initialized: true, Sealed: false},
	}

	rep := recoverOnce(context.Background(), cfg, client, strings.NewReader(""))

	assertCondition(t, rep, "OpenBaoRecoveryComplete", "True", "Available")
	if len(client.revokedTokens) != 0 {
		t.Fatalf("revoked tokens = %#v", client.revokedTokens)
	}
}

func TestInitializedSealedWaitsForUnsealMaterial(t *testing.T) {
	cfg := testConfig(t)
	client := &fakeOpenBaoClient{
		status: baoStatus{Initialized: true, Sealed: true},
	}

	rep := recoverOnce(context.Background(), cfg, client, strings.NewReader(""))

	assertCondition(t, rep, "OpenBaoUnsealed", "False", "UnsealQuorumIncomplete")
	assertCondition(t, rep, "OpenBaoRecoveryComplete", "False", "WaitingForUnseal")
}

func TestInitializedSealedUnsealsFromStdin(t *testing.T) {
	cfg := testConfig(t)
	cfg.unsealStdin = true
	client := &fakeOpenBaoClient{
		status:       baoStatus{Initialized: true, Sealed: true},
		unsealShares: []string{"share-a"},
	}

	rep := recoverOnce(context.Background(), cfg, client, strings.NewReader("share-a\n"))

	assertCondition(t, rep, "OpenBaoUnsealed", "True", "UnsealComplete")
	assertCondition(t, rep, "OpenBaoRecoveryComplete", "True", "Recovered")
}

func TestSnapshotRestoreRequiresRestartAndRestoredUnseal(t *testing.T) {
	cfg := testConfig(t)
	snapshotPath, manifestPath := writeSnapshotAndManifest(t, []byte("snapshot"))
	cfg.snapshotPath = snapshotPath
	cfg.snapshotManifest = manifestPath
	client := &fakeOpenBaoClient{
		status:       baoStatus{Initialized: false, Sealed: true},
		rootToken:    "temporary-root-token",
		unsealShares: []string{"temp-share"},
	}

	rep := recoverOnce(context.Background(), cfg, client, strings.NewReader(""))

	assertCondition(t, rep, "OpenBaoSnapshotVerified", "True", "DigestVerified")
	assertCondition(t, rep, "OpenBaoSnapshotRestored", "True", "RestoreSubmitted")
	assertCondition(t, rep, "OpenBaoRecoveryComplete", "False", "WaitingForUnseal")
	if !client.restored {
		t.Fatal("snapshot was not restored")
	}
}

func TestApplyResourceGraphRejectsUnknownOpenBaoField(t *testing.T) {
	cfg := testConfig(t)
	cfg.resourceGraph = filepath.Join(t.TempDir(), "document.json")
	body := []byte(`{
		"entrypoint": {},
		"resources": [{
			"apiVersion": "openbao.guardianintelligence.org/v1alpha1",
			"kind": "OpenBaoCluster",
			"metadata": {"name": "openbao"},
			"spec": {
				"address": "https://127.0.0.1:8200",
				"caCert": "/etc/verself/openbao/ca.pem",
				"runtimeRoot": "/var/lib/openbao/runtime",
				"dataDir": "/var/lib/openbao/raft",
				"configPath": "/etc/openbao/openbao.hcl",
				"reportPath": "/run/verself/recovery/openbao/report.json",
				"initMaterialPath": "/run/verself/recovery/openbao/init-material.json",
				"seal": {"shamir": {"keyShares": 3, "keyThreshold": 2, "pgpRecipientRefs": []}},
				"snapshots": {},
				"unsupported": true
			}
		}]
	}`)
	if err := os.WriteFile(cfg.resourceGraph, body, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := applyResourceGraphConfig(cfg); err == nil {
		t.Fatal("applyResourceGraphConfig accepted unknown OpenBao field")
	}
}

func testConfig(t *testing.T) config {
	t.Helper()
	repoRoot := t.TempDir()
	artifact := filepath.Join(repoRoot, "bazel-bin/src/infrastructure-components/openbao/openbao-runtime.tar")
	if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifact, minimalRuntimeTar(t), 0o644); err != nil {
		t.Fatal(err)
	}
	return config{
		repoRoot:    repoRoot,
		runtimeRoot: filepath.Join(t.TempDir(), "runtime"),
		dataDir:     filepath.Join(t.TempDir(), "raft"),
		configPath:  filepath.Join(t.TempDir(), "openbao.hcl"),
		reportPath:  "",
		addr:        defaultAddr,
		caCert:      "",
		keyShares:   defaultKeyShares,
		threshold:   defaultThreshold,
	}
}

func minimalRuntimeTar(t *testing.T) []byte {
	t.Helper()
	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	files := map[string][]byte{
		"bin/bao":              []byte("bao\n"),
		"bin/openbao-recover":  []byte("recover\n"),
		"share/openbao/README": []byte("runtime\n"),
	}
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o755, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}

func writeSnapshotAndManifest(t *testing.T, snapshot []byte) (string, string) {
	t.Helper()
	dir := t.TempDir()
	snapshotPath := filepath.Join(dir, "openbao.snap")
	manifestPath := filepath.Join(dir, "openbao.manifest.json")
	if err := os.WriteFile(snapshotPath, snapshot, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(snapshot)
	manifest := snapshotManifest{
		APIVersion: "backup.openbao.guardianintelligence.org/v1alpha1",
		Kind:       "OpenBaoRaftSnapshot",
		Metadata:   snapshotManifestMeta{Name: "openbao"},
		Spec: snapshotManifestSpec{
			SnapshotSHA256: "sha256:" + hex.EncodeToString(sum[:]),
			SnapshotBytes:  int64(len(snapshot)),
		},
	}
	body, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return snapshotPath, manifestPath
}

func writeTestPGPRecipientFiles(t *testing.T, count int) stringList {
	t.Helper()
	dir := t.TempDir()
	out := make(stringList, 0, count)
	for i := 0; i < count; i++ {
		entity, err := openpgp.NewEntity(fmt.Sprintf("operator-%d", i), "", fmt.Sprintf("operator-%d@example.invalid", i), nil)
		if err != nil {
			t.Fatalf("generate PGP entity: %v", err)
		}
		var public bytes.Buffer
		if err := entity.Serialize(&public); err != nil {
			t.Fatalf("serialize PGP public key: %v", err)
		}
		path := filepath.Join(dir, fmt.Sprintf("operator-%d.pgp.b64", i))
		if err := os.WriteFile(path, []byte(base64.StdEncoding.EncodeToString(public.Bytes())+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		out = append(out, path)
	}
	return out
}

func assertCondition(t *testing.T, rep report, conditionType string, status string, reason string) {
	t.Helper()
	for _, cond := range rep.Conditions {
		if cond.Type == conditionType && cond.Status == status && cond.Reason == reason {
			return
		}
	}
	t.Fatalf("condition %s/%s/%s not found in %#v", conditionType, status, reason, rep.Conditions)
}

func assertDoesNotContain(t *testing.T, body []byte, secret string) {
	t.Helper()
	if bytes.Contains(body, []byte(secret)) {
		t.Fatalf("body contains secret %q: %s", secret, string(body))
	}
}
