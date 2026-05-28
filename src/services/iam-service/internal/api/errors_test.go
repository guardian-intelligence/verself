package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
)

func TestAuthProblemTypeUsesDurableURN(t *testing.T) {
	got := problemType("auth.session_revoked")
	want := "urn:verself:problem:auth:session_revoked"
	if got != want {
		t.Fatalf("problemType() = %q, want %q", got, want)
	}
}

func TestPublicProblemTerminalizesTaggedErrorContract(t *testing.T) {
	err := badRequest(context.Background(), "iam.password.breached", "password appears in known breach data", errors.New("provider detail"))
	payload, marshalErr := json.Marshal(err)
	if marshalErr != nil {
		t.Fatalf("marshal problem: %v", marshalErr)
	}
	var got struct {
		Tag       string `json:"_tag"`
		Type      string `json:"type"`
		Code      string `json:"code"`
		Status    int    `json:"status"`
		Message   string `json:"message"`
		Retryable bool   `json:"retryable"`
	}
	if unmarshalErr := json.Unmarshal(payload, &got); unmarshalErr != nil {
		t.Fatalf("unmarshal problem: %v", unmarshalErr)
	}
	if got.Tag != "urn:verself:problem:iam:password_breached" || got.Type != got.Tag {
		t.Fatalf("problem tag/type = %q/%q", got.Tag, got.Type)
	}
	if got.Code != "iam.password.breached" || got.Status != http.StatusBadRequest {
		t.Fatalf("problem code/status = %q/%d", got.Code, got.Status)
	}
	if got.Message != "This password has appeared in a public data breach. Please use a different one." {
		t.Fatalf("problem message = %q", got.Message)
	}
	if got.Retryable {
		t.Fatalf("password breached retryable = true")
	}
}

func TestPublicProblemTerminalizesRetryableAvailability(t *testing.T) {
	err := internalFailure(context.Background(), "service.unavailable", "IAM operation failed", errors.New("store down"))
	payload, marshalErr := json.Marshal(err)
	if marshalErr != nil {
		t.Fatalf("marshal problem: %v", marshalErr)
	}
	var got struct {
		Tag       string `json:"_tag"`
		Code      string `json:"code"`
		Status    int    `json:"status"`
		Retryable bool   `json:"retryable"`
	}
	if unmarshalErr := json.Unmarshal(payload, &got); unmarshalErr != nil {
		t.Fatalf("unmarshal problem: %v", unmarshalErr)
	}
	if got.Tag != "urn:verself:problem:service:unavailable" {
		t.Fatalf("problem tag = %q", got.Tag)
	}
	if got.Code != "service.unavailable" || got.Status != http.StatusInternalServerError {
		t.Fatalf("problem code/status = %q/%d", got.Code, got.Status)
	}
	if !got.Retryable {
		t.Fatalf("service unavailable retryable = false")
	}
}
