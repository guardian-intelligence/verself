package objectstorageclient

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

type (
	ObjectName              = string
	ObjectNamespace         = string
	ObjectStorageCapability = string
	ObjectTransferURL       = string
	ObjectWriteAction       = string
	ObjectWriteSessionId    = string
	SHA256Hex               = string
	SiteName                = string
)

type ObjectTransferHeaders map[string]string

const (
	ObjectStorageCapabilityDeploymentArtifacts ObjectStorageCapability = "deployment_artifacts"
	ObjectWriteActionPresent                   ObjectWriteAction       = "present"
	ObjectWriteActionPut                       ObjectWriteAction       = "put"
)

type ObjectWriteRequest struct {
	Name      ObjectName `json:"name"`
	SHA256    SHA256Hex  `json:"sha256"`
	SizeBytes int64      `json:"size_bytes"`
}

type S3CompatibleObject struct {
	Method    string                `json:"method"`
	URL       ObjectTransferURL     `json:"url"`
	Headers   ObjectTransferHeaders `json:"headers"`
	ExpiresAt string                `json:"expires_at"`
	SHA256    *SHA256Hex            `json:"sha256,omitempty"`
}

type ObjectWriteSessionObject struct {
	Name         ObjectName          `json:"name"`
	Key          string              `json:"key"`
	Action       ObjectWriteAction   `json:"action"`
	Write        *S3CompatibleObject `json:"write,omitempty"`
	Read         *S3CompatibleObject `json:"read,omitempty"`
	GetterSource *ObjectTransferURL  `json:"getter_source,omitempty"`
}

type ObjectWriteSessionObjects []ObjectWriteSessionObject

type CreateObjectWriteSessionInputBody struct {
	Capability ObjectStorageCapability `json:"capability"`
	Namespace  ObjectNamespace         `json:"namespace"`
	Objects    []ObjectWriteRequest    `json:"objects"`
	TTLSeconds int                     `json:"ttl_seconds"`
}

type CreateObjectWriteSessionRequest struct {
	Site           SiteName
	IdempotencyKey IdempotencyKey
	Body           CreateObjectWriteSessionInputBody
}

type CreateObjectWriteSessionResponse struct {
	StatusCode   int
	Body         []byte
	Result       *ObjectWriteSession
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type CompleteObjectWriteSessionRequest struct {
	Site      SiteName
	SessionID ObjectWriteSessionId
}

type CompleteObjectWriteSessionResponse struct {
	StatusCode   int
	Body         []byte
	Result       *ObjectWriteSession
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type ObjectWriteSession struct {
	SessionID   ObjectWriteSessionId      `json:"session_id"`
	ExpiresAt   string                    `json:"expires_at,omitempty"`
	CompletedAt *string                   `json:"completed_at,omitempty"`
	Objects     ObjectWriteSessionObjects `json:"objects"`
}

func (c *Client) CreateObjectWriteSession(ctx context.Context, request CreateObjectWriteSessionRequest, reqEditors ...RequestEditorFn) (*CreateObjectWriteSessionResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newCreateObjectWriteSessionRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	if err := c.applyRequestEditors(ctx, req, reqEditors...); err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	return parseCreateObjectWriteSessionResponse(resp)
}

func (c *Client) newCreateObjectWriteSessionRequest(ctx context.Context, request CreateObjectWriteSessionRequest) (*http.Request, error) {
	path := "/internal/v1/object-storage/sites/{site}/write-sessions"
	path = strings.ReplaceAll(path, "{site}", url.PathEscape(fmt.Sprint(request.Site)))
	endpoint, err := url.Parse(c.server + path)
	if err != nil {
		return nil, err
	}
	requestBody, err := json.Marshal(request.Body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(requestBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", fmt.Sprint(request.IdempotencyKey))
	return req, nil
}

func (c *Client) CompleteObjectWriteSession(ctx context.Context, request CompleteObjectWriteSessionRequest, reqEditors ...RequestEditorFn) (*CompleteObjectWriteSessionResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newCompleteObjectWriteSessionRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	if err := c.applyRequestEditors(ctx, req, reqEditors...); err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	return parseCompleteObjectWriteSessionResponse(resp)
}

func (c *Client) newCompleteObjectWriteSessionRequest(ctx context.Context, request CompleteObjectWriteSessionRequest) (*http.Request, error) {
	path := "/internal/v1/object-storage/sites/{site}/write-sessions/{session_id}/complete"
	path = strings.ReplaceAll(path, "{site}", url.PathEscape(fmt.Sprint(request.Site)))
	path = strings.ReplaceAll(path, "{session_id}", url.PathEscape(fmt.Sprint(request.SessionID)))
	endpoint, err := url.Parse(c.server + path)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func (c *Client) applyRequestEditors(ctx context.Context, req *http.Request, reqEditors ...RequestEditorFn) error {
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

func parseCreateObjectWriteSessionResponse(resp *http.Response) (*CreateObjectWriteSessionResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &CreateObjectWriteSessionResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == http.StatusCreated {
		var decoded ObjectWriteSession
		if len(body) > 0 {
			if err := json.Unmarshal(body, &decoded); err != nil {
				return nil, err
			}
		}
		result.Result = &decoded
		return result, nil
	}
	result.Problem = decodeProblem(body)
	return result, nil
}

func parseCompleteObjectWriteSessionResponse(resp *http.Response) (*CompleteObjectWriteSessionResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &CompleteObjectWriteSessionResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == http.StatusOK {
		var decoded ObjectWriteSession
		if len(body) > 0 {
			if err := json.Unmarshal(body, &decoded); err != nil {
				return nil, err
			}
		}
		result.Result = &decoded
		return result, nil
	}
	result.Problem = decodeProblem(body)
	return result, nil
}
