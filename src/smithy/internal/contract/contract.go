package contract

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

const IRVersion = "verself.contract-ir.v1"

type IR struct {
	IRVersion  string                    `json:"irVersion"`
	Package    string                    `json:"package"`
	Projection string                    `json:"projection"`
	Visibility string                    `json:"visibility"`
	Service    Service                   `json:"service"`
	Shapes     map[string]Shape          `json:"shapes"`
	Operations []Operation               `json:"operations"`
	Resources  []Resource                `json:"resources"`
	Problems   []Problem                 `json:"problems"`
	Catalogs   Catalogs                  `json:"catalogs"`
	Source     map[string]SourceLocation `json:"source"`
}

type Service struct {
	ShapeID   string         `json:"shapeId"`
	Namespace string         `json:"namespace"`
	Name      string         `json:"name"`
	Version   string         `json:"version"`
	Runtime   ServiceRuntime `json:"runtime"`
}

type ServiceRuntime struct {
	ServiceName      string `json:"serviceName"`
	PublicAudience   string `json:"publicAudience"`
	InternalAudience string `json:"internalAudience"`
}

type Shape struct {
	Kind        string      `json:"kind"`
	Name        string      `json:"name"`
	MediaType   string      `json:"mediaType"`
	Streaming   bool        `json:"streaming"`
	Sensitive   bool        `json:"sensitive"`
	Input       bool        `json:"input"`
	Output      bool        `json:"output"`
	Members     []Member    `json:"members"`
	Member      *ListMember `json:"member"`
	Key         *ListMember `json:"key"`
	Value       *ListMember `json:"value"`
	Enum        []EnumValue `json:"enum"`
	Constraints Constraints `json:"constraints"`
}

type Constraints struct {
	Length  Bound  `json:"length"`
	Range   Bound  `json:"range"`
	Pattern string `json:"pattern"`
}

type Bound struct {
	Min *int `json:"min"`
	Max *int `json:"max"`
}

type Member struct {
	Name                string       `json:"name"`
	GoName              string       `json:"-"`
	Target              string       `json:"target"`
	Type                string       `json:"-"`
	JSONName            string       `json:"jsonName"`
	Required            bool         `json:"required"`
	HTTPBinding         *HTTPBinding `json:"httpBinding"`
	ProtoField          *ProtoField  `json:"protoField"`
	NestedProperties    bool         `json:"nestedProperties"`
	IdempotencyToken    bool         `json:"idempotencyToken"`
	NotResourceProperty bool         `json:"notResourceProperty"`
	Sensitive           bool         `json:"sensitive"`
	Tags                []string     `json:"-"`
}

type ListMember struct {
	Target string `json:"target"`
}

type EnumValue struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type HTTPBinding struct {
	Location string `json:"location"`
	Name     string `json:"name"`
}

type ProtoField struct {
	Number int    `json:"number"`
	Name   string `json:"name"`
}

type Operation struct {
	ShapeID     string             `json:"shapeId"`
	Name        string             `json:"name"`
	OperationID string             `json:"operationId"`
	Readonly    bool               `json:"readonly"`
	Idempotent  bool               `json:"idempotent"`
	Paginated   bool               `json:"paginated"`
	HTTP        HTTPRoute          `json:"http"`
	Input       string             `json:"input"`
	Output      string             `json:"output"`
	Errors      []string           `json:"errors"`
	Bindings    HTTPBindingPlan    `json:"bindings"`
	Verself     OperationSemantics `json:"verself"`
	Problems    []Problem          `json:"problems"`
}

type HTTPRoute struct {
	Method        string `json:"method"`
	Path          string `json:"path"`
	SuccessStatus int    `json:"successStatus"`
}

type HTTPBindingPlan struct {
	Labels                  []BoundMember   `json:"labels"`
	Headers                 []BoundMember   `json:"headers"`
	Queries                 []BoundMember   `json:"queries"`
	DocumentMembers         []string        `json:"documentMembers"`
	Payload                 *PayloadBinding `json:"payload,omitempty"`
	ResponseHeaders         []BoundMember   `json:"responseHeaders"`
	ResponseDocumentMembers []string        `json:"responseDocumentMembers"`
	ResponsePayload         *PayloadBinding `json:"responsePayload,omitempty"`
}

type BoundMember struct {
	Member string `json:"member"`
	Name   string `json:"name"`
}

type PayloadBinding struct {
	Member    string `json:"member"`
	Target    string `json:"target"`
	Kind      string `json:"kind"`
	MediaType string `json:"mediaType"`
	Streaming bool   `json:"streaming"`
	Sensitive bool   `json:"sensitive"`
	Required  bool   `json:"required"`
}

type OperationSemantics struct {
	Identity      IdentityPolicy      `json:"identity"`
	Authz         AuthorizationPolicy `json:"authz"`
	Audit         AuditPolicy         `json:"audit"`
	RateLimit     RateLimitPolicy     `json:"rateLimit"`
	RequestBudget RequestBudget       `json:"requestBudget"`
	SDK           SDKPolicy           `json:"sdk"`
	Idempotency   IdempotencyPolicy   `json:"idempotency"`
}

type IdentityPolicy struct {
	Mode       string   `json:"mode"`
	Audience   string   `json:"audience"`
	Principals []string `json:"principals"`
}

type AuthorizationPolicy struct {
	Permission      string             `json:"permission"`
	PermissionShape string             `json:"permissionShape"`
	Organization    OrganizationPolicy `json:"organization"`
}

type OrganizationPolicy struct {
	Source string `json:"source"`
	Member string `json:"member"`
}

type AuditPolicy struct {
	Event         string `json:"event"`
	EventShape    string `json:"eventShape"`
	Resource      string `json:"resource"`
	ResourceShape string `json:"resourceShape"`
	Action        string `json:"action"`
}

type RateLimitPolicy struct {
	Bucket string `json:"bucket"`
}

type RequestBudget struct {
	MaxBytes int `json:"maxBytes"`
}

type SDKPolicy struct {
	Module    string `json:"module"`
	Method    string `json:"method"`
	Paginated bool   `json:"paginated"`
	Retryable bool   `json:"retryable"`
}

type IdempotencyPolicy struct {
	Policy string `json:"policy"`
	Header string `json:"header"`
	Member string `json:"member"`
}

type Resource struct {
	ShapeID     string            `json:"shapeId"`
	Name        string            `json:"name"`
	Identifiers map[string]string `json:"identifiers"`
	Properties  map[string]string `json:"properties"`
}

type Problem struct {
	ShapeID string `json:"shapeId"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Code    string `json:"code"`
	Status  int    `json:"status"`
}

type Catalogs struct {
	IAM           []IAMCatalogOperation `json:"iam"`
	Audit         []AuditCatalogEvent   `json:"audit"`
	Observability []ObservabilityItem   `json:"observability"`
}

type IAMCatalogOperation struct {
	OperationID    string `json:"operationId"`
	Permission     string `json:"permission"`
	Resource       string `json:"resource"`
	Action         string `json:"action"`
	OrgScope       string `json:"orgScope"`
	MemberEligible bool   `json:"memberEligible"`
}

type AuditCatalogEvent struct {
	OperationID   string `json:"operationId"`
	Event         string `json:"event"`
	Resource      string `json:"resource"`
	ResourceShape string `json:"resourceShape"`
	Action        string `json:"action"`
	OrgScope      string `json:"orgScope"`
}

type ObservabilityItem struct {
	OperationID     string `json:"operationId"`
	Service         string `json:"service"`
	HTTPMethod      string `json:"httpMethod"`
	HTTPRoute       string `json:"httpRoute"`
	Permission      string `json:"permission"`
	AuditEvent      string `json:"auditEvent"`
	RateLimitBucket string `json:"rateLimitBucket"`
	BodyBudgetBytes int64  `json:"bodyBudgetBytes"`
}

type SourceLocation struct {
	Filename string `json:"filename"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
}

func ReadFile(path string) (IR, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return IR{}, err
	}
	ir, err := Decode(data)
	if err != nil {
		return IR{}, err
	}
	return ir, nil
}

func Decode(data []byte) (IR, error) {
	var ir IR
	if err := json.Unmarshal(data, &ir); err != nil {
		return IR{}, err
	}
	if ir.IRVersion != IRVersion {
		return IR{}, fmt.Errorf("irVersion = %q, want %q", ir.IRVersion, IRVersion)
	}
	return ir, nil
}

func (ir IR) SortedOperations() []Operation {
	out := append([]Operation(nil), ir.Operations...)
	sort.Slice(out, func(i, j int) bool {
		return out[i].OperationID < out[j].OperationID
	})
	return out
}

func (ir IR) Shape(id string) (Shape, bool) {
	shape, ok := ir.Shapes[id]
	return shape, ok
}
