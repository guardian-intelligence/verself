package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/format"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

type contractIR struct {
	IRVersion  string               `json:"irVersion"`
	Package    string               `json:"package"`
	Projection string               `json:"projection"`
	Service    serviceSpec          `json:"service"`
	Shapes     map[string]shapeSpec `json:"shapes"`
	Operations []operationSpec      `json:"operations"`
	Problems   []problemSpec        `json:"problems"`
	Catalogs   catalogSpec          `json:"catalogs"`
}

type serviceSpec struct {
	ShapeID string             `json:"shapeId"`
	Name    string             `json:"name"`
	Version string             `json:"version"`
	Runtime serviceRuntimeSpec `json:"runtime"`
}

type serviceRuntimeSpec struct {
	ServiceName string `json:"serviceName"`
}

type shapeSpec struct {
	Kind        string          `json:"kind"`
	Name        string          `json:"name"`
	Constraints constraintsSpec `json:"constraints"`
	Sensitive   bool            `json:"sensitive"`
	Input       bool            `json:"input"`
	Output      bool            `json:"output"`
	Members     []memberSpec    `json:"members"`
	Member      *listMemberSpec `json:"member"`
	Enum        []enumValueSpec `json:"enum"`
}

type constraintsSpec struct {
	Length  boundSpec `json:"length"`
	Range   boundSpec `json:"range"`
	Pattern string    `json:"pattern"`
}

type boundSpec struct {
	Min *int `json:"min"`
	Max *int `json:"max"`
}

type memberSpec struct {
	Name                string `json:"name"`
	GoName              string
	Target              string `json:"target"`
	Type                string
	JSONName            string           `json:"jsonName"`
	Required            bool             `json:"required"`
	HTTPBinding         *httpBindingSpec `json:"httpBinding"`
	NestedProperties    bool             `json:"nestedProperties"`
	IdempotencyToken    bool             `json:"idempotencyToken"`
	NotResourceProperty bool             `json:"notResourceProperty"`
	ProtoField          *protoFieldSpec  `json:"protoField"`
	Tags                []string
}

type listMemberSpec struct {
	Target string `json:"target"`
}

type enumValueSpec struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type protoFieldSpec struct {
	Number int    `json:"number"`
	Name   string `json:"name"`
}

type httpBindingSpec struct {
	Location string `json:"location"`
	Name     string `json:"name"`
}

type operationSpec struct {
	ShapeID     string        `json:"shapeId"`
	Name        string        `json:"name"`
	OperationID string        `json:"operationId"`
	Readonly    bool          `json:"readonly"`
	Idempotent  bool          `json:"idempotent"`
	Paginated   bool          `json:"paginated"`
	HTTP        httpSpec      `json:"http"`
	Input       string        `json:"input"`
	Output      string        `json:"output"`
	Errors      []string      `json:"errors"`
	Bindings    bindingsSpec  `json:"bindings"`
	Verself     verselfSpec   `json:"verself"`
	Problems    []problemSpec `json:"problems"`
}

type httpSpec struct {
	Method        string `json:"method"`
	Path          string `json:"path"`
	SuccessStatus int    `json:"successStatus"`
}

type bindingsSpec struct {
	Labels          []bindingSpec `json:"labels"`
	Headers         []bindingSpec `json:"headers"`
	Queries         []bindingSpec `json:"queries"`
	DocumentMembers []string      `json:"documentMembers"`
}

type bindingSpec struct {
	Member string `json:"member"`
	Name   string `json:"name"`
}

type verselfSpec struct {
	Identity      identitySpec      `json:"identity"`
	Authz         authzSpec         `json:"authz"`
	Audit         auditSpec         `json:"audit"`
	RateLimit     rateLimitSpec     `json:"rateLimit"`
	RequestBudget requestBudgetSpec `json:"requestBudget"`
	SDK           sdkSpec           `json:"sdk"`
	Conformance   []string          `json:"conformance"`
	Idempotency   idempotencySpec   `json:"idempotency"`
}

type identitySpec struct {
	Mode       string   `json:"mode"`
	Audience   string   `json:"audience"`
	Principals []string `json:"principals"`
}

type authzSpec struct {
	Permission      string           `json:"permission"`
	PermissionShape string           `json:"permissionShape"`
	Organization    organizationSpec `json:"organization"`
}

type organizationSpec struct {
	Source string `json:"source"`
	Member string `json:"member"`
}

type auditSpec struct {
	Event         string `json:"event"`
	EventShape    string `json:"eventShape"`
	Resource      string `json:"resource"`
	ResourceShape string `json:"resourceShape"`
	Action        string `json:"action"`
}

type rateLimitSpec struct {
	Bucket string `json:"bucket"`
}

type requestBudgetSpec struct {
	MaxBytes int64 `json:"maxBytes"`
}

type idempotencySpec struct {
	Policy string `json:"policy"`
	Header string `json:"header"`
	Member string `json:"member"`
}

type sdkSpec struct {
	Module    string `json:"module"`
	Method    string `json:"method"`
	Paginated bool   `json:"paginated"`
	Retryable bool   `json:"retryable"`
}

type problemSpec struct {
	ShapeID string `json:"shapeId"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Code    string `json:"code"`
	Status  int    `json:"status"`
}

type catalogSpec struct {
	IAM           []iamCatalogOperation `json:"iam"`
	Audit         []auditCatalogEvent   `json:"audit"`
	Observability []observabilityItem   `json:"observability"`
}

type iamCatalogOperation struct {
	OperationID    string `json:"operationId"`
	Permission     string `json:"permission"`
	Resource       string `json:"resource"`
	Action         string `json:"action"`
	OrgScope       string `json:"orgScope"`
	MemberEligible bool   `json:"memberEligible"`
}

type auditCatalogEvent struct {
	OperationID   string `json:"operationId"`
	Event         string `json:"event"`
	Resource      string `json:"resource"`
	ResourceShape string `json:"resourceShape"`
	Action        string `json:"action"`
	OrgScope      string `json:"orgScope"`
}

type observabilityItem struct {
	OperationID     string   `json:"operationId"`
	Service         string   `json:"service"`
	HTTPMethod      string   `json:"httpMethod"`
	HTTPRoute       string   `json:"httpRoute"`
	Permission      string   `json:"permission"`
	AuditEvent      string   `json:"auditEvent"`
	RateLimitBucket string   `json:"rateLimitBucket"`
	BodyBudgetBytes int64    `json:"bodyBudgetBytes"`
	Conformance     []string `json:"conformance"`
}

func main() {
	var irPath string
	var mode string
	var packageName string
	var catalog string
	var outPath string
	flag.StringVar(&irPath, "ir", "", "Verself contract IR JSON")
	flag.StringVar(&mode, "mode", "huma", "generation mode: huma, identity-catalog, catalog-json, conformance-json, or proto")
	flag.StringVar(&packageName, "package", "", "generated Go package name")
	flag.StringVar(&catalog, "catalog", "", "catalog-json name: iam, audit, or observability")
	flag.StringVar(&outPath, "out", "", "output file")
	flag.Parse()

	if strings.TrimSpace(irPath) == "" || strings.TrimSpace(outPath) == "" {
		fail(errors.New("-ir and -out are required"))
	}
	ir, err := readIR(irPath)
	if err != nil {
		fail(err)
	}
	if len(ir.Operations) == 0 {
		fail(errors.New("IR has no operations"))
	}

	var data []byte
	formatGo := false
	switch mode {
	case "huma":
		if packageName == "" {
			packageName = "contractapi"
		}
		data, err = generateHuma(ir, packageName)
		formatGo = true
	case "identity-catalog":
		if packageName == "" {
			packageName = "identity"
		}
		data, err = generateIdentityCatalog(ir, packageName)
		formatGo = true
	case "catalog-json":
		data, err = generateCatalogJSON(ir, catalog)
	case "conformance-json":
		data, err = generateConformanceJSON(ir)
	case "proto":
		data, err = generateProto(ir)
	default:
		err = fmt.Errorf("unsupported mode %q", mode)
	}
	if err != nil {
		fail(err)
	}
	if formatGo {
		data, err = format.Source(data)
		if err != nil {
			_, _ = os.Stderr.Write(data)
			fail(fmt.Errorf("format generated Go: %w", err))
		}
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		data = append(data, '\n')
	}
	if err := os.WriteFile(outPath, data, 0o644); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func readIR(path string) (*contractIR, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var ir contractIR
	if err := json.Unmarshal(data, &ir); err != nil {
		return nil, err
	}
	for id, shape := range ir.Shapes {
		shape.Name = localName(id)
		for i := range shape.Members {
			shape.Members[i].GoName = goName(shape.Members[i].Name)
			shape.Members[i].Type = goTypeForTarget(&ir, shape.Members[i].Target)
			if shape.Members[i].JSONName == "" {
				shape.Members[i].JSONName = shape.Members[i].Name
			}
		}
		ir.Shapes[id] = shape
	}
	sortOperations(ir.Operations)
	return &ir, nil
}

func generateHuma(ir *contractIR, packageName string) ([]byte, error) {
	scalars, enums, structs, lists := classifyShapes(ir)
	var b bytes.Buffer
	fmt.Fprintf(&b, "package %s\n\n", packageName)
	fmt.Fprintf(&b, "import (\n")
	fmt.Fprintf(&b, "\t\"context\"\n")
	fmt.Fprintf(&b, "\t\"github.com/danielgtaylor/huma/v2\"\n")
	fmt.Fprintf(&b, ")\n\n")
	fmt.Fprintf(&b, "// Code generated by //src/smithy/cmd/ir-go from %s. DO NOT EDIT.\n\n", ir.Projection)
	writeRuntimeTypes(&b)
	writeScalars(&b, ir, scalars)
	writeEnums(&b, ir, enums)
	writeLists(&b, ir, lists)
	writeStructs(&b, ir, structs)
	writeOutputBodies(&b, ir)
	writeDescriptors(&b, ir)
	writeRegistration(&b, ir.Operations)
	return b.Bytes(), nil
}

func generateIdentityCatalog(ir *contractIR, packageName string) ([]byte, error) {
	serviceName := strings.TrimSpace(ir.Service.Runtime.ServiceName)
	if serviceName == "" {
		return nil, errors.New("IR service runtime missing serviceName")
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "package %s\n\n", packageName)
	fmt.Fprintf(&b, "import runtimeiam \"github.com/verself/service-runtime/iam\"\n\n")
	fmt.Fprintf(&b, "// Code generated by //src/smithy/cmd/ir-go from %s. DO NOT EDIT.\n\n", ir.Projection)
	fmt.Fprintf(&b, "func smithyIAMServiceOperations() ServiceOperations {\n")
	fmt.Fprintf(&b, "\treturn ServiceOperations{Service: %q, Operations: []Operation{\n", serviceName)
	for _, op := range ir.Catalogs.IAM {
		fmt.Fprintf(&b, "\t\t{OperationID: %q, Permission: runtimeiam.Permission(%q), Resource: runtimeiam.ResourceKind(%q), Action: runtimeiam.Action(%q), OrgScope: runtimeiam.OrgScope(%q), MemberEligible: %t},\n",
			op.OperationID, op.Permission, op.Resource, op.Action, op.OrgScope, op.MemberEligible)
	}
	fmt.Fprintf(&b, "\t}}\n")
	fmt.Fprintf(&b, "}\n")
	return b.Bytes(), nil
}

func generateCatalogJSON(ir *contractIR, catalog string) ([]byte, error) {
	var value any
	switch strings.TrimSpace(catalog) {
	case "iam":
		value = ir.Catalogs.IAM
	case "audit":
		value = ir.Catalogs.Audit
	case "observability":
		value = ir.Catalogs.Observability
	default:
		return nil, fmt.Errorf("unsupported catalog %q", catalog)
	}
	return json.MarshalIndent(value, "", "  ")
}

func generateConformanceJSON(ir *contractIR) ([]byte, error) {
	type fixture struct {
		Service    string          `json:"service"`
		Projection string          `json:"projection"`
		Operations []operationSpec `json:"operations"`
		Problems   []problemSpec   `json:"problems"`
	}
	return json.MarshalIndent(fixture{
		Service:    ir.Service.Runtime.ServiceName,
		Projection: ir.Projection,
		Operations: ir.Operations,
		Problems:   ir.Problems,
	}, "", "  ")
}

func generateProto(ir *contractIR) ([]byte, error) {
	var b bytes.Buffer
	fmt.Fprintf(&b, "syntax = \"proto3\";\n\n")
	fmt.Fprintf(&b, "package verself.iam.v1;\n\n")
	fmt.Fprintf(&b, "option go_package = \"github.com/verself/iam-service/proto/v1;iampb\";\n\n")
	for _, id := range sortedShapeIDs(ir, "enum") {
		shape := ir.Shapes[id]
		fmt.Fprintf(&b, "enum %s {\n", goName(shape.Name))
		fmt.Fprintf(&b, "  %s_UNSPECIFIED = 0;\n", strings.ToUpper(lowerSnake(shape.Name)))
		for i, value := range shape.Enum {
			fmt.Fprintf(&b, "  %s_%s = %d;\n", strings.ToUpper(lowerSnake(shape.Name)), strings.ToUpper(lowerSnake(value.Name)), i+1)
		}
		fmt.Fprintf(&b, "}\n\n")
	}
	for _, id := range sortedShapeIDs(ir, "structure") {
		shape := ir.Shapes[id]
		if len(shape.Members) == 0 {
			continue
		}
		fmt.Fprintf(&b, "message %s {\n", goName(shape.Name))
		for _, member := range shape.Members {
			number := 0
			if member.ProtoField != nil {
				number = member.ProtoField.Number
			}
			if number == 0 {
				continue
			}
			fmt.Fprintf(&b, "  %s %s = %d;\n", protoType(ir, member.Target), lowerSnake(member.Name), number)
		}
		fmt.Fprintf(&b, "}\n\n")
	}
	fmt.Fprintf(&b, "service %s {\n", goName(ir.Service.Name))
	for _, op := range ir.Operations {
		fmt.Fprintf(&b, "  rpc %s(%s) returns (%s);\n", goName(op.Name), goName(localName(op.Input)), goName(localName(op.Output)))
	}
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

func writeScalars(b *bytes.Buffer, ir *contractIR, ids []string) {
	for _, id := range ids {
		goType := scalarGoType(ir.Shapes[id].Kind)
		if goType == "" {
			continue
		}
		fmt.Fprintf(b, "type %s %s\n\n", goName(localName(id)), goType)
	}
}

func writeEnums(b *bytes.Buffer, ir *contractIR, ids []string) {
	for _, id := range ids {
		shape := ir.Shapes[id]
		name := goName(shape.Name)
		fmt.Fprintf(b, "type %s string\n\n", name)
		if len(shape.Enum) == 0 {
			continue
		}
		fmt.Fprintln(b, "const (")
		values := append([]enumValueSpec(nil), shape.Enum...)
		sort.Slice(values, func(i, j int) bool { return values[i].Name < values[j].Name })
		for _, value := range values {
			fmt.Fprintf(b, "\t%s%s %s = %q\n", name, goName(strings.ToLower(value.Name)), name, value.Value)
		}
		fmt.Fprintln(b, ")\n")
	}
}

func writeLists(b *bytes.Buffer, ir *contractIR, ids []string) {
	for _, id := range ids {
		shape := ir.Shapes[id]
		if shape.Member == nil {
			continue
		}
		fmt.Fprintf(b, "type %s []%s\n\n", goName(shape.Name), goTypeForTarget(ir, shape.Member.Target))
	}
}

func writeStructs(b *bytes.Buffer, ir *contractIR, ids []string) {
	for _, id := range ids {
		shape := ir.Shapes[id]
		if shape.Output {
			continue
		}
		if shape.Input {
			transportMembers, bodyMembers := inputMemberSpecs(ir, shape)
			if len(bodyMembers) > 0 {
				fmt.Fprintf(b, "type %sBody struct {\n", goName(shape.Name))
				for _, member := range bodyMembers {
					fmt.Fprintf(b, "\t%s %s `%s`\n", member.GoName, member.Type, strings.Join(member.Tags, " "))
				}
				fmt.Fprintln(b, "}\n")
			}
			fmt.Fprintf(b, "type %s struct {\n", goName(shape.Name))
			for _, member := range transportMembers {
				if len(member.Tags) == 0 {
					fmt.Fprintf(b, "\t%s %s\n", member.GoName, member.Type)
					continue
				}
				fmt.Fprintf(b, "\t%s %s `%s`\n", member.GoName, member.Type, strings.Join(member.Tags, " "))
			}
			if len(bodyMembers) > 0 {
				fmt.Fprintf(b, "\tBody %sBody\n", goName(shape.Name))
			}
			fmt.Fprintln(b, "}\n")
			continue
		}
		members := jsonMemberSpecsForStructure(ir, shape)
		fmt.Fprintf(b, "type %s struct {\n", goName(shape.Name))
		for _, member := range members {
			fmt.Fprintf(b, "\t%s %s `%s`\n", member.GoName, member.Type, strings.Join(member.Tags, " "))
		}
		fmt.Fprintln(b, "}\n")
	}
}

func writeOutputBodies(b *bytes.Buffer, ir *contractIR) {
	written := map[string]bool{}
	for _, op := range ir.Operations {
		output := ir.Shapes[op.Output]
		if nested := nestedOutputMember(output); nested != nil {
			fmt.Fprintf(b, "type %s struct {\n\tBody %s\n}\n\n", goName(output.Name), goTypeForTarget(ir, nested.Target))
			continue
		}
		bodyName := goName(output.Name) + "Body"
		if !written[bodyName] {
			written[bodyName] = true
			fmt.Fprintf(b, "type %s struct {\n", bodyName)
			for _, member := range jsonMemberSpecsForStructure(ir, output) {
				fmt.Fprintf(b, "\t%s %s `%s`\n", member.GoName, member.Type, strings.Join(member.Tags, " "))
			}
			fmt.Fprintln(b, "}\n")
		}
		fmt.Fprintf(b, "type %s struct {\n\tBody %s\n}\n\n", goName(output.Name), bodyName)
	}
}

func writeDescriptors(b *bytes.Buffer, ir *contractIR) {
	fmt.Fprintln(b, "var Operations = []OperationDescriptor{")
	for _, op := range ir.Operations {
		fmt.Fprintf(b, "\t%sOperation,\n", goName(op.Name))
	}
	fmt.Fprintln(b, "}\n")
	for _, op := range ir.Operations {
		fmt.Fprintf(b, "var %sOperation = OperationDescriptor{\n", goName(op.Name))
		fmt.Fprintf(b, "\tShapeID: %q,\n", op.ShapeID)
		fmt.Fprintf(b, "\tOperationID: %q,\n", op.OperationID)
		fmt.Fprintf(b, "\tMethod: %q,\n", op.HTTP.Method)
		fmt.Fprintf(b, "\tPath: %q,\n", op.HTTP.Path)
		fmt.Fprintf(b, "\tDefaultStatus: %d,\n", op.HTTP.SuccessStatus)
		fmt.Fprintf(b, "\tReadonly: %t,\n", op.Readonly)
		fmt.Fprintf(b, "\tPaginated: %t,\n", op.Paginated)
		fmt.Fprintf(b, "\tIdentity: IdentityDescriptor{Mode: %q, Audience: %q, Principals: %#v},\n", op.Verself.Identity.Mode, op.Verself.Identity.Audience, op.Verself.Identity.Principals)
		fmt.Fprintf(b, "\tAuthorization: AuthorizationDescriptor{Permission: %q, OrganizationSource: %q, OrganizationMember: %q},\n", op.Verself.Authz.Permission, op.Verself.Authz.Organization.Source, op.Verself.Authz.Organization.Member)
		fmt.Fprintf(b, "\tAudit: AuditDescriptor{Event: %q, Resource: %q, Action: %q},\n", op.Verself.Audit.Event, op.Verself.Audit.Resource, op.Verself.Audit.Action)
		fmt.Fprintf(b, "\tRateLimitBucket: %q,\n", op.Verself.RateLimit.Bucket)
		fmt.Fprintf(b, "\tRequestBodyMaxBytes: %d,\n", op.Verself.RequestBudget.MaxBytes)
		fmt.Fprintf(b, "\tIdempotency: IdempotencyDescriptor{Policy: %q, Header: %q, Member: %q},\n", op.Verself.Idempotency.Policy, op.Verself.Idempotency.Header, op.Verself.Idempotency.Member)
		fmt.Fprintf(b, "\tSDK: SDKDescriptor{Module: %q, Method: %q, Paginated: %t, Retryable: %t},\n", op.Verself.SDK.Module, op.Verself.SDK.Method, op.Verself.SDK.Paginated, op.Verself.SDK.Retryable)
		fmt.Fprintf(b, "\tConformanceCases: %#v,\n", op.Verself.Conformance)
		fmt.Fprintln(b, "\tProblems: []ProblemDescriptor{")
		for _, problem := range op.Problems {
			fmt.Fprintf(b, "\t\t{ShapeID: %q, Type: %q, Code: %q, Status: %d},\n", problem.ShapeID, problem.Type, problem.Code, problem.Status)
		}
		fmt.Fprintln(b, "\t},")
		fmt.Fprintln(b, "}\n")
	}
}

func writeRegistration(b *bytes.Buffer, ops []operationSpec) {
	fmt.Fprintln(b, "type PublicHandlers interface {")
	for _, op := range ops {
		fmt.Fprintf(b, "\t%s(context.Context, *%s) (*%s, error)\n", goName(op.Name), goName(localName(op.Input)), goName(localName(op.Output)))
	}
	fmt.Fprintln(b, "}\n")
	fmt.Fprintln(b, "func RegisterPublic(api huma.API, runtime Runtime, handlers PublicHandlers) {")
	fmt.Fprintln(b, "\tif runtime == nil { panic(\"contractapi: runtime is required\") }")
	fmt.Fprintln(b, "\tif handlers == nil { panic(\"contractapi: public handlers are required\") }")
	for _, op := range ops {
		fmt.Fprintf(b, "\tregister%s(api, runtime, handlers.%s)\n", goName(op.Name), goName(op.Name))
	}
	fmt.Fprintln(b, "}\n")
	for _, op := range ops {
		input := goName(localName(op.Input))
		output := goName(localName(op.Output))
		goOp := goName(op.Name)
		fmt.Fprintf(b, "func register%s(api huma.API, runtime Runtime, handler func(context.Context, *%s) (*%s, error)) {\n", goOp, input, output)
		fmt.Fprintf(b, "\top := runtime.PrepareOperation(%sOperation, huma.Operation{OperationID: %sOperation.OperationID, Method: %sOperation.Method, Path: %sOperation.Path, Summary: %q, DefaultStatus: %sOperation.DefaultStatus})\n", goOp, goOp, goOp, goOp, sentence(op.Name), goOp)
		fmt.Fprintf(b, "\thuma.Register(api, op, func(ctx context.Context, input *%s) (*%s, error) {\n", input, output)
		fmt.Fprintf(b, "\t\tidentity, err := runtime.BeforeOperation(ctx, %sOperation, input)\n", goOp)
		fmt.Fprintln(b, "\t\tif err != nil {")
		fmt.Fprintf(b, "\t\t\truntime.AfterOperation(ctx, %sOperation, identity, input, nil, err)\n", goOp)
		fmt.Fprintln(b, "\t\t\treturn nil, err")
		fmt.Fprintln(b, "\t\t}")
		fmt.Fprintln(b, "\t\toutput, err := handler(ctx, input)")
		fmt.Fprintf(b, "\t\truntime.AfterOperation(ctx, %sOperation, identity, input, output, err)\n", goOp)
		fmt.Fprintln(b, "\t\treturn output, err")
		fmt.Fprintln(b, "\t})")
		fmt.Fprintln(b, "}\n")
	}
}

func classifyShapes(ir *contractIR) (scalars, enums, structs, lists []string) {
	for id, shape := range ir.Shapes {
		switch shape.Kind {
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

func jsonMemberSpecsForStructure(ir *contractIR, shape shapeSpec) []memberSpec {
	out := make([]memberSpec, 0, len(shape.Members))
	for _, member := range shape.Members {
		member.GoName = goName(member.Name)
		member.Type = goTypeForTarget(ir, member.Target)
		member.Tags = append(member.Tags, jsonTag(member.JSONName, member.Required))
		member.Tags = append(member.Tags, constraintTags(ir.Shapes[member.Target].Constraints, member.Required)...)
		out = append(out, member)
	}
	return out
}

func inputMemberSpecs(ir *contractIR, shape shapeSpec) ([]memberSpec, []memberSpec) {
	transport := make([]memberSpec, 0, len(shape.Members))
	body := make([]memberSpec, 0)
	for _, member := range shape.Members {
		member.GoName = goName(member.Name)
		member.Type = goTypeForTarget(ir, member.Target)
		if member.HTTPBinding == nil || member.HTTPBinding.Location == "document" {
			member.Tags = append(member.Tags, jsonTag(member.JSONName, member.Required))
			member.Tags = append(member.Tags, constraintTags(ir.Shapes[member.Target].Constraints, member.Required)...)
			body = append(body, member)
			continue
		}
		switch member.HTTPBinding.Location {
		case "label":
			member.Tags = append(member.Tags, fmt.Sprintf("path:%q", member.HTTPBinding.Name))
		case "header":
			member.Tags = append(member.Tags, fmt.Sprintf("header:%q", member.HTTPBinding.Name))
		case "query":
			member.Tags = append(member.Tags, fmt.Sprintf("query:%q", member.HTTPBinding.Name))
		}
		member.Tags = append(member.Tags, constraintTags(ir.Shapes[member.Target].Constraints, member.Required)...)
		transport = append(transport, member)
	}
	return transport, body
}

func nestedOutputMember(shape shapeSpec) *memberSpec {
	for _, member := range shape.Members {
		if member.NestedProperties {
			return &member
		}
	}
	return nil
}

func goTypeForTarget(ir *contractIR, target string) string {
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
	return goName(localName(target))
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

func jsonTag(name string, required bool) string {
	if required {
		return fmt.Sprintf("json:%q", name)
	}
	return fmt.Sprintf("json:%q", name+",omitempty")
}

func constraintTags(constraints constraintsSpec, required bool) []string {
	var tags []string
	if required {
		tags = append(tags, `required:"true"`)
	}
	if constraints.Length.Min != nil {
		tags = append(tags, fmt.Sprintf("minLength:%q", strconv.Itoa(*constraints.Length.Min)))
	}
	if constraints.Length.Max != nil {
		tags = append(tags, fmt.Sprintf("maxLength:%q", strconv.Itoa(*constraints.Length.Max)))
	}
	if constraints.Range.Min != nil {
		tags = append(tags, fmt.Sprintf("minimum:%q", strconv.Itoa(*constraints.Range.Min)))
	}
	if constraints.Range.Max != nil {
		tags = append(tags, fmt.Sprintf("maximum:%q", strconv.Itoa(*constraints.Range.Max)))
	}
	if constraints.Pattern != "" {
		tags = append(tags, fmt.Sprintf("pattern:%q", constraints.Pattern))
	}
	return tags
}

func sortOperations(ops []operationSpec) {
	sort.Slice(ops, func(i, j int) bool {
		if ops[i].HTTP.Path == ops[j].HTTP.Path {
			return ops[i].HTTP.Method < ops[j].HTTP.Method
		}
		return ops[i].HTTP.Path < ops[j].HTTP.Path
	})
}

func sortedShapeIDs(ir *contractIR, kind string) []string {
	var ids []string
	for id, shape := range ir.Shapes {
		if shape.Kind == kind {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

func protoType(ir *contractIR, target string) string {
	if strings.HasPrefix(target, "smithy.api#") {
		switch localName(target) {
		case "String":
			return "string"
		case "Integer":
			return "int32"
		case "Long":
			return "int64"
		case "Boolean":
			return "bool"
		default:
			return "string"
		}
	}
	shape := ir.Shapes[target]
	switch shape.Kind {
	case "string":
		return "string"
	case "integer":
		return "int32"
	case "long":
		return "int64"
	case "boolean":
		return "bool"
	case "list":
		if shape.Member == nil {
			return "repeated string"
		}
		return "repeated " + protoType(ir, shape.Member.Target)
	default:
		return goName(localName(target))
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
