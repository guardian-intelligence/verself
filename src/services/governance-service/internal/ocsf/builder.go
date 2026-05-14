package ocsf

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"
)

type Builder struct {
	Product Product
}

type APIActivityInput struct {
	OrgID        string
	EventUID     uuid.UUID
	Time         time.Time
	Operation    Operation
	Actor        ActorIdentity
	Resources    []Resource
	Request      Request
	Response     Response
	Decision     AuthorizationDecision
	Status       Status
	StatusCode   string
	StatusDetail string
	Trace        TraceContext
	HMACKeyID    string
	Unmapped     map[string]any
}

type Operation struct {
	Service    string
	ID         string
	EventCode  string
	Action     string
	Permission string
	Version    string
}

type ActorIdentity struct {
	Type          string
	UID           string
	DisplayName   string
	Email         string
	CredentialUID string
}

type Resource struct {
	Type     string
	UID      string
	Name     string
	FullName string
	Role     string
	RoleID   uint8
}

type Request struct {
	UID           string
	Method        string
	Route         string
	SafeParams    string
	UserAgent     string
	XForwardedFor string
	Referrer      string
	Host          string
	Scheme        string
	ClientIP      string
	SourceName    string
}

type Response struct {
	Code    uint16
	Message string
	Status  string
}

type AuthorizationDecision string

const (
	AuthorizationAllowed AuthorizationDecision = "Allowed"
	AuthorizationDenied  AuthorizationDecision = "Denied"
)

type Status string

const (
	StatusSuccess Status = "Success"
	StatusFailure Status = "Failure"
	StatusOther   Status = "Other"
)

type TraceContext struct {
	TraceID string
	SpanID  string
	Service string
}

func (b Builder) Build(ctx context.Context, input APIActivityInput) (BuildOutput, error) {
	input = normalizeInput(ctx, b.Product, input)
	if err := validate(input); err != nil {
		return BuildOutput{}, err
	}
	activityID, activityName := activityFromAction(input.Operation.Action)
	actionID, actionName := actionFromDecision(input.Decision)
	statusID, statusName := statusFromStatus(input.Status)
	resources := normalizeResources(input.Resources)
	primary := primaryResource(resources)
	unmapped, err := unmappedJSON(input, primary)
	if err != nil {
		return BuildOutput{}, err
	}
	event := APIActivity{
		ActivityId:     activityID,
		ActivityName:   stringPtr(activityName),
		Action:         stringPtr(actionName),
		ActionId:       int32Ptr(actionID),
		Actor:          actorObject(input.Actor),
		Api:            apiObject(input.Operation, input.Request, input.Response),
		CategoryName:   stringPtr(CategoryNameApplication),
		CategoryUid:    CategoryUIDApplicationActivity,
		ClassName:      stringPtr(ClassNameAPIActivity),
		ClassUid:       ClassUIDAPIActivity,
		HttpRequest:    httpRequest(input.Request),
		HttpResponse:   httpResponse(input.Response),
		Metadata:       metadataObject(b.Product, input),
		Resources:      ocsfResources(resources),
		Severity:       stringPtr(SeverityInformational),
		SeverityId:     SeverityIDInformational,
		SrcEndpoint:    sourceEndpoint(input.Request),
		Status:         stringPtr(statusName),
		StatusCode:     stringPtr(input.StatusCode),
		StatusDetail:   stringPtrNonEmpty(input.StatusDetail),
		StatusId:       int32Ptr(statusID),
		Time:           unixMillis(input.Time),
		TimezoneOffset: int32Ptr(0),
		TypeName:       stringPtr(fmt.Sprintf("%s: %s", ClassNameAPIActivity, activityName)),
		TypeUid:        int64(ClassUIDAPIActivity)*100 + int64(activityID),
		Unmapped:       unmapped,
	}
	body, err := CanonicalJSON(event)
	if err != nil {
		return BuildOutput{}, err
	}
	sum := sha256.Sum256(body)
	return BuildOutput{
		Event:     event,
		JSON:      string(body),
		SHA256Hex: hex.EncodeToString(sum[:]),
		Resources: resources,
	}, nil
}

func normalizeInput(ctx context.Context, product Product, input APIActivityInput) APIActivityInput {
	input.OrgID = strings.TrimSpace(input.OrgID)
	if input.EventUID == uuid.Nil {
		input.EventUID = uuid.New()
	}
	if input.Time.IsZero() {
		input.Time = time.Now().UTC()
	}
	input.Time = input.Time.UTC()
	input.Operation.Service = strings.TrimSpace(input.Operation.Service)
	input.Operation.ID = strings.TrimSpace(input.Operation.ID)
	input.Operation.EventCode = strings.TrimSpace(input.Operation.EventCode)
	input.Operation.Action = strings.TrimSpace(input.Operation.Action)
	input.Operation.Permission = strings.TrimSpace(input.Operation.Permission)
	input.Operation.Version = strings.TrimSpace(input.Operation.Version)
	input.Actor.Type = firstNonEmpty(input.Actor.Type, "user")
	input.Actor.UID = strings.TrimSpace(input.Actor.UID)
	input.Actor.DisplayName = strings.TrimSpace(input.Actor.DisplayName)
	input.Actor.Email = strings.TrimSpace(input.Actor.Email)
	input.Actor.CredentialUID = strings.TrimSpace(input.Actor.CredentialUID)
	input.Request.UID = strings.TrimSpace(input.Request.UID)
	input.Request.Method = strings.ToUpper(strings.TrimSpace(input.Request.Method))
	input.Request.Route = strings.TrimSpace(input.Request.Route)
	input.Request.SafeParams = strings.TrimSpace(input.Request.SafeParams)
	input.Request.UserAgent = strings.TrimSpace(input.Request.UserAgent)
	input.Request.XForwardedFor = strings.TrimSpace(input.Request.XForwardedFor)
	input.Request.Referrer = strings.TrimSpace(input.Request.Referrer)
	input.Request.Host = strings.TrimSpace(input.Request.Host)
	input.Request.Scheme = firstNonEmpty(input.Request.Scheme, "https")
	input.Request.ClientIP = strings.TrimSpace(input.Request.ClientIP)
	input.Request.SourceName = strings.TrimSpace(input.Request.SourceName)
	input.Response.Message = strings.TrimSpace(input.Response.Message)
	input.Response.Status = strings.TrimSpace(input.Response.Status)
	input.StatusCode = strings.TrimSpace(input.StatusCode)
	input.StatusDetail = strings.TrimSpace(input.StatusDetail)
	if input.Decision == "" {
		input.Decision = AuthorizationAllowed
	}
	if input.Status == "" {
		if input.Response.Code >= 200 && input.Response.Code < 400 {
			input.Status = StatusSuccess
		} else {
			input.Status = StatusFailure
		}
	}
	if input.StatusCode == "" && input.Response.Code > 0 {
		input.StatusCode = fmt.Sprintf("%d", input.Response.Code)
	}
	if input.Trace.TraceID == "" || input.Trace.SpanID == "" {
		sc := trace.SpanContextFromContext(ctx)
		if input.Trace.TraceID == "" && sc.HasTraceID() {
			input.Trace.TraceID = sc.TraceID().String()
		}
		if input.Trace.SpanID == "" && sc.HasSpanID() {
			input.Trace.SpanID = sc.SpanID().String()
		}
	}
	input.Trace.Service = firstNonEmpty(input.Trace.Service, input.Operation.Service, stringValue(product.Name))
	return input
}

func validate(input APIActivityInput) error {
	if input.OrgID == "" {
		return fmt.Errorf("org_id is required")
	}
	if input.Operation.Service == "" || input.Operation.ID == "" || input.Operation.Action == "" {
		return fmt.Errorf("api service, operation, and action are required")
	}
	if input.Actor.Type == "" || input.Actor.UID == "" {
		return fmt.Errorf("actor type and uid are required")
	}
	if len(input.Resources) == 0 {
		return fmt.Errorf("at least one resource is required")
	}
	if input.Request.Method == "" || input.Request.Route == "" {
		return fmt.Errorf("http method and route are required")
	}
	if input.Response.Code == 0 {
		return fmt.Errorf("http response code is required")
	}
	return nil
}

func actorObject(identity ActorIdentity) Actor {
	switch strings.TrimSpace(identity.Type) {
	case "workload", "service_workload", "service_delegated", "sandbox_execution", "service":
		return Actor{AppName: stringPtr(firstNonEmpty(identity.DisplayName, identity.Type)), AppUid: stringPtr(identity.UID)}
	default:
		userTypeID := int32(1)
		if strings.Contains(identity.Type, "service") {
			userTypeID = 4
		}
		return Actor{User: &User{
			CredentialUid: stringPtrNonEmpty(identity.CredentialUID),
			DisplayName:   stringPtrNonEmpty(identity.DisplayName),
			EmailAddr:     stringPtrNonEmpty(identity.Email),
			Name:          stringPtr(firstNonEmpty(identity.DisplayName, identity.UID)),
			Type:          stringPtr(identity.Type),
			TypeId:        int32Ptr(userTypeID),
			Uid:           stringPtr(identity.UID),
		}}
	}
}

func apiObject(operation Operation, request Request, response Response) API {
	api := API{
		Operation: operation.ID,
		Service:   &Service{Name: stringPtr(operation.Service)},
		Version:   stringPtrNonEmpty(operation.Version),
	}
	if request.UID != "" {
		api.Request = &RequestElements{Uid: request.UID}
	}
	if response.Code != 0 {
		code := int32(response.Code)
		api.Response = &ResponseElements{Code: &code}
	}
	return api
}

func httpRequest(request Request) *HTTPRequest {
	return &HTTPRequest{
		Args:          stringPtrNonEmpty(request.SafeParams),
		HttpMethod:    stringPtr(request.Method),
		Referrer:      stringPtrNonEmpty(request.Referrer),
		Uid:           stringPtrNonEmpty(request.UID),
		Url:           requestURL(request),
		UserAgent:     stringPtrNonEmpty(request.UserAgent),
		Version:       stringPtr("HTTP/1.1"),
		XForwardedFor: splitHeaderList(request.XForwardedFor),
	}
}

func requestURL(request Request) *UniformResourceLocator {
	if request.Route == "" {
		return nil
	}
	host := request.Host
	if host == "" {
		return &UniformResourceLocator{Path: stringPtr(request.Route), QueryString: stringPtrNonEmpty(request.SafeParams)}
	}
	u := url.URL{Scheme: firstNonEmpty(request.Scheme, "https"), Host: host, Path: request.Route, RawQuery: request.SafeParams}
	hostname := host
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		hostname = parsedHost
	}
	return &UniformResourceLocator{
		Hostname:    stringPtr(hostname),
		Path:        stringPtr(request.Route),
		QueryString: stringPtrNonEmpty(request.SafeParams),
		Scheme:      stringPtr(u.Scheme),
		UrlString:   stringPtr(u.String()),
	}
}

func httpResponse(response Response) *HTTPResponse {
	return &HTTPResponse{Code: int32(response.Code), Message: stringPtrNonEmpty(response.Message), Status: stringPtrNonEmpty(response.Status)}
}

func metadataObject(product Product, input APIActivityInput) Metadata {
	originalTime := input.Time.Format(time.RFC3339Nano)
	return Metadata{
		EventCode:    stringPtrNonEmpty(input.Operation.EventCode),
		LogName:      stringPtr("verself_governance_api_activity"),
		LogProvider:  stringPtr("governance-service"),
		LoggedTime:   unixMillis(input.Time),
		OriginalTime: &originalTime,
		Product:      product,
		Profiles:     []string{"trace"},
		TenantUid:    stringPtr(input.OrgID),
		Uid:          stringPtr(input.EventUID.String()),
		Version:      SchemaVersion,
	}
}

func sourceEndpoint(request Request) NetworkEndpoint {
	return NetworkEndpoint{
		Ip:      stringPtrNonEmpty(request.ClientIP),
		Name:    stringPtrNonEmpty(request.SourceName),
		SvcName: stringPtrNonEmpty(request.SourceName),
		Uid:     stringPtrNonEmpty(firstNonEmpty(request.ClientIP, request.SourceName)),
	}
}

func normalizeResources(resources []Resource) []ActivityResource {
	out := make([]ActivityResource, 0, len(resources))
	for index, resource := range resources {
		item := ActivityResource{
			Type:     strings.TrimSpace(resource.Type),
			UID:      strings.TrimSpace(resource.UID),
			Name:     strings.TrimSpace(resource.Name),
			FullName: strings.TrimSpace(resource.FullName),
			Role:     strings.TrimSpace(resource.Role),
			RoleID:   resource.RoleID,
		}
		if item.RoleID == 0 {
			if index == 0 {
				item.RoleID = 1
				item.Role = "Target"
			} else {
				item.RoleID = 4
				item.Role = "Related"
			}
		}
		if item.Role == "" {
			item.Role = resourceRole(item.RoleID)
		}
		if item.Name == "" {
			item.Name = firstNonEmpty(item.FullName, item.UID, item.Type)
		}
		if item.Type == "" && item.UID == "" && item.Name == "" && item.FullName == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

func primaryResource(resources []ActivityResource) ActivityResource {
	for _, resource := range resources {
		if resource.RoleID == 1 {
			return resource
		}
	}
	if len(resources) == 0 {
		return ActivityResource{}
	}
	return resources[0]
}

func ocsfResources(resources []ActivityResource) []ResourceDetails {
	out := make([]ResourceDetails, 0, len(resources))
	for _, resource := range resources {
		out = append(out, ResourceDetails{
			Data:      resourceData(resource),
			Name:      stringPtrNonEmpty(firstNonEmpty(resource.FullName, resource.Name)),
			Namespace: stringPtrNonEmpty(resource.Type),
			Type:      stringPtrNonEmpty(resource.Type),
			Uid:       stringPtrNonEmpty(resource.UID),
		})
	}
	return out
}

func resourceData(resource ActivityResource) *string {
	data := compactMap(map[string]any{
		"full_name": resource.FullName,
		"role":      resource.Role,
		"role_id":   resource.RoleID,
	})
	if len(data) == 0 {
		return nil
	}
	body, err := CanonicalJSON(data)
	if err != nil {
		return nil
	}
	return stringPtr(string(body))
}

func unmappedJSON(input APIActivityInput, primary ActivityResource) (*string, error) {
	out := compactMap(input.Unmapped)
	if input.Operation.Permission != "" {
		out["verself.permission"] = input.Operation.Permission
	}
	if primary.FullName != "" {
		out["verself.primary_resource_name"] = primary.FullName
	}
	if input.HMACKeyID != "" {
		out["verself.hmac_key_id"] = input.HMACKeyID
	}
	if input.Trace.TraceID != "" {
		// go-ocsf v1.5 does not expose API Activity's Trace profile field yet.
		traceValue := map[string]any{
			"uid":     input.Trace.TraceID,
			"service": input.Trace.Service,
		}
		if input.Trace.SpanID != "" {
			traceValue["span"] = map[string]any{
				"uid":       input.Trace.SpanID,
				"operation": input.Operation.ID,
				"service":   input.Trace.Service,
			}
		}
		out["trace"] = traceValue
	}
	if len(out) == 0 {
		return nil, nil
	}
	body, err := CanonicalJSON(out)
	if err != nil {
		return nil, fmt.Errorf("marshal OCSF unmapped data: %w", err)
	}
	return stringPtr(string(body)), nil
}

func compactMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		switch typed := value.(type) {
		case string:
			if strings.TrimSpace(typed) != "" {
				out[key] = typed
			}
		case nil:
		default:
			out[key] = value
		}
	}
	return out
}

func activityFromAction(action string) (int32, string) {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "create", "register", "invite", "ingest":
		return ActivityIDCreate, "Create"
	case "read", "list", "query", "search", "download", "export", "resolve", "decrypt", "verify":
		return ActivityIDRead, "Read"
	case "update", "write", "set", "archive", "restore", "roll", "rotate", "pause", "resume", "cancel", "connect", "encrypt", "sign", "sync", "test", "manage":
		return ActivityIDUpdate, "Update"
	case "delete", "erase", "revoke", "disable":
		return ActivityIDDelete, "Delete"
	default:
		return ActivityIDOther, "Other"
	}
}

func actionFromDecision(decision AuthorizationDecision) (int32, string) {
	switch decision {
	case AuthorizationDenied:
		return ActionIDDenied, "Denied"
	default:
		return ActionIDAllowed, "Allowed"
	}
}

func statusFromStatus(status Status) (int32, string) {
	switch status {
	case StatusFailure:
		return StatusIDFailure, "Failure"
	case StatusOther:
		return StatusIDOther, "Other"
	default:
		return StatusIDSuccess, "Success"
	}
}

func resourceRole(roleID uint8) string {
	switch roleID {
	case 1:
		return "Target"
	case 2:
		return "Actor"
	case 3:
		return "Affected"
	case 4:
		return "Related"
	default:
		return ""
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func unixMillis(t time.Time) int64 {
	return t.UTC().UnixNano() / int64(time.Millisecond)
}

func stringPtr(value string) *string {
	return &value
}

func stringPtrNonEmpty(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func int32Ptr(value int32) *int32 {
	return &value
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func splitHeaderList(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
