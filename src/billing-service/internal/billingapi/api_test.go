package billingapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOrgPathDecimalUint64Validation(t *testing.T) {
	mux := http.NewServeMux()
	NewAPI(mux, Config{Version: "test", ListenAddr: "127.0.0.1:0"})

	t.Run("accepts decimal string path param", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/internal/billing/v1/orgs/367889413595774308/grants", nil)
		mux.ServeHTTP(rec, req)

		if rec.Code == http.StatusUnprocessableEntity {
			t.Fatalf("expected decimal org_id path param to pass Huma validation, got 422: %s", rec.Body.String())
		}
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected handler to run and fail on nil billing client, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("rejects non-decimal path param", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/internal/billing/v1/orgs/notdecimal/grants", nil)
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected non-decimal org_id to fail validation, got %d: %s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "expected string to match pattern") {
			t.Fatalf("expected decimal pattern validation error, got: %s", rec.Body.String())
		}
	})
}
