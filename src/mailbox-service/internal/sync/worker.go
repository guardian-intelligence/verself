package sync

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/forge-metal/mailbox-service/internal/jmap"
	"github.com/forge-metal/mailbox-service/internal/mailstore"
)

type workerConfig struct {
	Principal         DiscoveryPrincipal
	StalwartBaseURL   string
	MailboxPassword   string
	ReconcileInterval time.Duration
	Store             *mailstore.Store
	Logger            *slog.Logger
	HTTPClient        *http.Client
	StreamClient      *http.Client
}

type Worker struct {
	cfg workerConfig

	mu        sync.RWMutex
	principal DiscoveryPrincipal
	status    AccountStatus
}

func NewWorker(cfg workerConfig) *Worker {
	return &Worker{
		cfg:       cfg,
		principal: cfg.Principal,
		status: AccountStatus{
			AccountID: cfg.Principal.AccountID,
		},
	}
}

func (w *Worker) UpdatePrincipal(principal DiscoveryPrincipal) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.principal = principal
	w.status.AccountID = principal.AccountID
}

func (w *Worker) Snapshot() AccountStatus {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.status
}

func (w *Worker) Run(ctx context.Context) {
	w.setRunning(true)
	defer w.setRunning(false)

	for {
		if err := ctx.Err(); err != nil {
			return
		}

		principal := w.getPrincipal()
		client, err := jmap.New(jmap.Config{
			BaseURL:  w.cfg.StalwartBaseURL,
			Username: principal.AccountID,
			Password: w.cfg.MailboxPassword,
		}, w.cfg.HTTPClient, w.cfg.StreamClient)
		if err != nil {
			w.setError(err)
			w.cfg.Logger.ErrorContext(ctx, "mailbox-service: sync worker client init failed", "error", err)
			if !sleepContext(ctx, 5*time.Second) {
				return
			}
			continue
		}

		if err := w.ensureSynced(ctx, client, principal); err != nil {
			w.setError(err)
			w.cfg.Logger.ErrorContext(ctx, "mailbox-service: sync worker bootstrap failed", "error", err)
			if !sleepContext(ctx, 5*time.Second) {
				return
			}
			continue
		}

		stream, err := client.OpenEventSource(ctx, []string{"Mailbox", "Email", "Thread"}, 30*time.Second)
		if err != nil {
			w.setError(err)
			w.cfg.Logger.ErrorContext(ctx, "mailbox-service: sync worker eventsource connect failed", "error", err)
			if !sleepContext(ctx, 5*time.Second) {
				return
			}
			continue
		}

		now := time.Now().UTC()
		w.setConnected(now, true)
		w.cfg.Logger.InfoContext(ctx, "mailbox-service: sync worker eventsource connected")

		eventCh := make(chan jmap.Event)
		errCh := make(chan error, 1)
		go func() {
			defer close(eventCh)
			for {
				event, err := stream.Next()
				if err != nil {
					errCh <- err
					return
				}
				eventCh <- event
			}
		}()

		reconcileTicker := time.NewTicker(w.cfg.ReconcileInterval)
	loop:
		for {
			select {
			case <-ctx.Done():
				reconcileTicker.Stop()
				stream.Close()
				w.setConnected(time.Time{}, false)
				return
			case event, ok := <-eventCh:
				if !ok {
					reconcileTicker.Stop()
					stream.Close()
					w.setConnected(time.Time{}, false)
					break loop
				}
				if err := w.handleEvent(ctx, client, principal, event); err != nil {
					w.setError(err)
					w.cfg.Logger.ErrorContext(ctx, "mailbox-service: sync worker event application failed", "error", err)
				}
			case err := <-errCh:
				reconcileTicker.Stop()
				stream.Close()
				w.setConnected(time.Time{}, false)
				if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, context.Canceled) {
					w.setError(err)
					w.cfg.Logger.ErrorContext(ctx, "mailbox-service: sync worker eventsource disconnected", "error", err)
				}
				break loop
			case <-reconcileTicker.C:
				if err := w.applyPendingChanges(ctx, client, principal, "reconcile"); err != nil {
					w.setError(err)
					w.cfg.Logger.ErrorContext(ctx, "mailbox-service: sync worker reconcile failed", "error", err)
				}
			}
		}

		if !sleepContext(ctx, 3*time.Second) {
			return
		}
	}
}

func (w *Worker) ensureSynced(ctx context.Context, client *jmap.Client, principal DiscoveryPrincipal) error {
	states, err := w.cfg.Store.GetSyncStates(ctx, principal.AccountID)
	if err != nil {
		return err
	}
	if states["Mailbox"] == "" || states["Email"] == "" || states["Thread"] == "" {
		return w.fullSync(ctx, client, principal)
	}
	return w.applyPendingChanges(ctx, client, principal, "startup")
}

func (w *Worker) handleEvent(ctx context.Context, client *jmap.Client, principal DiscoveryPrincipal, event jmap.Event) error {
	if len(event.Data) == 0 {
		return nil
	}
	change, err := event.DecodeStateChange()
	if err != nil {
		return err
	}
	if change.Type != "StateChange" {
		return nil
	}

	account, err := w.cfg.Store.LookupAccount(ctx, principal.AccountID)
	if err != nil {
		return err
	}
	types, ok := change.Changed[account.JMAPAccountID]
	if !ok {
		return nil
	}
	if _, ok := types["Mailbox"]; !ok {
		if _, ok := types["Email"]; !ok {
			if _, ok := types["Thread"]; !ok {
				return nil
			}
		}
	}

	now := time.Now().UTC()
	w.markEvent(now)
	return w.applyPendingChanges(ctx, client, principal, "event")
}

func (w *Worker) getPrincipal() DiscoveryPrincipal {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.principal
}

func (w *Worker) setRunning(running bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.status.Running = running
}

func (w *Worker) setConnected(now time.Time, connected bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.status.Connected = connected
	if connected {
		w.status.LastConnectedAt = &now
	}
}

func (w *Worker) markSynced(now time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.status.LastSyncAt = &now
	w.status.LastError = ""
}

func (w *Worker) markEvent(now time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.status.LastEventAt = &now
}

func (w *Worker) setError(err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.status.LastError = err.Error()
}

func sleepContext(ctx context.Context, wait time.Duration) bool {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
