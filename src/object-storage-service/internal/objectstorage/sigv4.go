package objectstorage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsv4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
)

const (
	awsEmptyPayloadSHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	awsUnsignedPayload    = "UNSIGNED-PAYLOAD"
)

var (
	ErrSigV4Unsupported          = errors.New("object-storage sigv4 unsupported")
	ErrSigV4InvalidAuthorization = errors.New("object-storage sigv4 invalid authorization")
	ErrSigV4SignatureMismatch    = errors.New("object-storage sigv4 signature mismatch")
	ErrSigV4TimeSkew             = errors.New("object-storage sigv4 request time skew")
)

const garageBodyMemoryThreshold = 1 << 20

var sigv4HexSHA256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type SigV4Auth struct {
	Algorithm     string
	AccessKeyID   string
	ScopeDate     string
	Region        string
	Service       string
	Terminal      string
	SignedHeaders []string
	Signature     string
	SigningTime   time.Time
	PayloadHash   string
}

func VerifySigV4Request(ctx context.Context, req *http.Request, secretAccessKey string, now time.Time) (SigV4Auth, error) {
	if req == nil {
		return SigV4Auth{}, fmt.Errorf("%w: request is required", ErrSigV4InvalidAuthorization)
	}
	if strings.TrimSpace(secretAccessKey) == "" {
		return SigV4Auth{}, fmt.Errorf("%w: secret access key is required", ErrSigV4InvalidAuthorization)
	}
	if hasSigV4QueryAuth(req.URL.Query()) {
		return SigV4Auth{}, ErrSigV4Unsupported
	}
	if strings.TrimSpace(req.Header.Get("X-Amz-Security-Token")) != "" {
		return SigV4Auth{}, ErrSigV4Unsupported
	}
	auth, err := parseSigV4Authorization(req.Header.Get("Authorization"))
	if err != nil {
		return SigV4Auth{}, err
	}
	if auth.Algorithm != "AWS4-HMAC-SHA256" || auth.Service != "s3" || auth.Terminal != "aws4_request" {
		return SigV4Auth{}, ErrSigV4Unsupported
	}
	// We must reload request metadata here; reparsing Authorization alone leaves SigningTime zeroed.
	if err := populateSigV4RequestMetadata(req, &auth); err != nil {
		return SigV4Auth{}, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if delta := now.Sub(auth.SigningTime); delta > 15*time.Minute || delta < -15*time.Minute {
		return SigV4Auth{}, ErrSigV4TimeSkew
	}
	if strings.HasPrefix(auth.PayloadHash, "STREAMING-AWS4-HMAC-SHA256") || strings.HasPrefix(auth.PayloadHash, "STREAMING-UNSIGNED-PAYLOAD") {
		return SigV4Auth{}, ErrSigV4Unsupported
	}

	signedReq := requestForSigV4Verification(req, auth.SignedHeaders)
	signer := awsv4.NewSigner()
	creds := aws.Credentials{
		AccessKeyID:     auth.AccessKeyID,
		SecretAccessKey: secretAccessKey,
		Source:          "object-storage-service",
	}
	if err := signer.SignHTTP(ctx, creds, signedReq, auth.PayloadHash, auth.Service, auth.Region, auth.SigningTime, func(options *awsv4.SignerOptions) {
		options.DisableURIPathEscaping = true
	}); err != nil {
		return SigV4Auth{}, fmt.Errorf("sign verification request: %w", err)
	}
	expected, err := parseSigV4Authorization(signedReq.Header.Get("Authorization"))
	if err != nil {
		return SigV4Auth{}, err
	}
	if subtle.ConstantTimeCompare([]byte(expected.Signature), []byte(auth.Signature)) != 1 {
		return SigV4Auth{}, ErrSigV4SignatureMismatch
	}
	return auth, nil
}

func SignGarageRequest(ctx context.Context, signer *awsv4.Signer, creds aws.Credentials, req *http.Request, region string) (func(), error) {
	if signer == nil {
		return func() {}, fmt.Errorf("aws signer is required")
	}
	if req == nil {
		return func() {}, fmt.Errorf("request is required")
	}
	payloadHash, cleanup, err := prepareGarageRequestBody(req)
	if err != nil {
		return func() {}, err
	}
	req.Header.Del("Authorization")
	req.Header.Del("X-Amz-Security-Token")
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	if err := signer.SignHTTP(ctx, creds, req, payloadHash, "s3", strings.TrimSpace(region), time.Now().UTC(), func(options *awsv4.SignerOptions) {
		options.DisableURIPathEscaping = true
	}); err != nil {
		cleanup()
		return func() {}, err
	}
	return cleanup, nil
}

func prepareGarageRequestBody(req *http.Request) (string, func(), error) {
	if req == nil {
		return "", func() {}, fmt.Errorf("request is required")
	}
	if req.Body == nil || req.Body == http.NoBody || req.ContentLength == 0 {
		req.Body = http.NoBody
		req.ContentLength = 0
		return awsEmptyPayloadSHA256, func() {}, nil
	}

	if payloadHash := strings.TrimSpace(req.Header.Get("X-Amz-Content-Sha256")); sigv4HexSHA256Pattern.MatchString(payloadHash) {
		return payloadHash, func() {}, nil
	}

	// Garage sits behind an HTTP loopback endpoint, so SigV4 must use a real payload hash.
	return spoolGarageRequestBody(req)
}

func spoolGarageRequestBody(req *http.Request) (string, func(), error) {
	hasher := sha256.New()
	originalBody := req.Body
	defer originalBody.Close()

	if req.ContentLength >= 0 && req.ContentLength <= garageBodyMemoryThreshold {
		var buf bytes.Buffer
		written, err := io.Copy(io.MultiWriter(&buf, hasher), req.Body)
		if err != nil {
			return "", func() {}, fmt.Errorf("buffer garage request body: %w", err)
		}
		req.ContentLength = written
		req.Body = io.NopCloser(bytes.NewReader(buf.Bytes()))
		return hex.EncodeToString(hasher.Sum(nil)), func() {}, nil
	}

	file, err := os.CreateTemp("", "object-storage-garage-body-*")
	if err != nil {
		return "", func() {}, fmt.Errorf("create garage request spool: %w", err)
	}
	cleanup := func() {
		_ = file.Close()
		_ = os.Remove(file.Name())
	}
	written, err := io.Copy(io.MultiWriter(file, hasher), req.Body)
	if err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("spool garage request body: %w", err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("rewind garage request spool: %w", err)
	}
	req.ContentLength = written
	req.Body = file
	return hex.EncodeToString(hasher.Sum(nil)), cleanup, nil
}

func parseSigV4Authorization(raw string) (SigV4Auth, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return SigV4Auth{}, fmt.Errorf("%w: authorization header is required", ErrSigV4InvalidAuthorization)
	}
	parts := strings.SplitN(raw, " ", 2)
	if len(parts) != 2 {
		return SigV4Auth{}, ErrSigV4InvalidAuthorization
	}
	auth := SigV4Auth{Algorithm: strings.TrimSpace(parts[0])}
	for _, field := range strings.Split(parts[1], ",") {
		field = strings.TrimSpace(field)
		name, value, ok := strings.Cut(field, "=")
		if !ok {
			return SigV4Auth{}, ErrSigV4InvalidAuthorization
		}
		switch name {
		case "Credential":
			scope := strings.Split(value, "/")
			if len(scope) != 5 {
				return SigV4Auth{}, ErrSigV4InvalidAuthorization
			}
			auth.AccessKeyID = scope[0]
			auth.ScopeDate = scope[1]
			auth.Region = scope[2]
			auth.Service = scope[3]
			auth.Terminal = scope[4]
		case "SignedHeaders":
			auth.SignedHeaders = strings.Split(strings.ToLower(value), ";")
		case "Signature":
			auth.Signature = strings.ToLower(value)
		}
	}
	if auth.AccessKeyID == "" || auth.Region == "" || auth.Service == "" || auth.Signature == "" || len(auth.SignedHeaders) == 0 {
		return SigV4Auth{}, ErrSigV4InvalidAuthorization
	}
	return auth, nil
}

func requestForSigV4Verification(req *http.Request, signedHeaders []string) *http.Request {
	clonedURL := *req.URL
	if clonedURL.Host == "" {
		clonedURL.Host = req.Host
	}
	if clonedURL.Scheme == "" {
		clonedURL.Scheme = "https"
	}
	clone := &http.Request{
		Method:        req.Method,
		URL:           &clonedURL,
		Header:        make(http.Header),
		Host:          req.Host,
		ContentLength: req.ContentLength,
	}
	for _, header := range signedHeaders {
		if header == "" || header == "authorization" || header == "host" {
			continue
		}
		for _, value := range req.Header.Values(header) {
			clone.Header.Add(header, value)
		}
	}
	clone.Header.Del("Authorization")
	payloadHash := strings.TrimSpace(req.Header.Get("X-Amz-Content-Sha256"))
	if payloadHash == "" {
		if req.ContentLength > 0 {
			payloadHash = awsUnsignedPayload
		} else {
			payloadHash = awsEmptyPayloadSHA256
		}
	}
	clone.Header.Set("X-Amz-Content-Sha256", payloadHash)
	signingTime, _ := time.Parse("20060102T150405Z", strings.TrimSpace(req.Header.Get("X-Amz-Date")))
	clone.Header.Set("X-Amz-Date", signingTime.UTC().Format("20060102T150405Z"))
	return clone
}

func populateSigV4RequestMetadata(req *http.Request, auth *SigV4Auth) error {
	if req == nil || auth == nil {
		return fmt.Errorf("%w: request and auth are required", ErrSigV4InvalidAuthorization)
	}
	signingTime, err := time.Parse("20060102T150405Z", strings.TrimSpace(req.Header.Get("X-Amz-Date")))
	if err != nil {
		return fmt.Errorf("%w: invalid x-amz-date", ErrSigV4InvalidAuthorization)
	}
	auth.SigningTime = signingTime.UTC()
	payloadHash := strings.TrimSpace(req.Header.Get("X-Amz-Content-Sha256"))
	if payloadHash == "" {
		if req.ContentLength > 0 {
			return fmt.Errorf("%w: x-amz-content-sha256 is required for requests with a body", ErrSigV4InvalidAuthorization)
		}
		payloadHash = awsEmptyPayloadSHA256
	}
	auth.PayloadHash = payloadHash
	return nil
}

func hasSigV4QueryAuth(values url.Values) bool {
	for key := range values {
		switch strings.ToLower(key) {
		case "x-amz-algorithm", "x-amz-credential", "x-amz-signature", "x-amz-date":
			return true
		}
	}
	return false
}
