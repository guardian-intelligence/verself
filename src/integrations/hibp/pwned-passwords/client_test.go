package pwnedpasswords

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCheckPasswordUsesRangeProtocol(t *testing.T) {
	var gotPath, gotPadding, gotAgent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotPadding = r.Header.Get("Add-Padding")
		gotAgent = r.Header.Get("User-Agent")
		if strings.Contains(r.URL.String(), "password") {
			t.Fatal("password leaked into HIBP request URL")
		}
		_, _ = w.Write([]byte("1E4C9B93F3F0682250B6CF8331B7EE68FD8:42\n00000000000000000000000000000000000:0\n"))
	}))
	defer server.Close()
	client, err := New(Config{
		RangeEndpoint: server.URL,
		HTTPClient:    server.Client(),
		UserAgent:     "verself-test",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	result, err := client.CheckPassword(context.Background(), "password")
	if err != nil {
		t.Fatalf("CheckPassword: %v", err)
	}
	if !result.Breached || result.Occurrences != 42 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if gotPath != "/range/5BAA6" || gotPadding != "true" || gotAgent != "verself-test" {
		t.Fatalf("unexpected request path/header: path=%q padding=%q agent=%q", gotPath, gotPadding, gotAgent)
	}
}

func TestCheckPasswordNotBreached(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("00000000000000000000000000000000000:0\n"))
	}))
	defer server.Close()
	client, err := New(Config{
		RangeEndpoint: server.URL,
		HTTPClient:    server.Client(),
		UserAgent:     "verself-test",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	result, err := client.CheckPassword(context.Background(), "not-this-one")
	if err != nil {
		t.Fatalf("CheckPassword: %v", err)
	}
	if result.Breached || result.Occurrences != 0 {
		t.Fatalf("unexpected result: %#v", result)
	}
}
