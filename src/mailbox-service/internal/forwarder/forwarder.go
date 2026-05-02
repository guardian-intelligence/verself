package forwarder

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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

	"github.com/verself/mailbox-service/internal/jmap"
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
	jmapClient     *jmap.Client
	ceoAddress     string
	resendEndpoint string

	mu     sync.RWMutex
	status Status
}

type state struct {
	SeenIDs []string `json:"seen_ids"`

	seenSet map[string]struct{} `json:"-"`
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
	jmapClient, _ := jmap.New(jmap.Config{
		BaseURL:  cfg.StalwartBaseURL,
		Username: strings.TrimSpace(cfg.MailboxUser),
		Password: strings.TrimSpace(creds.MailboxPassword),
	}, httpClient, httpClient)
	return &Runner{
		cfg:            cfg,
		creds:          creds,
		logger:         logger,
		httpClient:     httpClient,
		jmapClient:     jmapClient,
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

func (r *Runner) latestEmails(ctx context.Context, limit int) ([]jmap.Email, error) {
	accountID, err := r.jmapClient.AccountID(ctx)
	if err != nil {
		return nil, err
	}

	query, err := r.jmapClient.EmailQuery(ctx, accountID, 0, limit)
	if err != nil {
		return nil, fmt.Errorf("email query: %w", err)
	}
	if len(query.IDs) == 0 {
		return nil, nil
	}

	ids := reverseStrings(query.IDs)
	result, err := r.jmapClient.EmailGet(ctx, accountID, ids, jmap.EmailGetOptions{
		FetchTextBodyValues: true,
	})
	if err != nil {
		return nil, fmt.Errorf("email get: %w", err)
	}
	return result.List, nil
}

func (r *Runner) forwardEmail(ctx context.Context, email jmap.Email) error {
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
			"X-Verself-Mailbox":           r.ceoAddress,
			"X-Verself-Original-Email-ID": email.ID,
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
	req.Header.Set("Idempotency-Key", resendIdempotencyKey(email))

	var resendResp resendSendResponse
	if err := doJSON(r.httpClient, req, &resendResp); err != nil {
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

func (r *Runner) buildForwardBody(email jmap.Email) string {
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
		"verself operator mailbox forward",
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

func (r *Runner) shouldSkip(email jmap.Email) bool {
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

func firstTextBody(email jmap.Email) string {
	var parts []string
	for _, part := range email.TextBody {
		value := strings.TrimSpace(email.BodyValues[part.PartID].Value)
		if value != "" {
			parts = append(parts, value)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func firstAddress(addrs []jmap.Address) string {
	if len(addrs) == 0 {
		return ""
	}
	return strings.TrimSpace(addrs[0].Email)
}

func formatAddresses(addrs []jmap.Address) string {
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

func resendIdempotencyKey(email jmap.Email) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		email.ID,
		email.ReceivedAt,
		email.Subject,
		firstAddress(email.From),
		firstAddress(email.To),
		firstTextBody(email),
		email.Preview,
	}, "\x00")))
	return "mailbox-service:" + email.ID + ":" + hex.EncodeToString(sum[:12])
}

func doJSON(client *http.Client, req *http.Request, out any) error {
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

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
