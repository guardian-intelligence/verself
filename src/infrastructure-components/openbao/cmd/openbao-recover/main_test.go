package main

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeOpenBaoClient struct {
	status         baoStatus
	rootToken      string
	unsealShares   []string
	snapshot       []byte
	baselineTokens []string
	baselines      []openBaoBaselineSpec
	revokedTokens  []string
	restored       bool
}

func (f *fakeOpenBaoClient) Status(context.Context) (baoStatus, error) {
	return f.status, nil
}

func (f *fakeOpenBaoClient) Init(_ context.Context, opts initOptions) (initResponse, error) {
	f.status.Initialized = true
	f.status.Sealed = false
	shares := f.unsealShares
	if len(shares) == 0 {
		for i := 0; i < opts.KeyShares; i++ {
			shares = append(shares, "generated-share")
		}
		f.unsealShares = shares
	}
	responseShares := shares
	if len(opts.PGPKeys) > 0 {
		responseShares = make([]string, 0, len(shares))
		for i, share := range shares {
			sum := sha256.Sum256([]byte(share + opts.PGPKeys[i%len(opts.PGPKeys)]))
			responseShares = append(responseShares, "encrypted:"+hex.EncodeToString(sum[:]))
		}
	}
	rootToken := f.rootToken
	if opts.RootTokenPGPKey != "" {
		sum := sha256.Sum256([]byte(rootToken + opts.RootTokenPGPKey))
		rootToken = "encrypted:" + hex.EncodeToString(sum[:])
	}
	return initResponse{
		RootToken:     rootToken,
		UnsealKeysB64: responseShares,
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

func (f *fakeOpenBaoClient) ReconcileBaseline(_ context.Context, token string, baseline openBaoBaselineSpec) error {
	f.baselineTokens = append(f.baselineTokens, token)
	f.baselines = append(f.baselines, baseline)
	return nil
}

func (f *fakeOpenBaoClient) RevokeSelf(_ context.Context, token string) error {
	f.revokedTokens = append(f.revokedTokens, token)
	return nil
}

func TestFreshInitWithRandomRootTrustMaterial(t *testing.T) {
	for i := 0; i < 5; i++ {
		rootToken := randomSecret(t)
		client := &fakeOpenBaoClient{
			status: baoStatus{
				Initialized: false,
				Sealed:      true,
				Version:     "2.5.2",
				SealType:    "shamir",
			},
			rootToken:    rootToken,
			unsealShares: []string{randomSecret(t), randomSecret(t), randomSecret(t)},
		}
		cfg := testConfig(t)
		cfg.keyShares = 3
		cfg.threshold = 2
		cfg.pgpKeys = stringList{"operator-a.asc", "operator-b.asc", "operator-c.asc"}
		cfg.rootTokenPGPKey = "operator-root.asc"
		cfg.initOutputPath = filepath.Join(t.TempDir(), "init-material.json")

		rep := recoverOnce(context.Background(), cfg, client, bytes.NewReader(nil))

		assertCondition(t, rep, "RootTrustMaterialAvailable", "True", "Created")
		assertCondition(t, rep, "OpenBaoInitMaterialDelivered", "True", "InitOutputWritten")
		assertCondition(t, rep, "OpenBaoRecoveryComplete", "False", "WaitingForRootTrustMaterial")
		if len(client.baselineTokens) != 0 {
			t.Fatalf("baseline reconciled before unseal material was presented")
		}
		if len(client.revokedTokens) != 0 {
			t.Fatalf("root token was revoked before operator decrypted and presented it")
		}
		assertReportDoesNotContain(t, rep, rootToken)
		for _, share := range client.unsealShares {
			assertReportDoesNotContain(t, rep, share)
		}
		material, err := os.ReadFile(cfg.initOutputPath)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(material, []byte(rootToken)) {
			t.Fatalf("init material leaked root token: %s", material)
		}
		for _, share := range client.unsealShares {
			if bytes.Contains(material, []byte(share)) {
				t.Fatalf("init material leaked unencrypted share %q: %s", share, material)
			}
		}
		var decoded encryptedInitMaterial
		if err := json.Unmarshal(material, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded.Spec.EncryptedRootTokenB64 == "" || !decoded.Spec.RootTokenPGPRecipient {
			t.Fatalf("init material omitted encrypted root token handoff: %#v", decoded.Spec)
		}
	}
}

func TestSealedOpenBaoRequiresPresentedRootTrustMaterial(t *testing.T) {
	share := randomSecret(t)
	client := &fakeOpenBaoClient{
		status: baoStatus{Initialized: true, Sealed: true, Threshold: 2, Progress: 0},
		unsealShares: []string{
			share,
		},
	}
	cfg := testConfig(t)

	rep := recoverOnce(context.Background(), cfg, client, bytes.NewReader(nil))

	assertCondition(t, rep, "RootTrustMaterialAvailable", "False", "UnsealQuorumIncomplete")
	assertReportDoesNotContain(t, rep, share)
}

func TestSealedOpenBaoAcceptsUnsealMaterialFromStdin(t *testing.T) {
	share := randomSecret(t)
	client := &fakeOpenBaoClient{
		status:       baoStatus{Initialized: true, Sealed: true, Threshold: 1},
		unsealShares: []string{share},
	}
	cfg := testConfig(t)
	cfg.unsealStdin = true

	rep := recoverOnce(context.Background(), cfg, client, strings.NewReader(share+"\n"))

	assertCondition(t, rep, "RootTrustMaterialAvailable", "True", "Presented")
	assertCondition(t, rep, "OpenBaoUnsealed", "True", "UnsealComplete")
	assertCondition(t, rep, "OpenBaoRecoveryComplete", "True", "Recovered")
	assertReportDoesNotContain(t, rep, share)
}

func TestUnsealedOpenBaoRequiresNoOperatorIntervention(t *testing.T) {
	token := randomSecret(t)
	client := &fakeOpenBaoClient{
		status:    baoStatus{Initialized: true, Sealed: false},
		rootToken: token,
	}
	cfg := testConfig(t)

	rep := recoverOnce(context.Background(), cfg, client, bytes.NewReader(nil))

	assertCondition(t, rep, "RootTrustMaterialAvailable", "True", "NotRequired")
	assertCondition(t, rep, "OpenBaoRecoveryComplete", "True", "Available")
	if len(client.baselineTokens) != 0 {
		t.Fatalf("baseline reconciled without an operator token")
	}
	assertReportDoesNotContain(t, rep, token)
}

func TestUnsealedOpenBaoUsesOperatorTokenFromStdin(t *testing.T) {
	token := randomSecret(t)
	client := &fakeOpenBaoClient{
		status: baoStatus{Initialized: true, Sealed: false},
	}
	cfg := testConfig(t)
	cfg.tokenStdin = true

	rep := recoverOnce(context.Background(), cfg, client, strings.NewReader(token+"\n"))

	assertCondition(t, rep, "RootTrustMaterialAvailable", "True", "Presented")
	assertCondition(t, rep, "OpenBaoRecoveryComplete", "True", "Recovered")
	if len(client.baselineTokens) != 1 || client.baselineTokens[0] != token {
		t.Fatalf("baseline did not use presented token")
	}
	assertReportDoesNotContain(t, rep, token)
}

func TestUnsealedOpenBaoUsesBaselineFromResourceGraph(t *testing.T) {
	token := randomSecret(t)
	client := &fakeOpenBaoClient{
		status: baoStatus{Initialized: true, Sealed: false},
	}
	cfg := testConfig(t)
	cfg.tokenStdin = true
	cfg.baseline = openBaoBaselineSpec{
		Reconcile: true,
		Mounts: []openBaoMountSpec{
			{Path: "kv-runtime", Type: "kv", Options: map[string]string{"version": "2"}},
		},
		Policies: []openBaoPolicySpec{
			{Name: "cloudflare-integration-recovery-runtime", HCL: `path "kv-runtime/data/secret/org/object-storage-service.r2.admin_access_key_id" { capabilities = ["create", "update"] }`},
		},
		NomadJWT: &openBaoNomadJWTAuthSpec{
			Path:          "jwt-nomad",
			Description:   "Verself Nomad workload identity auth",
			JWKSURL:       "http://127.0.0.1:4646/.well-known/jwks.json",
			SupportedAlgs: []string{"RS256", "EdDSA"},
			Roles: []openBaoNomadJWTRoleSpec{
				{
					Name:                 "cloudflare-integration-recovery-runtime",
					RoleType:             "jwt",
					BoundAudiences:       []string{"vault.io"},
					BoundClaims:          map[string]string{"nomad_job_id": "cloudflare-integration-recovery"},
					UserClaim:            "/nomad_job_id",
					UserClaimJSONPointer: true,
					ClaimMappings:        map[string]string{"nomad_job_id": "nomad_job_id"},
					TokenType:            "service",
					TokenPolicies:        []string{"cloudflare-integration-recovery-runtime"},
					TokenPeriod:          "30m",
				},
			},
		},
	}

	rep := recoverOnce(context.Background(), cfg, client, strings.NewReader(token+"\n"))

	assertCondition(t, rep, "OpenBaoBaselineReconciled", "True", "BaselineReady")
	if len(client.baselines) != 1 {
		t.Fatalf("baseline reconcile calls = %d", len(client.baselines))
	}
	if got := client.baselines[0].NomadJWT.Roles[0].Name; got != "cloudflare-integration-recovery-runtime" {
		t.Fatalf("jwt role = %q", got)
	}
}

func TestRestoreSnapshotVerifiesDigestAndThenRequiresSnapshotUnsealMaterial(t *testing.T) {
	snapshot := []byte(randomSecret(t) + "\n")
	snapshotPath, manifestPath := writeSnapshotAndManifest(t, snapshot)
	client := &fakeOpenBaoClient{
		status:    baoStatus{Initialized: false, Sealed: true},
		rootToken: randomSecret(t),
	}
	cfg := testConfig(t)
	cfg.snapshotPath = snapshotPath
	cfg.snapshotManifest = manifestPath

	rep := recoverOnce(context.Background(), cfg, client, bytes.NewReader(nil))

	if !client.restored {
		t.Fatalf("snapshot restore was not invoked")
	}
	assertCondition(t, rep, "OpenBaoSnapshotVerified", "True", "DigestVerified")
	assertCondition(t, rep, "OpenBaoServerRestartRequired", "True", "AfterSnapshotRestore")
	assertCondition(t, rep, "RootTrustMaterialAvailable", "False", "UnsealQuorumIncomplete")
	assertReportDoesNotContain(t, rep, client.rootToken)
}

func TestRestoreSnapshotRejectsDigestMismatch(t *testing.T) {
	snapshot := []byte(randomSecret(t) + "\n")
	snapshotPath, manifestPath := writeSnapshotAndManifest(t, snapshot)
	if err := os.WriteFile(snapshotPath, []byte("different snapshot\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	client := &fakeOpenBaoClient{status: baoStatus{Initialized: false, Sealed: true}}
	cfg := testConfig(t)
	cfg.snapshotPath = snapshotPath
	cfg.snapshotManifest = manifestPath

	rep := recoverOnce(context.Background(), cfg, client, bytes.NewReader(nil))

	if client.restored {
		t.Fatalf("snapshot restore ran after digest mismatch")
	}
	assertCondition(t, rep, "OpenBaoSnapshotVerified", "False", "DigestMismatch")
	assertCondition(t, rep, "OpenBaoRecoveryComplete", "False", "SnapshotInvalid")
}

func TestFreshInitRequiresOperatorRecipientIdentities(t *testing.T) {
	client := &fakeOpenBaoClient{
		status: baoStatus{
			Initialized: false,
			Sealed:      true,
		},
		rootToken: randomSecret(t),
	}
	cfg := testConfig(t)
	cfg.keyShares = 3
	cfg.threshold = 2

	rep := recoverOnce(context.Background(), cfg, client, bytes.NewReader(nil))

	assertCondition(t, rep, "RootTrustMaterialAvailable", "False", "InitRecipientIdentityRequired")
	assertCondition(t, rep, "OpenBaoRecoveryComplete", "False", "WaitingForRootTrustMaterial")
	if len(client.baselineTokens) != 0 {
		t.Fatalf("baseline reconciled before recipient identities were available")
	}
	assertReportDoesNotContain(t, rep, client.rootToken)
}

func TestFreshInitRequiresInitMaterialOutput(t *testing.T) {
	client := &fakeOpenBaoClient{
		status: baoStatus{
			Initialized: false,
			Sealed:      true,
		},
		rootToken: randomSecret(t),
	}
	cfg := testConfig(t)
	cfg.keyShares = 3
	cfg.threshold = 2
	cfg.pgpKeys = stringList{"operator-a.asc", "operator-b.asc", "operator-c.asc"}
	cfg.rootTokenPGPKey = "operator-root.asc"

	rep := recoverOnce(context.Background(), cfg, client, bytes.NewReader(nil))

	assertCondition(t, rep, "RootTrustMaterialAvailable", "False", "InitMaterialDeliveryRequired")
	assertCondition(t, rep, "OpenBaoRecoveryComplete", "False", "WaitingForRootTrustMaterial")
	if len(client.baselineTokens) != 0 {
		t.Fatalf("baseline reconciled before init material delivery was configured")
	}
	assertReportDoesNotContain(t, rep, client.rootToken)
}

func TestFreshInitRequiresRootTokenRecipientIdentity(t *testing.T) {
	client := &fakeOpenBaoClient{
		status: baoStatus{
			Initialized: false,
			Sealed:      true,
		},
		rootToken: randomSecret(t),
	}
	cfg := testConfig(t)
	cfg.keyShares = 3
	cfg.threshold = 2
	cfg.pgpKeys = stringList{"operator-a.asc", "operator-b.asc", "operator-c.asc"}
	cfg.initOutputPath = filepath.Join(t.TempDir(), "init-material.json")

	rep := recoverOnce(context.Background(), cfg, client, bytes.NewReader(nil))

	assertCondition(t, rep, "RootTrustMaterialAvailable", "False", "InitRootTokenRecipientIdentityRequired")
	assertCondition(t, rep, "OpenBaoRecoveryComplete", "False", "WaitingForRootTrustMaterial")
	if _, err := os.Stat(cfg.initOutputPath); !os.IsNotExist(err) {
		t.Fatalf("init material should not be written without root token recipient, stat err=%v", err)
	}
}

func TestParseConfigLoadsOpenBaoClusterFromGuardianGraph(t *testing.T) {
	dir := t.TempDir()
	graphPath := filepath.Join(dir, "document.json")
	pgpDir := filepath.Join(dir, "pgp")
	doc := map[string]any{
		"entrypoint": map[string]any{
			"apiVersion": "guardian.guardianintelligence.org/v1alpha1",
			"kind":       "FlyProcedure",
			"name":       "gamma",
		},
		"resources": []map[string]any{
			{
				"apiVersion": "openbao.guardianintelligence.org/v1alpha1",
				"kind":       "OpenBaoCluster",
				"metadata": map[string]any{
					"name": "openbao",
				},
				"spec": map[string]any{
					"address":          "https://127.0.0.1:8200",
					"caCert":           "/etc/openbao/tls/cert.pem",
					"runtimeRoot":      "/var/lib/openbao/runtime-from-graph",
					"dataDir":          "/var/lib/openbao/raft-from-graph",
					"configPath":       "/etc/openbao/openbao-from-graph.hcl",
					"reportPath":       "/run/verself/recovery/openbao/report-from-graph.json",
					"initMaterialPath": "/run/verself/recovery/openbao/init-material-from-graph.json",
					"loopInterval":     "7s",
					"seal": map[string]any{
						"shamir": map[string]any{
							"keyShares":    3,
							"keyThreshold": 2,
							"pgpRecipientRefs": []map[string]any{
								{"apiVersion": "openbao.guardianintelligence.org/v1alpha1", "kind": "PGPRecipient", "name": "operator-a"},
								{"apiVersion": "openbao.guardianintelligence.org/v1alpha1", "kind": "PGPRecipient", "name": "operator-b"},
								{"apiVersion": "openbao.guardianintelligence.org/v1alpha1", "kind": "PGPRecipient", "name": "operator-c"},
							},
							"rootTokenRecipientRef": map[string]any{
								"apiVersion": "openbao.guardianintelligence.org/v1alpha1",
								"kind":       "PGPRecipient",
								"name":       "operator-root",
							},
						},
					},
					"baseline": map[string]any{
						"reconcile": true,
						"mounts": []map[string]any{
							{"path": "kv-runtime", "type": "kv", "options": map[string]string{"version": "2"}},
						},
						"policies": []map[string]any{
							{"name": "cloudflare-integration-recovery-runtime", "hcl": `path "kv-runtime/data/secret/org/object-storage-service.r2.admin_access_key_id" { capabilities = ["create", "update"] }`},
						},
						"nomadJWT": map[string]any{
							"path":          "jwt-nomad",
							"description":   "Verself Nomad workload identity auth",
							"jwksURL":       "http://127.0.0.1:4646/.well-known/jwks.json",
							"supportedAlgs": []string{"RS256", "EdDSA"},
							"roles": []map[string]any{
								{
									"name":                 "cloudflare-integration-recovery-runtime",
									"roleType":             "jwt",
									"boundAudiences":       []string{"vault.io"},
									"boundClaims":          map[string]string{"nomad_job_id": "cloudflare-integration-recovery"},
									"userClaim":            "/nomad_job_id",
									"userClaimJSONPointer": true,
									"claimMappings":        map[string]string{"nomad_job_id": "nomad_job_id"},
									"tokenType":            "service",
									"tokenPolicies":        []string{"cloudflare-integration-recovery-runtime"},
									"tokenPeriod":          "30m",
									"tokenExplicitMaxTTL":  0,
								},
							},
						},
					},
				},
			},
			pgpRecipient("operator-a", "pubkey-a"),
			pgpRecipient("operator-b", "pubkey-b"),
			pgpRecipient("operator-c", "pubkey-c"),
			pgpRecipient("operator-root", "pubkey-root"),
		},
	}
	body, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(graphPath, body, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := parseConfig("test", []string{
		"--repo-root=" + dir,
		"--resource-graph=" + graphPath,
		"--resource-name=openbao",
		"--pgp-key-dir=" + pgpDir,
	}, true, false)
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.runtimeRoot != "/var/lib/openbao/runtime-from-graph" {
		t.Fatalf("runtimeRoot = %q", cfg.runtimeRoot)
	}
	if cfg.bao != "/var/lib/openbao/runtime-from-graph/current/bin/bao" {
		t.Fatalf("bao path was not derived from graph runtimeRoot: %q", cfg.bao)
	}
	if cfg.loopInterval != 7*time.Second {
		t.Fatalf("loopInterval = %s", cfg.loopInterval)
	}
	if len(cfg.pgpKeys) != 3 {
		t.Fatalf("pgpKeys = %#v", cfg.pgpKeys)
	}
	if !cfg.baseline.Reconcile || cfg.baseline.NomadJWT == nil || len(cfg.baseline.NomadJWT.Roles) != 1 {
		t.Fatalf("baseline = %#v", cfg.baseline)
	}
	for _, path := range append([]string(cfg.pgpKeys), cfg.rootTokenPGPKey) {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read materialized PGP key %s: %v", path, err)
		}
		if !strings.HasPrefix(string(body), "pubkey-") {
			t.Fatalf("unexpected PGP key body in %s: %q", path, body)
		}
	}
}

func pgpRecipient(name string, publicKey string) map[string]any {
	return map[string]any{
		"apiVersion": "openbao.guardianintelligence.org/v1alpha1",
		"kind":       "PGPRecipient",
		"metadata": map[string]any{
			"name": name,
		},
		"spec": map[string]any{
			"publicKeyBase64": publicKey,
		},
	}
}

func TestSnapshotSaveWritesManifestWithoutSecretBytes(t *testing.T) {
	snapshot := []byte(randomSecret(t) + "\n")
	token := randomSecret(t)
	client := &fakeOpenBaoClient{
		status: baoStatus{
			Initialized: true,
			Sealed:      false,
			Version:     "2.5.2",
			SealType:    "shamir",
			ClusterID:   "cluster-id",
		},
		snapshot: snapshot,
	}
	cfg := testConfig(t)
	cfg.snapshotOut = filepath.Join(t.TempDir(), "openbao.snap")
	cfg.manifestOut = filepath.Join(t.TempDir(), "openbao.manifest.json")

	rep := saveSnapshot(context.Background(), cfg, client, token)

	assertCondition(t, rep, "OpenBaoSnapshotSaved", "True", "SnapshotSaved")
	body, err := os.ReadFile(cfg.manifestOut)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(body, snapshot) || bytes.Contains(body, []byte(token)) {
		t.Fatalf("manifest leaked snapshot or token bytes: %s", body)
	}
	var manifest snapshotManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(snapshot)
	if manifest.Spec.SnapshotSHA256 != "sha256:"+hex.EncodeToString(sum[:]) {
		t.Fatalf("manifest digest = %s", manifest.Spec.SnapshotSHA256)
	}
	if err := verifySnapshotDigest(cfg.snapshotOut, manifest); err != nil {
		t.Fatal(err)
	}
}

func TestRunSnapshotVerifyAcceptsDocumentedFlags(t *testing.T) {
	snapshot := []byte(randomSecret(t) + "\n")
	snapshotPath, manifestPath := writeSnapshotAndManifest(t, snapshot)
	cfg := testConfig(t)
	reportPath := filepath.Join(t.TempDir(), "verify.report")
	var stdout bytes.Buffer

	err := run(context.Background(), []string{
		"snapshot",
		"verify",
		"--repo-root=" + cfg.repoRoot,
		"--runtime-root=" + cfg.runtimeRoot,
		"--data-dir=" + cfg.dataDir,
		"--config=" + cfg.configPath,
		"--report=" + reportPath,
		"--snapshot=" + snapshotPath,
		"--snapshot-manifest=" + manifestPath,
	}, &stdout, bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("snapshot verify command failed: %v", err)
	}
	var rep report
	if err := json.Unmarshal(stdout.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	assertCondition(t, rep, "OpenBaoSnapshotVerified", "True", "DigestVerified")
}

func TestSnapshotSaveRequiresUnsealedOpenBao(t *testing.T) {
	token := randomSecret(t)
	client := &fakeOpenBaoClient{
		status: baoStatus{Initialized: true, Sealed: true},
	}
	cfg := testConfig(t)
	cfg.snapshotOut = filepath.Join(t.TempDir(), "openbao.snap")
	cfg.manifestOut = filepath.Join(t.TempDir(), "openbao.manifest.json")

	rep := saveSnapshot(context.Background(), cfg, client, token)

	assertCondition(t, rep, "OpenBaoSnapshotSaved", "False", "OpenBaoNotUnsealed")
	if _, err := os.Stat(cfg.snapshotOut); !os.IsNotExist(err) {
		t.Fatalf("snapshot file unexpectedly exists or stat failed: %v", err)
	}
	assertReportDoesNotContain(t, rep, token)
}

func TestInstallRuntimeRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	artifact := filepath.Join(root, "bazel-bin/src/infrastructure-components/openbao/openbao-runtime.tar")
	if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	if err := tw.WriteHeader(&tar.Header{Name: "../escape", Typeflag: tar.TypeReg, Mode: 0o644, Size: 4}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("nope")); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifact, archive.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := testConfig(t)
	cfg.repoRoot = root
	cfg.runtimeRoot = filepath.Join(t.TempDir(), "runtime")

	err := installRuntime(cfg)
	if err == nil || !strings.Contains(err.Error(), "unsafe OpenBao runtime tar entry") {
		t.Fatalf("installRuntime error = %v", err)
	}
}

func TestRuntimeInstalledRequiresRecoveryBinary(t *testing.T) {
	release := filepath.Join(t.TempDir(), "release")
	if err := os.MkdirAll(filepath.Join(release, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(release, "bin", "bao"), []byte("bao\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	if runtimeInstalled(release) {
		t.Fatalf("runtimeInstalled accepted a release without openbao-recover")
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

func randomSecret(t *testing.T) string {
	t.Helper()
	body := make([]byte, 32)
	if _, err := rand.Read(body); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(body)
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

func assertReportDoesNotContain(t *testing.T, rep report, secret string) {
	t.Helper()
	body, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), secret) {
		t.Fatalf("report leaked secret %q: %s", secret, body)
	}
}
