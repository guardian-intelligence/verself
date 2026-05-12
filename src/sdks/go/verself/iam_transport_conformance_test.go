package verself

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	iamcore "github.com/verself/verself-go/internal/generated/iam"
)

type iamConformanceFixture struct {
	SchemaVersion string                    `json:"schemaVersion"`
	Service       string                    `json:"service"`
	Projection    string                    `json:"projection"`
	Operations    []iamConformanceOperation `json:"operations"`
}

type iamConformanceOperation struct {
	Name          string               `json:"name"`
	OperationID   string               `json:"operationId"`
	Method        string               `json:"method"`
	PathTemplate  string               `json:"pathTemplate"`
	SuccessStatus int                  `json:"successStatus"`
	Cases         []iamConformanceCase `json:"cases"`
}

type iamConformanceCase struct {
	Name     string                  `json:"name"`
	Request  *iamConformanceRequest  `json:"request,omitempty"`
	Response *iamConformanceResponse `json:"response,omitempty"`
}

type iamConformanceRequest struct {
	Input   map[string]any    `json:"input"`
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Query   map[string]string `json:"query,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    any               `json:"body,omitempty"`
}

type iamConformanceResponse struct {
	Status  int            `json:"status"`
	Body    any            `json:"body"`
	Result  any            `json:"result,omitempty"`
	Problem map[string]any `json:"problem,omitempty"`
}

type iamTransportResult struct {
	StatusCode int
	Result     any
	Problem    *iamcore.ErrorModel
	Body       []byte
}

func TestIAMIRTransportConformanceFixtures(t *testing.T) {
	fixture := loadIAMConformanceFixture(t)
	if fixture.SchemaVersion != "verself.sdk-conformance.v1" {
		t.Fatalf("schemaVersion = %q", fixture.SchemaVersion)
	}
	for _, operation := range fixture.Operations {
		operation := operation
		t.Run(operation.OperationID+"/success", func(t *testing.T) {
			serialization := operation.caseByName(t, "http_serialization")
			responseCase := operation.caseByName(t, "response_parsing")
			result := runIAMTransportConformanceCase(t, operation, serialization, responseCase)
			if result.StatusCode != responseCase.Response.Status {
				t.Fatalf("status = %d, want %d", result.StatusCode, responseCase.Response.Status)
			}
			if result.Result == nil {
				t.Fatalf("result is nil; body=%s", string(result.Body))
			}
			assertCanonicalJSON(t, "result", result.Result, responseCase.Response.Result)
		})
		t.Run(operation.OperationID+"/problem", func(t *testing.T) {
			serialization := operation.caseByName(t, "http_serialization")
			responseCase := operation.caseByName(t, "problem_parsing")
			result := runIAMTransportConformanceCase(t, operation, serialization, responseCase)
			if result.StatusCode != responseCase.Response.Status {
				t.Fatalf("status = %d, want %d", result.StatusCode, responseCase.Response.Status)
			}
			if result.Result != nil {
				t.Fatalf("result = %#v, want nil", result.Result)
			}
			assertCanonicalJSON(t, "problem", result.Problem, responseCase.Response.Problem)
		})
	}
}

func runIAMTransportConformanceCase(t *testing.T, operation iamConformanceOperation, serialization iamConformanceCase, responseCase iamConformanceCase) iamTransportResult {
	t.Helper()
	expectedRequest := serialization.Request
	if expectedRequest == nil {
		t.Fatalf("%s has no request fixture", serialization.Name)
	}
	expectedResponse := responseCase.Response
	if expectedResponse == nil {
		t.Fatalf("%s has no response fixture", responseCase.Name)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertIAMConformanceHTTPRequest(t, expectedRequest, r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(expectedResponse.Status)
		if err := json.NewEncoder(w).Encode(expectedResponse.Body); err != nil {
			t.Fatal(err)
		}
	}))
	defer server.Close()

	client, err := iamcore.NewClient(
		server.URL,
		iamcore.WithRequestEditorFn(func(ctx context.Context, req *http.Request) error {
			req.Header.Set("Authorization", "Bearer conformance-token")
			req.Header.Set("traceparent", "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01")
			return nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := callIAMConformanceOperation(context.Background(), client, operation.OperationID, expectedRequest.Input)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func callIAMConformanceOperation(ctx context.Context, client *iamcore.Client, operationID string, input map[string]any) (iamTransportResult, error) {
	switch operationID {
	case "list-organizations":
		pageSize := iamcore.PageSize(intInput(input, "pageSize"))
		pageToken := iamcore.PageToken(stringInput(input, "pageToken"))
		response, err := client.ListOrganizations(ctx, iamcore.ListOrganizationsRequest{PageSize: &pageSize, PageToken: &pageToken})
		if err != nil {
			return iamTransportResult{}, err
		}
		return iamTransportResult{StatusCode: response.StatusCode, Result: nilIfTypedNil(response.Result), Problem: response.Problem, Body: response.Body}, nil
	case "get-organization":
		response, err := client.GetOrganization(ctx, iamcore.GetOrganizationRequest{OrgID: stringInput(input, "orgId")})
		if err != nil {
			return iamTransportResult{}, err
		}
		return iamTransportResult{StatusCode: response.StatusCode, Result: nilIfTypedNil(response.Result), Problem: response.Problem, Body: response.Body}, nil
	case "update-organization":
		displayName := iamcore.DisplayName(stringInput(input, "displayName"))
		slug := iamcore.OrgSlug(stringInput(input, "slug"))
		response, err := client.UpdateOrganization(ctx, iamcore.UpdateOrganizationRequest{
			OrgID:          stringInput(input, "orgId"),
			IdempotencyKey: stringInput(input, "idempotencyKey"),
			Body: iamcore.UpdateOrganizationInputBody{
				DisplayName: &displayName,
				Slug:        &slug,
				Version:     iamcore.OrganizationVersion(intInput(input, "version")),
			},
		})
		if err != nil {
			return iamTransportResult{}, err
		}
		return iamTransportResult{StatusCode: response.StatusCode, Result: nilIfTypedNil(response.Result), Problem: response.Problem, Body: response.Body}, nil
	case "list-members":
		pageSize := iamcore.PageSize(intInput(input, "pageSize"))
		pageToken := iamcore.PageToken(stringInput(input, "pageToken"))
		response, err := client.ListMembers(ctx, iamcore.ListMembersRequest{
			OrgID:     stringInput(input, "orgId"),
			PageSize:  &pageSize,
			PageToken: &pageToken,
		})
		if err != nil {
			return iamTransportResult{}, err
		}
		return iamTransportResult{StatusCode: response.StatusCode, Result: nilIfTypedNil(response.Result), Problem: response.Problem, Body: response.Body}, nil
	case "get-member":
		response, err := client.GetMember(ctx, iamcore.GetMemberRequest{
			OrgID:    stringInput(input, "orgId"),
			MemberID: stringInput(input, "memberId"),
		})
		if err != nil {
			return iamTransportResult{}, err
		}
		return iamTransportResult{StatusCode: response.StatusCode, Result: nilIfTypedNil(response.Result), Problem: response.Problem, Body: response.Body}, nil
	case "update-member-role":
		response, err := client.UpdateMemberRole(ctx, iamcore.UpdateMemberRoleRequest{
			OrgID:          stringInput(input, "orgId"),
			MemberID:       stringInput(input, "memberId"),
			IdempotencyKey: stringInput(input, "idempotencyKey"),
			Body: iamcore.UpdateMemberRoleInputBody{
				Role:                  iamcore.OrganizationRole(stringInput(input, "role")),
				ExpectedRole:          iamcore.OrganizationRole(stringInput(input, "expectedRole")),
				ExpectedOrgACLVersion: iamcore.OrgAclVersion(intInput(input, "expectedOrgAclVersion")),
			},
		})
		if err != nil {
			return iamTransportResult{}, err
		}
		return iamTransportResult{StatusCode: response.StatusCode, Result: nilIfTypedNil(response.Result), Problem: response.Problem, Body: response.Body}, nil
	default:
		return iamTransportResult{}, &APIError{Service: "IAM conformance", Operation: operationID, StatusCode: http.StatusInternalServerError, Title: "unknown operation"}
	}
}

func assertIAMConformanceHTTPRequest(t *testing.T, expected *iamConformanceRequest, actual *http.Request) {
	t.Helper()
	if actual.Method != expected.Method {
		t.Fatalf("method = %s, want %s", actual.Method, expected.Method)
	}
	if actual.URL.Path != expected.Path {
		t.Fatalf("path = %s, want %s", actual.URL.Path, expected.Path)
	}
	for name, value := range expected.Query {
		if actual.URL.Query().Get(name) != value {
			t.Fatalf("query %s = %q, want %q", name, actual.URL.Query().Get(name), value)
		}
	}
	for name, value := range expected.Headers {
		if actual.Header.Get(name) != value {
			t.Fatalf("header %s = %q, want %q", name, actual.Header.Get(name), value)
		}
	}
	if actual.Header.Get("Authorization") != "Bearer conformance-token" {
		t.Fatalf("Authorization header was not applied by request editor")
	}
	if actual.Header.Get("traceparent") == "" {
		t.Fatalf("traceparent header was not applied by request editor")
	}
	body, err := io.ReadAll(actual.Body)
	if err != nil {
		t.Fatal(err)
	}
	if expected.Body == nil {
		if len(bytes.TrimSpace(body)) != 0 {
			t.Fatalf("body = %s, want empty", string(body))
		}
		return
	}
	var actualBody any
	if err := json.Unmarshal(body, &actualBody); err != nil {
		t.Fatal(err)
	}
	assertCanonicalJSON(t, "request body", actualBody, expected.Body)
}

func (op iamConformanceOperation) caseByName(t *testing.T, name string) iamConformanceCase {
	t.Helper()
	for _, fixtureCase := range op.Cases {
		if fixtureCase.Name == name {
			return fixtureCase
		}
	}
	t.Fatalf("operation %s missing case %s", op.OperationID, name)
	return iamConformanceCase{}
}

func loadIAMConformanceFixture(t *testing.T) iamConformanceFixture {
	t.Helper()
	path := runfilePath(t, "src/smithy/models/verself/iam-conformance.fixtures.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fixture iamConformanceFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func runfilePath(t *testing.T, relativePath string) string {
	t.Helper()
	if _, err := os.Stat(relativePath); err == nil {
		return relativePath
	}
	if dir := os.Getenv("TEST_SRCDIR"); dir != "" {
		if workspace := os.Getenv("TEST_WORKSPACE"); workspace != "" {
			path := filepath.Join(dir, workspace, relativePath)
			if _, err := os.Stat(path); err == nil {
				return path
			}
		}
		path := filepath.Join(dir, relativePath)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return relativePath
}

func assertCanonicalJSON(t *testing.T, label string, actual any, expected any) {
	t.Helper()
	actualJSON, err := json.Marshal(actual)
	if err != nil {
		t.Fatal(err)
	}
	expectedJSON, err := json.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actualJSON, expectedJSON) {
		var actualValue any
		var expectedValue any
		if err := json.Unmarshal(actualJSON, &actualValue); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(expectedJSON, &expectedValue); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(actualValue, expectedValue) {
			t.Fatalf("%s = %s, want %s", label, string(actualJSON), string(expectedJSON))
		}
	}
}

func nilIfTypedNil(input any) any {
	if input == nil {
		return nil
	}
	value := reflect.ValueOf(input)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		if value.IsNil() {
			return nil
		}
	}
	return input
}

func stringInput(input map[string]any, name string) string {
	value, _ := input[name].(string)
	return value
}

func intInput(input map[string]any, name string) int64 {
	switch value := input[name].(type) {
	case float64:
		return int64(value)
	case int64:
		return value
	case int:
		return int64(value)
	default:
		return 0
	}
}
