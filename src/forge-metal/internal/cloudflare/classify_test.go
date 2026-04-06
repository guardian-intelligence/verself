package cloudflare

import (
	"strings"
	"testing"
)

const testDomain = "anveio.com"

// Fixture 1: Valid zone-scoped DNS token — happy path
var fixtureValidDNS = []byte(`{
  "success": true,
  "errors": [],
  "result": [{
    "id": "abc123",
    "name": "anveio.com",
    "status": "active",
    "permissions": ["#dns_records:edit", "#dns_records:read", "#zone:read"]
  }],
  "result_info": {"count": 1, "total_count": 1}
}`)

// Fixture 2: Valid token but only read permissions
var fixtureReadOnly = []byte(`{
  "success": true,
  "errors": [],
  "result": [{
    "id": "abc123",
    "name": "anveio.com",
    "status": "active",
    "permissions": ["#dns_records:read", "#zone:read"]
  }],
  "result_info": {"count": 1, "total_count": 1}
}`)

// Fixture 3: Valid token scoped to wrong zone (empty result)
var fixtureWrongZone = []byte(`{
  "success": true,
  "errors": [],
  "result": [],
  "result_info": {"count": 0, "total_count": 0}
}`)

// Fixture 4: Completely invalid / expired token
var fixtureInvalid = []byte(`{
  "success": false,
  "errors": [{"code": 1000, "message": "Invalid API Token"}],
  "result": null
}`)

// Fixture 5: Valid token with zone access but no DNS permissions (Workers token)
var fixtureNoDNS = []byte(`{
  "success": true,
  "errors": [],
  "result": [{
    "id": "abc123",
    "name": "anveio.com",
    "status": "active",
    "permissions": ["#zone:read"]
  }],
  "result_info": {"count": 1, "total_count": 1}
}`)

// Fixture 6: Malformed / empty response
var fixtureEmpty = []byte(`{}`)

func TestClassifyToken_ValidDNS(t *testing.T) {
	tc := ClassifyToken(fixtureValidDNS, testDomain)
	if tc.Status != TokenValid {
		t.Fatalf("expected TokenValid, got %d", tc.Status)
	}
	if !strings.Contains(tc.Message, "Valid DNS token") {
		t.Fatalf("expected message to contain 'Valid DNS token', got: %s", tc.Message)
	}
}

func TestClassifyToken_ReadOnly(t *testing.T) {
	tc := ClassifyToken(fixtureReadOnly, testDomain)
	if tc.Status != TokenWrongPerms {
		t.Fatalf("expected TokenWrongPerms, got %d", tc.Status)
	}
	if !strings.Contains(tc.Message, "lacks DNS edit permission") {
		t.Fatalf("expected 'lacks DNS edit permission', got: %s", tc.Message)
	}
	if !strings.Contains(tc.Message, "Edit zone DNS") {
		t.Fatalf("expected 'Edit zone DNS' template instruction, got: %s", tc.Message)
	}
	if !strings.Contains(tc.Message, "#dns_records:read") {
		t.Fatalf("expected current permissions listed, got: %s", tc.Message)
	}
}

func TestClassifyToken_WrongZone(t *testing.T) {
	tc := ClassifyToken(fixtureWrongZone, testDomain)
	if tc.Status != TokenWrongZone {
		t.Fatalf("expected TokenWrongZone, got %d", tc.Status)
	}
	if !strings.Contains(tc.Message, "does not have access to zone") {
		t.Fatalf("expected 'does not have access to zone', got: %s", tc.Message)
	}
	if !strings.Contains(tc.Message, testDomain) {
		t.Fatalf("expected domain in message, got: %s", tc.Message)
	}
}

func TestClassifyToken_Invalid(t *testing.T) {
	tc := ClassifyToken(fixtureInvalid, testDomain)
	if tc.Status != TokenInvalid {
		t.Fatalf("expected TokenInvalid, got %d", tc.Status)
	}
	if !strings.Contains(tc.Message, "Invalid API token") {
		t.Fatalf("expected 'Invalid API token', got: %s", tc.Message)
	}
}

func TestClassifyToken_NoDNSPerms(t *testing.T) {
	tc := ClassifyToken(fixtureNoDNS, testDomain)
	if tc.Status != TokenWrongPerms {
		t.Fatalf("expected TokenWrongPerms, got %d", tc.Status)
	}
	if !strings.Contains(tc.Message, "lacks DNS edit permission") {
		t.Fatalf("expected 'lacks DNS edit permission', got: %s", tc.Message)
	}
	if !strings.Contains(tc.Message, "#zone:read") {
		t.Fatalf("expected current permissions listed, got: %s", tc.Message)
	}
}

func TestClassifyToken_Malformed(t *testing.T) {
	tc := ClassifyToken(fixtureEmpty, testDomain)
	if tc.Status != TokenInvalid {
		t.Fatalf("expected TokenInvalid, got %d", tc.Status)
	}
}
