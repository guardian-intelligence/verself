package api

import (
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/verself/iam-service/internal/authz"
	"github.com/verself/iam-service/internal/identity"
	"github.com/verself/service-runtime/humaapi"
)

type Config struct {
	Version        string
	ListenAddr     string
	InstallationID string
	ProductBaseURL string
	Service        *identity.Service
	Authz          *authz.Service
	InviteNotifier InviteNotifier
}

func NewAPI(mux *http.ServeMux, cfg Config) huma.API {
	version := cfg.Version
	if version == "" {
		version = "1.0.0"
	}
	config := humaapi.DefaultConfig("IAM Service", version)
	if cfg.ListenAddr != "" {
		config.Servers = []*huma.Server{{URL: serverURL(cfg.ListenAddr)}}
	}
	api := humago.New(mux, config)
	applyPublicAPISecurityScheme(api)
	RegisterRoutes(api, cfg.Service, cfg.Authz, cfg.InstallationID, cfg.ProductBaseURL, cfg.InviteNotifier)
	humaapi.ApplyOpenAPIWireDefaults(api)
	return api
}

func serverURL(addr string) string {
	if strings.Contains(addr, "://") {
		return addr
	}
	return "http://" + addr
}
