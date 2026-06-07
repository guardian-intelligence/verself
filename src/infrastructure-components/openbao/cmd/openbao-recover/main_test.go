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
	mounts        map[string]openBaoMountInfo
	enabledMounts []string
	authMethods   map[string]openBaoAuthInfo
	enabledAuth   []string
	jwtConfigs    map[string]openBaoJWTAuthConfig
	jwtRoles      map[string]openBaoJWTRole
	policies      map[string]string
	kvData        map[string]map[string]string
	createdTokens []openBaoTokenSpec
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

func (f *fakeOpenBaoClient) Mounts(context.Context, string) (map[string]openBaoMountInfo, error) {
	if f.mounts == nil {
		f.mounts = map[string]openBaoMountInfo{}
	}
	return f.mounts, nil
}

func (f *fakeOpenBaoClient) EnableKVv2Mount(_ context.Context, _ string, mount string) error {
	if f.mounts == nil {
		f.mounts = map[string]openBaoMountInfo{}
	}
	f.enabledMounts = append(f.enabledMounts, mount)
	f.mounts[mount+"/"] = openBaoMountInfo{Type: "kv", Options: map[string]string{"version": "2"}}
	return nil
}

func (f *fakeOpenBaoClient) AuthMethods(context.Context, string) (map[string]openBaoAuthInfo, error) {
	if f.authMethods == nil {
		f.authMethods = map[string]openBaoAuthInfo{}
	}
	return f.authMethods, nil
}

func (f *fakeOpenBaoClient) EnableJWTAuth(_ context.Context, _ string, path string) error {
	if f.authMethods == nil {
		f.authMethods = map[string]openBaoAuthInfo{}
	}
	f.enabledAuth = append(f.enabledAuth, path)
	f.authMethods[path+"/"] = openBaoAuthInfo{Type: "jwt"}
	return nil
}

func (f *fakeOpenBaoClient) ConfigureJWTAuth(_ context.Context, _ string, path string, cfg openBaoJWTAuthConfig) error {
	if f.jwtConfigs == nil {
		f.jwtConfigs = map[string]openBaoJWTAuthConfig{}
	}
	f.jwtConfigs[path] = cfg
	return nil
}

func (f *fakeOpenBaoClient) WritePolicy(_ context.Context, _ string, name string, hcl string) error {
	if f.policies == nil {
		f.policies = map[string]string{}
	}
	f.policies[name] = hcl
	return nil
}

func (f *fakeOpenBaoClient) WriteJWTRole(_ context.Context, _ string, path string, name string, role openBaoJWTRole) error {
	if f.jwtRoles == nil {
		f.jwtRoles = map[string]openBaoJWTRole{}
	}
	f.jwtRoles[path+"/"+name] = role
	return nil
}

func (f *fakeOpenBaoClient) WriteKV2Data(_ context.Context, _ string, path string, data map[string]string) error {
	if f.kvData == nil {
		f.kvData = map[string]map[string]string{}
	}
	f.kvData[path] = data
	return nil
}

func (f *fakeOpenBaoClient) CreateToken(_ context.Context, _ string, spec openBaoTokenSpec) (string, error) {
	f.createdTokens = append(f.createdTokens, spec)
	return fmt.Sprintf("created-token-%d", len(f.createdTokens)), nil
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

func TestFreshInitBootstrapsOperatorImportHandoff(t *testing.T) {
	cfg := testConfig(t)
	cfg.pgpKeys = writeTestPGPRecipientFiles(t, 3)
	cfg.initOutputPath = filepath.Join(t.TempDir(), "init-material.json")
	cfg.secretPaths = []openBaoSecretPathSpec{
		{
			Name:   "cloudflare.account-admin",
			Path:   "kv-controller/data/integrations/cloudflare/account-admin",
			Key:    "api_token",
			Source: "operatorImport",
		},
		{
			Name:   "postgresql.pgbackrest.cipher_pass",
			Path:   "kv-runtime/data/secret/org/postgresql.pgbackrest.cipher_pass",
			Key:    "value",
			Source: "generated",
			Generate: &openBaoGenerateSpec{
				Bytes:    32,
				Encoding: "base64url",
			},
		},
		{
			Name:   "object-storage-service.r2.admin_access_key_id",
			Path:   "kv-runtime/data/secret/org/object-storage-service.r2.admin_access_key_id",
			Key:    "value",
			Source: "producedBy",
			ProducerRef: &objectRef{
				APIVersion: "cloudflare.guardianintelligence.org/v1alpha1",
				Kind:       "CloudflareControlPlane",
				Name:       "gamma-cloudflare",
			},
		},
		{
			Name:   "cloudflare.r2.recovery",
			Path:   "kv-controller/data/integrations/cloudflare/r2/capabilities/recovery",
			Key:    "access_key_id",
			Source: "producedBy",
			ProducerRef: &objectRef{
				APIVersion: "cloudflare.guardianintelligence.org/v1alpha1",
				Kind:       "CloudflareControlPlane",
				Name:       "gamma-cloudflare",
			},
		},
	}
	client := &fakeOpenBaoClient{
		status:       baoStatus{Initialized: false, Sealed: true},
		rootToken:    "root-token-secret",
		unsealShares: []string{"share-a", "share-b", "share-c"},
	}

	rep := recoverOnce(context.Background(), cfg, client, strings.NewReader(""))

	assertCondition(t, rep, "OpenBaoSecretStoreBootstrapped", "True", "BootstrapComplete")
	assertCondition(t, rep, "OpenBaoTransientTokenRevoked", "True", "Revoked")
	if !containsString(client.enabledMounts, "kv-controller") || !containsString(client.enabledMounts, "kv-runtime") {
		t.Fatalf("enabled mounts = %#v", client.enabledMounts)
	}
	if got := client.kvData["kv-runtime/data/secret/org/postgresql.pgbackrest.cipher_pass"]["value"]; got == "" {
		t.Fatalf("generated pgBackRest cipher pass was not written: %#v", client.kvData)
	}
	policy := client.policies["guardian-operator-import"]
	if !strings.Contains(policy, `path "kv-controller/data/integrations/cloudflare/account-admin"`) {
		t.Fatalf("operator import policy = %q", policy)
	}
	if strings.Contains(policy, "postgresql.pgbackrest") {
		t.Fatalf("operator import policy included generated secret path: %q", policy)
	}
	cloudflarePolicy := client.policies[cloudflareRecoveryRole]
	if !strings.Contains(cloudflarePolicy, `path "kv-controller/data/integrations/cloudflare/account-admin"`) {
		t.Fatalf("cloudflare recovery policy missing account-admin read: %q", cloudflarePolicy)
	}
	if !strings.Contains(cloudflarePolicy, `path "kv-runtime/data/secret/org/object-storage-service.r2.admin_access_key_id"`) {
		t.Fatalf("cloudflare recovery policy missing produced path write: %q", cloudflarePolicy)
	}
	postgresqlPolicy := client.policies[postgresqlRuntimeRole]
	if !strings.Contains(postgresqlPolicy, `path "kv-controller/data/integrations/cloudflare/r2/capabilities/recovery"`) {
		t.Fatalf("postgresql runtime policy missing R2 recovery read: %q", postgresqlPolicy)
	}
	if !strings.Contains(postgresqlPolicy, `path "kv-runtime/data/secret/org/postgresql.pgbackrest.cipher_pass"`) {
		t.Fatalf("postgresql runtime policy missing cipher pass read: %q", postgresqlPolicy)
	}
	if !containsString(client.enabledAuth, nomadJWTAuthPath) {
		t.Fatalf("enabled auth methods = %#v", client.enabledAuth)
	}
	if got := client.jwtConfigs[nomadJWTAuthPath]; got.JWKSURL != nomadJWKSURL || len(got.JWTSupportedAlgs) != 2 {
		t.Fatalf("jwt config = %#v", got)
	}
	role, ok := client.jwtRoles[nomadJWTAuthPath+"/"+cloudflareRecoveryRole]
	if !ok {
		t.Fatalf("jwt roles = %#v", client.jwtRoles)
	}
	if role.BoundClaims["nomad_job_id"] != cloudflareRecoveryRole || len(role.TokenPolicies) != 1 || role.TokenPolicies[0] != cloudflareRecoveryRole {
		t.Fatalf("jwt role = %#v", role)
	}
	postgresqlRole, ok := client.jwtRoles[nomadJWTAuthPath+"/"+postgresqlRuntimeRole]
	if !ok {
		t.Fatalf("jwt roles = %#v", client.jwtRoles)
	}
	if postgresqlRole.BoundClaims["nomad_job_id"] != postgresqlJobID || len(postgresqlRole.TokenPolicies) != 1 || postgresqlRole.TokenPolicies[0] != postgresqlRuntimeRole {
		t.Fatalf("postgresql jwt role = %#v", postgresqlRole)
	}
	if len(client.createdTokens) != 1 {
		t.Fatalf("created tokens = %#v", client.createdTokens)
	}
	if got := client.createdTokens[0]; len(got.Policies) != 1 || got.Policies[0] != "guardian-operator-import" || got.TTL != operatorImportTokenTTL || got.NumUses != operatorImportTokenUses || !got.Orphan {
		t.Fatalf("created token spec = %#v", got)
	}
	body, err := os.ReadFile(cfg.initOutputPath)
	if err != nil {
		t.Fatal(err)
	}
	assertDoesNotContain(t, body, "root-token-secret")
	assertDoesNotContain(t, body, "share-a")
	assertDoesNotContain(t, body, "created-token-1")
	var material encryptedInitMaterial
	if err := json.Unmarshal(body, &material); err != nil {
		t.Fatal(err)
	}
	if len(material.Spec.OperatorImportTokens) != 1 {
		t.Fatalf("operator import handoff = %#v", material.Spec.OperatorImportTokens)
	}
	if got := material.Spec.OperatorImportTokens[0]; got.Name != "guardian-operator-import" || got.Policy != "guardian-operator-import" || got.TTL != operatorImportTokenTTL || got.Uses != operatorImportTokenUses || len(got.EncryptedTokensB64) != 3 {
		t.Fatalf("operator import handoff = %#v", got)
	}
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

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
