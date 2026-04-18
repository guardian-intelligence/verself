package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/forge-metal/identity-service/internal/identity"
)

const (
	zitadelActionSigningHeader       = "X-ZITADEL-Signature"
	zitadelActionLegacySigningHeader = "ZITADEL-Signature"
	zitadelActionMaxBodyBytes        = 64 << 10
	zitadelActionTolerance           = 5 * time.Minute
)

type zitadelActionClaim struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

type zitadelActionResponse struct {
	AppendClaims []zitadelActionClaim `json:"append_claims,omitempty"`
}

func RegisterZitadelActionRoutes(mux *http.ServeMux, svc *identity.Service, signingKey string) {
	signingKey = strings.TrimSpace(signingKey)
	mux.Handle("/internal/zitadel/actions/api-credential-claims", zitadelActionHandler(svc, signingKey))
}

func zitadelActionHandler(svc *identity.Service, signingKey string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		payload, err := io.ReadAll(io.LimitReader(r.Body, zitadelActionMaxBodyBytes+1))
		if err != nil {
			http.Error(w, "read request", http.StatusBadRequest)
			return
		}
		if len(payload) > zitadelActionMaxBodyBytes {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		signatureHeader := firstNonEmptyHeader(r.Header, zitadelActionSigningHeader, zitadelActionLegacySigningHeader)
		if err := validateZitadelActionSignature(payload, signatureHeader, signingKey, time.Now()); err != nil {
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
		var raw map[string]any
		if err := json.Unmarshal(payload, &raw); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		subjectID := actionSubjectID(raw)
		if subjectID == "" {
			slog.Default().WarnContext(r.Context(), "zitadel action api credential subject missing")
			writeActionResponse(w, zitadelActionResponse{})
			return
		}
		claims, err := svc.ResolveAPICredentialClaims(r.Context(), subjectID)
		if err != nil {
			slog.Default().WarnContext(r.Context(), "zitadel action api credential claims denied", "subject_id", subjectID, "error", err)
			writeActionResponse(w, zitadelActionResponse{})
			return
		}
		writeActionResponse(w, zitadelActionResponse{AppendClaims: []zitadelActionClaim{
			{Key: "forge_metal:credential_id", Value: claims.CredentialID},
			{Key: "forge_metal:credential_name", Value: claims.DisplayName},
			{Key: "forge_metal:credential_fingerprint", Value: claims.Fingerprint},
			{Key: "forge_metal:credential_owner_id", Value: claims.OwnerID},
			{Key: "forge_metal:credential_owner_display", Value: claims.OwnerDisplay},
			{Key: "forge_metal:credential_auth_method", Value: string(claims.AuthMethod)},
			{Key: "org_id", Value: claims.OrgID},
			{Key: "permissions", Value: claims.Permissions},
		}})
	})
}

func actionSubjectID(raw map[string]any) string {
	for _, path := range [][]string{{"user", "id"}, {"userID"}, {"subject"}, {"request", "subject"}, {"request", "userId"}} {
		if value := nestedString(raw, path...); value != "" {
			return value
		}
	}
	return ""
}

func nestedString(value any, path ...string) string {
	current := value
	for _, key := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = object[key]
	}
	text, _ := current.(string)
	return strings.TrimSpace(text)
}

func firstNonEmptyHeader(header http.Header, names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(header.Get(name)); value != "" {
			return value
		}
	}
	return ""
}

func validateZitadelActionSignature(payload []byte, header, signingKey string, now time.Time) error {
	signingKey = strings.TrimSpace(signingKey)
	if signingKey == "" {
		return errors.New("zitadel action signing key is empty")
	}
	timestamp, signatures, err := parseZitadelActionSignatureHeader(header)
	if err != nil {
		return err
	}
	if now.Sub(timestamp) > zitadelActionTolerance || timestamp.Sub(now) > zitadelActionTolerance {
		return errors.New("zitadel action signature timestamp outside tolerance")
	}
	expected := computeZitadelActionSignature(timestamp, payload, signingKey)
	for _, signature := range signatures {
		if hmac.Equal(expected, signature) {
			return nil
		}
	}
	return errors.New("zitadel action signature mismatch")
}

func parseZitadelActionSignatureHeader(header string) (time.Time, [][]byte, error) {
	if strings.TrimSpace(header) == "" {
		return time.Time{}, nil, errors.New("zitadel action signature missing")
	}
	var timestamp time.Time
	signatures := [][]byte{}
	for _, pair := range strings.Split(header, ",") {
		parts := strings.SplitN(strings.TrimSpace(pair), "=", 2)
		if len(parts) != 2 {
			return time.Time{}, nil, errors.New("zitadel action signature malformed")
		}
		switch parts[0] {
		case "t":
			unix, err := strconv.ParseInt(parts[1], 10, 64)
			if err != nil {
				return time.Time{}, nil, fmt.Errorf("zitadel action signature timestamp: %w", err)
			}
			timestamp = time.Unix(unix, 0)
		case "v1":
			signature, err := hex.DecodeString(parts[1])
			if err == nil {
				signatures = append(signatures, signature)
			}
		}
	}
	if timestamp.IsZero() || len(signatures) == 0 {
		return time.Time{}, nil, errors.New("zitadel action signature incomplete")
	}
	return timestamp, signatures, nil
}

func computeZitadelActionSignature(timestamp time.Time, payload []byte, signingKey string) []byte {
	mac := hmac.New(sha256.New, []byte(signingKey))
	_, _ = fmt.Fprintf(mac, "%d", timestamp.Unix())
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write(payload)
	return mac.Sum(nil)
}

func writeActionResponse(w http.ResponseWriter, response zitadelActionResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(response)
}
