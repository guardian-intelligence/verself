package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/forge-metal/mailbox-service/internal/jmap"
	"github.com/forge-metal/mailbox-service/internal/mailstore"
)

type Config struct {
	StalwartBaseURL   string
	AdminPassword     string
	MailboxPasswords  map[string]string
	DiscoveryInterval time.Duration
	ReconcileInterval time.Duration
}

type DiscoveryPrincipal struct {
	AccountID     string
	EmailAddress  string
	DisplayName   string
	PrincipalType string
}

type AccountStatus struct {
	AccountID       string     `json:"account_id"`
	Running         bool       `json:"running"`
	Connected       bool       `json:"connected"`
	LastSyncAt      *time.Time `json:"last_sync_at,omitempty"`
	LastEventAt     *time.Time `json:"last_event_at,omitempty"`
	LastConnectedAt *time.Time `json:"last_connected_at,omitempty"`
	LastError       string     `json:"last_error,omitempty"`
}

type Status struct {
	Running         bool                     `json:"running"`
	LastDiscoveryAt *time.Time               `json:"last_discovery_at,omitempty"`
	LastError       string                   `json:"last_error,omitempty"`
	Accounts        map[string]AccountStatus `json:"accounts"`
}

type workerHandle struct {
	cancel context.CancelFunc
	worker *Worker
}

type Manager struct {
	cfg          Config
	store        *mailstore.Store
	logger       *slog.Logger
	httpClient   *http.Client
	streamClient *http.Client

	mu      sync.RWMutex
	status  Status
	workers map[string]*workerHandle
}

func New(cfg Config, store *mailstore.Store, logger *slog.Logger, httpClient, streamClient *http.Client) *Manager {
	return &Manager{
		cfg:          cfg,
		store:        store,
		logger:       logger,
		httpClient:   httpClient,
		streamClient: streamClient,
		status: Status{
			Accounts: map[string]AccountStatus{},
		},
		workers: map[string]*workerHandle{},
	}
}

func (m *Manager) Run(ctx context.Context) {
	m.setRunning(true)
	defer m.setRunning(false)

	if err := m.reconcileWorkers(ctx); err != nil {
		m.setError(err)
		m.logger.ErrorContext(ctx, "mailbox-service: mailbox sync discovery failed", "error", err)
	}

	ticker := time.NewTicker(m.cfg.DiscoveryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.stopWorkers()
			return
		case <-ticker.C:
			if err := m.reconcileWorkers(ctx); err != nil {
				m.setError(err)
				m.logger.ErrorContext(ctx, "mailbox-service: mailbox sync discovery failed", "error", err)
			}
		}
	}
}

func (m *Manager) Ready(ctx context.Context) error {
	if err := m.store.Ready(ctx); err != nil {
		return err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.status.Running {
		return fmt.Errorf("sync manager is not running")
	}
	if m.status.LastDiscoveryAt == nil {
		return fmt.Errorf("sync manager has not completed discovery yet")
	}
	return nil
}

func (m *Manager) Snapshot() Status {
	m.mu.RLock()
	defer m.mu.RUnlock()
	status := m.status
	status.Accounts = make(map[string]AccountStatus, len(m.workers))
	for accountID, handle := range m.workers {
		status.Accounts[accountID] = handle.worker.Snapshot()
	}
	return status
}

func (m *Manager) mailboxPassword(accountID string) (string, error) {
	password, ok := m.cfg.MailboxPasswords[accountID]
	if !ok || strings.TrimSpace(password) == "" {
		return "", fmt.Errorf("no password configured for account %q", accountID)
	}
	return strings.TrimSpace(password), nil
}

func (m *Manager) AccountClient(ctx context.Context, accountID string) (*jmap.Client, mailstore.Account, error) {
	account, err := m.store.LookupAccount(ctx, accountID)
	if err != nil {
		return nil, mailstore.Account{}, err
	}
	password, err := m.mailboxPassword(accountID)
	if err != nil {
		return nil, mailstore.Account{}, err
	}
	client, err := jmap.New(jmap.Config{
		BaseURL:  m.cfg.StalwartBaseURL,
		Username: accountID,
		Password: password,
	}, m.httpClient, m.streamClient)
	if err != nil {
		return nil, mailstore.Account{}, err
	}
	return client, account, nil
}

func (m *Manager) reconcileWorkers(ctx context.Context) error {
	principals, err := m.discoverPrincipals(ctx)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	m.markDiscovery(now)

	seen := map[string]DiscoveryPrincipal{}
	for _, principal := range principals {
		seen[principal.AccountID] = principal
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for accountID, principal := range seen {
		handle, ok := m.workers[accountID]
		if ok {
			handle.worker.UpdatePrincipal(principal)
			continue
		}

		password, err := m.mailboxPassword(accountID)
		if err != nil {
			return err
		}

		workerCtx, cancel := context.WithCancel(ctx)
		worker := NewWorker(workerConfig{
			Principal:         principal,
			StalwartBaseURL:   m.cfg.StalwartBaseURL,
			MailboxPassword:   password,
			ReconcileInterval: m.cfg.ReconcileInterval,
			Store:             m.store,
			Logger:            m.logger.With("mailbox_account", principal.AccountID),
			HTTPClient:        m.httpClient,
			StreamClient:      m.streamClient,
		})
		m.workers[accountID] = &workerHandle{
			cancel: cancel,
			worker: worker,
		}
		go worker.Run(workerCtx)
	}

	for accountID, handle := range m.workers {
		if _, ok := seen[accountID]; ok {
			continue
		}
		handle.cancel()
		delete(m.workers, accountID)
		delete(m.status.Accounts, accountID)
	}

	return nil
}

func (m *Manager) discoverPrincipals(ctx context.Context) ([]DiscoveryPrincipal, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(m.cfg.StalwartBaseURL, "/")+"/api/principal?limit=1000", nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth("admin", m.cfg.AdminPassword)

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("principal discovery returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var payload struct {
		Data struct {
			Items []struct {
				Name        string   `json:"name"`
				Type        string   `json:"type"`
				Description string   `json:"description"`
				Emails      []string `json:"emails"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}

	var principals []DiscoveryPrincipal
	for _, item := range payload.Data.Items {
		if item.Type != "individual" {
			continue
		}
		if strings.TrimSpace(item.Name) == "" {
			continue
		}
		emailAddress := ""
		if len(item.Emails) > 0 {
			emailAddress = item.Emails[0]
		}
		principals = append(principals, DiscoveryPrincipal{
			AccountID:     item.Name,
			EmailAddress:  emailAddress,
			DisplayName:   item.Description,
			PrincipalType: item.Type,
		})
	}
	return principals, nil
}

func (m *Manager) stopWorkers() {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, handle := range m.workers {
		handle.cancel()
	}
}

func (m *Manager) setRunning(running bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status.Running = running
}

func (m *Manager) markDiscovery(now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status.LastDiscoveryAt = &now
	m.status.LastError = ""
}

func (m *Manager) setError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status.LastError = err.Error()
}

func shouldResync(err error) bool {
	var methodErr jmap.MethodError
	if !errors.As(err, &methodErr) {
		return false
	}
	return strings.Contains(methodErr.Type, "cannotCalculateChanges") || strings.Contains(methodErr.Description, "cannotCalculateChanges")
}
