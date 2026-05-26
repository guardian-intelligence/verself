package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRegisterPublicRoutesServesOCIPing(t *testing.T) {
	mux := http.NewServeMux()
	RegisterPublicRoutes(mux, Config{})

	req := httptest.NewRequest(http.MethodGet, "/v2/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v2/ status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Docker-Distribution-API-Version"); got != "registry/2.0" {
		t.Fatalf("Docker-Distribution-API-Version = %q, want registry/2.0", got)
	}
}
