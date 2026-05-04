package objectstorage

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsv4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	workloadauth "github.com/verself/auth-middleware/workload"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"
)

type S3Handler struct {
	Service       *Service
	Logger        *slog.Logger
	GarageURLs    []*url.URL
	GarageHTTP    *http.Client
	Signer        *awsv4.Signer
	Credentials   aws.Credentials
	Region        string
	endpointIndex atomic.Uint32
}

type s3Operation struct {
	Name            string
	RequestedBucket string
	ObjectKey       string
	SupportsBody    bool
	NeedsXMLRewrite bool
}

func NewS3Handler(service *Service, garageURLs []string, garageHTTP *http.Client, proxyAccessKeyID, proxySecretAccessKey, region string, logger *slog.Logger) (*S3Handler, error) {
	if service == nil {
		return nil, fmt.Errorf("object-storage service is required")
	}
	parsed, err := parseGarageURLs(garageURLs, "s3")
	if err != nil {
		return nil, err
	}
	if garageHTTP == nil {
		garageHTTP = http.DefaultClient
	}
	return &S3Handler{
		Service:    service,
		Logger:     logger,
		GarageURLs: parsed,
		GarageHTTP: garageHTTP,
		Signer:     awsv4.NewSigner(),
		Credentials: aws.Credentials{
			AccessKeyID:     strings.TrimSpace(proxyAccessKeyID),
			SecretAccessKey: strings.TrimSpace(proxySecretAccessKey),
			Source:          "object-storage-service",
		},
		Region: firstNonEmpty(region, "garage"),
	}, nil
}

func (h *S3Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.Start(r.Context(), "object_storage.s3.request")
	defer span.End()

	startedAt := time.Now().UTC()
	operation, err := classifyS3Operation(r)
	if err != nil {
		writeS3Error(w, http.StatusNotImplemented, "NotImplemented", err.Error(), r.URL.EscapedPath())
		h.recordAccessEvent(ctx, ObjectAccessEvent{
			RecordedAt:         startedAt,
			RequestedBucket:    strings.Trim(strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")[0], " "),
			Operation:          "Unsupported",
			AuthMode:           "",
			Status:             http.StatusNotImplemented,
			ErrorClass:         "unsupported",
			ErrorMessage:       err.Error(),
			ClientIPHash:       hashRemoteAddr(r.RemoteAddr),
			UserAgentHash:      hashForAudit(r.UserAgent()),
			UpstreamRequestURI: r.URL.RequestURI(),
		})
		return
	}

	event := ObjectAccessEvent{
		RecordedAt:         startedAt,
		RequestedBucket:    operation.RequestedBucket,
		Operation:          operation.Name,
		Status:             http.StatusInternalServerError,
		ClientIPHash:       hashRemoteAddr(r.RemoteAddr),
		UserAgentHash:      hashForAudit(r.UserAgent()),
		UpstreamRequestURI: r.URL.RequestURI(),
	}

	bodyCounter := &countingReadCloser{ReadCloser: r.Body}
	r.Body = bodyCounter
	defer func() {
		event.BytesIn = uint64FromNonNegativeInt64(bodyCounter.N, "s3 request bytes in")
		event.LatencyMS = latencyMillis(time.Since(startedAt), "s3 request latency")
		h.recordAccessEvent(traceContext(ctx), event)
	}()

	authz, principal, _, err := h.authenticate(ctx, r)
	if err != nil {
		event.AuthMode = authz.AuthMode
		event.AccessKeyID = authz.AccessKeyID
		event.SPIFFEPeerID = authz.SPIFFEPeerID
		event.Status, event.ErrorClass, event.ErrorMessage = s3ErrorForAuthError(err)
		writeS3Error(w, int(event.Status), s3ErrorCodeForAuthError(err), event.ErrorMessage, r.URL.EscapedPath())
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return
	}
	event.AuthMode = authz.AuthMode
	event.AccessKeyID = authz.AccessKeyID
	event.SPIFFEPeerID = authz.SPIFFEPeerID

	targetBucket, alias, aliasUsed, err := h.Service.ResolveBucketTarget(ctx, operation.RequestedBucket)
	if err != nil {
		event.Status = http.StatusNotFound
		event.ErrorClass = classifyError(err)
		event.ErrorMessage = err.Error()
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "bucket not found", r.URL.EscapedPath())
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return
	}
	if principal.Bucket.BucketID != targetBucket.BucketID {
		err = ErrUnauthorized
		event.Status = http.StatusForbidden
		event.ErrorClass = classifyError(err)
		event.ErrorMessage = "bucket access denied"
		writeS3Error(w, http.StatusForbidden, "AccessDenied", "bucket access denied", r.URL.EscapedPath())
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return
	}

	event.OrgID = targetBucket.OrgID
	event.BucketID = targetBucket.BucketID.String()
	event.BucketName = targetBucket.BucketName
	if aliasUsed {
		event.ResolvedAlias = alias.Alias
		event.ResolvedPrefix = alias.Prefix
	}

	upstreamReq, cleanupUpstreamReq, err := h.buildGarageRequest(ctx, r, operation, targetBucket, alias.Prefix)
	if err != nil {
		event.Status = http.StatusInternalServerError
		event.ErrorClass = classifyError(err)
		event.ErrorMessage = err.Error()
		writeS3Error(w, http.StatusInternalServerError, "InternalError", "failed to proxy request", r.URL.EscapedPath())
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return
	}
	defer cleanupUpstreamReq()
	event.UpstreamRequestURI = upstreamReq.URL.RequestURI()
	resp, err := h.GarageHTTP.Do(upstreamReq)
	if err != nil {
		event.Status = http.StatusBadGateway
		event.ErrorClass = "upstream"
		event.ErrorMessage = err.Error()
		writeS3Error(w, http.StatusBadGateway, "InternalError", "garage request failed", r.URL.EscapedPath())
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return
	}
	defer func() { _ = resp.Body.Close() }()
	event.UpstreamStatus = uint16FromStatus(resp.StatusCode, "upstream status")

	copyHeaders(w.Header(), resp.Header)
	if operation.NeedsXMLRewrite && (aliasUsed || operation.RequestedBucket != targetBucket.BucketName) {
		rawBody, err := io.ReadAll(resp.Body)
		if err != nil {
			event.Status = http.StatusBadGateway
			event.ErrorClass = "upstream"
			event.ErrorMessage = err.Error()
			writeS3Error(w, http.StatusBadGateway, "InternalError", "garage response read failed", r.URL.EscapedPath())
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return
		}
		rewritten, err := rewriteS3XMLResponse(operation.Name, operation.RequestedBucket, alias.Prefix, r.Host, rawBody)
		if err != nil {
			event.Status = http.StatusBadGateway
			event.ErrorClass = "upstream"
			event.ErrorMessage = err.Error()
			writeS3Error(w, http.StatusBadGateway, "InternalError", "garage response rewrite failed", r.URL.EscapedPath())
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return
		}
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(rewritten)))
		w.WriteHeader(resp.StatusCode)
		if r.Method != http.MethodHead {
			_, _ = w.Write(rewritten)
			event.BytesOut = uint64(len(rewritten))
		}
		event.Status = uint16FromStatus(resp.StatusCode, "response status")
		span.SetAttributes(
			attribute.String("verself.org_id", event.OrgID),
			attribute.String("verself.bucket_id", event.BucketID),
			attribute.String("verself.object_storage.operation", operation.Name),
		)
		return
	}

	writer := &countingResponseWriter{ResponseWriter: w}
	writer.WriteHeader(resp.StatusCode)
	if r.Method != http.MethodHead {
		if _, err := io.Copy(writer, resp.Body); err != nil {
			event.ErrorClass = "upstream"
			event.ErrorMessage = err.Error()
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
	}
	event.Status = uint16FromStatus(resp.StatusCode, "response status")
	event.BytesOut = uint64FromNonNegativeInt64(writer.N, "s3 response bytes out")
	span.SetAttributes(
		attribute.String("verself.org_id", event.OrgID),
		attribute.String("verself.bucket_id", event.BucketID),
		attribute.String("verself.object_storage.operation", operation.Name),
	)
}

type authenticatedRequest struct {
	AuthMode     string
	AccessKeyID  string
	SPIFFEPeerID string
}

func (h *S3Handler) authenticate(ctx context.Context, r *http.Request) (authenticatedRequest, AccessPrincipal, string, error) {
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	peerID, hasPeer := workloadauth.PeerIDFromContext(ctx)
	if hasPeer && authHeader != "" {
		return authenticatedRequest{}, AccessPrincipal{}, "", ErrUnauthorized
	}
	if hasPeer {
		principal, err := h.Service.ResolvePrincipalBySPIFFE(ctx, peerID.String())
		if err != nil {
			return authenticatedRequest{AuthMode: AuthModeSPIFFEMTLS, SPIFFEPeerID: peerID.String()}, AccessPrincipal{}, "", err
		}
		return authenticatedRequest{AuthMode: AuthModeSPIFFEMTLS, SPIFFEPeerID: peerID.String()}, principal, "", nil
	}
	if authHeader == "" {
		return authenticatedRequest{}, AccessPrincipal{}, "", ErrUnauthorized
	}
	parsed, err := parseSigV4Authorization(authHeader)
	if err != nil {
		return authenticatedRequest{AuthMode: AuthModeSigV4Static}, AccessPrincipal{}, "", err
	}
	if err := populateSigV4RequestMetadata(r, &parsed); err != nil {
		return authenticatedRequest{AuthMode: AuthModeSigV4Static, AccessKeyID: parsed.AccessKeyID}, AccessPrincipal{}, "", err
	}
	principal, secretAccessKey, err := h.Service.ResolvePrincipalByAccessKey(ctx, parsed.AccessKeyID)
	if err != nil {
		return authenticatedRequest{AuthMode: AuthModeSigV4Static, AccessKeyID: parsed.AccessKeyID}, AccessPrincipal{}, "", err
	}
	if _, err := VerifySigV4Request(ctx, r, secretAccessKey, time.Now().UTC()); err != nil {
		return authenticatedRequest{AuthMode: AuthModeSigV4Static, AccessKeyID: parsed.AccessKeyID}, AccessPrincipal{}, "", err
	}
	return authenticatedRequest{AuthMode: AuthModeSigV4Static, AccessKeyID: parsed.AccessKeyID}, principal, secretAccessKey, nil
}

func (h *S3Handler) buildGarageRequest(ctx context.Context, incoming *http.Request, operation s3Operation, bucket Bucket, aliasPrefix string) (*http.Request, func(), error) {
	targetKey := operation.ObjectKey
	if aliasPrefix != "" && targetKey != "" {
		targetKey = aliasPrefix + targetKey
	} else if aliasPrefix != "" && operation.Name == "ListObjectsV2" {
		targetKey = ""
	}
	selectedURL, err := h.selectGarageURL(ctx)
	if err != nil {
		return nil, func() {}, err
	}
	garageURL := *selectedURL
	garageURL.Scheme = firstNonEmpty(garageURL.Scheme, "http")
	garageURL.Host = selectedURL.Host
	garageURL.Path = "/" + bucket.BucketName
	garageURL.RawPath = "/" + bucket.BucketName
	if targetKey != "" {
		garageURL.Path += "/" + targetKey
		garageURL.RawPath += "/" + escapeS3Key(targetKey)
	}
	garageURL.RawQuery = rewriteQueryForAlias(operation.Name, incoming.URL.Query(), aliasPrefix).Encode()
	body := incoming.Body
	if incoming.Body == nil {
		body = http.NoBody
	}
	req, err := http.NewRequestWithContext(ctx, incoming.Method, garageURL.String(), body)
	if err != nil {
		return nil, func() {}, err
	}
	req.URL.Path = garageURL.Path
	req.URL.RawPath = garageURL.RawPath
	req.Host = selectedURL.Host
	req.ContentLength = incoming.ContentLength
	copyUpstreamHeaders(req.Header, incoming.Header)
	cleanup, err := SignGarageRequest(ctx, h.Signer, h.Credentials, req, h.Region)
	if err != nil {
		return nil, func() {}, err
	}
	return req, cleanup, nil
}

func (h *S3Handler) selectGarageURL(ctx context.Context) (*url.URL, error) {
	if h == nil || len(h.GarageURLs) == 0 {
		return nil, fmt.Errorf("garage s3 endpoints are not configured")
	}
	start := int(h.endpointIndex.Add(1)-1) % len(h.GarageURLs)
	dialer := &net.Dialer{Timeout: 200 * time.Millisecond}
	var lastErr error
	for offset := 0; offset < len(h.GarageURLs); offset++ {
		index := (start + offset) % len(h.GarageURLs)
		endpoint := h.GarageURLs[index]
		conn, err := dialer.DialContext(ctx, "tcp", endpoint.Host)
		if err != nil {
			lastErr = err
			continue
		}
		_ = conn.Close()
		h.endpointIndex.Store(uint32FromIndex((index+1)%len(h.GarageURLs), "garage endpoint index"))
		selected := *endpoint
		return &selected, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("garage s3 endpoints are not configured")
	}
	return nil, fmt.Errorf("select garage s3 endpoint: %w", lastErr)
}

func classifyS3Operation(r *http.Request) (s3Operation, error) {
	bucket, key, err := splitS3Path(r.URL)
	if err != nil {
		return s3Operation{}, err
	}
	query := r.URL.Query()
	switch {
	case r.Method == http.MethodHead && key == "":
		return s3Operation{Name: "HeadBucket", RequestedBucket: bucket, ObjectKey: key}, nil
	case r.Method == http.MethodHead && key != "":
		return s3Operation{Name: "HeadObject", RequestedBucket: bucket, ObjectKey: key}, nil
	case r.Method == http.MethodGet && query.Get("list-type") == "2" && key == "":
		return s3Operation{Name: "ListObjectsV2", RequestedBucket: bucket, NeedsXMLRewrite: true}, nil
	case r.Method == http.MethodGet && query.Has("uploads") && key == "":
		return s3Operation{Name: "ListMultipartUploads", RequestedBucket: bucket, NeedsXMLRewrite: true}, nil
	case r.Method == http.MethodGet && query.Get("uploadId") != "" && key != "":
		return s3Operation{Name: "ListParts", RequestedBucket: bucket, ObjectKey: key, NeedsXMLRewrite: true}, nil
	case r.Method == http.MethodGet && key != "":
		return s3Operation{Name: "GetObject", RequestedBucket: bucket, ObjectKey: key, SupportsBody: true}, nil
	case r.Method == http.MethodPut && query.Get("uploadId") != "" && query.Get("partNumber") != "" && key != "":
		return s3Operation{Name: "UploadPart", RequestedBucket: bucket, ObjectKey: key, SupportsBody: true}, nil
	case r.Method == http.MethodPut && key != "":
		return s3Operation{Name: "PutObject", RequestedBucket: bucket, ObjectKey: key, SupportsBody: true}, nil
	case r.Method == http.MethodPost && query.Has("uploads") && key != "":
		return s3Operation{Name: "CreateMultipartUpload", RequestedBucket: bucket, ObjectKey: key, SupportsBody: true, NeedsXMLRewrite: true}, nil
	case r.Method == http.MethodPost && query.Get("uploadId") != "" && key != "":
		return s3Operation{Name: "CompleteMultipartUpload", RequestedBucket: bucket, ObjectKey: key, SupportsBody: true, NeedsXMLRewrite: true}, nil
	case r.Method == http.MethodDelete && query.Get("uploadId") != "" && key != "":
		return s3Operation{Name: "AbortMultipartUpload", RequestedBucket: bucket, ObjectKey: key}, nil
	case r.Method == http.MethodDelete && key != "":
		return s3Operation{Name: "DeleteObject", RequestedBucket: bucket, ObjectKey: key}, nil
	default:
		return s3Operation{}, fmt.Errorf("unsupported S3 operation %s %s", r.Method, r.URL.RequestURI())
	}
}

func splitS3Path(u *url.URL) (string, string, error) {
	path := u.EscapedPath()
	path = strings.TrimPrefix(path, "/")
	if strings.TrimSpace(path) == "" {
		return "", "", fmt.Errorf("path-style bucket is required")
	}
	parts := strings.SplitN(path, "/", 2)
	bucket := normalizeBucketName(parts[0])
	if bucket == "" {
		return "", "", fmt.Errorf("path-style bucket is required")
	}
	if len(parts) == 1 {
		return bucket, "", nil
	}
	key, err := url.PathUnescape(parts[1])
	if err != nil {
		return "", "", err
	}
	return bucket, key, nil
}

func copyUpstreamHeaders(dst, src http.Header) {
	for name, values := range src {
		lowerName := strings.ToLower(name)
		switch lowerName {
		// otelhttp injects tracing headers after signing; forwarding caller trace
		// propagation into the signed header set breaks downstream SigV4.
		case "authorization", "host", "x-amz-security-token", "x-amz-content-sha256",
			"traceparent", "tracestate", "baggage",
			"x-b3-traceid", "x-b3-spanid", "x-b3-parentspanid", "x-b3-sampled", "x-b3-flags", "b3",
			"uber-trace-id":
			continue
		}
		for _, value := range values {
			dst.Add(name, value)
		}
	}
}

func copyHeaders(dst, src http.Header) {
	for name, values := range src {
		if strings.EqualFold(name, "Transfer-Encoding") {
			continue
		}
		dst[textproto.CanonicalMIMEHeaderKey(name)] = append([]string(nil), values...)
	}
}

func rewriteQueryForAlias(operation string, values url.Values, prefix string) url.Values {
	cloned := make(url.Values, len(values))
	for key, entry := range values {
		cloned[key] = append([]string(nil), entry...)
	}
	if prefix == "" {
		return cloned
	}
	switch operation {
	case "ListObjectsV2":
		if value := cloned.Get("prefix"); value != "" {
			cloned.Set("prefix", prefix+value)
		} else {
			cloned.Set("prefix", prefix)
		}
		if value := cloned.Get("start-after"); value != "" {
			cloned.Set("start-after", prefix+value)
		}
	case "ListMultipartUploads":
		if value := cloned.Get("prefix"); value != "" {
			cloned.Set("prefix", prefix+value)
		} else {
			cloned.Set("prefix", prefix)
		}
		if value := cloned.Get("key-marker"); value != "" {
			cloned.Set("key-marker", prefix+value)
		}
	}
	return cloned
}

func writeS3Error(w http.ResponseWriter, status int, code, message, resource string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	_ = xml.NewEncoder(w).Encode(s3ErrorResponse{
		Code:     code,
		Message:  message,
		Resource: resource,
	})
}

func hashRemoteAddr(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	return hashForAudit(host)
}

func traceContext(ctx context.Context) context.Context {
	return oteltrace.ContextWithSpanContext(context.Background(), oteltrace.SpanContextFromContext(ctx))
}

func escapeS3Key(key string) string {
	if key == "" {
		return ""
	}
	parts := strings.Split(key, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

func s3ErrorForAuthError(err error) (uint16, string, string) {
	switch {
	case errors.Is(err, ErrSigV4SignatureMismatch):
		return http.StatusForbidden, "signature_mismatch", "signature does not match"
	case errors.Is(err, ErrSigV4TimeSkew):
		return http.StatusForbidden, "time_skew", "request time skew too large"
	case errors.Is(err, ErrSigV4Unsupported):
		return http.StatusNotImplemented, "unsupported_auth", "unsupported SigV4 mode"
	case errors.Is(err, ErrNotFound):
		return http.StatusForbidden, "unknown_access_key", "unknown access key"
	case errors.Is(err, ErrUnauthorized):
		return http.StatusForbidden, "unauthorized", "access denied"
	default:
		return http.StatusForbidden, classifyError(err), "access denied"
	}
}

func s3ErrorCodeForAuthError(err error) string {
	switch {
	case errors.Is(err, ErrSigV4SignatureMismatch):
		return "SignatureDoesNotMatch"
	case errors.Is(err, ErrSigV4TimeSkew):
		return "RequestTimeTooSkewed"
	case errors.Is(err, ErrSigV4Unsupported):
		return "NotImplemented"
	case errors.Is(err, ErrNotFound):
		return "InvalidAccessKeyId"
	default:
		return "AccessDenied"
	}
}

func (h *S3Handler) recordAccessEvent(ctx context.Context, event ObjectAccessEvent) {
	if event.TraceID == "" {
		if sc := oteltrace.SpanContextFromContext(ctx); sc.HasTraceID() {
			event.TraceID = sc.TraceID().String()
		}
	}
	if event.SpanID == "" {
		if sc := oteltrace.SpanContextFromContext(ctx); sc.HasSpanID() {
			event.SpanID = sc.SpanID().String()
		}
	}
	if err := h.Service.RecordAccessEvent(ctx, event); err != nil && h.Logger != nil {
		h.Logger.ErrorContext(ctx, "object-storage access event write failed", "error", err)
	}
}

type countingReadCloser struct {
	io.ReadCloser
	N int64
}

func (c *countingReadCloser) Read(p []byte) (int, error) {
	if c.ReadCloser == nil {
		return 0, io.EOF
	}
	n, err := c.ReadCloser.Read(p)
	c.N += int64(n)
	return n, err
}

type countingResponseWriter struct {
	http.ResponseWriter
	N int64
}

func (c *countingResponseWriter) Write(p []byte) (int, error) {
	n, err := c.ResponseWriter.Write(p)
	c.N += int64(n)
	return n, err
}
