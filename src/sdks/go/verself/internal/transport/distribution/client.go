package distribution

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const ServiceName = "distribution-service"

type RequestEditorFn func(ctx context.Context, req *http.Request) error

type HTTPRequestDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type ClientOption func(*Client)

type Client struct {
	server         string
	client         HTTPRequestDoer
	requestEditors []RequestEditorFn
}

type CheckUpdateRequest struct {
	PackageName      string `json:"package_name"`
	ChannelName      string `json:"channel_name"`
	PlatformOS       string `json:"platform_os"`
	PlatformArch     string `json:"platform_arch"`
	Flavor           string `json:"flavor"`
	InstalledDigest  string `json:"installed_digest,omitempty"`
	InstalledVersion string `json:"installed_version,omitempty"`
}

type ResolveTargetRequest struct {
	PackageName  string
	ChannelName  string
	PlatformOS   string
	PlatformArch string
	Flavor       string
}

type GetArtifactRequest struct {
	ArtifactDigestRef string
}

type ListTargetsRequest struct {
	PackageName  string
	ChannelName  string
	PlatformOS   string
	PlatformArch string
	Flavor       string
	PageSize     int32
	PageToken    string
}

type EvidenceRecord struct {
	EvidenceKind      string `json:"evidence_kind"`
	PredicateType     string `json:"predicate_type"`
	SubjectDigest     string `json:"subject_digest"`
	DocumentDigest    string `json:"document_digest"`
	OCIReferrerDigest string `json:"oci_referrer_digest"`
	CreatedAt         string `json:"created_at"`
}

type VerificationRecord struct {
	Decision         string           `json:"decision"`
	Reason           string           `json:"reason"`
	BuilderID        string           `json:"builder_id"`
	SignerIdentity   string           `json:"signer_identity"`
	SourceRepository string           `json:"source_repository"`
	SourceCommit     string           `json:"source_commit"`
	SourceRef        string           `json:"source_ref"`
	Evidence         []EvidenceRecord `json:"evidence"`
	CheckedAt        string           `json:"checked_at"`
}

type ReplicationRecord struct {
	State             string `json:"state"`
	OriginRegistryURL string `json:"origin_registry_url"`
	PublicRegistryURL string `json:"public_registry_url"`
	CheckedAt         string `json:"checked_at"`
}

type ArtifactRecord struct {
	ArtifactDigest     string             `json:"artifact_digest"`
	ArtifactDigestRef  string             `json:"artifact_digest_ref"`
	ResourceName       string             `json:"resourceName"`
	PackageName        string             `json:"package_name"`
	PackageVersion     string             `json:"package_version"`
	PlatformOS         string             `json:"platform_os"`
	PlatformArch       string             `json:"platform_arch"`
	Flavor             string             `json:"flavor"`
	OCIRepository      string             `json:"oci_repository"`
	PublicOCIReference string             `json:"public_oci_reference"`
	OCIMediaType       string             `json:"oci_media_type"`
	OCISizeBytes       int64              `json:"oci_size_bytes"`
	State              string             `json:"state"`
	RetentionClass     string             `json:"retention_class"`
	Verification       VerificationRecord `json:"verification"`
	Replication        ReplicationRecord  `json:"replication"`
	CreatedAt          string             `json:"created_at"`
	UpdatedAt          string             `json:"updated_at"`
	QuarantinedAt      *string            `json:"quarantined_at,omitempty"`
	QuarantineReason   *string            `json:"quarantine_reason,omitempty"`
}

type TargetRecord struct {
	ResourceName       string  `json:"resourceName"`
	PackageName        string  `json:"package_name"`
	ChannelName        string  `json:"channel_name"`
	PlatformOS         string  `json:"platform_os"`
	PlatformArch       string  `json:"platform_arch"`
	Flavor             string  `json:"flavor"`
	ArtifactDigest     string  `json:"artifact_digest"`
	ArtifactDigestRef  string  `json:"artifact_digest_ref"`
	PackageVersion     string  `json:"package_version"`
	State              string  `json:"state"`
	PublicOCIReference string  `json:"public_oci_reference"`
	DownloadURL        string  `json:"download_url"`
	PublishedAt        string  `json:"published_at"`
	SupersededAt       *string `json:"superseded_at,omitempty"`
	SupersededByDigest *string `json:"superseded_by_digest,omitempty"`
}

type CheckUpdateOutput struct {
	UpdateAvailable bool         `json:"update_available"`
	Target          TargetRecord `json:"target"`
	Traceparent     string       `json:"traceparent"`
}

type ResolveTargetOutput struct {
	Target      TargetRecord `json:"target"`
	Traceparent string       `json:"traceparent"`
}

type ArtifactOutput struct {
	Artifact    ArtifactRecord `json:"artifact"`
	Traceparent string         `json:"traceparent"`
}

type ListTargetsOutput struct {
	Targets       []TargetRecord `json:"targets"`
	NextPageToken string         `json:"next_page_token,omitempty"`
	Traceparent   string         `json:"traceparent"`
}

type ErrorModel struct {
	Type        *string `json:"type,omitempty"`
	Title       *string `json:"title,omitempty"`
	Status      *int64  `json:"status,omitempty"`
	Detail      *string `json:"detail,omitempty"`
	Code        *string `json:"code,omitempty"`
	RequestID   *string `json:"requestId,omitempty"`
	Traceparent *string `json:"traceparent,omitempty"`
}

type CheckUpdateResponse struct {
	StatusCode   int
	Body         []byte
	Result       *CheckUpdateOutput
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type ResolveTargetResponse struct {
	StatusCode   int
	Body         []byte
	Result       *ResolveTargetOutput
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type ArtifactResponse struct {
	StatusCode   int
	Body         []byte
	Result       *ArtifactOutput
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type ListTargetsResponse struct {
	StatusCode   int
	Body         []byte
	Result       *ListTargetsOutput
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

func NewClient(server string, opts ...ClientOption) (*Client, error) {
	server = strings.TrimRight(strings.TrimSpace(server), "/")
	if server == "" {
		return nil, fmt.Errorf("%s SDK transport: server URL is required", ServiceName)
	}
	client := &Client{server: server, client: http.DefaultClient}
	for _, opt := range opts {
		opt(client)
	}
	if client.client == nil {
		return nil, fmt.Errorf("%s SDK transport: HTTP client is required", ServiceName)
	}
	return client, nil
}

func WithHTTPClient(client HTTPRequestDoer) ClientOption {
	return func(c *Client) { c.client = client }
}

func WithRequestEditorFn(editor RequestEditorFn) ClientOption {
	return func(c *Client) {
		if editor != nil {
			c.requestEditors = append(c.requestEditors, editor)
		}
	}
}

func (c *Client) CheckUpdate(ctx context.Context, request CheckUpdateRequest, reqEditors ...RequestEditorFn) (*CheckUpdateResponse, error) {
	req, err := c.newJSONRequest(ctx, http.MethodPost, "/api/v1/distribution/updates:check", request)
	if err != nil {
		return nil, err
	}
	if err := c.edit(ctx, req, reqEditors...); err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	return parseCheckUpdateResponse(resp, http.StatusOK)
}

func (c *Client) ResolveTarget(ctx context.Context, request ResolveTargetRequest, reqEditors ...RequestEditorFn) (*ResolveTargetResponse, error) {
	path := "/api/v1/distribution/packages/" + url.PathEscape(request.PackageName) + "/channels/" + url.PathEscape(request.ChannelName) + "/resolve"
	req, err := c.newRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	query := req.URL.Query()
	if strings.TrimSpace(request.PlatformOS) != "" {
		query.Set("platform_os", strings.TrimSpace(request.PlatformOS))
	}
	if strings.TrimSpace(request.PlatformArch) != "" {
		query.Set("platform_arch", strings.TrimSpace(request.PlatformArch))
	}
	if strings.TrimSpace(request.Flavor) != "" {
		query.Set("flavor", strings.TrimSpace(request.Flavor))
	}
	req.URL.RawQuery = query.Encode()
	if err := c.edit(ctx, req, reqEditors...); err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	return parseResolveTargetResponse(resp, http.StatusOK)
}

func (c *Client) GetArtifact(ctx context.Context, request GetArtifactRequest, reqEditors ...RequestEditorFn) (*ArtifactResponse, error) {
	path := "/api/v1/distribution/artifacts/" + url.PathEscape(request.ArtifactDigestRef)
	req, err := c.newRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	if err := c.edit(ctx, req, reqEditors...); err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	return parseArtifactResponse(resp, http.StatusOK)
}

func (c *Client) ListTargets(ctx context.Context, request ListTargetsRequest, reqEditors ...RequestEditorFn) (*ListTargetsResponse, error) {
	path := "/api/v1/distribution/packages/" + url.PathEscape(request.PackageName) + "/channels/" + url.PathEscape(request.ChannelName) + "/targets"
	req, err := c.newRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	query := req.URL.Query()
	if strings.TrimSpace(request.PlatformOS) != "" {
		query.Set("platform_os", strings.TrimSpace(request.PlatformOS))
	}
	if strings.TrimSpace(request.PlatformArch) != "" {
		query.Set("platform_arch", strings.TrimSpace(request.PlatformArch))
	}
	if strings.TrimSpace(request.Flavor) != "" {
		query.Set("flavor", strings.TrimSpace(request.Flavor))
	}
	if request.PageSize > 0 {
		query.Set("page_size", fmt.Sprintf("%d", request.PageSize))
	}
	if strings.TrimSpace(request.PageToken) != "" {
		query.Set("page_token", strings.TrimSpace(request.PageToken))
	}
	req.URL.RawQuery = query.Encode()
	if err := c.edit(ctx, req, reqEditors...); err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	return parseListTargetsResponse(resp, http.StatusOK)
}

func (c *Client) newJSONRequest(ctx context.Context, method string, path string, body any) (*http.Request, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	return c.newRequest(ctx, method, path, bytes.NewReader(payload))
}

func (c *Client) newRequest(ctx context.Context, method string, path string, body io.Reader) (*http.Request, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	endpoint, err := url.Parse(c.server + path)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func (c *Client) edit(ctx context.Context, req *http.Request, reqEditors ...RequestEditorFn) error {
	for _, editor := range c.requestEditors {
		if err := editor(ctx, req); err != nil {
			return err
		}
	}
	for _, editor := range reqEditors {
		if editor != nil {
			if err := editor(ctx, req); err != nil {
				return err
			}
		}
	}
	return nil
}

func decodeProblem(body []byte) *ErrorModel {
	var problem ErrorModel
	if len(body) > 0 {
		_ = json.Unmarshal(body, &problem)
	}
	return &problem
}

func responseBody(resp *http.Response) ([]byte, error) {
	defer func() { _ = resp.Body.Close() }()
	return io.ReadAll(resp.Body)
}

func parseCheckUpdateResponse(resp *http.Response, successStatus int) (*CheckUpdateResponse, error) {
	body, err := responseBody(resp)
	if err != nil {
		return nil, err
	}
	result := &CheckUpdateResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode != successStatus {
		result.Problem = decodeProblem(body)
		return result, nil
	}
	var decoded CheckUpdateOutput
	if len(body) > 0 {
		if err := json.Unmarshal(body, &decoded); err != nil {
			return nil, err
		}
	}
	result.Result = &decoded
	return result, nil
}

func parseResolveTargetResponse(resp *http.Response, successStatus int) (*ResolveTargetResponse, error) {
	body, err := responseBody(resp)
	if err != nil {
		return nil, err
	}
	result := &ResolveTargetResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode != successStatus {
		result.Problem = decodeProblem(body)
		return result, nil
	}
	var decoded ResolveTargetOutput
	if len(body) > 0 {
		if err := json.Unmarshal(body, &decoded); err != nil {
			return nil, err
		}
	}
	result.Result = &decoded
	return result, nil
}

func parseArtifactResponse(resp *http.Response, successStatus int) (*ArtifactResponse, error) {
	body, err := responseBody(resp)
	if err != nil {
		return nil, err
	}
	result := &ArtifactResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode != successStatus {
		result.Problem = decodeProblem(body)
		return result, nil
	}
	var decoded ArtifactOutput
	if len(body) > 0 {
		if err := json.Unmarshal(body, &decoded); err != nil {
			return nil, err
		}
	}
	result.Result = &decoded
	return result, nil
}

func parseListTargetsResponse(resp *http.Response, successStatus int) (*ListTargetsResponse, error) {
	body, err := responseBody(resp)
	if err != nil {
		return nil, err
	}
	result := &ListTargetsResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode != successStatus {
		result.Problem = decodeProblem(body)
		return result, nil
	}
	var decoded ListTargetsOutput
	if len(body) > 0 {
		if err := json.Unmarshal(body, &decoded); err != nil {
			return nil, err
		}
	}
	result.Result = &decoded
	return result, nil
}
