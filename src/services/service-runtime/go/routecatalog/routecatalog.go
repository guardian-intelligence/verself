package routecatalog

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	runtimeiam "github.com/verself/service-runtime/iam"
)

const SchemaVersion = "verself.route-catalog.v1"

type Catalog struct {
	SchemaVersion string      `json:"schema_version"`
	Projection    string      `json:"projection"`
	Service       Service     `json:"service"`
	Operations    []Operation `json:"operations"`
}

type Service struct {
	ShapeID          string `json:"shape_id"`
	Name             string `json:"name"`
	Version          string `json:"version"`
	PublicAudience   string `json:"public_audience"`
	InternalAudience string `json:"internal_audience"`
}

type Operation struct {
	ShapeID       string        `json:"shape_id"`
	OperationID   string        `json:"operation_id"`
	Method        string        `json:"method"`
	Path          string        `json:"path"`
	DefaultStatus int           `json:"default_status"`
	InputShape    string        `json:"input_shape"`
	OutputShape   string        `json:"output_shape"`
	Effect        string        `json:"effect"`
	Paginated     bool          `json:"paginated"`
	Identity      Identity      `json:"identity"`
	Authorization Authorization `json:"authorization"`
	Audit         Audit         `json:"audit"`
	RateLimit     RateLimit     `json:"rate_limit"`
	RequestBody   RequestBody   `json:"request_body"`
	Idempotency   Idempotency   `json:"idempotency"`
	SDK           SDK           `json:"sdk"`
	Errors        []Problem     `json:"errors"`
}

type Identity struct {
	Mode       string   `json:"mode"`
	Audience   string   `json:"audience"`
	Principals []string `json:"principals"`
}

type Authorization struct {
	Permission         string `json:"permission"`
	OrganizationSource string `json:"organization_source"`
	OrganizationMember string `json:"organization_member"`
}

type Audit struct {
	Event         string `json:"event"`
	Resource      string `json:"resource"`
	Action        string `json:"action"`
	OCSFClassUID  int    `json:"ocsf_class_uid"`
	OCSFClassName string `json:"ocsf_class_name"`
}

type RateLimit struct {
	Bucket string `json:"bucket"`
}

type RequestBody struct {
	MaxBytes int64 `json:"max_bytes"`
}

type Idempotency struct {
	Policy string `json:"policy"`
	Header string `json:"header"`
	Member string `json:"member"`
}

type SDK struct {
	Module    string `json:"module"`
	Method    string `json:"method"`
	Paginated bool   `json:"paginated"`
	Retryable bool   `json:"retryable"`
}

type Problem struct {
	ShapeID string `json:"shape_id"`
	Type    string `json:"type"`
	Code    string `json:"code"`
	Status  int    `json:"status"`
}

func Parse(data []byte) (Catalog, error) {
	var catalog Catalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		return Catalog{}, fmt.Errorf("route catalog: decode: %w", err)
	}
	if err := catalog.Validate(); err != nil {
		return Catalog{}, err
	}
	return catalog, nil
}

func MustParse(data []byte) Catalog {
	catalog, err := Parse(data)
	if err != nil {
		panic(err)
	}
	return catalog
}

func (c Catalog) Validate() error {
	if strings.TrimSpace(c.SchemaVersion) != SchemaVersion {
		return fmt.Errorf("route catalog: unsupported schema_version %q", c.SchemaVersion)
	}
	if strings.TrimSpace(c.Service.ShapeID) == "" {
		return fmt.Errorf("route catalog: service shape_id is required")
	}
	if strings.TrimSpace(c.Service.Name) == "" {
		return fmt.Errorf("route catalog: service name is required")
	}
	seen := map[string]struct{}{}
	for _, operation := range c.Operations {
		if _, ok := seen[operation.OperationID]; ok {
			return fmt.Errorf("route catalog: duplicate operation_id %q", operation.OperationID)
		}
		seen[operation.OperationID] = struct{}{}
		if err := operation.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func (c Catalog) Operation(operationID string) (Operation, bool) {
	operationID = strings.TrimSpace(operationID)
	for _, operation := range c.Operations {
		if operation.OperationID == operationID {
			return operation, true
		}
	}
	return Operation{}, false
}

func (c Catalog) MustOperation(operationID string) Operation {
	operation, ok := c.Operation(operationID)
	if !ok {
		panic(fmt.Sprintf("route catalog: missing operation %q", operationID))
	}
	return operation
}

func (o Operation) Validate() error {
	operationID := strings.TrimSpace(o.OperationID)
	if operationID == "" {
		return fmt.Errorf("route catalog: operation_id is required")
	}
	if strings.TrimSpace(o.ShapeID) == "" {
		return fmt.Errorf("%s: shape_id is required", operationID)
	}
	if strings.TrimSpace(o.Method) == "" {
		return fmt.Errorf("%s: method is required", operationID)
	}
	if !strings.HasPrefix(strings.TrimSpace(o.Path), "/") {
		return fmt.Errorf("%s: path must be absolute", operationID)
	}
	if o.DefaultStatus < 100 || o.DefaultStatus > 599 {
		return fmt.Errorf("%s: default_status must be an HTTP status code", operationID)
	}
	if o.Effect != "read" && o.Effect != "write" {
		return fmt.Errorf("%s: effect must be read or write", operationID)
	}
	if strings.TrimSpace(o.Identity.Mode) == "" {
		return fmt.Errorf("%s: identity.mode is required", operationID)
	}
	if strings.TrimSpace(o.Identity.Audience) == "" && o.Identity.Mode != "public" {
		return fmt.Errorf("%s: identity.audience is required", operationID)
	}
	for _, problem := range o.Errors {
		if problem.Status < 100 || problem.Status > 599 {
			return fmt.Errorf("%s: problem %s has invalid status %d", operationID, problem.ShapeID, problem.Status)
		}
	}
	if err := o.OperationPolicy().ValidateHTTPOperation(o.Method, operationID); err != nil {
		return err
	}
	return nil
}

func (o Operation) OperationPolicy() runtimeiam.OperationPolicy {
	return runtimeiam.OperationPolicy{
		Permission:     runtimeiam.Permission(o.Authorization.Permission),
		Resource:       runtimeiam.ResourceKind(o.Audit.Resource),
		Action:         runtimeiam.Action(o.Audit.Action),
		OrgScope:       runtimeiam.OrgScope(runtimeOrgScope(o.Authorization.OrganizationSource)),
		RateLimitClass: runtimeiam.RateLimitClass(o.RateLimit.Bucket),
		Idempotency:    runtimeiam.IdempotencyPolicy(o.Idempotency.Policy),
		AuditEvent:     runtimeiam.AuditEvent(o.Audit.Event),
		BodyLimitBytes: o.RequestBody.MaxBytes,
	}
}

func (o Operation) ProblemStatuses() []int {
	statuses := make([]int, 0, len(o.Errors))
	for _, problem := range o.Errors {
		if problem.Status > 0 {
			statuses = append(statuses, problem.Status)
		}
	}
	sort.Ints(statuses)
	out := statuses[:0]
	previous := 0
	for _, status := range statuses {
		if status == previous {
			continue
		}
		out = append(out, status)
		previous = status
	}
	return out
}

func runtimeOrgScope(source string) string {
	switch source {
	case "request_subject":
		return string(runtimeiam.OrgScopeTokenSubject)
	case "input_member":
		return string(runtimeiam.OrgScopePathOrgID)
	default:
		return source
	}
}
