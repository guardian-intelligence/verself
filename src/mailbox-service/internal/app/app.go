package app

import (
	"context"
	"net/http"
	"time"

	"github.com/forge-metal/mailbox-service/internal/forwarder"
	"github.com/forge-metal/mailbox-service/internal/sessionproxy"
)

type ServiceStatus struct {
	StartedAt       time.Time        `json:"started_at"`
	StalwartBaseURL string           `json:"stalwart_base_url"`
	PublicBaseURL   string           `json:"public_base_url"`
	Forwarder       forwarder.Status `json:"forwarder"`
}

type Service struct {
	startedAt       time.Time
	stalwartBaseURL string
	publicBaseURL   string
	proxy           *sessionproxy.Handler
	forwarder       *forwarder.Runner
}

func New(stalwartBaseURL, publicBaseURL string, proxy *sessionproxy.Handler, forwarderRunner *forwarder.Runner) *Service {
	return &Service{
		startedAt:       time.Now().UTC(),
		stalwartBaseURL: stalwartBaseURL,
		publicBaseURL:   publicBaseURL,
		proxy:           proxy,
		forwarder:       forwarderRunner,
	}
}

func (s *Service) RegisterRoutes(mux *http.ServeMux) {
	mux.Handle("/jmap/session", s.proxy)
}

func (s *Service) StartBackground(ctx context.Context) {
	if s.forwarder == nil {
		return
	}
	go s.forwarder.Run(ctx)
}

func (s *Service) Ready(ctx context.Context) error {
	return s.proxy.Ready(ctx)
}

func (s *Service) Status() ServiceStatus {
	status := ServiceStatus{
		StartedAt:       s.startedAt,
		StalwartBaseURL: s.stalwartBaseURL,
		PublicBaseURL:   s.publicBaseURL,
	}
	if s.forwarder != nil {
		status.Forwarder = s.forwarder.Snapshot()
	}
	return status
}
