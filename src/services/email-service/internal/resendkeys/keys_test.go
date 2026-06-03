package resendkeys

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestApplySkipsWhenChildKeysExist(t *testing.T) {
	store := memoryStore{
		DefaultAdminSecret:             "re_admin",
		"email-service.resend.api_key": "re_child",
		"zitadel.smtp.password":        "re_child",
	}
	api := &recordingResendAPI{}
	result, err := Apply(context.Background(), Config{Site: "gamma"}, store, api)
	if err != nil {
		t.Fatal(err)
	}
	if result.Created {
		t.Fatalf("result = %#v, want no created key", result)
	}
	if api.calls != 0 {
		t.Fatalf("Resend create calls = %d, want 0", api.calls)
	}
}

func TestApplyCreatesSendingKeyAndWritesRuntimeSecrets(t *testing.T) {
	store := memoryStore{DefaultAdminSecret: "re_admin"}
	api := &recordingResendAPI{
		response: CreateAPIKeyResponse{ID: "key_gamma_1", Token: "re_gamma_child"},
	}
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	result, err := Apply(context.Background(), Config{
		Site: "gamma",
		Now:  func() time.Time { return now },
	}, store, api)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Created || result.KeyID != "key_gamma_1" {
		t.Fatalf("result = %#v", result)
	}
	if api.req.Name != "verself-gamma-email-service-sending-20260601" {
		t.Fatalf("key name = %q", api.req.Name)
	}
	if api.req.Permission != "sending_access" {
		t.Fatalf("request = %#v", api.req)
	}
	for _, secret := range DefaultChildSecrets {
		if got := store[secret]; got != "re_gamma_child" {
			t.Fatalf("%s = %q", secret, got)
		}
	}
	var metadata Metadata
	if err := json.Unmarshal([]byte(store[DefaultMetadataSecret]), &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.Site != "gamma" || metadata.KeyID != "key_gamma_1" || metadata.TokenSHA256 == "" {
		t.Fatalf("metadata = %#v", metadata)
	}
	if strings.Contains(store[DefaultMetadataSecret], "re_gamma_child") || strings.Contains(store[DefaultMetadataSecret], "re_admin") {
		t.Fatalf("metadata leaked raw key material: %s", store[DefaultMetadataSecret])
	}
}

func TestApplyRotatesAndDeletesPriorKey(t *testing.T) {
	prior := Metadata{
		Version:      "verself.email.resend.key.v1",
		Site:         "gamma",
		KeyID:        "key_old",
		KeyName:      "verself-gamma-email-service-sending-20260531",
		Permission:   "sending_access",
		TokenSHA256:  strings.Repeat("a", 64),
		ChildSecrets: append([]string(nil), DefaultChildSecrets...),
		CreatedAt:    "2026-05-31T12:00:00Z",
	}
	priorBody, err := json.Marshal(prior)
	if err != nil {
		t.Fatal(err)
	}
	store := memoryStore{
		DefaultMetadataSecret: string(priorBody),
	}
	api := &recordingResendAPI{
		response: CreateAPIKeyResponse{ID: "key_new", Token: "re_gamma_new_child"},
	}
	result, err := Apply(context.Background(), Config{
		Site:   "gamma",
		Rotate: true,
		Now:    func() time.Time { return time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC) },
	}, store, api)
	if err != nil {
		t.Fatal(err)
	}
	if result.RevokedKeyID != "key_old" {
		t.Fatalf("revoked key = %q", result.RevokedKeyID)
	}
	if len(api.deleted) != 1 || api.deleted[0] != "key_old" {
		t.Fatalf("deleted keys = %#v", api.deleted)
	}
	var metadata Metadata
	if err := json.Unmarshal([]byte(store[DefaultMetadataSecret]), &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.KeyID != "key_new" || metadata.PreviousKeyID != "key_old" || metadata.RevokedKeyID != "key_old" || metadata.PendingRevocationKeyID != "" {
		t.Fatalf("metadata = %#v", metadata)
	}
}

func TestApplyCompletesPendingRevocationWithoutCreatingAnotherKey(t *testing.T) {
	pending := Metadata{
		Version:                "verself.email.resend.key.v1",
		Site:                   "gamma",
		KeyID:                  "key_new",
		KeyName:                "verself-gamma-email-service-sending-20260601",
		Permission:             "sending_access",
		TokenSHA256:            strings.Repeat("a", 64),
		ChildSecrets:           append([]string(nil), DefaultChildSecrets...),
		CreatedAt:              "2026-06-01T12:00:00Z",
		PreviousKeyID:          "key_old",
		PendingRevocationKeyID: "key_old",
	}
	pendingBody, err := json.Marshal(pending)
	if err != nil {
		t.Fatal(err)
	}
	store := memoryStore{
		DefaultMetadataSecret:          string(pendingBody),
		"email-service.resend.api_key": "re_child",
		"zitadel.smtp.password":        "re_child",
	}
	api := &recordingResendAPI{}
	result, err := Apply(context.Background(), Config{Site: "gamma"}, store, api)
	if err != nil {
		t.Fatal(err)
	}
	if result.Created {
		t.Fatalf("result = %#v, want no new key", result)
	}
	if api.calls != 0 {
		t.Fatalf("Resend create calls = %d, want 0", api.calls)
	}
	if len(api.deleted) != 1 || api.deleted[0] != "key_old" {
		t.Fatalf("deleted keys = %#v", api.deleted)
	}
	var metadata Metadata
	if err := json.Unmarshal([]byte(store[DefaultMetadataSecret]), &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.PendingRevocationKeyID != "" || metadata.RevokedKeyID != "key_old" {
		t.Fatalf("metadata = %#v", metadata)
	}
}

func TestHTTPResendAPISendsDocumentedCreateRequest(t *testing.T) {
	var gotAuth string
	var gotUserAgent string
	var gotBody map[string]string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotUserAgent = r.Header.Get("User-Agent")
		if r.URL.Path != "/api-keys" || r.Method != http.MethodPost {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"id":"key_1","token":"re_child"}`))
	}))
	t.Cleanup(server.Close)

	api, err := NewHTTPResendAPI(server.URL, "re_admin", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	resp, err := api.CreateAPIKey(context.Background(), CreateAPIKeyRequest{
		Name:       "verself-gamma-email-service-sending-20260601",
		Permission: "sending_access",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.ID != "key_1" || resp.Token != "re_child" {
		t.Fatalf("response = %#v", resp)
	}
	if gotAuth != "Bearer re_admin" || gotUserAgent == "" {
		t.Fatalf("headers auth=%q user-agent=%q", gotAuth, gotUserAgent)
	}
	if gotBody["name"] != "verself-gamma-email-service-sending-20260601" || gotBody["permission"] != "sending_access" || len(gotBody) != 2 {
		t.Fatalf("body = %#v", gotBody)
	}
}

func TestHTTPResendAPIDeletesKey(t *testing.T) {
	var gotAuth string
	var gotPath string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		if r.Method != http.MethodDelete {
			t.Fatalf("unexpected method %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	api, err := NewHTTPResendAPI(server.URL, "re_admin", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if err := api.DeleteAPIKey(context.Background(), "key_old"); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer re_admin" || gotPath != "/api-keys/key_old" {
		t.Fatalf("request auth=%q path=%q", gotAuth, gotPath)
	}
}

type memoryStore map[string]string

func (s memoryStore) Read(_ context.Context, name string) (string, bool, error) {
	value, ok := s[name]
	return value, ok, nil
}

func (s memoryStore) Write(_ context.Context, name string, value string) error {
	s[name] = value
	return nil
}

type recordingResendAPI struct {
	calls    int
	req      CreateAPIKeyRequest
	response CreateAPIKeyResponse
	deleted  []string
}

func (a *recordingResendAPI) CreateAPIKey(_ context.Context, req CreateAPIKeyRequest) (CreateAPIKeyResponse, error) {
	a.calls++
	a.req = req
	if a.response.ID == "" {
		a.response = CreateAPIKeyResponse{ID: "key_default", Token: "re_child_default"}
	}
	return a.response, nil
}

func (a *recordingResendAPI) DeleteAPIKey(_ context.Context, id string) error {
	a.deleted = append(a.deleted, id)
	return nil
}
