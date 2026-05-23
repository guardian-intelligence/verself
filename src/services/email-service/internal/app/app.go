package app

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/verself/email-service/internal/mailstore"
	"github.com/verself/email-service/internal/provider"
	"github.com/verself/email-service/internal/sessionproxy"
	mailboxsync "github.com/verself/email-service/internal/sync"
)

type ServiceStatus struct {
	StartedAt       time.Time          `json:"started_at"`
	StalwartBaseURL string             `json:"stalwart_base_url"`
	PublicBaseURL   string             `json:"public_base_url"`
	MailboxSync     mailboxsync.Status `json:"mailbox_sync"`
	SenderProvider  string             `json:"sender_provider"`
}

type Service struct {
	startedAt       time.Time
	stalwartBaseURL string
	publicBaseURL   string
	proxy           *sessionproxy.Handler
	store           *mailstore.Store
	syncManager     *mailboxsync.Manager
	sender          provider.Sender
}

func New(stalwartBaseURL, publicBaseURL string, proxy *sessionproxy.Handler, store *mailstore.Store, syncManager *mailboxsync.Manager, sender provider.Sender) *Service {
	return &Service{
		startedAt:       time.Now().UTC(),
		stalwartBaseURL: stalwartBaseURL,
		publicBaseURL:   publicBaseURL,
		proxy:           proxy,
		store:           store,
		syncManager:     syncManager,
		sender:          sender,
	}
}

func (s *Service) RegisterRoutes(mux *http.ServeMux) {
	mux.Handle("/jmap/session", s.proxy)
}

func (s *Service) StartBackground(ctx context.Context) {
	if s.syncManager != nil {
		go s.syncManager.Run(ctx)
	}
}

func (s *Service) Ready(ctx context.Context) error {
	if s.store != nil {
		if err := s.store.Ready(ctx); err != nil {
			return err
		}
	}
	if s.syncManager != nil {
		if err := s.syncManager.Ready(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) GetBoundAccount(ctx context.Context, subject string) (mailstore.Account, error) {
	if s.store == nil {
		return mailstore.Account{}, fmt.Errorf("mailstore is not configured")
	}
	accountID, err := s.store.ResolveBinding(ctx, subject)
	if err != nil {
		return mailstore.Account{}, err
	}
	return s.store.LookupAccount(ctx, accountID)
}

func (s *Service) Status() ServiceStatus {
	status := ServiceStatus{
		StartedAt:       s.startedAt,
		StalwartBaseURL: s.stalwartBaseURL,
		PublicBaseURL:   s.publicBaseURL,
	}
	if s.syncManager != nil {
		status.MailboxSync = s.syncManager.Snapshot()
	}
	if s.sender != nil {
		status.SenderProvider = s.sender.Name()
	}
	return status
}
