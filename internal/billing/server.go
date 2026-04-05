package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"
)

// Server is the billing HTTP server. It wraps a Client and exposes the
// localhost JSON API defined in spec §8.4, plus the Stripe webhook endpoint.
// The task worker runs as a background goroutine alongside the HTTP listener.
type Server struct {
	client *Client
	addr   string
}

// NewServer constructs a billing HTTP server bound to addr.
func NewServer(client *Client, addr string) *Server {
	return &Server{client: client, addr: addr}
}

// Handler returns the http.Handler with all routes mounted.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Read-only endpoints for Next.js apps (spec §8.4).
	mux.HandleFunc("GET /internal/billing/v1/orgs/{org_id}/balance", s.handleGetBalance)
	mux.HandleFunc("GET /internal/billing/v1/orgs/{org_id}/products/{product_id}/balance", s.handleGetProductBalance)
	mux.HandleFunc("GET /internal/billing/v1/orgs/{org_id}/subscriptions", s.handleGetSubscriptions)
	mux.HandleFunc("GET /internal/billing/v1/orgs/{org_id}/grants", s.handleGetGrants)
	mux.HandleFunc("GET /internal/billing/v1/orgs/{org_id}/usage", s.handleGetUsage)

	// Write endpoints — Next.js apps call these to create Stripe sessions.
	mux.HandleFunc("POST /internal/billing/v1/checkout", s.handleCreateCheckout)
	mux.HandleFunc("POST /internal/billing/v1/subscribe", s.handleCreateSubscription)

	// Stripe webhook — Caddy proxies /webhooks/stripe to this.
	mux.Handle("POST /webhooks/stripe", s.client.WebhookHandler())

	return mux
}

// ListenAndServe starts the HTTP server and task worker. Blocks until ctx
// is cancelled, then drains in-flight work with a 10s grace period.
func (s *Server) ListenAndServe(ctx context.Context) error {
	srv := &http.Server{
		Addr:              s.addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	workerCtx, workerCancel := context.WithCancel(ctx)
	defer workerCancel()

	workerDone := make(chan error, 1)
	go func() {
		workerDone <- s.client.RunWorker(workerCtx, 5*time.Second)
	}()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("billing: shutdown: %v", err)
		}
	}()

	log.Printf("billing: listening on %s", s.addr)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		workerCancel()
		return fmt.Errorf("billing: listen: %w", err)
	}

	workerCancel()
	<-workerDone
	return nil
}

// --- helpers ---

func parseOrgID(r *http.Request) (OrgID, error) {
	v, err := strconv.ParseUint(r.PathValue("org_id"), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid org_id: %w", err)
	}
	return OrgID(v), nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// --- handlers ---

func (s *Server) handleGetBalance(w http.ResponseWriter, r *http.Request) {
	orgID, err := parseOrgID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	balance, err := s.client.GetOrgBalance(r.Context(), orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "get balance: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"org_id":             r.PathValue("org_id"),
		"free_tier_available": balance.FreeTierAvailable,
		"free_tier_pending":   balance.FreeTierPending,
		"credit_available":    balance.CreditAvailable,
		"credit_pending":      balance.CreditPending,
		"total_available":     balance.TotalAvailable,
	})
}

func (s *Server) handleGetProductBalance(w http.ResponseWriter, r *http.Request) {
	// Spec §2.4 — not yet implemented in the billing client.
	writeError(w, http.StatusNotImplemented, "product balance not yet implemented")
}

func (s *Server) handleGetSubscriptions(w http.ResponseWriter, r *http.Request) {
	orgID, err := parseOrgID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	orgIDStr := strconv.FormatUint(uint64(orgID), 10)
	rows, err := s.client.pg.QueryContext(r.Context(), `
		SELECT subscription_id, plan_id, product_id, cadence, status,
		       stripe_subscription_id, current_period_start, current_period_end,
		       overage_cap_units, created_at
		FROM subscriptions
		WHERE org_id = $1
		ORDER BY created_at DESC
	`, orgIDStr)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query subscriptions: "+err.Error())
		return
	}
	defer rows.Close()

	type subscriptionRow struct {
		SubscriptionID      int64      `json:"subscription_id"`
		PlanID              string     `json:"plan_id"`
		ProductID           string     `json:"product_id"`
		Cadence             string     `json:"cadence"`
		Status              string     `json:"status"`
		StripeSubscriptionID *string   `json:"stripe_subscription_id,omitempty"`
		CurrentPeriodStart  *time.Time `json:"current_period_start,omitempty"`
		CurrentPeriodEnd    *time.Time `json:"current_period_end,omitempty"`
		OverageCapUnits     *int64     `json:"overage_cap_units,omitempty"`
		CreatedAt           time.Time  `json:"created_at"`
	}

	var subs []subscriptionRow
	for rows.Next() {
		var sub subscriptionRow
		if err := rows.Scan(
			&sub.SubscriptionID, &sub.PlanID, &sub.ProductID, &sub.Cadence, &sub.Status,
			&sub.StripeSubscriptionID, &sub.CurrentPeriodStart, &sub.CurrentPeriodEnd,
			&sub.OverageCapUnits, &sub.CreatedAt,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "scan subscription: "+err.Error())
			return
		}
		subs = append(subs, sub)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "iterate subscriptions: "+err.Error())
		return
	}
	if subs == nil {
		subs = []subscriptionRow{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"org_id":        r.PathValue("org_id"),
		"subscriptions": subs,
	})
}

func (s *Server) handleGetGrants(w http.ResponseWriter, r *http.Request) {
	orgID, err := parseOrgID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	orgIDStr := strconv.FormatUint(uint64(orgID), 10)

	query := `
		SELECT grant_id, product_id, amount, source, expires_at, closed_at, created_at
		FROM credit_grants
		WHERE org_id = $1
	`
	args := []any{orgIDStr}
	argIdx := 2

	if pid := r.URL.Query().Get("product_id"); pid != "" {
		query += fmt.Sprintf(" AND product_id = $%d", argIdx)
		args = append(args, pid)
		argIdx++
	}
	if r.URL.Query().Get("active") == "true" {
		query += " AND closed_at IS NULL"
	}
	query += " ORDER BY created_at DESC"

	rows, err := s.client.pg.QueryContext(r.Context(), query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query grants: "+err.Error())
		return
	}
	defer rows.Close()

	type grantRow struct {
		GrantID   string     `json:"grant_id"`
		ProductID string     `json:"product_id"`
		Amount    int64      `json:"amount"`
		Source    string     `json:"source"`
		ExpiresAt *time.Time `json:"expires_at,omitempty"`
		ClosedAt  *time.Time `json:"closed_at,omitempty"`
		CreatedAt time.Time  `json:"created_at"`
	}

	var grants []grantRow
	for rows.Next() {
		var g grantRow
		if err := rows.Scan(&g.GrantID, &g.ProductID, &g.Amount, &g.Source, &g.ExpiresAt, &g.ClosedAt, &g.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "scan grant: "+err.Error())
			return
		}
		grants = append(grants, g)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "iterate grants: "+err.Error())
		return
	}
	if grants == nil {
		grants = []grantRow{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"org_id": r.PathValue("org_id"),
		"grants": grants,
	})
}

func (s *Server) handleGetUsage(w http.ResponseWriter, r *http.Request) {
	orgID, err := parseOrgID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	orgIDStr := strconv.FormatUint(uint64(orgID), 10)

	query := `
		SELECT event_id, event_type, payload, created_at
		FROM billing_events
		WHERE org_id = $1
	`
	args := []any{orgIDStr}
	argIdx := 2

	if pid := r.URL.Query().Get("product_id"); pid != "" {
		query += fmt.Sprintf(" AND payload->>'product_id' = $%d", argIdx)
		args = append(args, pid)
		argIdx++
	}
	if since := r.URL.Query().Get("since"); since != "" {
		t, err := time.Parse(time.RFC3339, since)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid since: "+err.Error())
			return
		}
		query += fmt.Sprintf(" AND created_at >= $%d", argIdx)
		args = append(args, t)
		argIdx++
	}
	query += " ORDER BY created_at DESC"

	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 1000 {
			limit = parsed
		}
	}
	query += fmt.Sprintf(" LIMIT $%d", argIdx)
	args = append(args, limit)

	rows, err := s.client.pg.QueryContext(r.Context(), query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query usage: "+err.Error())
		return
	}
	defer rows.Close()

	type usageRow struct {
		EventID   int64           `json:"event_id"`
		EventType string          `json:"event_type"`
		Payload   json.RawMessage `json:"payload"`
		CreatedAt time.Time       `json:"created_at"`
	}

	var events []usageRow
	for rows.Next() {
		var e usageRow
		if err := rows.Scan(&e.EventID, &e.EventType, &e.Payload, &e.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "scan event: "+err.Error())
			return
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "iterate events: "+err.Error())
		return
	}
	if events == nil {
		events = []usageRow{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"org_id": r.PathValue("org_id"),
		"events": events,
	})
}

func (s *Server) handleCreateCheckout(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OrgID       uint64 `json:"org_id"`
		ProductID   string `json:"product_id"`
		AmountCents int64  `json:"amount_cents"`
		SuccessURL  string `json:"success_url"`
		CancelURL   string `json:"cancel_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if req.OrgID == 0 || req.ProductID == "" || req.AmountCents <= 0 || req.SuccessURL == "" || req.CancelURL == "" {
		writeError(w, http.StatusBadRequest, "org_id, product_id, amount_cents, success_url, and cancel_url are required")
		return
	}

	url, err := s.client.CreateCheckoutSession(r.Context(), OrgID(req.OrgID), req.ProductID, CheckoutParams{
		AmountCents: req.AmountCents,
		SuccessURL:  req.SuccessURL,
		CancelURL:   req.CancelURL,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create checkout: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"url": url})
}

func (s *Server) handleCreateSubscription(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OrgID      uint64 `json:"org_id"`
		PlanID     string `json:"plan_id"`
		Cadence    string `json:"cadence"`
		SuccessURL string `json:"success_url"`
		CancelURL  string `json:"cancel_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if req.OrgID == 0 || req.PlanID == "" || req.SuccessURL == "" || req.CancelURL == "" {
		writeError(w, http.StatusBadRequest, "org_id, plan_id, success_url, and cancel_url are required")
		return
	}

	cadence := BillingCadence(req.Cadence)
	if cadence == "" {
		cadence = CadenceMonthly
	}

	url, err := s.client.CreateSubscription(r.Context(), OrgID(req.OrgID), req.PlanID, cadence, req.SuccessURL, req.CancelURL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create subscription: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"url": url})
}
