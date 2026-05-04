package sessionproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
)

type Config struct {
	StalwartBaseURL string
	PublicBaseURL   string
}

type Handler struct {
	stalwartBaseURL string
	publicBaseURL   string
	client          *http.Client
	logger          *slog.Logger
}

func New(cfg Config, client *http.Client, logger *slog.Logger) (*Handler, error) {
	if strings.TrimSpace(cfg.StalwartBaseURL) == "" {
		return nil, fmt.Errorf("stalwart base URL is empty")
	}
	if strings.TrimSpace(cfg.PublicBaseURL) == "" {
		return nil, fmt.Errorf("public base URL is empty")
	}
	if _, err := websocketBaseURL(strings.TrimSpace(cfg.PublicBaseURL)); err != nil {
		return nil, err
	}
	return &Handler{
		stalwartBaseURL: strings.TrimRight(strings.TrimSpace(cfg.StalwartBaseURL), "/"),
		publicBaseURL:   strings.TrimRight(strings.TrimSpace(cfg.PublicBaseURL), "/"),
		client:          client,
		logger:          logger,
	}, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || r.URL.Path != "/jmap/session" {
		http.NotFound(w, r)
		return
	}
	if err := h.proxySession(r.Context(), w, r); err != nil {
		h.logger.ErrorContext(r.Context(), "mailbox-service: jmap session proxy failed", "error", err)
		http.Error(w, "bad gateway", http.StatusBadGateway)
	}
}

func (h *Handler) Ready(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.stalwartBaseURL+"/jmap/session", nil)
	if err != nil {
		return fmt.Errorf("build readiness request: %w", err)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return fmt.Errorf("stalwart jmap session request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("stalwart returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

func (h *Handler) proxySession(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.stalwartBaseURL+"/jmap/session", nil)
	if err != nil {
		return fmt.Errorf("build backend request: %w", err)
	}
	if auth := r.Header.Get("Authorization"); auth != "" {
		req.Header.Set("Authorization", auth)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return fmt.Errorf("backend request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read backend body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		copyHeaders(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(body)
		return nil
	}

	rewritten, err := rewriteSession(body, h.publicBaseURL)
	if err != nil {
		return fmt.Errorf("rewrite session: %w", err)
	}

	copyHeaders(w.Header(), resp.Header)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(rewritten)))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(rewritten)
	return nil
}

func rewriteSession(body []byte, publicBaseURL string) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()

	var payload map[string]any
	if err := decoder.Decode(&payload); err != nil {
		return nil, err
	}

	base := strings.TrimRight(publicBaseURL, "/")
	wsBase, err := websocketBaseURL(base)
	if err != nil {
		return nil, err
	}

	payload["apiUrl"] = base + "/jmap/"
	payload["uploadUrl"] = base + "/jmap/upload/{accountId}/"
	payload["downloadUrl"] = base + "/jmap/download/{accountId}/{blobId}/{name}?accept={type}"
	payload["eventSourceUrl"] = base + "/jmap/eventsource/?types={types}&closeafter={closeafter}&ping={ping}"

	if caps, ok := payload["capabilities"].(map[string]any); ok {
		if wsCap, ok := caps["urn:ietf:params:jmap:websocket"].(map[string]any); ok {
			wsCap["url"] = wsBase + "/jmap/ws"
		}
	}

	return json.Marshal(payload)
}

func websocketBaseURL(publicBaseURL string) (string, error) {
	u, err := url.Parse(publicBaseURL)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	default:
		return "", fmt.Errorf("unsupported public base URL scheme %q", u.Scheme)
	}
	return strings.TrimRight(u.String(), "/"), nil
}

func copyHeaders(dst, src http.Header) {
	for k, vv := range src {
		dst.Del(k)
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}
