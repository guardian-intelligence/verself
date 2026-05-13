package api

import (
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/verself/sandbox-rental-service/internal/jobs"
	"github.com/verself/sandbox-rental-service/internal/recurring"
	"github.com/verself/service-runtime/humaapi"
	runtimeiam "github.com/verself/service-runtime/iam"
)

type PublicAPIConfig struct {
	PublicBaseURL  string
	Authorizer     runtimeiam.OperationAuthorizer
	InstallationID string
}

func NewAPI(mux *http.ServeMux, version, listenAddr string, svc *jobs.Service, recurringSvc *recurring.Service, publicConfig PublicAPIConfig) huma.API {
	config := humaapi.DefaultConfig("Sandbox Rental Service", version)
	config.Servers = []*huma.Server{{URL: serverURL(listenAddr)}}
	api := humago.New(mux, config)
	applyPublicAPISecurityScheme(api)
	RegisterRoutes(api, svc, recurringSvc, publicConfig)
	humaapi.ApplyOpenAPIWireDefaults(api)
	return api
}

func NewInternalAPI(mux *http.ServeMux, version, listenAddr string, svc *jobs.Service) huma.API {
	config := humaapi.DefaultConfig("Sandbox Rental Service Internal API", version)
	config.Servers = []*huma.Server{{URL: serverURL(listenAddr)}}
	api := humago.New(mux, config)
	applyInternalAPISecurityScheme(api)
	RegisterInternalRoutes(api, svc)
	humaapi.ApplyOpenAPIWireDefaults(api)
	return api
}

func serverURL(addr string) string {
	if strings.Contains(addr, "://") {
		return addr
	}
	return "http://" + addr
}
