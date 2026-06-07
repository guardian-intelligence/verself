package main

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ProtonMail/go-crypto/openpgp"
)

type fakeOpenBaoClient struct {
	status                 baoStatus
	statuses               []baoStatus
	rootToken              string
	unsealShares           []string
	snapshot               []byte
	baselineTokens         []string
	baselines              []openBaoBaselineSpec
	jwtAuthPath            string
	jwtRole                string
	jwtToken               string
	jwtLoginToken          string
	jwtLoginErr            error
	createdTokens          []createdTokenCall
	revokedTokens          []string
	restored               bool
	reconcileErrs          []error
	reconcileErr           error
	revokeErr              error
	generatedRootToken     string
	generateRootNonce      string
	generateRootOTP        string
	generateRootEncoded    string
	generateRootShares     []string
	generateRootCanceled   bool
	generateRootInitErr    error
	generateRootUpdateErr  error
	generateRootCancelErr  error
	decodeGeneratedRootErr error
}

type createdTokenCall struct {
	rootToken string
	spec      openBaoOperatorImportTokenSpec
	token     string
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
			shares = append(shares, "generated-share")
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

func (f *fakeOpenBaoClient) ReconcileBaseline(_ context.Context, token string, baseline openBaoBaselineSpec) error {
	f.baselineTokens = append(f.baselineTokens, token)
	f.baselines = append(f.baselines, baseline)
	if len(f.reconcileErrs) > 0 {
		err := f.reconcileErrs[0]
		f.reconcileErrs = f.reconcileErrs[1:]
		return err
	}
	return f.reconcileErr
}

func (f *fakeOpenBaoClient) RevokeSelf(_ context.Context, token string) error {
	f.revokedTokens = append(f.revokedTokens, token)
	return f.revokeErr
}

func (f *fakeOpenBaoClient) LoginJWT(_ context.Context, authPath string, role string, jwt string) (string, error) {
	f.jwtAuthPath = authPath
	f.jwtRole = role
	f.jwtToken = jwt
	if f.jwtLoginErr != nil {
		return "", f.jwtLoginErr
	}
	if strings.TrimSpace(f.jwtLoginToken) == "" {
		return "", errors.New("jwt login token is empty")
	}
	return f.jwtLoginToken, nil
}

func (f *fakeOpenBaoClient) CreateToken(_ context.Context, rootToken string, spec openBaoOperatorImportTokenSpec) (string, error) {
	token := "token-" + randomHex(16)
	f.createdTokens = append(f.createdTokens, createdTokenCall{rootToken: rootToken, spec: spec, token: token})
	return token, nil
}

func (f *fakeOpenBaoClient) GenerateRootInit(context.Context) (generateRootAttempt, error) {
	if f.generateRootInitErr != nil {
		return generateRootAttempt{}, f.generateRootInitErr
	}
	if f.generateRootNonce == "" {
		f.generateRootNonce = "nonce-" + randomHex(8)
	}
	if f.generateRootOTP == "" {
		f.generateRootOTP = "otp-" + randomHex(8)
	}
	if f.generateRootEncoded == "" {
		f.generateRootEncoded = "encoded-" + randomHex(8)
	}
	required := f.status.Threshold
	if required <= 0 {
		required = 2
	}
	return generateRootAttempt{
		Started:  true,
		Nonce:    f.generateRootNonce,
		Required: required,
		OTP:      f.generateRootOTP,
	}, nil
}

func (f *fakeOpenBaoClient) GenerateRootUpdate(_ context.Context, share string, nonce string) (generateRootAttempt, error) {
	if f.generateRootUpdateErr != nil {
		return generateRootAttempt{}, f.generateRootUpdateErr
	}
	if nonce != f.generateRootNonce {
		return generateRootAttempt{}, fmt.Errorf("unexpected nonce %q", nonce)
	}
	f.generateRootShares = append(f.generateRootShares, share)
	required := f.status.Threshold
	if required <= 0 {
		required = 2
	}
	attempt := generateRootAttempt{
		Started:  true,
		Nonce:    f.generateRootNonce,
		Progress: len(f.generateRootShares),
		Required: required,
	}
	if len(f.generateRootShares) >= required {
		attempt.Complete = true
		attempt.EncodedToken = f.generateRootEncoded
	}
	return attempt, nil
}

func (f *fakeOpenBaoClient) GenerateRootCancel(context.Context) error {
	f.generateRootCanceled = true
	return f.generateRootCancelErr
}

func (f *fakeOpenBaoClient) DecodeGeneratedRootToken(_ context.Context, encoded string, otp string) (string, error) {
	if f.decodeGeneratedRootErr != nil {
		return "", f.decodeGeneratedRootErr
	}
	if encoded != f.generateRootEncoded {
		return "", fmt.Errorf("unexpected encoded token %q", encoded)
	}
	if otp != f.generateRootOTP {
		return "", fmt.Errorf("unexpected OTP %q", otp)
	}
	if strings.TrimSpace(f.generatedRootToken) == "" {
		return "", errors.New("generated root token is empty")
	}
	return f.generatedRootToken, nil
}

func TestFreshInitWritesEncryptedInitMaterial(t *testing.T) {
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
		cfg.pgpKeys = writeTestPGPRecipientFiles(t, 3)
		cfg.initOutputPath = filepath.Join(t.TempDir(), "init-material.json")

		rep := recoverOnce(context.Background(), cfg, client, bytes.NewReader(nil))

		assertCondition(t, rep, "OpenBaoInitialized", "True", "FreshInitComplete")
		assertCondition(t, rep, "OpenBaoInitMaterialDelivered", "True", "InitOutputWritten")
		assertCondition(t, rep, "OpenBaoUnsealed", "True", "UnsealComplete")
		assertCondition(t, rep, "OpenBaoTransientTokenRevoked", "True", "Revoked")
		assertCondition(t, rep, "OpenBaoRecoveryComplete", "True", "Recovered")
		if len(client.baselineTokens) != 0 {
			t.Fatalf("baseline reconciled when baseline reconcile was disabled")
		}
		if len(client.revokedTokens) != 1 || client.revokedTokens[0] != rootToken {
			t.Fatalf("initial root token was not revoked after fresh init")
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
		if decoded.APIVersion != "openbao.guardianintelligence.org/v1alpha1" {
			t.Fatalf("init material apiVersion = %q", decoded.APIVersion)
		}
		if len(decoded.Spec.EncryptedUnsealSharesB64) != 3 {
			t.Fatalf("encrypted share count = %d", len(decoded.Spec.EncryptedUnsealSharesB64))
		}
		for _, encrypted := range decoded.Spec.EncryptedUnsealSharesB64 {
			if _, err := base64.StdEncoding.DecodeString(encrypted); err != nil {
				t.Fatalf("encrypted share was not base64: %v", err)
			}
		}
	}
}

func TestFreshInitReconcilesBaselineWithInitialRootTokenAndRevokes(t *testing.T) {
	rootToken := randomSecret(t)
	client := &fakeOpenBaoClient{
		status:       baoStatus{Initialized: false, Sealed: true, Version: "2.5.2", SealType: "shamir"},
		rootToken:    rootToken,
		unsealShares: []string{randomSecret(t), randomSecret(t), randomSecret(t)},
	}
	cfg := testConfig(t)
	cfg.keyShares = 3
	cfg.threshold = 2
	cfg.pgpKeys = writeTestPGPRecipientFiles(t, 3)
	cfg.initOutputPath = filepath.Join(t.TempDir(), "init-material.json")
	cfg.baseline = openBaoBaselineSpec{Reconcile: true}

	rep := recoverOnce(context.Background(), cfg, client, bytes.NewReader(nil))

	assertCondition(t, rep, "OpenBaoInitialized", "True", "FreshInitComplete")
	assertCondition(t, rep, "OpenBaoUnsealed", "True", "UnsealComplete")
	assertCondition(t, rep, "OpenBaoBaselineReconciled", "True", "BaselineReady")
	assertCondition(t, rep, "OpenBaoTransientTokenRevoked", "True", "Revoked")
	assertCondition(t, rep, "OpenBaoRecoveryComplete", "True", "Recovered")
	if len(client.baselineTokens) != 1 || client.baselineTokens[0] != rootToken {
		t.Fatalf("baseline did not use initial root token")
	}
	if len(client.revokedTokens) != 1 || client.revokedTokens[0] != rootToken {
		t.Fatalf("initial root token was not revoked")
	}
	assertReportDoesNotContain(t, rep, rootToken)
	for _, share := range client.unsealShares {
		assertReportDoesNotContain(t, rep, share)
	}
}

func TestFreshInitWritesEncryptedOperatorImportTokenHandoff(t *testing.T) {
	rootToken := randomSecret(t)
	client := &fakeOpenBaoClient{
		status:       baoStatus{Initialized: false, Sealed: true, Version: "2.5.2", SealType: "shamir"},
		rootToken:    rootToken,
		unsealShares: []string{randomSecret(t), randomSecret(t), randomSecret(t)},
	}
	cfg := testConfig(t)
	cfg.keyShares = 3
	cfg.threshold = 2
	cfg.pgpKeys = writeTestPGPRecipientFiles(t, 3)
	cfg.initOutputPath = filepath.Join(t.TempDir(), "init-material.json")
	cfg.baseline = openBaoBaselineSpec{
		Reconcile: true,
		OperatorImportTokens: []openBaoOperatorImportTokenSpec{
			{Name: "cloudflare-account-admin-import", Policy: "cloudflare-account-admin-import", TTL: "4h", Uses: 5},
		},
	}

	rep := recoverOnce(context.Background(), cfg, client, bytes.NewReader(nil))

	assertCondition(t, rep, "OpenBaoOperatorImportTokenDelivered", "True", "EncryptedImportTokensWritten")
	assertCondition(t, rep, "OpenBaoTransientTokenRevoked", "True", "Revoked")
	assertCondition(t, rep, "OpenBaoRecoveryComplete", "True", "Recovered")
	if len(client.createdTokens) != 1 {
		t.Fatalf("created tokens = %d", len(client.createdTokens))
	}
	if client.createdTokens[0].rootToken != rootToken {
		t.Fatalf("operator import token was not minted by initial root token")
	}
	if client.createdTokens[0].spec.Policy != "cloudflare-account-admin-import" {
		t.Fatalf("operator import policy = %q", client.createdTokens[0].spec.Policy)
	}
	material, err := os.ReadFile(cfg.initOutputPath)
	if err != nil {
		t.Fatal(err)
	}
	var decoded encryptedInitMaterial
	if err := json.Unmarshal(material, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Spec.OperatorImportTokens) != 1 {
		t.Fatalf("operator import handoffs = %#v", decoded.Spec.OperatorImportTokens)
	}
	handoff := decoded.Spec.OperatorImportTokens[0]
	if handoff.Name != "cloudflare-account-admin-import" || handoff.Policy != "cloudflare-account-admin-import" || handoff.TTL != "4h" || handoff.Uses != 5 {
		t.Fatalf("handoff metadata = %#v", handoff)
	}
	if len(handoff.EncryptedTokensB64) != 3 {
		t.Fatalf("encrypted token count = %d", len(handoff.EncryptedTokensB64))
	}
	if bytes.Contains(material, []byte(rootToken)) || bytes.Contains(material, []byte(client.createdTokens[0].token)) {
		t.Fatalf("init material leaked plaintext authority: %s", material)
	}
	assertReportDoesNotContain(t, rep, rootToken)
	assertReportDoesNotContain(t, rep, client.createdTokens[0].token)
}

func TestSealedOpenBaoRequiresUnsealMaterial(t *testing.T) {
	share := randomSecret(t)
	client := &fakeOpenBaoClient{
		status: baoStatus{Initialized: true, Sealed: true, Threshold: 2, Progress: 0},
		unsealShares: []string{
			share,
		},
	}
	cfg := testConfig(t)

	rep := recoverOnce(context.Background(), cfg, client, bytes.NewReader(nil))

	assertCondition(t, rep, "OpenBaoUnsealed", "False", "UnsealQuorumIncomplete")
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

	assertCondition(t, rep, "OpenBaoUnsealed", "True", "UnsealComplete")
	assertCondition(t, rep, "OpenBaoRecoveryComplete", "True", "Recovered")
	assertReportDoesNotContain(t, rep, share)
}

func TestSealedOpenBaoReportsBaselineBlockedAfterUnsealWithoutRootAuthority(t *testing.T) {
	share := randomSecret(t)
	client := &fakeOpenBaoClient{
		status:       baoStatus{Initialized: true, Sealed: true, Threshold: 1},
		unsealShares: []string{share},
	}
	cfg := testConfig(t)
	cfg.unsealStdin = true
	cfg.baseline = openBaoBaselineSpec{Reconcile: true}

	rep := recoverOnce(context.Background(), cfg, client, strings.NewReader(share+"\n"))

	assertCondition(t, rep, "OpenBaoUnsealed", "True", "UnsealComplete")
	assertCondition(t, rep, "OpenBaoBaselineReconciled", "False", "BaselineAuthorityRequired")
	assertCondition(t, rep, "OpenBaoRecoveryComplete", "False", "BaselineBlocked")
	if len(client.baselineTokens) != 0 {
		t.Fatalf("baseline reconciled without operator authority")
	}
	assertReportDoesNotContain(t, rep, share)
}

func TestSealedOpenBaoBreakglassGeneratesRootTokenAfterUnsealForBaseline(t *testing.T) {
	token := randomSecret(t)
	shareA := randomSecret(t)
	shareB := randomSecret(t)
	client := &fakeOpenBaoClient{
		status:             baoStatus{Initialized: true, Sealed: true, Threshold: 2},
		unsealShares:       []string{shareA, shareB},
		generatedRootToken: token,
	}
	cfg := testConfig(t)
	cfg.breakglassGenerateRootStdin = true
	cfg.baseline = openBaoBaselineSpec{Reconcile: true}

	rep := recoverOnce(context.Background(), cfg, client, strings.NewReader(shareA+"\n"+shareB+"\n"))

	assertCondition(t, rep, "OpenBaoUnsealed", "True", "UnsealComplete")
	assertCondition(t, rep, "OpenBaoBreakglassRootToken", "True", "BreakglassGenerated")
	assertCondition(t, rep, "OpenBaoBaselineReconciled", "True", "BaselineReady")
	assertCondition(t, rep, "OpenBaoTransientTokenRevoked", "True", "Revoked")
	assertCondition(t, rep, "OpenBaoRecoveryComplete", "True", "Recovered")
	if len(client.baselineTokens) != 1 || client.baselineTokens[0] != token {
		t.Fatalf("baseline did not use generated root token")
	}
	if len(client.revokedTokens) != 1 || client.revokedTokens[0] != token {
		t.Fatalf("generated root token was not revoked after baseline reconciliation")
	}
	if client.generateRootCanceled {
		t.Fatalf("completed generate-root attempt was canceled")
	}
	assertReportDoesNotContain(t, rep, token)
	assertReportDoesNotContain(t, rep, shareA)
	assertReportDoesNotContain(t, rep, shareB)
}

func TestUnsealedOpenBaoRequiresNoOperatorIntervention(t *testing.T) {
	token := randomSecret(t)
	client := &fakeOpenBaoClient{
		status:    baoStatus{Initialized: true, Sealed: false},
		rootToken: token,
	}
	cfg := testConfig(t)

	rep := recoverOnce(context.Background(), cfg, client, bytes.NewReader(nil))

	assertCondition(t, rep, "OpenBaoRecoveryComplete", "True", "Available")
	if len(client.baselineTokens) != 0 {
		t.Fatalf("baseline reconciled without an operator token")
	}
	assertReportDoesNotContain(t, rep, token)
}

func TestUnsealedOpenBaoReportsBaselineBlockedWithoutOperatorToken(t *testing.T) {
	client := &fakeOpenBaoClient{
		status: baoStatus{Initialized: true, Sealed: false},
	}
	cfg := testConfig(t)
	cfg.baseline = openBaoBaselineSpec{Reconcile: true}

	rep := recoverOnce(context.Background(), cfg, client, bytes.NewReader(nil))

	assertCondition(t, rep, "OpenBaoBaselineReconciled", "False", "BaselineAuthorityRequired")
	assertCondition(t, rep, "OpenBaoRecoveryComplete", "False", "BaselineBlocked")
	if len(client.baselineTokens) != 0 {
		t.Fatalf("baseline reconciled without an operator token")
	}
}

func TestUnsealedOpenBaoUsesNomadWorkloadJWTForBaseline(t *testing.T) {
	jwt := randomSecret(t)
	token := randomSecret(t)
	jwtFile := filepath.Join(t.TempDir(), "openbao-reconcile.jwt")
	if err := os.WriteFile(jwtFile, []byte(jwt+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	client := &fakeOpenBaoClient{
		status:        baoStatus{Initialized: true, Sealed: false},
		jwtLoginToken: token,
	}
	cfg := testConfig(t)
	cfg.initOutputPath = filepath.Join(t.TempDir(), "init-material.json")
	body, err := json.Marshal(encryptedInitMaterial{
		Spec: encryptedInitMaterialSpec{
			OperatorImportTokens: []encryptedOperatorImportTokenMaterial{
				{Name: "cloudflare-account-admin-import", Policy: "cloudflare-account-admin-import", TTL: "4h", Uses: 5, EncryptedTokensB64: []string{"encrypted"}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.initOutputPath, body, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.nomadWorkloadJWTFile = jwtFile
	cfg.nomadWorkloadRole = "openbao-reconcile-runtime"
	cfg.baseline = openBaoBaselineSpec{
		Reconcile: true,
		NomadJWT:  &openBaoNomadJWTAuthSpec{Path: "jwt-nomad"},
		OperatorImportTokens: []openBaoOperatorImportTokenSpec{
			{Name: "cloudflare-account-admin-import", Policy: "cloudflare-account-admin-import", TTL: "4h", Uses: 5},
		},
	}

	rep := recoverOnce(context.Background(), cfg, client, bytes.NewReader(nil))

	assertCondition(t, rep, "OpenBaoWorkloadToken", "True", "JWTAccepted")
	assertCondition(t, rep, "OpenBaoOperatorImportTokenDelivered", "True", "EncryptedImportTokensPresent")
	assertCondition(t, rep, "OpenBaoBaselineReconciled", "True", "BaselineReady")
	assertCondition(t, rep, "OpenBaoTransientTokenRevoked", "True", "Revoked")
	assertCondition(t, rep, "OpenBaoRecoveryComplete", "True", "Recovered")
	if client.jwtAuthPath != "jwt-nomad" || client.jwtRole != "openbao-reconcile-runtime" || client.jwtToken != jwt {
		t.Fatalf("jwt login = path %q role %q token %q", client.jwtAuthPath, client.jwtRole, client.jwtToken)
	}
	if len(client.baselineTokens) != 1 || client.baselineTokens[0] != token {
		t.Fatalf("baseline did not use workload token")
	}
	if len(client.revokedTokens) != 1 || client.revokedTokens[0] != token {
		t.Fatalf("workload token was not revoked after baseline reconciliation")
	}
	assertReportDoesNotContain(t, rep, jwt)
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

	assertCondition(t, rep, "OpenBaoRecoveryComplete", "True", "Recovered")
	if len(client.baselineTokens) != 1 || client.baselineTokens[0] != token {
		t.Fatalf("baseline did not use presented token")
	}
	if len(client.revokedTokens) != 1 || client.revokedTokens[0] != token {
		t.Fatalf("operator token was not revoked after baseline reconciliation")
	}
	assertReportDoesNotContain(t, rep, token)
}

func TestBaselineReconcileRetriesTransientOpenBaoErrors(t *testing.T) {
	token := randomSecret(t)
	client := &fakeOpenBaoClient{
		status: baoStatus{Initialized: true, Sealed: false},
		reconcileErrs: []error{
			errors.New(`openbao GET sys/mounts status 500: {"errors":["internal error"]}`),
			nil,
		},
	}
	cfg := testConfig(t)
	cfg.tokenStdin = true
	cfg.baseline = openBaoBaselineSpec{Reconcile: true}

	rep := recoverOnce(context.Background(), cfg, client, strings.NewReader(token+"\n"))

	assertCondition(t, rep, "OpenBaoBaselineReconciled", "True", "BaselineReady")
	assertCondition(t, rep, "OpenBaoTransientTokenRevoked", "True", "Revoked")
	assertCondition(t, rep, "OpenBaoRecoveryComplete", "True", "Recovered")
	if len(client.baselineTokens) != 2 {
		t.Fatalf("baseline reconcile attempts = %d", len(client.baselineTokens))
	}
	if len(client.revokedTokens) != 1 || client.revokedTokens[0] != token {
		t.Fatalf("operator token was not revoked after transient baseline retry")
	}
	assertReportDoesNotContain(t, rep, token)
}

func TestUnsealedOpenBaoRevokesOperatorTokenAfterBaselineFailure(t *testing.T) {
	token := randomSecret(t)
	client := &fakeOpenBaoClient{
		status:       baoStatus{Initialized: true, Sealed: false},
		reconcileErr: errors.New("policy write failed"),
	}
	cfg := testConfig(t)
	cfg.tokenStdin = true
	cfg.baseline = openBaoBaselineSpec{Reconcile: true}

	rep := recoverOnce(context.Background(), cfg, client, strings.NewReader(token+"\n"))

	assertCondition(t, rep, "OpenBaoBaselineReconciled", "False", "ReconcileFailed")
	assertCondition(t, rep, "OpenBaoTransientTokenRevoked", "True", "Revoked")
	assertCondition(t, rep, "OpenBaoRecoveryComplete", "False", "BaselineFailed")
	if len(client.revokedTokens) != 1 || client.revokedTokens[0] != token {
		t.Fatalf("operator token was not revoked after baseline failure")
	}
	assertReportDoesNotContain(t, rep, token)
}

func TestUnsealedOpenBaoReportsInsufficientOperatorTokenAuthority(t *testing.T) {
	token := randomSecret(t)
	client := &fakeOpenBaoClient{
		status:       baoStatus{Initialized: true, Sealed: false},
		reconcileErr: errors.New(`openbao GET sys/mounts status 403: {"errors":["permission denied"]}`),
	}
	cfg := testConfig(t)
	cfg.tokenStdin = true
	cfg.baseline = openBaoBaselineSpec{Reconcile: true}

	rep := recoverOnce(context.Background(), cfg, client, strings.NewReader(token+"\n"))

	assertCondition(t, rep, "OpenBaoBaselineReconciled", "False", "BaselineAuthorityInsufficient")
	assertCondition(t, rep, "OpenBaoRecoveryComplete", "False", "BaselineBlocked")
	assertCondition(t, rep, "OpenBaoTransientTokenRevoked", "True", "Revoked")
	assertReportDoesNotContain(t, rep, token)
}

func TestUnsealedOpenBaoReportsOperatorTokenRevocationFailure(t *testing.T) {
	token := randomSecret(t)
	client := &fakeOpenBaoClient{
		status:    baoStatus{Initialized: true, Sealed: false},
		revokeErr: errors.New("revoke denied"),
	}
	cfg := testConfig(t)
	cfg.tokenStdin = true
	cfg.baseline = openBaoBaselineSpec{Reconcile: true}

	rep := recoverOnce(context.Background(), cfg, client, strings.NewReader(token+"\n"))

	assertCondition(t, rep, "OpenBaoBaselineReconciled", "True", "BaselineReady")
	assertCondition(t, rep, "OpenBaoTransientTokenRevoked", "False", "RevokeSelfFailed")
	assertCondition(t, rep, "OpenBaoRecoveryComplete", "False", "TransientTokenRevocationFailed")
	assertReportDoesNotContain(t, rep, token)
}

func TestUnsealedOpenBaoBreakglassGeneratesRootTokenFromUnsealSharesForBaseline(t *testing.T) {
	token := randomSecret(t)
	shareA := randomSecret(t)
	shareB := randomSecret(t)
	client := &fakeOpenBaoClient{
		status:             baoStatus{Initialized: true, Sealed: false, Threshold: 2},
		generatedRootToken: token,
	}
	cfg := testConfig(t)
	cfg.breakglassGenerateRootStdin = true
	cfg.baseline = openBaoBaselineSpec{Reconcile: true}

	rep := recoverOnce(context.Background(), cfg, client, strings.NewReader(shareA+"\n"+shareB+"\n"))

	assertCondition(t, rep, "OpenBaoBreakglassRootToken", "True", "BreakglassGenerated")
	assertCondition(t, rep, "OpenBaoBaselineReconciled", "True", "BaselineReady")
	assertCondition(t, rep, "OpenBaoTransientTokenRevoked", "True", "Revoked")
	assertCondition(t, rep, "OpenBaoRecoveryComplete", "True", "Recovered")
	if len(client.baselineTokens) != 1 || client.baselineTokens[0] != token {
		t.Fatalf("baseline did not use generated root token")
	}
	if len(client.revokedTokens) != 1 || client.revokedTokens[0] != token {
		t.Fatalf("generated root token was not revoked after baseline reconciliation")
	}
	if client.generateRootCanceled {
		t.Fatalf("completed generate-root attempt was canceled")
	}
	assertReportDoesNotContain(t, rep, token)
	assertReportDoesNotContain(t, rep, shareA)
	assertReportDoesNotContain(t, rep, shareB)
}

func TestUnsealedOpenBaoBreakglassCancelsIncompleteGenerateRootAttempt(t *testing.T) {
	token := randomSecret(t)
	share := randomSecret(t)
	client := &fakeOpenBaoClient{
		status:             baoStatus{Initialized: true, Sealed: false, Threshold: 2},
		generatedRootToken: token,
	}
	cfg := testConfig(t)
	cfg.breakglassGenerateRootStdin = true
	cfg.baseline = openBaoBaselineSpec{Reconcile: true}

	rep := recoverOnce(context.Background(), cfg, client, strings.NewReader(share+"\n"))

	assertCondition(t, rep, "OpenBaoBreakglassRootToken", "False", "BreakglassGenerateRootFailed")
	assertCondition(t, rep, "OpenBaoRecoveryComplete", "False", "BaselineBlocked")
	if !client.generateRootCanceled {
		t.Fatalf("incomplete generate-root attempt was not canceled")
	}
	if len(client.baselineTokens) != 0 {
		t.Fatalf("baseline reconciled without generated root token")
	}
	if len(client.revokedTokens) != 0 {
		t.Fatalf("token revocation ran without a generated root token")
	}
	assertReportDoesNotContain(t, rep, token)
	assertReportDoesNotContain(t, rep, share)
}

func TestUnsealedOpenBaoClassifiesOperatorTokenRevocationPermissionDenied(t *testing.T) {
	token := randomSecret(t)
	client := &fakeOpenBaoClient{
		status:    baoStatus{Initialized: true, Sealed: false},
		revokeErr: errors.New(`openbao POST auth/token/revoke-self status 403: {"errors":["permission denied"]}`),
	}
	cfg := testConfig(t)
	cfg.tokenStdin = true
	cfg.baseline = openBaoBaselineSpec{Reconcile: true}

	rep := recoverOnce(context.Background(), cfg, client, strings.NewReader(token+"\n"))

	assertCondition(t, rep, "OpenBaoBaselineReconciled", "True", "BaselineReady")
	assertCondition(t, rep, "OpenBaoTransientTokenRevoked", "False", "RevokeSelfPermissionDenied")
	assertCondition(t, rep, "OpenBaoRecoveryComplete", "False", "TransientTokenRevocationFailed")
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
	if len(client.revokedTokens) != 1 || client.revokedTokens[0] != token {
		t.Fatalf("operator token was not revoked after baseline reconciliation")
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
	assertCondition(t, rep, "OpenBaoUnsealed", "False", "UnsealQuorumIncomplete")
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

	assertCondition(t, rep, "OpenBaoInitialized", "False", "InitRecipientIdentityRequired")
	assertCondition(t, rep, "OpenBaoRecoveryComplete", "False", "WaitingForInit")
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

	rep := recoverOnce(context.Background(), cfg, client, bytes.NewReader(nil))

	assertCondition(t, rep, "OpenBaoInitMaterialDelivered", "False", "InitMaterialDeliveryRequired")
	assertCondition(t, rep, "OpenBaoRecoveryComplete", "False", "WaitingForInit")
	if len(client.baselineTokens) != 0 {
		t.Fatalf("baseline reconciled before init material delivery was configured")
	}
	assertReportDoesNotContain(t, rep, client.rootToken)
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
					"caCert":           "/etc/verself/openbao/ca.pem",
					"runtimeRoot":      "/var/lib/openbao/runtime-from-graph",
					"dataDir":          "/var/lib/openbao/raft-from-graph",
					"configPath":       "/etc/openbao/openbao-from-graph.hcl",
					"reportPath":       "/run/verself/recovery/openbao/report-from-graph.json",
					"initMaterialPath": "/run/verself/recovery/openbao/init-material-from-graph.json",
					"seal": map[string]any{
						"shamir": map[string]any{
							"keyShares":    3,
							"keyThreshold": 2,
							"pgpRecipientRefs": []map[string]any{
								{"apiVersion": "openbao.guardianintelligence.org/v1alpha1", "kind": "PGPRecipient", "name": "operator-a"},
								{"apiVersion": "openbao.guardianintelligence.org/v1alpha1", "kind": "PGPRecipient", "name": "operator-b"},
								{"apiVersion": "openbao.guardianintelligence.org/v1alpha1", "kind": "PGPRecipient", "name": "operator-c"},
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
			secretPath("object-storage-service.credential_kek", map[string]any{
				"path":   "kv-runtime/data/secret/org/object-storage-service.credential_kek",
				"key":    "value",
				"source": "generated",
				"generate": map[string]any{
					"bytes":    32,
					"encoding": "hex",
				},
			}),
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
	if len(cfg.pgpKeys) != 3 {
		t.Fatalf("pgpKeys = %#v", cfg.pgpKeys)
	}
	if !cfg.baseline.Reconcile || cfg.baseline.NomadJWT == nil || len(cfg.baseline.NomadJWT.Roles) != 1 {
		t.Fatalf("baseline = %#v", cfg.baseline)
	}
	if len(cfg.baseline.SecretPaths) != 1 {
		t.Fatalf("secret paths = %#v", cfg.baseline.SecretPaths)
	}
	if got := cfg.baseline.SecretPaths[0].Path; got != "kv-runtime/data/secret/org/object-storage-service.credential_kek" {
		t.Fatalf("secret path = %q", got)
	}
	for _, path := range []string(cfg.pgpKeys) {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read materialized PGP key %s: %v", path, err)
		}
		if !strings.HasPrefix(string(body), "pubkey-") {
			t.Fatalf("unexpected PGP key body in %s: %q", path, body)
		}
	}

	reportOverride := filepath.Join(dir, "override-report.json")
	overrideCfg, err := parseConfig("test", []string{
		"--repo-root=" + dir,
		"--resource-graph=" + graphPath,
		"--resource-name=openbao",
		"--pgp-key-dir=" + pgpDir,
		"--report=" + reportOverride,
	}, true, false)
	if err != nil {
		t.Fatalf("parseConfig with report override: %v", err)
	}
	if overrideCfg.reportPath != reportOverride {
		t.Fatalf("reportPath override = %q, want %q", overrideCfg.reportPath, reportOverride)
	}
}

func TestGeneratedSecretValue(t *testing.T) {
	hexValue, err := generatedSecretValue(openBaoGenerateSpec{Bytes: 32, Encoding: "hex"})
	if err != nil {
		t.Fatalf("generatedSecretValue hex: %v", err)
	}
	if len(hexValue) != 64 {
		t.Fatalf("hex generated length = %d", len(hexValue))
	}
	if _, err := hex.DecodeString(hexValue); err != nil {
		t.Fatalf("hex generated value did not decode: %v", err)
	}

	base64Value, err := generatedSecretValue(openBaoGenerateSpec{Bytes: 32, Encoding: "base64url"})
	if err != nil {
		t.Fatalf("generatedSecretValue base64url: %v", err)
	}
	if strings.ContainsAny(base64Value, "+/=") {
		t.Fatalf("base64url generated value used non-url characters: %q", base64Value)
	}

	alphanumericValue, err := generatedSecretValue(openBaoGenerateSpec{Bytes: 32, Encoding: "alphanumeric"})
	if err != nil {
		t.Fatalf("generatedSecretValue alphanumeric: %v", err)
	}
	if len(alphanumericValue) != 32 {
		t.Fatalf("alphanumeric generated length = %d", len(alphanumericValue))
	}
	for _, char := range alphanumericValue {
		if !strings.ContainsRune("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789", char) {
			t.Fatalf("alphanumeric generated value used invalid character %q in %q", char, alphanumericValue)
		}
	}

	passwordValue, err := generatedSecretValue(openBaoGenerateSpec{Bytes: 32, Encoding: "password"})
	if err != nil {
		t.Fatalf("generatedSecretValue password: %v", err)
	}
	if !passwordMeetsBootstrapPolicy(passwordValue, 32) {
		t.Fatalf("password generated value did not meet bootstrap policy: %q", passwordValue)
	}
}

func TestGeneratedSecretMatchesValidatesEncodedLength(t *testing.T) {
	valid, err := generatedSecretMatches(openBaoGenerateSpec{Bytes: 32, Encoding: "base64url"}, "AQID")
	if err != nil {
		t.Fatalf("generatedSecretMatches: %v", err)
	}
	if valid {
		t.Fatalf("short base64url value matched 32-byte generator")
	}
	valid, err = generatedSecretMatches(openBaoGenerateSpec{Bytes: 3, Encoding: "base64url"}, "AQID")
	if err != nil {
		t.Fatalf("generatedSecretMatches: %v", err)
	}
	if !valid {
		t.Fatalf("3-byte base64url value did not match 3-byte generator")
	}
}

func TestEnsureGeneratedSecretWritesOnlyWhenAbsent(t *testing.T) {
	secret := openBaoSecretPathSpec{
		Name:   "object-storage-service.credential_kek",
		Path:   "kv-runtime/data/secret/org/object-storage-service.credential_kek",
		Key:    "value",
		Source: "generated",
		Generate: &openBaoGenerateSpec{
			Bytes:    32,
			Encoding: "hex",
		},
	}
	var posts int
	exists := false
	value := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Vault-Token") != "root-token" {
			t.Fatalf("OpenBao token header = %q", r.Header.Get("X-Vault-Token"))
		}
		if r.URL.Path != "/v1/"+secret.Path {
			t.Fatalf("OpenBao path = %s", r.URL.Path)
		}
		switch r.Method {
		case http.MethodGet:
			if exists {
				w.WriteHeader(http.StatusOK)
				_, _ = fmt.Fprintf(w, `{"data":{"data":{"value":%q}}}`, value)
				return
			}
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errors":[]}`))
		case http.MethodPost:
			posts++
			exists = true
			var body struct {
				Data map[string]string `json:"data"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode write body: %v", err)
			}
			if len(body.Data["value"]) != 64 {
				t.Fatalf("generated value length = %d", len(body.Data["value"]))
			}
			value = body.Data["value"]
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected OpenBao method %s", r.Method)
		}
	}))
	defer server.Close()
	client := &realOpenBaoClient{cfg: config{addr: server.URL}, client: server.Client()}

	if err := client.ensureGeneratedSecret(context.Background(), "root-token", secret); err != nil {
		t.Fatalf("first ensureGeneratedSecret: %v", err)
	}
	if err := client.ensureGeneratedSecret(context.Background(), "root-token", secret); err != nil {
		t.Fatalf("second ensureGeneratedSecret: %v", err)
	}
	if posts != 1 {
		t.Fatalf("generated secret writes = %d", posts)
	}
}

func TestEnsureGeneratedSecretRepairsInvalidPassword(t *testing.T) {
	secret := openBaoSecretPathSpec{
		Name:   "zitadel.admin_password",
		Path:   "kv-runtime/data/secret/org/zitadel.admin_password",
		Key:    "value",
		Source: "generated",
		Generate: &openBaoGenerateSpec{
			Bytes:    32,
			Encoding: "password",
		},
	}
	value := "abcdefghijklmnopqrstuvwxyz123456"
	var posts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/"+secret.Path {
			t.Fatalf("OpenBao path = %s", r.URL.Path)
		}
		switch r.Method {
		case http.MethodGet:
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintf(w, `{"data":{"data":{"value":%q}}}`, value)
		case http.MethodPost:
			posts++
			var body struct {
				Data map[string]string `json:"data"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode write body: %v", err)
			}
			value = body.Data["value"]
			if !passwordMeetsBootstrapPolicy(value, 32) {
				t.Fatalf("repaired password did not meet bootstrap policy: %q", value)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected OpenBao method %s", r.Method)
		}
	}))
	defer server.Close()
	client := &realOpenBaoClient{cfg: config{addr: server.URL}, client: server.Client()}

	if err := client.ensureGeneratedSecret(context.Background(), "root-token", secret); err != nil {
		t.Fatalf("first ensureGeneratedSecret: %v", err)
	}
	if err := client.ensureGeneratedSecret(context.Background(), "root-token", secret); err != nil {
		t.Fatalf("second ensureGeneratedSecret: %v", err)
	}
	if posts != 1 {
		t.Fatalf("generated secret repairs = %d", posts)
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

func secretPath(name string, spec map[string]any) map[string]any {
	return map[string]any{
		"apiVersion": "openbao.guardianintelligence.org/v1alpha1",
		"kind":       "SecretPath",
		"metadata": map[string]any{
			"name": name,
		},
		"spec": spec,
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

func randomSecret(t *testing.T) string {
	t.Helper()
	body := make([]byte, 32)
	if _, err := rand.Read(body); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(body)
}

func randomHex(n int) string {
	body := make([]byte, n)
	if _, err := rand.Read(body); err != nil {
		panic(err)
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
