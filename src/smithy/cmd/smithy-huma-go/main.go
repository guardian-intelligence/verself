package main

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/format"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

const (
	traitAudit            = "verself.common.v1#audit"
	traitAuditEvent       = "verself.common.v1#auditEvent"
	traitAuthz            = "verself.common.v1#authz"
	traitConformance      = "verself.common.v1#conformance"
	traitDocumentation    = "smithy.api#documentation"
	traitEnumValue        = "smithy.api#enumValue"
	traitHTTP             = "smithy.api#http"
	traitHTTPHeader       = "smithy.api#httpHeader"
	traitHTTPLabel        = "smithy.api#httpLabel"
	traitHTTPQuery        = "smithy.api#httpQuery"
	traitIdentity         = "verself.common.v1#identity"
	traitIdempotencyToken = "smithy.api#idempotencyToken"
	traitInput            = "smithy.api#input"
	traitLength           = "smithy.api#length"
	traitNestedProperties = "smithy.api#nestedProperties"
	traitOutput           = "smithy.api#output"
	traitPaginated        = "smithy.api#paginated"
	traitPattern          = "smithy.api#pattern"
	traitPermission       = "verself.common.v1#permission"
	traitRange            = "smithy.api#range"
	traitRateLimit        = "verself.common.v1#rateLimit"
	traitReadonly         = "smithy.api#readonly"
	traitRequestBudget    = "verself.common.v1#requestBudget"
	traitRequired         = "smithy.api#required"
	traitSDK              = "verself.common.v1#sdk"
	traitServiceRuntime   = "verself.common.v1#serviceRuntime"
)

type smithyModel struct {
	Shapes map[string]*smithyShape `json:"shapes"`
}

type smithyShape struct {
	Type        string                     `json:"type"`
	Traits      map[string]json.RawMessage `json:"traits,omitempty"`
	Members     map[string]*smithyMember   `json:"members,omitempty"`
	Mixins      []targetRef                `json:"mixins,omitempty"`
	Member      *smithyMember              `json:"member,omitempty"`
	Input       *targetRef                 `json:"input,omitempty"`
	Output      *targetRef                 `json:"output,omitempty"`
	Errors      []targetRef                `json:"errors,omitempty"`
	Resources   []targetRef                `json:"resources,omitempty"`
	Operations  []targetRef                `json:"operations,omitempty"`
	List        *targetRef                 `json:"list,omitempty"`
	Read        *targetRef                 `json:"read,omitempty"`
	Create      *targetRef                 `json:"create,omitempty"`
	Put         *targetRef                 `json:"put,omitempty"`
	Update      *targetRef                 `json:"update,omitempty"`
	Delete      *targetRef                 `json:"delete,omitempty"`
	Identifiers map[string]*smithyMember   `json:"identifiers,omitempty"`
	Properties  map[string]*smithyMember   `json:"properties,omitempty"`
}

type smithyMember struct {
	Target string                     `json:"target"`
	Traits map[string]json.RawMessage `json:"traits,omitempty"`
}

type targetRef struct {
	Target string `json:"target"`
}

type operationSpec struct {
	ShapeID       string
	Name          string
	GoName        string
	OperationID   string
	Method        string
	Path          string
	Summary       string
	InputID       string
	OutputID      string
	Errors        []string
	Identity      identitySpec
	Authz         authzSpec
	Audit         auditSpec
	RateLimit     string
	RequestBytes  int64
	Idempotency   idempotencySpec
	Conformance   []string
	SDK           sdkSpec
	Readonly      bool
	Paginated     bool
	DefaultStatus int
}

type identitySpec struct {
	Mode       string
	Audience   string
	Principals []string
}

type authzSpec struct {
	Permission         string
	OrganizationSource string
	OrganizationMember string
}

type auditSpec struct {
	Event    string
	Resource string
	Action   string
}

type idempotencySpec struct {
	Policy string
	Header string
	Member string
}

type sdkSpec struct {
	Module    string
	Method    string
	Paginated bool
	Retryable bool
}

type memberSpec struct {
	Name      string
	GoName    string
	Target    string
	Type      string
	Tags      []string
	Required  bool
	JSONName  string
	BodyField bool
}

func main() {
	var modelTar string
	var serviceID string
	var mode string
	var packageName string
	var outPath string
	flag.StringVar(&modelTar, "model-tar", "", "Smithy build tar containing source/model/model.json")
	flag.StringVar(&serviceID, "service", "", "Smithy service shape ID")
	flag.StringVar(&mode, "mode", "huma", "generation mode: huma or identity-catalog")
	flag.StringVar(&packageName, "package", "", "generated Go package name")
	flag.StringVar(&outPath, "out", "", "output file")
	flag.Parse()

	if strings.TrimSpace(modelTar) == "" || strings.TrimSpace(serviceID) == "" || strings.TrimSpace(outPath) == "" {
		fail(errors.New("-model-tar, -service, and -out are required"))
	}
	model, err := readModelFromTar(modelTar)
	if err != nil {
		fail(err)
	}
	service := model.shape(serviceID)
	if service == nil || service.Type != "service" {
		fail(fmt.Errorf("service shape %q not found", serviceID))
	}
	ops, err := model.serviceOperations(serviceID)
	if err != nil {
		fail(err)
	}
	if len(ops) == 0 {
		fail(fmt.Errorf("service %s has no operations", serviceID))
	}

	var src []byte
	switch mode {
	case "huma":
		if packageName == "" {
			packageName = "contractapi"
		}
		src, err = generateHuma(model, serviceID, packageName, ops)
	case "identity-catalog":
		if packageName == "" {
			packageName = "identity"
		}
		src, err = generateIdentityCatalog(model, serviceID, packageName, ops)
	default:
		err = fmt.Errorf("unsupported mode %q", mode)
	}
	if err != nil {
		fail(err)
	}
	formatted, err := format.Source(src)
	if err != nil {
		_, _ = os.Stderr.Write(src)
		fail(fmt.Errorf("format generated Go: %w", err))
	}
	if err := os.WriteFile(outPath, formatted, 0o644); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func readModelFromTar(path string) (*smithyModel, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	tr := tar.NewReader(f)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		name := strings.TrimPrefix(hdr.Name, "./")
		if name != "source/model/model.json" {
			continue
		}
		var model smithyModel
		if err := json.NewDecoder(tr).Decode(&model); err != nil {
			return nil, err
		}
		return &model, nil
	}
	return nil, fmt.Errorf("source/model/model.json not found in %s", path)
}

func (m *smithyModel) shape(id string) *smithyShape {
	if m == nil {
		return nil
	}
	return m.Shapes[id]
}

func (m *smithyModel) serviceOperations(serviceID string) ([]operationSpec, error) {
	service := m.shape(serviceID)
	if service == nil {
		return nil, fmt.Errorf("service %s not found", serviceID)
	}
	seenResources := map[string]bool{}
	seenOps := map[string]bool{}
	var ids []string
	var collectResource func(string) error
	collectResource = func(resourceID string) error {
		if seenResources[resourceID] {
			return nil
		}
		seenResources[resourceID] = true
		resource := m.shape(resourceID)
		if resource == nil || resource.Type != "resource" {
			return fmt.Errorf("resource %s not found", resourceID)
		}
		for _, ref := range []targetRefFromPtr{asRef(resource.List), asRef(resource.Read), asRef(resource.Create), asRef(resource.Put), asRef(resource.Update), asRef(resource.Delete)} {
			if ref.target == "" {
				continue
			}
			if !seenOps[ref.target] {
				seenOps[ref.target] = true
				ids = append(ids, ref.target)
			}
		}
		for _, ref := range resource.Operations {
			if !seenOps[ref.Target] {
				seenOps[ref.Target] = true
				ids = append(ids, ref.Target)
			}
		}
		for _, ref := range resource.Resources {
			if err := collectResource(ref.Target); err != nil {
				return err
			}
		}
		return nil
	}
	for _, ref := range service.Resources {
		if err := collectResource(ref.Target); err != nil {
			return nil, err
		}
	}
	sort.Strings(ids)
	ops := make([]operationSpec, 0, len(ids))
	for _, id := range ids {
		op, err := m.operationSpec(id)
		if err != nil {
			return nil, err
		}
		ops = append(ops, op)
	}
	sort.Slice(ops, func(i, j int) bool {
		if ops[i].Path == ops[j].Path {
			return ops[i].Method < ops[j].Method
		}
		return ops[i].Path < ops[j].Path
	})
	return ops, nil
}

type targetRefFromPtr struct {
	target string
}

func asRef(ref *targetRef) targetRefFromPtr {
	if ref == nil {
		return targetRefFromPtr{}
	}
	return targetRefFromPtr{target: ref.Target}
}

func (m *smithyModel) operationSpec(id string) (operationSpec, error) {
	shape := m.shape(id)
	if shape == nil || shape.Type != "operation" {
		return operationSpec{}, fmt.Errorf("operation %s not found", id)
	}
	httpTrait := objectTrait(shape.Traits, traitHTTP)
	method := stringFromMap(httpTrait, "method")
	path := stringFromMap(httpTrait, "uri")
	if method == "" || path == "" {
		return operationSpec{}, fmt.Errorf("operation %s missing @http method or uri", id)
	}
	name := localName(id)
	spec := operationSpec{
		ShapeID:       id,
		Name:          name,
		GoName:        goName(name),
		OperationID:   lowerKebab(name),
		Method:        method,
		Path:          path,
		Summary:       sentence(name),
		InputID:       requiredTarget(shape.Input, "input", id),
		OutputID:      requiredTarget(shape.Output, "output", id),
		DefaultStatus: statusFromHTTP(httpTrait),
		Readonly:      hasTrait(shape.Traits, traitReadonly),
	}
	spec.Paginated = hasTrait(shape.Traits, traitPaginated)
	for _, ref := range shape.Errors {
		spec.Errors = append(spec.Errors, ref.Target)
	}
	sort.Strings(spec.Errors)
	var err error
	spec.Identity, err = m.identitySpec(shape)
	if err != nil {
		return operationSpec{}, fmt.Errorf("%s: %w", id, err)
	}
	spec.Authz, err = m.authzSpec(shape)
	if err != nil {
		return operationSpec{}, fmt.Errorf("%s: %w", id, err)
	}
	spec.Audit, err = m.auditSpec(shape)
	if err != nil {
		return operationSpec{}, fmt.Errorf("%s: %w", id, err)
	}
	spec.RateLimit = stringFromMap(objectTrait(shape.Traits, traitRateLimit), "bucket")
	spec.RequestBytes = int64FromMap(objectTrait(shape.Traits, traitRequestBudget), "maxBytes")
	spec.Conformance = stringSliceFromMap(objectTrait(shape.Traits, traitConformance), "cases")
	spec.SDK = sdkFromTrait(objectTrait(shape.Traits, traitSDK))
	spec.Idempotency = m.idempotencySpec(spec.InputID)
	if spec.RateLimit == "" {
		return operationSpec{}, fmt.Errorf("%s missing @rateLimit bucket", id)
	}
	return spec, nil
}

func requiredTarget(ref *targetRef, label, owner string) string {
	if ref == nil || ref.Target == "" {
		fail(fmt.Errorf("%s missing %s target", owner, label))
	}
	return ref.Target
}

func statusFromHTTP(values map[string]any) int {
	status := intFromAny(values["code"])
	if status == 0 {
		return 200
	}
	return status
}

func (m *smithyModel) identitySpec(shape *smithyShape) (identitySpec, error) {
	values := objectTrait(shape.Traits, traitIdentity)
	out := identitySpec{
		Mode:       stringFromMap(values, "mode"),
		Audience:   stringFromMap(values, "audience"),
		Principals: stringSliceFromMap(values, "principals"),
	}
	if out.Mode == "" || out.Audience == "" || len(out.Principals) == 0 {
		return out, errors.New("missing @identity mode, audience, or principals")
	}
	return out, nil
}

func (m *smithyModel) authzSpec(shape *smithyShape) (authzSpec, error) {
	values := objectTrait(shape.Traits, traitAuthz)
	permissionID := stringFromMap(values, "permission")
	permissionShape := m.shape(permissionID)
	permission := stringFromMap(objectTrait(permissionShape.Traits, traitPermission), "name")
	org := objectFromMap(values, "organization")
	out := authzSpec{
		Permission:         permission,
		OrganizationSource: stringFromMap(org, "source"),
		OrganizationMember: stringFromMap(org, "member"),
	}
	if out.Permission == "" || out.OrganizationSource == "" {
		return out, errors.New("missing @authz permission or organization source")
	}
	return out, nil
}

func (m *smithyModel) auditSpec(shape *smithyShape) (auditSpec, error) {
	values := objectTrait(shape.Traits, traitAudit)
	eventID := stringFromMap(values, "event")
	eventShape := m.shape(eventID)
	event := stringFromMap(objectTrait(eventShape.Traits, traitAuditEvent), "name")
	resourceID := stringFromMap(values, "resource")
	action := stringFromMap(values, "action")
	out := auditSpec{
		Event:    event,
		Resource: lowerSnake(localName(resourceID)),
		Action:   action,
	}
	if out.Event == "" || out.Resource == "" || out.Action == "" {
		return out, errors.New("missing @audit event, resource, or action")
	}
	return out, nil
}

func (m *smithyModel) idempotencySpec(inputID string) idempotencySpec {
	input := m.shape(inputID)
	for name, member := range mergedMembers(m, input) {
		if hasTrait(member.Traits, traitIdempotencyToken) {
			header := traitString(member.Traits, traitHTTPHeader)
			if header == "" {
				continue
			}
			return idempotencySpec{
				Policy: "idempotency_key_header",
				Header: header,
				Member: name,
			}
		}
	}
	return idempotencySpec{}
}

func sdkFromTrait(values map[string]any) sdkSpec {
	return sdkSpec{
		Module:    stringFromMap(values, "module"),
		Method:    stringFromMap(values, "method"),
		Paginated: boolFromMap(values, "paginated"),
		Retryable: boolFromMap(values, "retryable"),
	}
}

func generateHuma(m *smithyModel, serviceID, packageName string, ops []operationSpec) ([]byte, error) {
	closure := shapeClosure(m, ops)
	scalars, enums, structs, lists := classifyShapes(m, closure)
	var b bytes.Buffer
	fmt.Fprintf(&b, "package %s\n\n", packageName)
	fmt.Fprintf(&b, "import (\n")
	fmt.Fprintf(&b, "\t\"context\"\n")
	fmt.Fprintf(&b, "\t\"github.com/danielgtaylor/huma/v2\"\n")
	fmt.Fprintf(&b, ")\n\n")
	fmt.Fprintf(&b, "// Code generated by //src/smithy/cmd/smithy-huma-go from %s. DO NOT EDIT.\n\n", serviceID)
	writeRuntimeTypes(&b)
	writeScalars(&b, m, scalars)
	writeEnums(&b, m, enums)
	writeLists(&b, m, lists)
	writeStructs(&b, m, structs)
	writeOutputBodies(&b, m, ops)
	writeDescriptors(&b, m, ops)
	writeRegistration(&b, ops)
	return b.Bytes(), nil
}

func generateIdentityCatalog(m *smithyModel, serviceID, packageName string, ops []operationSpec) ([]byte, error) {
	service := m.shape(serviceID)
	runtime := objectTrait(service.Traits, traitServiceRuntime)
	serviceName := stringFromMap(runtime, "serviceName")
	if serviceName == "" {
		return nil, fmt.Errorf("%s missing @serviceRuntime serviceName", serviceID)
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "package %s\n\n", packageName)
	fmt.Fprintf(&b, "import runtimeiam \"github.com/verself/service-runtime/iam\"\n\n")
	fmt.Fprintf(&b, "// Code generated by //src/smithy/cmd/smithy-huma-go from %s. DO NOT EDIT.\n\n", serviceID)
	fmt.Fprintf(&b, "func smithyIAMServiceOperations() ServiceOperations {\n")
	fmt.Fprintf(&b, "\treturn ServiceOperations{Service: %q, Operations: []Operation{\n", serviceName)
	for _, op := range ops {
		memberEligible := op.Audit.Action == "read" || op.Audit.Action == "list"
		orgScope := runtimeOrgScope(op.Authz.OrganizationSource)
		fmt.Fprintf(&b, "\t\t{OperationID: %q, Permission: runtimeiam.Permission(%q), Resource: runtimeiam.ResourceKind(%q), Action: runtimeiam.Action(%q), OrgScope: runtimeiam.OrgScope(%q), MemberEligible: %t},\n",
			op.OperationID, op.Authz.Permission, op.Audit.Resource, op.Audit.Action, orgScope, memberEligible)
	}
	fmt.Fprintf(&b, "\t}}\n")
	fmt.Fprintf(&b, "}\n")
	return b.Bytes(), nil
}

func writeRuntimeTypes(b *bytes.Buffer) {
	fmt.Fprint(b, `type OperationDescriptor struct {
	ShapeID             string
	OperationID         string
	Method              string
	Path                string
	DefaultStatus       int
	Readonly            bool
	Paginated           bool
	Identity            IdentityDescriptor
	Authorization       AuthorizationDescriptor
	Audit               AuditDescriptor
	RateLimitBucket     string
	RequestBodyMaxBytes int64
	Idempotency         IdempotencyDescriptor
	SDK                 SDKDescriptor
	ConformanceCases    []string
	Problems            []ProblemDescriptor
}

type IdentityDescriptor struct {
	Mode       string
	Audience   string
	Principals []string
}

type AuthorizationDescriptor struct {
	Permission         string
	OrganizationSource string
	OrganizationMember string
}

type AuditDescriptor struct {
	Event    string
	Resource string
	Action   string
}

type IdempotencyDescriptor struct {
	Policy string
	Header string
	Member string
}

type SDKDescriptor struct {
	Module    string
	Method    string
	Paginated bool
	Retryable bool
}

type ProblemDescriptor struct {
	ShapeID string
	Type    string
	Code    string
	Status  int
}

type Runtime interface {
	PrepareOperation(OperationDescriptor, huma.Operation) huma.Operation
	BeforeOperation(context.Context, OperationDescriptor, any) (any, error)
	AfterOperation(context.Context, OperationDescriptor, any, any, any, error)
}

`)
}

func writeScalars(b *bytes.Buffer, m *smithyModel, ids []string) {
	for _, id := range ids {
		shape := m.shape(id)
		goType := scalarGoType(shape.Type)
		if goType == "" {
			continue
		}
		fmt.Fprintf(b, "type %s %s\n\n", goName(localName(id)), goType)
	}
}

func writeEnums(b *bytes.Buffer, m *smithyModel, ids []string) {
	for _, id := range ids {
		name := goName(localName(id))
		fmt.Fprintf(b, "type %s string\n\n", name)
		shape := m.shape(id)
		keys := sortedKeys(shape.Members)
		if len(keys) > 0 {
			fmt.Fprintln(b, "const (")
			for _, key := range keys {
				member := shape.Members[key]
				value := traitString(member.Traits, traitEnumValue)
				if value == "" {
					value = key
				}
				fmt.Fprintf(b, "\t%s%s %s = %q\n", name, goName(strings.ToLower(key)), name, value)
			}
			fmt.Fprintln(b, ")\n")
		}
	}
}

func writeLists(b *bytes.Buffer, m *smithyModel, ids []string) {
	for _, id := range ids {
		shape := m.shape(id)
		if shape.Member == nil {
			continue
		}
		fmt.Fprintf(b, "type %s []%s\n\n", goName(localName(id)), goTypeForTarget(m, shape.Member.Target))
	}
}

func writeStructs(b *bytes.Buffer, m *smithyModel, ids []string) {
	for _, id := range ids {
		shape := m.shape(id)
		if shape == nil || shape.Type != "structure" {
			continue
		}
		if hasTrait(shape.Traits, traitOutput) {
			continue
		}
		if hasTrait(shape.Traits, traitInput) {
			transportMembers, bodyMembers := inputMemberSpecs(m, shape)
			if len(bodyMembers) > 0 {
				fmt.Fprintf(b, "type %sBody struct {\n", goName(localName(id)))
				for _, member := range bodyMembers {
					fmt.Fprintf(b, "\t%s %s `%s`\n", member.GoName, member.Type, strings.Join(member.Tags, " "))
				}
				fmt.Fprintln(b, "}\n")
			}
			fmt.Fprintf(b, "type %s struct {\n", goName(localName(id)))
			for _, member := range transportMembers {
				if len(member.Tags) == 0 {
					fmt.Fprintf(b, "\t%s %s\n", member.GoName, member.Type)
					continue
				}
				fmt.Fprintf(b, "\t%s %s `%s`\n", member.GoName, member.Type, strings.Join(member.Tags, " "))
			}
			if len(bodyMembers) > 0 {
				fmt.Fprintf(b, "\tBody %sBody\n", goName(localName(id)))
			}
			fmt.Fprintln(b, "}\n")
			continue
		}
		members := jsonMemberSpecsForStructure(m, shape)
		fmt.Fprintf(b, "type %s struct {\n", goName(localName(id)))
		for _, member := range members {
			fmt.Fprintf(b, "\t%s %s `%s`\n", member.GoName, member.Type, strings.Join(member.Tags, " "))
		}
		fmt.Fprintln(b, "}\n")
	}
}

func writeOutputBodies(b *bytes.Buffer, m *smithyModel, ops []operationSpec) {
	written := map[string]bool{}
	for _, op := range ops {
		output := m.shape(op.OutputID)
		if output == nil {
			continue
		}
		if nested := nestedOutputMember(output); nested != nil {
			fmt.Fprintf(b, "type %s struct {\n\tBody %s\n}\n\n", goName(localName(op.OutputID)), goTypeForTarget(m, nested.Target))
			continue
		}
		bodyName := goName(localName(op.OutputID)) + "Body"
		if !written[bodyName] {
			written[bodyName] = true
			fmt.Fprintf(b, "type %s struct {\n", bodyName)
			for _, member := range jsonMemberSpecsForStructure(m, output) {
				fmt.Fprintf(b, "\t%s %s `%s`\n", member.GoName, member.Type, strings.Join(member.Tags, " "))
			}
			fmt.Fprintln(b, "}\n")
		}
		fmt.Fprintf(b, "type %s struct {\n\tBody %s\n}\n\n", goName(localName(op.OutputID)), bodyName)
	}
}

func writeDescriptors(b *bytes.Buffer, m *smithyModel, ops []operationSpec) {
	fmt.Fprintln(b, "var Operations = []OperationDescriptor{")
	for _, op := range ops {
		fmt.Fprintf(b, "\t%sOperation,\n", op.GoName)
	}
	fmt.Fprintln(b, "}\n")
	for _, op := range ops {
		fmt.Fprintf(b, "var %sOperation = OperationDescriptor{\n", op.GoName)
		fmt.Fprintf(b, "\tShapeID: %q,\n", op.ShapeID)
		fmt.Fprintf(b, "\tOperationID: %q,\n", op.OperationID)
		fmt.Fprintf(b, "\tMethod: %q,\n", op.Method)
		fmt.Fprintf(b, "\tPath: %q,\n", op.Path)
		fmt.Fprintf(b, "\tDefaultStatus: %d,\n", op.DefaultStatus)
		fmt.Fprintf(b, "\tReadonly: %t,\n", op.Readonly)
		fmt.Fprintf(b, "\tPaginated: %t,\n", op.Paginated)
		fmt.Fprintf(b, "\tIdentity: IdentityDescriptor{Mode: %q, Audience: %q, Principals: %#v},\n", op.Identity.Mode, op.Identity.Audience, op.Identity.Principals)
		fmt.Fprintf(b, "\tAuthorization: AuthorizationDescriptor{Permission: %q, OrganizationSource: %q, OrganizationMember: %q},\n", op.Authz.Permission, op.Authz.OrganizationSource, op.Authz.OrganizationMember)
		fmt.Fprintf(b, "\tAudit: AuditDescriptor{Event: %q, Resource: %q, Action: %q},\n", op.Audit.Event, op.Audit.Resource, op.Audit.Action)
		fmt.Fprintf(b, "\tRateLimitBucket: %q,\n", op.RateLimit)
		fmt.Fprintf(b, "\tRequestBodyMaxBytes: %d,\n", op.RequestBytes)
		fmt.Fprintf(b, "\tIdempotency: IdempotencyDescriptor{Policy: %q, Header: %q, Member: %q},\n", op.Idempotency.Policy, op.Idempotency.Header, op.Idempotency.Member)
		fmt.Fprintf(b, "\tSDK: SDKDescriptor{Module: %q, Method: %q, Paginated: %t, Retryable: %t},\n", op.SDK.Module, op.SDK.Method, op.SDK.Paginated, op.SDK.Retryable)
		fmt.Fprintf(b, "\tConformanceCases: %#v,\n", op.Conformance)
		fmt.Fprintln(b, "\tProblems: []ProblemDescriptor{")
		for _, errID := range op.Errors {
			errShape := m.shape(errID)
			problem := objectTrait(errShape.Traits, "verself.common.v1#problem")
			status := traitInt(errShape.Traits, "smithy.api#httpError")
			if status == 0 {
				status = 500
			}
			fmt.Fprintf(b, "\t\t{ShapeID: %q, Type: %q, Code: %q, Status: %d},\n", errID, stringFromMap(problem, "type"), stringFromMap(problem, "code"), status)
		}
		fmt.Fprintln(b, "\t},")
		fmt.Fprintln(b, "}\n")
	}
}

func writeRegistration(b *bytes.Buffer, ops []operationSpec) {
	fmt.Fprintln(b, "type PublicHandlers interface {")
	for _, op := range ops {
		fmt.Fprintf(b, "\t%s(context.Context, *%s) (*%s, error)\n", op.GoName, goName(localName(op.InputID)), goName(localName(op.OutputID)))
	}
	fmt.Fprintln(b, "}\n")
	fmt.Fprintln(b, "func RegisterPublic(api huma.API, runtime Runtime, handlers PublicHandlers) {")
	fmt.Fprintln(b, "\tif runtime == nil { panic(\"contractapi: runtime is required\") }")
	fmt.Fprintln(b, "\tif handlers == nil { panic(\"contractapi: public handlers are required\") }")
	for _, op := range ops {
		fmt.Fprintf(b, "\tregister%s(api, runtime, handlers.%s)\n", op.GoName, op.GoName)
	}
	fmt.Fprintln(b, "}\n")
	for _, op := range ops {
		input := goName(localName(op.InputID))
		output := goName(localName(op.OutputID))
		fmt.Fprintf(b, "func register%s(api huma.API, runtime Runtime, handler func(context.Context, *%s) (*%s, error)) {\n", op.GoName, input, output)
		fmt.Fprintf(b, "\top := runtime.PrepareOperation(%sOperation, huma.Operation{OperationID: %sOperation.OperationID, Method: %sOperation.Method, Path: %sOperation.Path, Summary: %q, DefaultStatus: %sOperation.DefaultStatus})\n", op.GoName, op.GoName, op.GoName, op.GoName, op.Summary, op.GoName)
		fmt.Fprintf(b, "\thuma.Register(api, op, func(ctx context.Context, input *%s) (*%s, error) {\n", input, output)
		fmt.Fprintf(b, "\t\tidentity, err := runtime.BeforeOperation(ctx, %sOperation, input)\n", op.GoName)
		fmt.Fprintln(b, "\t\tif err != nil {")
		fmt.Fprintf(b, "\t\t\truntime.AfterOperation(ctx, %sOperation, identity, input, nil, err)\n", op.GoName)
		fmt.Fprintln(b, "\t\t\treturn nil, err")
		fmt.Fprintln(b, "\t\t}")
		fmt.Fprintln(b, "\t\toutput, err := handler(ctx, input)")
		fmt.Fprintf(b, "\t\truntime.AfterOperation(ctx, %sOperation, identity, input, output, err)\n", op.GoName)
		fmt.Fprintln(b, "\t\treturn output, err")
		fmt.Fprintln(b, "\t})")
		fmt.Fprintln(b, "}\n")
	}
}

func shapeClosure(m *smithyModel, ops []operationSpec) map[string]bool {
	seen := map[string]bool{}
	var visit func(string)
	visit = func(id string) {
		if id == "" || strings.HasPrefix(id, "smithy.api#") || seen[id] {
			return
		}
		shape := m.shape(id)
		if shape == nil {
			return
		}
		seen[id] = true
		for _, mixin := range shape.Mixins {
			visit(mixin.Target)
		}
		for _, member := range shape.Members {
			visit(member.Target)
		}
		if shape.Member != nil {
			visit(shape.Member.Target)
		}
	}
	for _, op := range ops {
		visit(op.InputID)
		visit(op.OutputID)
	}
	return seen
}

func classifyShapes(m *smithyModel, closure map[string]bool) (scalars, enums, structs, lists []string) {
	for id := range closure {
		shape := m.shape(id)
		if shape == nil {
			continue
		}
		switch shape.Type {
		case "string", "integer", "long", "boolean":
			scalars = append(scalars, id)
		case "enum":
			enums = append(enums, id)
		case "structure":
			structs = append(structs, id)
		case "list":
			lists = append(lists, id)
		}
	}
	sort.Strings(scalars)
	sort.Strings(enums)
	sort.Strings(structs)
	sort.Strings(lists)
	return scalars, enums, structs, lists
}

func jsonMemberSpecsForStructure(m *smithyModel, shape *smithyShape) []memberSpec {
	members := mergedMembers(m, shape)
	keys := sortedMemberKeys(members)
	out := make([]memberSpec, 0, len(keys))
	for _, key := range keys {
		member := members[key]
		spec := memberSpec{
			Name:     key,
			GoName:   goName(key),
			Target:   member.Target,
			Type:     goTypeForTarget(m, member.Target),
			Required: hasTrait(member.Traits, traitRequired),
			JSONName: key,
		}
		spec.Tags = append(spec.Tags, jsonTag(key, spec.Required))
		spec.Tags = append(spec.Tags, constraintTags(m.shape(member.Target), spec.Required)...)
		out = append(out, spec)
	}
	return out
}

func inputMemberSpecs(m *smithyModel, shape *smithyShape) ([]memberSpec, []memberSpec) {
	members := mergedMembers(m, shape)
	keys := sortedMemberKeys(members)
	transport := make([]memberSpec, 0, len(keys))
	body := make([]memberSpec, 0)
	for _, key := range keys {
		member := members[key]
		required := hasTrait(member.Traits, traitRequired)
		spec := memberSpec{
			Name:     key,
			GoName:   goName(key),
			Target:   member.Target,
			Type:     goTypeForTarget(m, member.Target),
			Required: required,
			JSONName: key,
		}
		switch {
		case hasTrait(member.Traits, traitHTTPLabel):
			spec.Tags = append(spec.Tags, fmt.Sprintf("path:%q", key))
			spec.Tags = append(spec.Tags, constraintTags(m.shape(member.Target), required)...)
			transport = append(transport, spec)
		case traitString(member.Traits, traitHTTPHeader) != "":
			spec.Tags = append(spec.Tags, fmt.Sprintf("header:%q", traitString(member.Traits, traitHTTPHeader)))
			spec.Tags = append(spec.Tags, constraintTags(m.shape(member.Target), required)...)
			transport = append(transport, spec)
		case traitString(member.Traits, traitHTTPQuery) != "":
			spec.Tags = append(spec.Tags, fmt.Sprintf("query:%q", traitString(member.Traits, traitHTTPQuery)))
			spec.Tags = append(spec.Tags, constraintTags(m.shape(member.Target), required)...)
			transport = append(transport, spec)
		default:
			spec.Tags = append(spec.Tags, jsonTag(key, required))
			spec.Tags = append(spec.Tags, constraintTags(m.shape(member.Target), required)...)
			body = append(body, spec)
		}
	}
	return transport, body
}

func mergedMembers(m *smithyModel, shape *smithyShape) map[string]*smithyMember {
	out := map[string]*smithyMember{}
	if shape == nil {
		return out
	}
	for _, mixin := range shape.Mixins {
		for name, member := range mergedMembers(m, m.shape(mixin.Target)) {
			out[name] = member
		}
	}
	for name, member := range shape.Members {
		out[name] = member
	}
	return out
}

func nestedOutputMember(shape *smithyShape) *smithyMember {
	keys := sortedKeys(shape.Members)
	for _, key := range keys {
		member := shape.Members[key]
		if hasTrait(member.Traits, traitNestedProperties) {
			return member
		}
	}
	return nil
}

func goTypeForTarget(m *smithyModel, target string) string {
	if strings.HasPrefix(target, "smithy.api#") {
		switch localName(target) {
		case "String":
			return "string"
		case "Integer":
			return "int"
		case "Long":
			return "int64"
		case "Boolean":
			return "bool"
		default:
			return "string"
		}
	}
	shape := m.shape(target)
	if shape == nil {
		return goName(localName(target))
	}
	switch shape.Type {
	case "string", "integer", "long", "boolean", "enum", "list", "structure":
		return goName(localName(target))
	default:
		return goName(localName(target))
	}
}

func scalarGoType(shapeType string) string {
	switch shapeType {
	case "string":
		return "string"
	case "integer":
		return "int"
	case "long":
		return "int64"
	case "boolean":
		return "bool"
	default:
		return ""
	}
}

func runtimeOrgScope(source string) string {
	switch source {
	case "token_org_id":
		return "token_org_id"
	case "token_role_assignments":
		return "token_role_assignment_org_ids"
	case "input_member":
		return "path_org_id"
	case "request_subject":
		return "token_subject"
	case "checkout_grant":
		return "checkout_grant"
	default:
		return source
	}
}

func jsonTag(name string, required bool) string {
	if required {
		return fmt.Sprintf("json:%q", name)
	}
	return fmt.Sprintf("json:%q", name+",omitempty")
}

func constraintTags(shape *smithyShape, required bool) []string {
	if shape == nil {
		return nil
	}
	var tags []string
	if required {
		tags = append(tags, `required:"true"`)
	}
	if values := objectTrait(shape.Traits, traitLength); len(values) > 0 {
		if min := intFromAny(values["min"]); min != 0 {
			tags = append(tags, fmt.Sprintf("minLength:%q", strconv.Itoa(min)))
		}
		if max := intFromAny(values["max"]); max != 0 {
			tags = append(tags, fmt.Sprintf("maxLength:%q", strconv.Itoa(max)))
		}
	}
	if values := objectTrait(shape.Traits, traitRange); len(values) > 0 {
		if min := intFromAny(values["min"]); min != 0 {
			tags = append(tags, fmt.Sprintf("minimum:%q", strconv.Itoa(min)))
		}
		if max := intFromAny(values["max"]); max != 0 {
			tags = append(tags, fmt.Sprintf("maximum:%q", strconv.Itoa(max)))
		}
	}
	if pattern := traitString(shape.Traits, traitPattern); pattern != "" {
		tags = append(tags, fmt.Sprintf("pattern:%q", pattern))
	}
	return tags
}

func objectTrait(traits map[string]json.RawMessage, id string) map[string]any {
	if len(traits) == 0 {
		return nil
	}
	raw, ok := traits[id]
	if !ok {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

func hasTrait(traits map[string]json.RawMessage, id string) bool {
	_, ok := traits[id]
	return ok
}

func traitString(traits map[string]json.RawMessage, id string) string {
	raw, ok := traits[id]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err == nil {
		return stringFromMap(obj, "value")
	}
	return ""
}

func traitInt(traits map[string]json.RawMessage, id string) int {
	raw, ok := traits[id]
	if !ok {
		return 0
	}
	var n int
	if err := json.Unmarshal(raw, &n); err == nil {
		return n
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err == nil {
		return intFromAny(obj["code"])
	}
	return 0
}

func stringFromMap(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}

func objectFromMap(values map[string]any, key string) map[string]any {
	if values == nil {
		return nil
	}
	obj, _ := values[key].(map[string]any)
	return obj
}

func stringSliceFromMap(values map[string]any, key string) []string {
	if values == nil {
		return nil
	}
	raw, ok := values[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, value := range raw {
		text, _ := value.(string)
		if strings.TrimSpace(text) != "" {
			out = append(out, strings.TrimSpace(text))
		}
	}
	return out
}

func int64FromMap(values map[string]any, key string) int64 {
	if values == nil {
		return 0
	}
	switch value := values[key].(type) {
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

func boolFromMap(values map[string]any, key string) bool {
	if values == nil {
		return false
	}
	value, _ := values[key].(bool)
	return value
}

func intFromAny(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case int64:
		return int(typed)
	default:
		return 0
	}
}

func localName(id string) string {
	if idx := strings.LastIndex(id, "#"); idx >= 0 {
		return id[idx+1:]
	}
	return id
}

func goName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	parts := strings.FieldsFunc(name, func(r rune) bool {
		return r == '_' || r == '-' || r == '.' || r == '/'
	})
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	out := strings.Join(parts, "")
	replacements := []struct{ old, new string }{
		{"Api", "API"},
		{"Iam", "IAM"},
		{"Oidc", "OIDC"},
		{"Url", "URL"},
		{"Http", "HTTP"},
		{"OrgId", "OrgID"},
		{"MemberId", "MemberID"},
		{"ResourceId", "ResourceID"},
		{"RequestId", "RequestID"},
	}
	for _, repl := range replacements {
		out = strings.ReplaceAll(out, repl.old, repl.new)
	}
	if strings.HasSuffix(out, "Id") {
		out = strings.TrimSuffix(out, "Id") + "ID"
	}
	return out
}

func lowerKebab(name string) string {
	return strings.ReplaceAll(lowerSnake(name), "_", "-")
}

var wordBoundary = regexp.MustCompile(`([a-z0-9])([A-Z])`)

func lowerSnake(name string) string {
	name = wordBoundary.ReplaceAllString(name, `${1}_${2}`)
	name = strings.ReplaceAll(name, "-", "_")
	name = strings.ReplaceAll(name, ".", "_")
	return strings.ToLower(name)
}

func sentence(name string) string {
	words := strings.Split(lowerSnake(name), "_")
	for i, word := range words {
		if i == 0 && word != "" {
			runes := []rune(word)
			runes[0] = unicode.ToUpper(runes[0])
			words[i] = string(runes)
		}
	}
	return strings.Join(words, " ")
}

func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedMemberKeys(m map[string]*smithyMember) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		return protoNumber(m[keys[i]]) < protoNumber(m[keys[j]])
	})
	return keys
}

func protoNumber(member *smithyMember) int {
	values := objectTrait(member.Traits, "verself.common.v1#protoField")
	n := intFromAny(values["number"])
	if n == 0 {
		return 1000000
	}
	return n
}
