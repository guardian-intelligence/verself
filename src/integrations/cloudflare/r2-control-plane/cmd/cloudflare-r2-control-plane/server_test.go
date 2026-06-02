package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLoadServerAuthTokenPrefersEnv(t *testing.T) {
	t.Setenv("TEST_R2_CONTROL_PLANE_TOKEN", "from-env")

	token, err := loadServerAuthToken(config{authTokenEnv: "TEST_R2_CONTROL_PLANE_TOKEN"})
	if err != nil {
		t.Fatal(err)
	}
	if token != "from-env" {
		t.Fatalf("token = %q, want env value", token)
	}
}

func TestLoadServerAuthTokenRejectsMissingToken(t *testing.T) {
	_, err := loadServerAuthToken(config{authTokenEnv: "TEST_R2_CONTROL_PLANE_TOKEN"})
	if err == nil || !strings.Contains(err.Error(), "auth token is required") {
		t.Fatalf("error = %v, want missing token refusal", err)
	}
}

func TestUploadServerRequiresBearerToken(t *testing.T) {
	server := uploadServer{authToken: "expected"}
	if server.authorized(httptest.NewRequest("POST", "/", nil)) {
		t.Fatal("request without bearer token was authorized")
	}
	rawTokenReq := httptest.NewRequest("POST", "/", nil)
	rawTokenReq.Header.Set("Authorization", "expected")
	if server.authorized(rawTokenReq) {
		t.Fatal("request without bearer scheme was authorized")
	}
	req := httptest.NewRequest("POST", "/", nil)
	req.Header.Set("Authorization", "Bearer expected")
	if !server.authorized(req) {
		t.Fatal("request with bearer token was not authorized")
	}
}
