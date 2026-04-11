package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLimitPublicAPIRequestBodiesRejectsKnownOversizeBody(t *testing.T) {
	handler := limitPublicAPIRequestBodies(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not receive an oversized API request")
	}), 8)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/repos", strings.NewReader("123456789"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestLimitPublicAPIRequestBodiesWrapsChunkedOversizeBody(t *testing.T) {
	handler := limitPublicAPIRequestBodies(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err == nil {
			t.Fatal("expected wrapped body reader to fail once the API limit is exceeded")
		}
		w.WriteHeader(http.StatusRequestEntityTooLarge)
	}), 8)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/repos", strings.NewReader("123456789"))
	req.ContentLength = -1
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestLimitPublicAPIRequestBodiesLeavesNonAPIRequestsAlone(t *testing.T) {
	handler := limitPublicAPIRequestBodies(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if got := string(body); got != "123456789" {
			t.Fatalf("body = %q", got)
		}
		w.WriteHeader(http.StatusNoContent)
	}), 8)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/forgejo", strings.NewReader("123456789"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}
