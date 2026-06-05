package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/scrypt"
)

const (
	wrappedKeyVersion = "verself.openbao.unseal-key.v1"
	defaultKeyShares  = 3
	defaultThreshold  = 2
)

type config struct {
	bao               string
	stateDir          string
	siteRootTokenFile string
	keyShares         int
	threshold         int
	addr              string
	caCert            string

	action              string
	rootTokenOutputFile string
	unsealKeyFiles      stringList
}

type baoStatus struct {
	Initialized bool `json:"initialized"`
	Sealed      bool `json:"sealed"`
}

type generateRootStatus struct {
	Nonce            string `json:"nonce"`
	Started          bool   `json:"started"`
	Progress         int    `json:"progress"`
	Required         int    `json:"required"`
	Complete         bool   `json:"complete"`
	EncodedToken     string `json:"encoded_token"`
	EncodedRootToken string `json:"encoded_root_token"`
}

type initResponse struct {
	RootToken     string   `json:"root_token"`
	UnsealKeysB64 []string `json:"unseal_keys_b64"`
	KeysBase64    []string `json:"keys_base64"`
}

type wrappedKey struct {
	Version    string `json:"version"`
	KDF        string `json:"kdf"`
	Salt       string `json:"salt"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

func main() {
	fs := flag.NewFlagSet("openbao-bootstrap", flag.ExitOnError)
	cfg := config{}
	fs.StringVar(&cfg.action, "action", "bootstrap", "action: bootstrap or generate-root-token")
	fs.StringVar(&cfg.bao, "bao", "bao", "bao binary path")
	fs.StringVar(&cfg.stateDir, "state-dir", "/var/lib/verself/recovery/openbao", "OpenBao recovery state directory")
	fs.StringVar(&cfg.siteRootTokenFile, "site-root-token-file", "/run/verself/recovery/openbao-site-root.token", "site root token file")
	fs.IntVar(&cfg.keyShares, "key-shares", defaultKeyShares, "OpenBao operator init key shares")
	fs.IntVar(&cfg.threshold, "key-threshold", defaultThreshold, "OpenBao operator init key threshold")
	fs.StringVar(&cfg.addr, "addr", firstNonEmpty(os.Getenv("BAO_ADDR"), "https://127.0.0.1:8200"), "OpenBao API address")
	fs.StringVar(&cfg.caCert, "ca-cert", os.Getenv("BAO_CACERT"), "OpenBao CA certificate")
	fs.StringVar(&cfg.rootTokenOutputFile, "root-token-output-file", "", "0600 file to receive a generated temporary root token")
	fs.Var(&cfg.unsealKeyFiles, "unseal-key-file", "unseal key file for action=generate-root-token; repeat to satisfy the threshold")
	if err := fs.Parse(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "openbao-bootstrap: %v\n", err)
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := run(ctx, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "openbao-bootstrap: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg config) error {
	cfg = normalizeConfig(cfg)
	switch cfg.action {
	case "bootstrap":
	case "generate-root-token":
		err := generateRootToken(ctx, cfg)
		removeErr := removeSiteRootTokenFile(cfg.siteRootTokenFile)
		if err != nil {
			return err
		}
		return removeErr
	default:
		return fmt.Errorf("unsupported action %q", cfg.action)
	}
	if err := os.MkdirAll(cfg.stateDir, 0o700); err != nil {
		return fmt.Errorf("create OpenBao bootstrap state dir: %w", err)
	}
	if err := os.Chmod(cfg.stateDir, 0o700); err != nil {
		return fmt.Errorf("chmod OpenBao bootstrap state dir: %w", err)
	}
	status, err := waitStatus(ctx, cfg)
	if err != nil {
		return err
	}
	var rootKey []byte
	if !status.Initialized || status.Sealed {
		var err error
		rootKey, err = readSiteRootToken(cfg.siteRootTokenFile)
		if err != nil {
			return err
		}
	}
	rootToken := ""
	if !status.Initialized {
		init, err := operatorInit(ctx, cfg)
		if err != nil {
			return err
		}
		keys := init.UnsealKeysB64
		if len(keys) == 0 {
			keys = init.KeysBase64
		}
		if len(keys) < cfg.threshold || strings.TrimSpace(init.RootToken) == "" {
			return errors.New("openbao init response did not include root token and enough unseal keys")
		}
		for index, key := range keys {
			if err := writeWrappedKey(cfg.stateDir, rootKey, index+1, key); err != nil {
				return err
			}
		}
		rootToken = strings.TrimSpace(init.RootToken)
		status, err = statusOnce(ctx, cfg)
		if err != nil {
			return err
		}
	}
	if status.Sealed {
		for index := 1; index <= cfg.threshold; index++ {
			key, err := readWrappedKey(cfg.stateDir, rootKey, index)
			if err != nil {
				return err
			}
			if err := baoCommand(ctx, cfg, "operator", "unseal", key); err != nil {
				return err
			}
		}
	}
	if rootToken != "" {
		if err := configureWorkloadIdentity(ctx, cfg, rootToken); err != nil {
			return err
		}
		if err := revokeRootToken(ctx, cfg, rootToken); err != nil {
			return err
		}
	}
	return removeSiteRootTokenFile(cfg.siteRootTokenFile)
}

func normalizeConfig(cfg config) config {
	cfg.action = strings.TrimSpace(cfg.action)
	cfg.bao = strings.TrimSpace(cfg.bao)
	cfg.stateDir = strings.TrimSpace(cfg.stateDir)
	cfg.siteRootTokenFile = strings.TrimSpace(cfg.siteRootTokenFile)
	cfg.addr = strings.TrimSpace(cfg.addr)
	cfg.caCert = strings.TrimSpace(cfg.caCert)
	cfg.rootTokenOutputFile = strings.TrimSpace(cfg.rootTokenOutputFile)
	if cfg.keyShares == 0 {
		cfg.keyShares = defaultKeyShares
	}
	if cfg.threshold == 0 {
		cfg.threshold = defaultThreshold
	}
	return cfg
}

type stringList []string

func (l *stringList) String() string {
	return strings.Join(*l, ",")
}

func (l *stringList) Set(value string) error {
	value = strings.TrimSpace(value)
	if value != "" {
		*l = append(*l, value)
	}
	return nil
}

func readSiteRootToken(path string) ([]byte, error) {
	if path == "" {
		return nil, errors.New("site root token file is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("site root token file %s is required before OpenBao bootstrap", path)
		}
		return nil, fmt.Errorf("inspect site root token file %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("site root token file %s must be a regular file", path)
	}
	if info.Size() == 0 {
		return nil, fmt.Errorf("site root token file %s is empty", path)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("site root token file %s must be readable only by root", path)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read site root token file %s: %w", path, err)
	}
	key := bytes.TrimSpace(body)
	if len(key) == 0 {
		return nil, fmt.Errorf("site root token file %s is empty", path)
	}
	return key, nil
}

func removeSiteRootTokenFile(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove consumed site root token file %s: %w", path, err)
	}
	return nil
}

func waitStatus(ctx context.Context, cfg config) (baoStatus, error) {
	var last error
	for {
		status, err := statusOnce(ctx, cfg)
		if err == nil {
			return status, nil
		}
		last = err
		select {
		case <-ctx.Done():
			return baoStatus{}, fmt.Errorf("openbao status did not become readable: %w: %v", ctx.Err(), last)
		case <-time.After(time.Second):
		}
	}
}

func statusOnce(ctx context.Context, cfg config) (baoStatus, error) {
	cmd := exec.CommandContext(ctx, cfg.bao, "status", "-format=json")
	cmd.Env = baoEnv(cfg, "")
	out, err := cmd.Output()
	if status, decodeErr := decodeStatusOutput(out); decodeErr == nil {
		return status, nil
	} else if err == nil {
		return baoStatus{}, decodeErr
	}
	return baoStatus{}, commandError("bao status", err)
}

func decodeStatusOutput(out []byte) (baoStatus, error) {
	var status baoStatus
	if err := json.Unmarshal(bytes.TrimSpace(out), &status); err != nil {
		return baoStatus{}, fmt.Errorf("decode bao status: %w", err)
	}
	return status, nil
}

func operatorInit(ctx context.Context, cfg config) (initResponse, error) {
	cmd := exec.CommandContext(ctx, cfg.bao, "operator", "init",
		fmt.Sprintf("-key-shares=%d", cfg.keyShares),
		fmt.Sprintf("-key-threshold=%d", cfg.threshold),
		"-format=json",
	)
	cmd.Env = baoEnv(cfg, "")
	out, err := cmd.Output()
	if err != nil {
		return initResponse{}, commandError("bao operator init", err)
	}
	var init initResponse
	if err := json.Unmarshal(out, &init); err != nil {
		return initResponse{}, fmt.Errorf("decode bao operator init: %w", err)
	}
	return init, nil
}

func baoCommand(ctx context.Context, cfg config, args ...string) error {
	cmd := exec.CommandContext(ctx, cfg.bao, args...)
	cmd.Env = baoEnv(cfg, "")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %w: %s", baoCommandLabel(args), err, sanitizeCommandOutput(out))
	}
	return nil
}

func baoOutput(ctx context.Context, cfg config, stdin string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, cfg.bao, args...)
	cmd.Env = baoEnv(cfg, "")
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	out, err := cmd.Output()
	if err != nil {
		return nil, commandError(baoCommandLabel(args), err)
	}
	return out, nil
}

func revokeRootToken(ctx context.Context, cfg config, rootToken string) error {
	cmd := exec.CommandContext(ctx, cfg.bao, "token", "revoke", "-self")
	cmd.Env = baoEnv(cfg, rootToken)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("revoke OpenBao bootstrap root token: %w: %s", err, sanitizeCommandOutput(out))
	}
	return nil
}

func baoEnv(cfg config, token string) []string {
	env := append([]string{}, os.Environ()...)
	env = append(env, "BAO_ADDR="+cfg.addr)
	if cfg.caCert != "" {
		env = append(env, "BAO_CACERT="+cfg.caCert)
	}
	if token != "" {
		env = append(env, "BAO_TOKEN="+token)
	}
	return env
}

func commandError(op string, err error) error {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return fmt.Errorf("%s: %w: %s", op, err, sanitizeCommandOutput(exit.Stderr))
	}
	return fmt.Errorf("%s: %w", op, err)
}

func baoCommandLabel(args []string) string {
	redacted := make([]string, 0, len(args))
	for index, arg := range args {
		if index == 2 && len(args) >= 3 && args[0] == "operator" && args[1] == "unseal" {
			redacted = append(redacted, "[redacted]")
			continue
		}
		if strings.HasPrefix(arg, "-otp=") {
			redacted = append(redacted, "-otp=[redacted]")
			continue
		}
		if strings.HasPrefix(arg, "-decode=") && arg != "-decode=-" {
			redacted = append(redacted, "-decode=[redacted]")
			continue
		}
		redacted = append(redacted, arg)
	}
	return "bao " + strings.Join(redacted, " ")
}

func sanitizeCommandOutput(out []byte) string {
	text := strings.TrimSpace(string(out))
	if len(text) > 512 {
		return text[:512]
	}
	return text
}

func writeWrappedKey(stateDir string, rootKey []byte, index int, plaintext string) error {
	envelope, err := encryptUnsealKey(rootKey, plaintext)
	if err != nil {
		return err
	}
	body, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return fmt.Errorf("encode wrapped unseal key: %w", err)
	}
	body = append(body, '\n')
	path := wrappedKeyPath(stateDir, index)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return fmt.Errorf("write wrapped unseal key: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace wrapped unseal key: %w", err)
	}
	return os.Chmod(path, 0o600)
}

func readWrappedKey(stateDir string, rootKey []byte, index int) (string, error) {
	path := wrappedKeyPath(stateDir, index)
	body, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("%s is required to unseal OpenBao", path)
		}
		return "", fmt.Errorf("read wrapped unseal key %s: %w", path, err)
	}
	var envelope wrappedKey
	if err := json.Unmarshal(body, &envelope); err != nil {
		return "", fmt.Errorf("decode wrapped unseal key %s: %w", path, err)
	}
	return decryptUnsealKey(rootKey, envelope)
}

func encryptUnsealKey(rootKey []byte, plaintext string) (wrappedKey, error) {
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		return wrappedKey{}, fmt.Errorf("generate scrypt salt: %w", err)
	}
	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	if _, err := rand.Read(nonce); err != nil {
		return wrappedKey{}, fmt.Errorf("generate unseal key nonce: %w", err)
	}
	key, err := deriveWrapKey(rootKey, salt)
	if err != nil {
		return wrappedKey{}, err
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return wrappedKey{}, fmt.Errorf("create unseal key AEAD: %w", err)
	}
	ciphertext := aead.Seal(nil, nonce, []byte(strings.TrimSpace(plaintext)), []byte(wrappedKeyVersion))
	return wrappedKey{
		Version:    wrappedKeyVersion,
		KDF:        "scrypt-n32768-r8-p1-sha256-root",
		Salt:       base64.StdEncoding.EncodeToString(salt),
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
	}, nil
}

func decryptUnsealKey(rootKey []byte, envelope wrappedKey) (string, error) {
	if envelope.Version != wrappedKeyVersion {
		return "", fmt.Errorf("wrapped unseal key version %q is not supported", envelope.Version)
	}
	salt, err := base64.StdEncoding.DecodeString(envelope.Salt)
	if err != nil {
		return "", fmt.Errorf("decode wrapped unseal key salt: %w", err)
	}
	nonce, err := base64.StdEncoding.DecodeString(envelope.Nonce)
	if err != nil {
		return "", fmt.Errorf("decode wrapped unseal key nonce: %w", err)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(envelope.Ciphertext)
	if err != nil {
		return "", fmt.Errorf("decode wrapped unseal key ciphertext: %w", err)
	}
	key, err := deriveWrapKey(rootKey, salt)
	if err != nil {
		return "", err
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return "", fmt.Errorf("create unseal key AEAD: %w", err)
	}
	plaintext, err := aead.Open(nil, nonce, ciphertext, []byte(wrappedKeyVersion))
	if err != nil {
		return "", errors.New("decrypt wrapped unseal key with site root token")
	}
	return string(plaintext), nil
}

func deriveWrapKey(rootKey, salt []byte) ([]byte, error) {
	rootDigest := sha256.Sum256(bytes.TrimSpace(rootKey))
	key, err := scrypt.Key(rootDigest[:], salt, 32768, 8, 1, chacha20poly1305.KeySize)
	if err != nil {
		return nil, fmt.Errorf("derive OpenBao unseal wrapping key: %w", err)
	}
	return key, nil
}

func wrappedKeyPath(stateDir string, index int) string {
	return filepath.Join(stateDir, fmt.Sprintf("unseal-key-%d.wrapped.json", index))
}

func generateRootToken(ctx context.Context, cfg config) error {
	if cfg.rootTokenOutputFile == "" {
		return errors.New("--root-token-output-file is required for action=generate-root-token")
	}
	keys, err := recoveryUnsealKeys(cfg)
	if err != nil {
		return err
	}
	otpOut, err := baoOutput(ctx, cfg, "", "operator", "generate-root", "-generate-otp")
	if err != nil {
		return err
	}
	otp := strings.TrimSpace(string(otpOut))
	if otp == "" {
		return errors.New("bao operator generate-root -generate-otp returned an empty OTP")
	}
	started := false
	defer func() {
		if started {
			cancelCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = baoCommand(cancelCtx, cfg, "operator", "generate-root", "-cancel")
		}
	}()
	initOut, err := baoOutput(ctx, cfg, "", "operator", "generate-root", "-init", "-otp="+otp, "-format=json")
	if err != nil {
		return err
	}
	started = true
	status, err := decodeGenerateRootStatus(initOut)
	if err != nil {
		return err
	}
	if strings.TrimSpace(status.Nonce) == "" {
		return errors.New("bao operator generate-root init did not return a nonce")
	}
	required := status.Required
	if required <= 0 {
		required = cfg.threshold
	}
	if len(keys) < required {
		return fmt.Errorf("generate-root requires %d unseal keys, got %d", required, len(keys))
	}
	encoded := firstNonEmpty(status.EncodedRootToken, status.EncodedToken)
	for i := 0; encoded == "" && i < required; i++ {
		out, err := baoOutput(ctx, cfg, keys[i], "operator", "generate-root", "-nonce="+status.Nonce, "-otp="+otp, "-format=json", "-")
		if err != nil {
			return err
		}
		status, err = decodeGenerateRootStatus(out)
		if err != nil {
			return err
		}
		encoded = firstNonEmpty(status.EncodedRootToken, status.EncodedToken)
	}
	if encoded == "" {
		return errors.New("bao operator generate-root did not return an encoded token after threshold keys")
	}
	tokenOut, err := baoOutput(ctx, cfg, encoded, "operator", "generate-root", "-decode=-", "-otp="+otp)
	if err != nil {
		return err
	}
	token := strings.TrimSpace(string(tokenOut))
	if token == "" {
		return errors.New("bao operator generate-root decode returned an empty token")
	}
	if err := writeSecretFile(cfg.rootTokenOutputFile, token); err != nil {
		return err
	}
	started = false
	return nil
}

func recoveryUnsealKeys(cfg config) ([]string, error) {
	if len(cfg.unsealKeyFiles) > 0 {
		keys := make([]string, 0, len(cfg.unsealKeyFiles))
		for _, path := range cfg.unsealKeyFiles {
			body, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("read unseal key file %s: %w", path, err)
			}
			key := strings.TrimSpace(string(body))
			if key == "" {
				return nil, fmt.Errorf("unseal key file %s is empty", path)
			}
			keys = append(keys, key)
		}
		return keys, nil
	}
	rootKey, err := readSiteRootToken(cfg.siteRootTokenFile)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, cfg.threshold)
	for index := 1; index <= cfg.threshold; index++ {
		key, err := readWrappedKey(cfg.stateDir, rootKey, index)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, nil
}

func decodeGenerateRootStatus(out []byte) (generateRootStatus, error) {
	var status generateRootStatus
	if err := json.Unmarshal(bytes.TrimSpace(out), &status); err != nil {
		return generateRootStatus{}, fmt.Errorf("decode bao operator generate-root response: %w", err)
	}
	return status, nil
}

func writeSecretFile(path, value string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create secret output directory: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strings.TrimSpace(value)+"\n"), 0o600); err != nil {
		return fmt.Errorf("write temporary secret output file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace secret output file: %w", err)
	}
	return os.Chmod(path, 0o600)
}

func configureWorkloadIdentity(ctx context.Context, cfg config, rootToken string) error {
	client, err := apiClient(cfg)
	if err != nil {
		return err
	}
	api := func(method, path string, body any, expected ...int) (map[string]any, error) {
		return apiRequest(ctx, client, cfg.addr, rootToken, method, path, body, expected...)
	}
	mounts, err := api(http.MethodGet, "sys/mounts", nil, http.StatusOK)
	if err != nil {
		return err
	}
	if _, ok := dataMap(mounts)["kv-runtime/"]; !ok {
		if _, err := api(http.MethodPost, "sys/mounts/kv-runtime", map[string]any{
			"type":    "kv",
			"options": map[string]any{"version": "2"},
		}, http.StatusNoContent); err != nil {
			return err
		}
	}
	if _, ok := dataMap(mounts)["kv-controller/"]; !ok {
		if _, err := api(http.MethodPost, "sys/mounts/kv-controller", map[string]any{
			"type":    "kv",
			"options": map[string]any{"version": "2"},
		}, http.StatusNoContent); err != nil {
			return err
		}
	}
	if _, ok := dataMap(mounts)["transit/"]; !ok {
		if _, err := api(http.MethodPost, "sys/mounts/transit", map[string]any{
			"type": "transit",
		}, http.StatusNoContent); err != nil {
			return err
		}
	}
	auth, err := api(http.MethodGet, "sys/auth", nil, http.StatusOK)
	if err != nil {
		return err
	}
	if _, ok := dataMap(auth)["jwt-nomad/"]; !ok {
		if _, err := api(http.MethodPost, "sys/auth/jwt-nomad", map[string]any{
			"type":        "jwt",
			"description": "Verself Nomad workload identity auth",
		}, http.StatusNoContent); err != nil {
			return err
		}
	}
	if _, err := api(http.MethodPost, "auth/jwt-nomad/config", map[string]any{
		"jwks_url":           "http://127.0.0.1:4646/.well-known/jwks.json",
		"jwt_supported_algs": []string{"RS256", "EdDSA"},
	}, http.StatusNoContent); err != nil {
		return err
	}
	return nil
}

func apiClient(cfg config) (*http.Client, error) {
	pool, err := x509.SystemCertPool()
	if err != nil {
		return nil, fmt.Errorf("load system cert pool: %w", err)
	}
	if cfg.caCert != "" {
		body, err := os.ReadFile(cfg.caCert)
		if err != nil {
			return nil, fmt.Errorf("read OpenBao CA cert: %w", err)
		}
		if !pool.AppendCertsFromPEM(body) {
			return nil, fmt.Errorf("OpenBao CA cert %s did not contain a PEM certificate", cfg.caCert)
		}
	}
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS13},
		},
	}, nil
}

func apiRequest(ctx context.Context, client *http.Client, addr, token, method, path string, body any, expected ...int) (map[string]any, error) {
	var requestBody io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encode OpenBao API body: %w", err)
		}
		requestBody = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(addr, "/")+"/v1/"+path, requestBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Vault-Token", token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openbao %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return nil, fmt.Errorf("read openbao %s %s response: %w", method, path, err)
	}
	for _, status := range expected {
		if resp.StatusCode == status {
			if len(bytes.TrimSpace(raw)) == 0 {
				return map[string]any{}, nil
			}
			var decoded map[string]any
			if err := json.Unmarshal(raw, &decoded); err != nil {
				return nil, fmt.Errorf("decode openbao %s %s response: %w", method, path, err)
			}
			return decoded, nil
		}
	}
	return nil, fmt.Errorf("openbao %s %s status %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(raw)))
}

func dataMap(response map[string]any) map[string]any {
	data, ok := response["data"].(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return data
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
