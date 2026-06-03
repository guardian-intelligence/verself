package secrets

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestNewOpaqueCredentialTokenUsesOpenBaoTransitRandom(t *testing.T) {
	credentialID := "11111111-1111-4111-8111-111111111111"
	random := []byte("0123456789abcdef0123456789abcdef")
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		if r.URL.Path != "/v1/transit-org_1/random/32" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("X-Vault-Token"); got != "bao-token" {
			t.Fatalf("X-Vault-Token = %q", got)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["format"] != "base64" {
			t.Fatalf("format = %q", body["format"])
		}
		if err := json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]string{
				"random_bytes": base64.StdEncoding.EncodeToString(random),
			},
		}); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	addr, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	store := &BaoStore{
		addr:          addr,
		httpClient:    server.Client(),
		transitPrefix: "transit",
	}

	token, prefix, err := store.newOpaqueCredentialToken(context.Background(), baoTokenEntry{token: "bao-token"}, "org_1", credentialID)
	if err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want one OpenBao transit/random call", requests)
	}
	if prefix != "fmoc_11111111" {
		t.Fatalf("prefix = %q", prefix)
	}
	wantSuffix := base64.RawURLEncoding.EncodeToString(random)
	if !strings.HasPrefix(token, "fmoc_"+credentialID+"_") || !strings.HasSuffix(token, wantSuffix) {
		t.Fatalf("token = %q, want transit random suffix %q", token, wantSuffix)
	}
}
