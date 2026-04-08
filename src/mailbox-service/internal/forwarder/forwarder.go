package forwarder

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const maxForwardBodyChars = 32000

var tracer = otel.Tracer("mailbox-service.forwarder")

type Config struct {
	StalwartBaseURL string
	MailboxUser     string
	LocalDomain     string
	FromAddress     string
	FromName        string
	StatePath       string
	PollInterval    time.Duration
	QueryLimit      int
	SeenLimit       int
	BootstrapWindow int
}

type Credentials struct {
	MailboxPassword string
	ForwardTo       string
	ResendAPIKey    string
}

type Status struct {
	Enabled                 bool       `json:"enabled"`
	Running                 bool       `json:"running"`
	Mailbox                 string     `json:"mailbox"`
	ForwardTargetConfigured bool       `json:"forward_target_configured"`
	LastError               string     `json:"last_error,omitempty"`
	LastSyncAt              *time.Time `json:"last_sync_at,omitempty"`
	LastForwardedAt         *time.Time `json:"last_forwarded_at,omitempty"`
	LastForwardedEmailID    string     `json:"last_forwarded_email_id,omitempty"`
}

type Runner struct {
	cfg            Config
	creds          Credentials
	logger         *slog.Logger
	httpClient     *http.Client
	stalwartAuth   string
	ceoAddress     string
	resendEndpoint string

	mu     sync.RWMutex
	status Status
}

type state struct {
	SeenIDs []string `json:"seen_ids"`

	seenSet map[string]struct{} `json:"-"`
}

type jmapSession struct {
	Accounts map[string]json.RawMessage `json:"accounts"`
}

type emailAddress struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type emailBodyPart struct {
	PartID string `json:"partId"`
}

type emailBodyValue struct {
	Value string `json:"value"`
}

type jmapEmail struct {
	ID         string                    `json:"id"`
	Subject    string                    `json:"subject"`
	ReceivedAt string                    `json:"receivedAt"`
	Preview    string                    `json:"preview"`
	From       []emailAddress            `json:"from"`
	To         []emailAddress            `json:"to"`
	TextBody   []emailBodyPart           `json:"textBody"`
	BodyValues map[string]emailBodyValue `json:"bodyValues"`
}

type queryResult struct {
	IDs []string `json:"ids"`
}

type emailGetResult struct {
	List []jmapEmail `json:"list"`
}

type resendSendRequest struct {
	From    string            `json:"from"`
	To      []string          `json:"to"`
	Subject string            `json:"subject"`
	Text    string            `json:"text"`
	Headers map[string]string `json:"headers,omitempty"`
}

type resendSendResponse struct {
	ID string `json:"id"`
}

func New(cfg Config, creds Credentials, logger *slog.Logger, httpClient *http.Client) *Runner {
	ceoAddress := fmt.Sprintf("%s@%s", strings.TrimSpace(cfg.MailboxUser), strings.TrimSpace(cfg.LocalDomain))
	return &Runner{
		cfg:            cfg,
		creds:          creds,
		logger:         logger,
		httpClient:     httpClient,
		stalwartAuth:   basicAuth(strings.TrimSpace(cfg.MailboxUser), strings.TrimSpace(creds.MailboxPassword)),
		ceoAddress:     ceoAddress,
		resendEndpoint: "https://api.resend.com/emails",
		status: Status{
			Enabled:                 strings.TrimSpace(creds.MailboxPassword) != "" && strings.TrimSpace(creds.ForwardTo) != "" && strings.TrimSpace(creds.ResendAPIKey) != "",
			Mailbox:                 ceoAddress,
			ForwardTargetConfigured: strings.TrimSpace(creds.ForwardTo) != "",
		},
	}
}

func (r *Runner) Snapshot() Status {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.status
}

func (r *Runner) Run(ctx context.Context) {
	if !r.Snapshot().Enabled {
		r.logger.InfoContext(ctx, "mailbox-service: operator forwarder disabled")
		return
	}
	if strings.EqualFold(strings.TrimSpace(r.creds.ForwardTo), r.ceoAddress) {
		err := fmt.Errorf("forward target %q loops back to mailbox %q", r.creds.ForwardTo, r.ceoAddress)
		r.setError(err)
		r.logger.ErrorContext(ctx, "mailbox-service: operator forwarder misconfigured", "error", err)
		return
	}

	st, err := loadState(r.cfg.StatePath)
	if err != nil {
		r.setError(err)
		r.logger.ErrorContext(ctx, "mailbox-service: operator forwarder state load failed", "error", err)
		return
	}
	if len(st.SeenIDs) == 0 {
		if err := r.bootstrapState(ctx, st); err != nil {
			r.setError(err)
			r.logger.ErrorContext(ctx, "mailbox-service: operator forwarder bootstrap failed", "error", err)
			return
		}
		r.setSynced()
	}

	r.setRunning(true)
	defer r.setRunning(false)

	ticker := time.NewTicker(r.cfg.PollInterval)
	defer ticker.Stop()

	r.logger.InfoContext(ctx, "mailbox-service: operator forwarder started",
		"mailbox", r.ceoAddress,
		"poll_interval", r.cfg.PollInterval.String(),
	)

	for {
		if err := r.sync(ctx, st); err != nil {
			r.setError(err)
			r.logger.ErrorContext(ctx, "mailbox-service: operator forwarder sync failed", "error", err)
		} else {
			r.setSynced()
		}

		select {
		case <-ctx.Done():
			r.logger.InfoContext(context.Background(), "mailbox-service: operator forwarder stopped")
			return
		case <-ticker.C:
		}
	}
}

func (r *Runner) bootstrapState(ctx context.Context, st *state) error {
	ctx, span := tracer.Start(ctx, "forwarder.bootstrap")
	defer span.End()

	emails, err := r.latestEmails(ctx, r.cfg.BootstrapWindow)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("bootstrap latest emails: %w", err)
	}

	for _, email := range emails {
		st.markSeen(email.ID, r.cfg.SeenLimit)
	}
	if err := saveState(r.cfg.StatePath, st); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("bootstrap save state: %w", err)
	}

	r.logger.InfoContext(ctx, "mailbox-service: operator forwarder bootstrapped state", "seen_messages", len(st.SeenIDs))
	return nil
}

func (r *Runner) sync(ctx context.Context, st *state) error {
	ctx, span := tracer.Start(ctx, "forwarder.sync")
	defer span.End()

	emails, err := r.latestEmails(ctx, r.cfg.QueryLimit)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("query latest emails: %w", err)
	}

	changed := false
	for _, email := range emails {
		if st.hasSeen(email.ID) {
			continue
		}

		if r.shouldSkip(email) {
			st.markSeen(email.ID, r.cfg.SeenLimit)
			changed = true
			r.logger.InfoContext(ctx, "mailbox-service: operator forwarder skipped self-generated message",
				"email_id", email.ID,
				"subject", email.Subject,
			)
			continue
		}

		if err := r.forwardEmail(ctx, email); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			if changed {
				if saveErr := saveState(r.cfg.StatePath, st); saveErr != nil {
					r.logger.ErrorContext(ctx, "mailbox-service: operator forwarder partial state save failed", "error", saveErr)
				}
			}
			return err
		}

		st.markSeen(email.ID, r.cfg.SeenLimit)
		changed = true
	}

	if changed {
		if err := saveState(r.cfg.StatePath, st); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return fmt.Errorf("save state: %w", err)
		}
	}
	r.clearError()
	return nil
}

func (r *Runner) latestEmails(ctx context.Context, limit int) ([]jmapEmail, error) {
	accountID, err := r.accountID(ctx)
	if err != nil {
		return nil, err
	}

	queryBody := map[string]any{
		"using": []string{"urn:ietf:params:jmap:core", "urn:ietf:params:jmap:mail"},
		"methodCalls": []any{
			[]any{
				"Email/query",
				map[string]any{
					"accountId": accountID,
					"sort": []map[string]any{
						{"property": "receivedAt", "isAscending": false},
					},
					"limit": limit,
				},
				"q",
			},
		},
	}

	var idsResp struct {
		MethodResponses []json.RawMessage `json:"methodResponses"`
	}
	if err := r.jmap(ctx, queryBody, &idsResp); err != nil {
		return nil, fmt.Errorf("email query: %w", err)
	}

	idsPayload, err := decodeMethodPayload[queryResult](idsResp.MethodResponses, "Email/query")
	if err != nil {
		return nil, err
	}
	if len(idsPayload.IDs) == 0 {
		return nil, nil
	}

	ids := reverseStrings(idsPayload.IDs)
	getBody := map[string]any{
		"using": []string{"urn:ietf:params:jmap:core", "urn:ietf:params:jmap:mail"},
		"methodCalls": []any{
			[]any{
				"Email/get",
				map[string]any{
					"accountId": accountID,
					"ids":       ids,
					"properties": []string{
						"id",
						"subject",
						"receivedAt",
						"preview",
						"from",
						"to",
						"textBody",
						"bodyValues",
					},
					"fetchTextBodyValues": true,
				},
				"g",
			},
		},
	}

	var emailResp struct {
		MethodResponses []json.RawMessage `json:"methodResponses"`
	}
	if err := r.jmap(ctx, getBody, &emailResp); err != nil {
		return nil, fmt.Errorf("email get: %w", err)
	}
	payload, err := decodeMethodPayload[emailGetResult](emailResp.MethodResponses, "Email/get")
	if err != nil {
		return nil, err
	}
	return payload.List, nil
}

func (r *Runner) accountID(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(r.cfg.StalwartBaseURL, "/")+"/jmap/session", nil)
	if err != nil {
		return "", fmt.Errorf("build session request: %w", err)
	}
	req.Header.Set("Authorization", r.stalwartAuth)

	var session jmapSession
	if err := r.doJSON(req, &session); err != nil {
		return "", fmt.Errorf("session request: %w", err)
	}
	for accountID := range session.Accounts {
		return accountID, nil
	}
	return "", errors.New("no JMAP accounts returned")
}

func (r *Runner) jmap(ctx context.Context, body any, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal jmap body: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(r.cfg.StalwartBaseURL, "/")+"/jmap", bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("build jmap request: %w", err)
	}
	req.Header.Set("Authorization", r.stalwartAuth)
	req.Header.Set("Content-Type", "application/json")
	return r.doJSON(req, out)
}

func (r *Runner) doJSON(req *http.Request, out any) error {
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected status %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode json: %w", err)
	}
	return nil
}

func (r *Runner) forwardEmail(ctx context.Context, email jmapEmail) error {
	ctx, span := tracer.Start(ctx, "forwarder.forward-email", trace.WithAttributes(
		attribute.String("email.id", email.ID),
		attribute.String("email.subject", email.Subject),
	))
	defer span.End()

	payload := resendSendRequest{
		From:    formatSender(r.cfg.FromName, r.cfg.FromAddress),
		To:      []string{strings.TrimSpace(r.creds.ForwardTo)},
		Subject: forwardSubject(email.Subject),
		Text:    r.buildForwardBody(email),
		Headers: map[string]string{
			"X-Forge-Metal-Mailbox":           r.ceoAddress,
			"X-Forge-Metal-Original-Email-ID": email.ID,
		},
	}
	if replyTo := firstAddress(email.From); replyTo != "" {
		payload.Headers["Reply-To"] = replyTo
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("marshal resend payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.resendEndpoint, bytes.NewReader(raw))
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("build resend request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(r.creds.ResendAPIKey))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "mailbox-service:"+email.ID)

	var resendResp resendSendResponse
	if err := r.doJSON(req, &resendResp); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("resend send email %s: %w", email.ID, err)
	}

	r.recordForward(email.ID)
	r.logger.InfoContext(ctx, "mailbox-service: forwarded email",
		"email_id", email.ID,
		"resend_id", resendResp.ID,
		"subject", email.Subject,
	)
	return nil
}

func (r *Runner) buildForwardBody(email jmapEmail) string {
	body := firstTextBody(email)
	if body == "" {
		body = email.Preview
	}
	body = strings.TrimSpace(body)
	if len(body) > maxForwardBodyChars {
		body = body[:maxForwardBodyChars] + "\n\n[truncated by mailbox-service]"
	}

	var lines []string
	lines = append(lines,
		"forge-metal operator mailbox forward",
		"",
		"Mailbox: "+r.ceoAddress,
		"Original From: "+formatAddresses(email.From),
		"Original To: "+formatAddresses(email.To),
		"Received At: "+email.ReceivedAt,
		"Subject: "+safeSubject(email.Subject),
		"Email ID: "+email.ID,
		"",
		"--- Original Message ---",
	)
	if body == "" {
		lines = append(lines, "(no text body available)")
	} else {
		lines = append(lines, body)
	}
	return strings.Join(lines, "\n")
}

func (r *Runner) shouldSkip(email jmapEmail) bool {
	return strings.EqualFold(firstAddress(email.From), r.cfg.FromAddress)
}

func (r *Runner) setRunning(running bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status.Running = running
}

func (r *Runner) setError(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status.LastError = err.Error()
}

func (r *Runner) clearError() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status.LastError = ""
}

func (r *Runner) setSynced() {
	now := time.Now().UTC()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status.LastSyncAt = &now
	r.status.LastError = ""
}

func (r *Runner) recordForward(emailID string) {
	now := time.Now().UTC()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status.LastForwardedAt = &now
	r.status.LastForwardedEmailID = emailID
	r.status.LastError = ""
}

func loadState(path string) (*state, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &state{seenSet: map[string]struct{}{}}, nil
		}
		return nil, err
	}
	var st state
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, fmt.Errorf("decode state: %w", err)
	}
	st.rebuild()
	return &st, nil
}

func saveState(path string, st *state) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("mkdir state dir: %w", err)
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o640); err != nil {
		return fmt.Errorf("write temp state: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename state: %w", err)
	}
	return nil
}

func (s *state) rebuild() {
	s.seenSet = make(map[string]struct{}, len(s.SeenIDs))
	for _, id := range s.SeenIDs {
		s.seenSet[id] = struct{}{}
	}
}

func (s *state) hasSeen(id string) bool {
	if s.seenSet == nil {
		s.rebuild()
	}
	_, ok := s.seenSet[id]
	return ok
}

func (s *state) markSeen(id string, limit int) {
	if s.seenSet == nil {
		s.rebuild()
	}
	if _, ok := s.seenSet[id]; ok {
		return
	}
	s.SeenIDs = append(s.SeenIDs, id)
	s.seenSet[id] = struct{}{}
	if len(s.SeenIDs) <= limit {
		return
	}
	drop := len(s.SeenIDs) - limit
	for _, oldID := range s.SeenIDs[:drop] {
		delete(s.seenSet, oldID)
	}
	s.SeenIDs = append([]string(nil), s.SeenIDs[drop:]...)
}

func decodeMethodPayload[T any](responses []json.RawMessage, expectedName string) (T, error) {
	var zero T
	for _, raw := range responses {
		var parts []json.RawMessage
		if err := json.Unmarshal(raw, &parts); err != nil {
			return zero, fmt.Errorf("decode method response wrapper: %w", err)
		}
		if len(parts) < 2 {
			return zero, errors.New("malformed method response")
		}
		var name string
		if err := json.Unmarshal(parts[0], &name); err != nil {
			return zero, fmt.Errorf("decode method name: %w", err)
		}
		if name != expectedName {
			continue
		}
		var payload T
		if err := json.Unmarshal(parts[1], &payload); err != nil {
			return zero, fmt.Errorf("decode %s payload: %w", expectedName, err)
		}
		return payload, nil
	}
	return zero, fmt.Errorf("method response %s not found", expectedName)
}

func firstTextBody(email jmapEmail) string {
	var parts []string
	for _, part := range email.TextBody {
		value := strings.TrimSpace(email.BodyValues[part.PartID].Value)
		if value != "" {
			parts = append(parts, value)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func firstAddress(addrs []emailAddress) string {
	if len(addrs) == 0 {
		return ""
	}
	return strings.TrimSpace(addrs[0].Email)
}

func formatAddresses(addrs []emailAddress) string {
	if len(addrs) == 0 {
		return "(unknown)"
	}
	formatted := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		if addr.Name != "" {
			formatted = append(formatted, fmt.Sprintf("%s <%s>", addr.Name, addr.Email))
			continue
		}
		formatted = append(formatted, addr.Email)
	}
	return strings.Join(formatted, ", ")
}

func forwardSubject(subject string) string {
	return "[ceo] " + safeSubject(subject)
}

func safeSubject(subject string) string {
	if strings.TrimSpace(subject) == "" {
		return "(no subject)"
	}
	return subject
}

func formatSender(name, address string) string {
	if name == "" {
		return address
	}
	return fmt.Sprintf("%s <%s>", name, address)
}

func reverseStrings(ids []string) []string {
	out := make([]string, len(ids))
	for i := range ids {
		out[len(ids)-1-i] = ids[i]
	}
	return out
}

func basicAuth(user, password string) string {
	token := user + ":" + password
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(token))
}
