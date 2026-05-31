package distributioninternalclient

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

type Evidence struct {
	EvidenceKind      string `json:"evidence_kind"`
	PredicateType     string `json:"predicate_type"`
	SubjectDigest     string `json:"subject_digest"`
	DocumentDigest    string `json:"document_digest"`
	OCIReferrerDigest string `json:"oci_referrer_digest"`
}

type ReleaseAttestation struct {
	DistributionChallenge      string      `json:"distribution_challenge"`
	ReleaseInputDigest         string      `json:"release_input_digest"`
	ArtifactDigest             string      `json:"artifact_digest"`
	ProvenanceDigest           string      `json:"provenance_digest"`
	SBOMDigest                 string      `json:"sbom_digest"`
	TPMReleasePublicName       string      `json:"tpm_release_public_name"`
	TPMReleasePublicBlobDigest string      `json:"tpm_release_public_blob_digest"`
	PolicyID                   string      `json:"policy_id"`
	TPM                        TPMEvidence `json:"tpm"`
}

type TPMEvidence struct {
	AKPublic                []byte                  `json:"ak_public"`
	Quotes                  []TPMQuote              `json:"quotes"`
	PCRs                    []TPMPCR                `json:"pcrs"`
	EventLog                []byte                  `json:"event_log"`
	ReleasePublicName       string                  `json:"release_public_name"`
	ReleasePublicBlob       []byte                  `json:"release_public_blob"`
	ReleasePublicBlobDigest string                  `json:"release_public_blob_digest"`
	ReleaseKeyCertification ReleaseKeyCertification `json:"release_key_certification"`
}

type TPMQuote struct {
	Quote     []byte `json:"quote"`
	Signature []byte `json:"signature"`
}

type TPMPCR struct {
	Index     int    `json:"index"`
	Digest    string `json:"digest"`
	DigestAlg string `json:"digest_alg"`
}

type ReleaseKeyCertification struct {
	Public            []byte `json:"public"`
	CreateData        []byte `json:"create_data"`
	CreateAttestation []byte `json:"create_attestation"`
	CreateSignature   []byte `json:"create_signature"`
}

type AdmitArtifactRequest struct {
	PackageName        string             `json:"package_name"`
	PackageVersion     string             `json:"package_version"`
	ChannelName        string             `json:"channel_name"`
	PlatformOS         string             `json:"platform_os"`
	PlatformArch       string             `json:"platform_arch"`
	Flavor             string             `json:"flavor"`
	OriginRegistryURL  string             `json:"origin_registry_url"`
	PublicRegistryURL  string             `json:"public_registry_url"`
	OCIRepository      string             `json:"oci_repository"`
	OCIDigest          string             `json:"oci_digest"`
	OCIMediaType       string             `json:"oci_media_type"`
	OCISizeBytes       int64              `json:"oci_size_bytes"`
	BuilderID          string             `json:"builder_id"`
	SignerIdentity     string             `json:"signer_identity"`
	SourceRepository   string             `json:"source_repository"`
	SourceCommit       string             `json:"source_commit"`
	SourceRef          string             `json:"source_ref"`
	PolicyRef          string             `json:"policy_ref"`
	Evidence           []Evidence         `json:"evidence"`
	ReleaseAttestation ReleaseAttestation `json:"release_attestation"`
	SubmittedBy        string             `json:"submitted_by"`
}

type PromoteTargetRequest struct {
	PackageName    string `json:"-"`
	ChannelName    string `json:"-"`
	ArtifactDigest string `json:"artifact_digest"`
	PlatformOS     string `json:"platform_os"`
	PlatformArch   string `json:"platform_arch"`
	Flavor         string `json:"flavor"`
	PolicyRef      string `json:"policy_ref"`
	PromotedBy     string `json:"promoted_by"`
	Reason         string `json:"reason"`
}

type ArtifactRecord struct {
	ResourceName       string `json:"resource_name"`
	PackageName        string `json:"package_name"`
	PackageVersion     string `json:"package_version"`
	ChannelName        string `json:"channel_name"`
	PlatformOS         string `json:"platform_os"`
	PlatformArch       string `json:"platform_arch"`
	Flavor             string `json:"flavor"`
	OCIRepository      string `json:"oci_repository"`
	OCIDigest          string `json:"oci_digest"`
	OCIMediaType       string `json:"oci_media_type"`
	OCISizeBytes       int64  `json:"oci_size_bytes"`
	PublicOCIReference string `json:"public_oci_reference"`
	State              string `json:"state"`
}

type TargetRecord struct {
	ResourceName       string `json:"resource_name"`
	PackageName        string `json:"package_name"`
	ChannelName        string `json:"channel_name"`
	PlatformOS         string `json:"platform_os"`
	PlatformArch       string `json:"platform_arch"`
	Flavor             string `json:"flavor"`
	ArtifactDigest     string `json:"artifact_digest"`
	PackageVersion     string `json:"package_version"`
	PublicOCIReference string `json:"public_oci_reference"`
	DownloadURL        string `json:"download_url"`
	State              string `json:"state"`
}

type ArtifactResponseBody struct {
	Artifact ArtifactRecord `json:"artifact"`
}

type TargetResponseBody struct {
	Target TargetRecord `json:"target"`
}

type ArtifactResponse struct {
	StatusCode   int
	Body         []byte
	Result       *ArtifactResponseBody
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type TargetResponse struct {
	StatusCode   int
	Body         []byte
	Result       *TargetResponseBody
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type ErrorModel struct {
	Type        *string `json:"type,omitempty"`
	Title       *string `json:"title,omitempty"`
	Status      *int64  `json:"status,omitempty"`
	Detail      *string `json:"detail,omitempty"`
	Code        *string `json:"code,omitempty"`
	Traceparent *string `json:"traceparent,omitempty"`
}

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

func NewClient(server string, opts ...ClientOption) (*Client, error) {
	server = strings.TrimRight(strings.TrimSpace(server), "/")
	if server == "" {
		return nil, fmt.Errorf("%s internal transport: server URL is required", ServiceName)
	}
	client := &Client{server: server, client: http.DefaultClient}
	for _, opt := range opts {
		opt(client)
	}
	if client.client == nil {
		return nil, fmt.Errorf("%s internal transport: HTTP client is required", ServiceName)
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

func (c *Client) AdmitArtifact(ctx context.Context, request AdmitArtifactRequest, idempotencyKey string, reqEditors ...RequestEditorFn) (*ArtifactResponse, error) {
	req, err := c.newJSONRequest(ctx, "POST", "/internal/v1/distribution/artifacts:admit", request, idempotencyKey)
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
	return parseArtifactResponse(resp, http.StatusCreated)
}

func (c *Client) PromoteTarget(ctx context.Context, request PromoteTargetRequest, idempotencyKey string, reqEditors ...RequestEditorFn) (*TargetResponse, error) {
	path := "/internal/v1/distribution/packages/" + url.PathEscape(request.PackageName) + "/channels/" + url.PathEscape(request.ChannelName) + "/targets/promote"
	req, err := c.newJSONRequest(ctx, "POST", path, request, idempotencyKey)
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
	return parseTargetResponse(resp, http.StatusOK)
}

func (c *Client) newJSONRequest(ctx context.Context, method string, path string, body any, idempotencyKey string) (*http.Request, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s internal transport: client is not initialized", ServiceName)
	}
	endpoint, err := url.Parse(c.server + path)
	if err != nil {
		return nil, err
	}
	requestBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), bytes.NewReader(requestBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(idempotencyKey) != "" {
		req.Header.Set("Idempotency-Key", strings.TrimSpace(idempotencyKey))
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

func parseArtifactResponse(resp *http.Response, successStatus int) (*ArtifactResponse, error) {
	body, problem, err := readResponse(resp, successStatus)
	if err != nil {
		return nil, err
	}
	result := &ArtifactResponse{StatusCode: resp.StatusCode, Body: body, Problem: problem, HTTPResponse: resp}
	if problem == nil {
		var decoded ArtifactResponseBody
		if len(body) > 0 {
			if err := json.Unmarshal(body, &decoded); err != nil {
				return nil, err
			}
		}
		result.Result = &decoded
	}
	return result, nil
}

func parseTargetResponse(resp *http.Response, successStatus int) (*TargetResponse, error) {
	body, problem, err := readResponse(resp, successStatus)
	if err != nil {
		return nil, err
	}
	result := &TargetResponse{StatusCode: resp.StatusCode, Body: body, Problem: problem, HTTPResponse: resp}
	if problem == nil {
		var decoded TargetResponseBody
		if len(body) > 0 {
			if err := json.Unmarshal(body, &decoded); err != nil {
				return nil, err
			}
		}
		result.Result = &decoded
	}
	return result, nil
}

func readResponse(resp *http.Response, successStatus int) ([]byte, *ErrorModel, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode == successStatus {
		return body, nil, nil
	}
	var problem ErrorModel
	if len(body) > 0 {
		if err := json.Unmarshal(body, &problem); err != nil {
			return nil, nil, err
		}
	}
	return body, &problem, nil
}
