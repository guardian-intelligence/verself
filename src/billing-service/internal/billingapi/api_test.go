package billingapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

func TestOrgPathAcceptsDecimalString(t *testing.T) {
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("test", "1.0.0"))

	type output struct {
		Body struct {
			OrgID string `json:"org_id"`
		}
	}

	huma.Get(api, "/orgs/{org_id}/balance", func(_ context.Context, input *OrgPath) (*output, error) {
		orgID, err := billingOrgID(input.OrgID)
		if err != nil {
			return nil, err
		}
		out := &output{}
		out.Body.OrgID = orgID.String()
		return out, nil
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/orgs/367889413595774308/balance", nil)
	mux.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected valid decimal org_id path to pass, got %d: %s", recorder.Code, recorder.Body.String())
	}

	var body struct {
		OrgID string `json:"org_id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.OrgID != "367889413595774308" {
		t.Fatalf("expected org_id to round-trip, got %q", body.OrgID)
	}
}

