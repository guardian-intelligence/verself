package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
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
	cfg       config
	siteCfg   siteArtifactConfig
	publisher r2control.ParentCredentials
	apiClient *r2control.CloudflareAPIClient
	authToken string
	mu        sync.Mutex
	sessions  map[string]uploadSession
}

func serveUploadAPI(ctx context.Context, cfg config, publisher r2control.ParentCredentials) error {
	siteCfg, err := siteArtifactConfigFromConfig(cfg)
	if err != nil {
		return err
	}
	if strings.TrimSpace(publisher.APIToken) == "" {
		return fmt.Errorf("publisher Cloudflare API token is required for upload sessions")
	}
	if strings.TrimSpace(publisher.AccessKeyID) == "" {
		return fmt.Errorf("publisher Cloudflare token id is required for upload sessions")
	}
	apiClient, err := r2control.NewCloudflareAPIClient(publisher.APIToken, cfg.timeout)
	if err != nil {
		return err
	}
	token, err := loadServerAuthToken(cfg)
	if err != nil {
		return err
	}
	srv := &uploadServer{
		cfg:       cfg,
		siteCfg:   siteCfg,
		publisher: publisher,
		apiClient: apiClient,
		authToken: token,
		sessions:  map[string]uploadSession{},
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
	prefix, token, ok := strings.Cut(strings.TrimSpace(r.Header.Get("Authorization")), " ")
	if !ok || !strings.EqualFold(prefix, "Bearer") {
		return false
	}
	got := strings.TrimSpace(token)
	return got != "" && len(got) == len(s.authToken) && subtle.ConstantTimeCompare([]byte(got), []byte(s.authToken)) == 1
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
	ctx := r.Context()
	bindings := make([]uploadBinding, 0, len(req.Artifacts))
	objectKeys := make([]string, 0, len(req.Artifacts))
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
		objectKeys = append(objectKeys, key)
		bindings = append(bindings, uploadBinding{
			output: output,
			sha256: artifact.SHA256,
			key:    key,
		})
	}
	tempClient, err := s.temporaryR2Client(ctx, r2control.TemporaryPermissionObjectReadWrite, objectKeys, s.cfg.uploadSessionTTL)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}
	for i := range bindings {
		status, err := tempClient.HeadObject(ctx, s.siteCfg.Bucket, bindings[i].key)
		if err != nil {
			writeJSONError(w, http.StatusBadGateway, err.Error())
			return
		}
		switch status {
		case http.StatusOK:
			bindings[i].present = true
		case http.StatusNotFound:
			bindings[i].present = false
		default:
			writeJSONError(w, http.StatusBadGateway, fmt.Sprintf("artifact %s HEAD returned status %d", bindings[i].output, status))
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
	objectKeys := make([]string, 0, len(session.Objects))
	for _, object := range session.Objects {
		objectKeys = append(objectKeys, object.Key)
	}
	tempClient, err := s.temporaryR2Client(r.Context(), r2control.TemporaryPermissionObjectReadOnly, objectKeys, completionCredentialTTL(session.ExpiresAt))
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}
	for _, object := range session.Objects {
		status, err := tempClient.HeadObject(r.Context(), object.Bucket, object.Key)
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

func (s *uploadServer) temporaryR2Client(ctx context.Context, permission string, objectKeys []string, ttl time.Duration) (*r2control.R2Client, error) {
	temp, err := s.apiClient.CreateTemporaryCredentials(ctx, s.siteCfg.AccountID, r2control.TemporaryCredentialRequest{
		ParentAccessKeyID: s.publisher.AccessKeyID,
		Bucket:            s.siteCfg.Bucket,
		Permission:        permission,
		Objects:           objectKeys,
		TTL:               ttl,
	})
	if err != nil {
		return nil, err
	}
	return r2control.NewR2Client(r2control.R2ClientConfig{
		Endpoint:        r2control.Endpoint(s.siteCfg.AccountID),
		Region:          s.siteCfg.Region,
		AccessKeyID:     temp.AccessKeyID,
		SecretAccessKey: temp.SecretAccessKey,
		SessionToken:    temp.SessionToken,
		Source:          "cloudflare-r2-control-plane-upload-session",
		Timeout:         s.cfg.timeout,
	})
}

func completionCredentialTTL(expiresAt time.Time) time.Duration {
	ttl := time.Until(expiresAt)
	if ttl < time.Minute {
		return time.Minute
	}
	return ttl
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
	return path.Join(cfg.SitePrefix, strings.Trim(cfg.KeyPrefix, "/"), digest, output+".tar")
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
	if token := strings.TrimSpace(os.Getenv(cfg.authTokenEnv)); token != "" {
		return token, nil
	}
	return "", fmt.Errorf("auth token is required via %s", cfg.authTokenEnv)
}
