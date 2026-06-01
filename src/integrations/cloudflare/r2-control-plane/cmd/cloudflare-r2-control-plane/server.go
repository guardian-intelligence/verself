package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	r2client "github.com/verself/integrations/cloudflare/r2-control-plane/client"
	"github.com/verself/integrations/cloudflare/r2-control-plane/internal/r2control"
)

type uploadSession struct {
	ID           string
	Site         string
	DeployRunKey string
	SHA          string
	ExpiresAt    time.Time
	Objects      []r2client.UploadObject
}

type uploadBinding struct {
	output  string
	sha256  string
	key     string
	present bool
}

type uploadServer struct {
	cfg          config
	siteCfg      siteArtifactConfig
	parent       r2control.ParentCredentials
	parentClient *r2control.R2Client
	authToken    string
	mu           sync.Mutex
	sessions     map[string]uploadSession
}

func serveUploadAPI(ctx context.Context, cfg config, parent r2control.ParentCredentials, parentClient *r2control.R2Client) error {
	siteCfg, err := loadSiteConfig(cfg.repoRoot, cfg.site)
	if err != nil {
		return err
	}
	token, err := loadServerAuthToken(cfg)
	if err != nil {
		return err
	}
	if token == "" && !listenIsLoopback(cfg.listenAddr) {
		return fmt.Errorf("auth token file is required when --listen is not loopback")
	}
	srv := &uploadServer{
		cfg:          cfg,
		siteCfg:      siteCfg,
		parent:       parent,
		parentClient: parentClient,
		authToken:    token,
		sessions:     map[string]uploadSession{},
	}
	httpServer := &http.Server{
		Addr:              cfg.listenAddr,
		Handler:           srv,
		ReadHeaderTimeout: 5 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- httpServer.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
		return ctx.Err()
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

func (s *uploadServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/healthz" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !s.authorized(r) {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) == 4 && parts[0] == "v1" && parts[1] == "sites" && parts[3] == "artifact-upload-sessions" && r.Method == http.MethodPost {
		s.handleCreateUploadSession(w, r, parts[2])
		return
	}
	if len(parts) == 6 && parts[0] == "v1" && parts[1] == "sites" && parts[3] == "artifact-upload-sessions" && parts[5] == "complete" && r.Method == http.MethodPost {
		s.handleCompleteUploadSession(w, r, parts[2], parts[4])
		return
	}
	writeJSONError(w, http.StatusNotFound, "not found")
}

func (s *uploadServer) authorized(r *http.Request) bool {
	if s.authToken == "" {
		return true
	}
	got := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	return got != "" && got == s.authToken
}

func (s *uploadServer) handleCreateUploadSession(w http.ResponseWriter, r *http.Request, site string) {
	if site != s.cfg.site {
		writeJSONError(w, http.StatusNotFound, "site not served by this control plane")
		return
	}
	var req r2client.CreateUploadSessionRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Site != site {
		writeJSONError(w, http.StatusBadRequest, "request site must match URL site")
		return
	}
	if strings.TrimSpace(req.DeployRunKey) == "" || strings.TrimSpace(req.SHA) == "" {
		writeJSONError(w, http.StatusBadRequest, "deploy_run_key and sha are required")
		return
	}
	if len(req.Artifacts) == 0 {
		writeJSONError(w, http.StatusBadRequest, "at least one artifact is required")
		return
	}
	if strings.TrimSpace(s.parent.APIToken) == "" {
		writeJSONError(w, http.StatusFailedDependency, "parent Cloudflare API token value is required")
		return
	}
	if strings.TrimSpace(s.parent.SessionToken) != "" {
		writeJSONError(w, http.StatusFailedDependency, "parent Cloudflare credential must not be temporary")
		return
	}
	ctx := r.Context()
	if _, _, err := s.parentClient.EnsureBucket(ctx, s.siteCfg.Bucket); err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}
	bindings := make([]uploadBinding, 0, len(req.Artifacts))
	missingObjectKeys := make([]string, 0, len(req.Artifacts))
	seen := map[string]bool{}
	for _, artifact := range req.Artifacts {
		output, err := cleanArtifactOutput(artifact.Output)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		if seen[output] {
			writeJSONError(w, http.StatusBadRequest, "duplicate artifact output "+output)
			return
		}
		seen[output] = true
		if !isSHA256Hex(artifact.SHA256) {
			writeJSONError(w, http.StatusBadRequest, "artifact "+output+" has invalid sha256")
			return
		}
		if artifact.SizeBytes <= 0 {
			writeJSONError(w, http.StatusBadRequest, "artifact "+output+" has invalid size_bytes")
			return
		}
		key := artifactKey(s.siteCfg, artifact.SHA256, output)
		status, err := s.parentClient.HeadObject(ctx, s.siteCfg.Bucket, key)
		if err != nil {
			writeJSONError(w, http.StatusBadGateway, err.Error())
			return
		}
		present := false
		switch status {
		case http.StatusOK:
			present = true
		case http.StatusNotFound:
			missingObjectKeys = append(missingObjectKeys, key)
		default:
			writeJSONError(w, http.StatusBadGateway, fmt.Sprintf("artifact %s HEAD returned status %d", output, status))
			return
		}
		bindings = append(bindings, uploadBinding{
			output:  output,
			sha256:  artifact.SHA256,
			key:     key,
			present: present,
		})
	}
	var tempClient *r2control.R2Client
	if len(missingObjectKeys) > 0 {
		apiClient, err := r2control.NewCloudflareAPIClient(s.parent.APIToken, s.cfg.timeout)
		if err != nil {
			writeJSONError(w, http.StatusFailedDependency, err.Error())
			return
		}
		temp, err := apiClient.CreateTemporaryCredentials(ctx, s.siteCfg.AccountID, r2control.TemporaryCredentialRequest{
			ParentAccessKeyID: s.parent.AccessKeyID,
			Bucket:            s.siteCfg.Bucket,
			Permission:        r2control.TemporaryPermissionObjectReadWrite,
			Objects:           missingObjectKeys,
			TTL:               s.cfg.uploadSessionTTL,
		})
		if err != nil {
			writeJSONError(w, http.StatusBadGateway, err.Error())
			return
		}
		tempClient, err = r2control.NewR2Client(r2control.R2ClientConfig{
			Endpoint:        r2control.Endpoint(s.siteCfg.AccountID),
			Region:          s.siteCfg.Region,
			AccessKeyID:     temp.AccessKeyID,
			SecretAccessKey: temp.SecretAccessKey,
			SessionToken:    temp.SessionToken,
			Source:          "cloudflare-r2-control-plane-upload-session",
			Timeout:         s.cfg.timeout,
		})
		if err != nil {
			writeJSONError(w, http.StatusFailedDependency, err.Error())
			return
		}
	}
	expiresAt := time.Now().UTC().Add(s.cfg.uploadSessionTTL)
	objects := make([]r2client.UploadObject, 0, len(bindings))
	for _, binding := range bindings {
		action := r2client.UploadActionPresent
		putURL := ""
		headers := http.Header{}
		if !binding.present {
			action = r2client.UploadActionPut
			var err error
			putURL, headers, err = tempClient.PresignPutObject(ctx, s.siteCfg.Bucket, binding.key, binding.sha256, s.cfg.uploadSessionTTL)
			if err != nil {
				writeJSONError(w, http.StatusBadGateway, err.Error())
				return
			}
		}
		objects = append(objects, r2client.UploadObject{
			Output:       binding.output,
			Bucket:       s.siteCfg.Bucket,
			Key:          binding.key,
			GetterSource: artifactGetterSource(s.siteCfg, binding.key),
			Action:       action,
			PutURL:       putURL,
			Headers:      flattenHeaders(headers),
			ExpiresAt:    expiresAt,
		})
	}
	sessionID, err := randomID()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	session := uploadSession{
		ID:           sessionID,
		Site:         site,
		DeployRunKey: req.DeployRunKey,
		SHA:          req.SHA,
		ExpiresAt:    expiresAt,
		Objects:      objects,
	}
	s.mu.Lock()
	s.sessions[sessionID] = session
	s.mu.Unlock()
	writeJSON(w, http.StatusCreated, r2client.CreateUploadSessionResponse{
		SessionID: sessionID,
		ExpiresAt: expiresAt,
		Objects:   objects,
	})
}

func (s *uploadServer) handleCompleteUploadSession(w http.ResponseWriter, r *http.Request, site, sessionID string) {
	if site != s.cfg.site {
		writeJSONError(w, http.StatusNotFound, "site not served by this control plane")
		return
	}
	s.mu.Lock()
	session, ok := s.sessions[sessionID]
	if ok {
		delete(s.sessions, sessionID)
	}
	s.mu.Unlock()
	if !ok {
		writeJSONError(w, http.StatusNotFound, "upload session not found")
		return
	}
	if time.Now().UTC().After(session.ExpiresAt) {
		writeJSONError(w, http.StatusGone, "upload session expired")
		return
	}
	for _, object := range session.Objects {
		status, err := s.parentClient.HeadObject(r.Context(), object.Bucket, object.Key)
		if err != nil {
			writeJSONError(w, http.StatusBadGateway, err.Error())
			return
		}
		if status != http.StatusOK {
			writeJSONError(w, http.StatusConflict, fmt.Sprintf("artifact %s HEAD returned status %d", object.Output, status))
			return
		}
	}
	writeJSON(w, http.StatusOK, r2client.CompleteUploadSessionResponse{
		SessionID:   session.ID,
		Site:        session.Site,
		CompletedAt: time.Now().UTC(),
		Objects:     session.Objects,
	})
}

func decodeJSON(r *http.Request, out any) error {
	defer func() { _ = r.Body.Close() }()
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("decode JSON request: %w", err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func flattenHeaders(headers http.Header) map[string]string {
	out := map[string]string{}
	for key, values := range headers {
		if len(values) > 0 {
			out[key] = values[0]
		}
	}
	return out
}

func randomID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate upload session ID: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

func artifactKey(cfg siteArtifactConfig, digest, output string) string {
	return strings.Trim(cfg.KeyPrefix, "/") + "/" + digest + "/" + output + ".tar"
}

func artifactGetterSource(cfg siteArtifactConfig, key string) string {
	return strings.TrimRight(cfg.GetterSourcePrefix, "/") + "/" + key
}

func cleanArtifactOutput(output string) (string, error) {
	output = strings.TrimSpace(output)
	if output == "" {
		return "", fmt.Errorf("artifact output is required")
	}
	if output != path.Base(output) || strings.Contains(output, "..") {
		return "", fmt.Errorf("artifact output %q must be a single path segment", output)
	}
	return output, nil
}

func isSHA256Hex(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func loadServerAuthToken(cfg config) (string, error) {
	path := strings.TrimSpace(cfg.authTokenFile)
	if path == "" {
		return "", nil
	}
	body, err := osReadFile(path)
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(body))
	if token == "" {
		return "", fmt.Errorf("auth token file is empty")
	}
	return token, nil
}

func listenIsLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func osReadFile(path string) ([]byte, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read auth token file: %w", err)
	}
	return body, nil
}
