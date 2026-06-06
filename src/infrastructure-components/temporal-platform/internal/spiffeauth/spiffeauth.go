package spiffeauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spiffe/go-spiffe/v2/svid/x509svid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.temporal.io/server/common/authorization"
	"google.golang.org/grpc"
)

var tracer = otel.Tracer("github.com/verself/temporal-platform/spiffeauth")

type Config struct {
	systemRoles    map[string]authorization.Role
	namespaceRoles map[string]map[string]authorization.Role
}

type Document struct {
	SystemAdminIDs  []string        `json:"systemAdminIDs,omitempty"`
	SystemWriterIDs []string        `json:"systemWriterIDs,omitempty"`
	SystemReaderIDs []string        `json:"systemReaderIDs,omitempty"`
	NamespaceRoles  []NamespaceRole `json:"namespaceRoles,omitempty"`
}

type NamespaceRole struct {
	SPIFFEID  string `json:"spiffeID"`
	Namespace string `json:"namespace"`
	Role      string `json:"role"`
}

func LoadFromFile(path string) (*Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open temporal authorization config: %w", err)
	}
	defer func() { _ = file.Close() }()
	var doc Document
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&doc); err != nil {
		return nil, fmt.Errorf("decode temporal authorization config: %w", err)
	}
	return FromDocument(doc)
}

func FromDocument(doc Document) (*Config, error) {
	cfg := &Config{
		systemRoles:    map[string]authorization.Role{},
		namespaceRoles: map[string]map[string]authorization.Role{},
	}
	cfg.mergeSystemRoleIDs(doc.SystemAdminIDs, authorization.RoleAdmin)
	cfg.mergeSystemRoleIDs(doc.SystemWriterIDs, authorization.RoleWriter)
	cfg.mergeSystemRoleIDs(doc.SystemReaderIDs, authorization.RoleReader)
	if err := cfg.mergeNamespaceRoles(doc.NamespaceRoles); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) mergeSystemRoleIDs(ids []string, role authorization.Role) {
	for _, item := range ids {
		id := strings.TrimSpace(item)
		if id == "" {
			continue
		}
		c.systemRoles[id] |= role
	}
}

func (c *Config) mergeNamespaceRoles(entries []NamespaceRole) error {
	for _, entry := range entries {
		id := strings.TrimSpace(entry.SPIFFEID)
		namespace := strings.TrimSpace(entry.Namespace)
		role, err := parseRole(entry.Role)
		if err != nil {
			return fmt.Errorf("namespace role for %s/%s: %w", id, namespace, err)
		}
		if id == "" || namespace == "" {
			return errors.New("temporal namespace role entries must not contain empty fields")
		}
		rolesByNamespace, ok := c.namespaceRoles[id]
		if !ok {
			rolesByNamespace = map[string]authorization.Role{}
			c.namespaceRoles[id] = rolesByNamespace
		}
		rolesByNamespace[namespace] |= role
	}
	return nil
}

type ClaimMapper struct {
	cfg *Config
}

func NewClaimMapper(cfg *Config) *ClaimMapper {
	return &ClaimMapper{cfg: cfg}
}

func (m *ClaimMapper) GetClaims(authInfo *authorization.AuthInfo) (*authorization.Claims, error) {
	if authInfo == nil {
		return nil, errors.New("temporal auth info is required")
	}
	cert := authorization.PeerCert(authInfo.TLSConnection)
	if cert == nil {
		return nil, errors.New("temporal client certificate is required")
	}
	id, err := x509svid.IDFromCert(cert)
	if err != nil {
		return nil, fmt.Errorf("parse client spiffe id: %w", err)
	}
	subject := id.String()
	claims := &authorization.Claims{
		Subject:    subject,
		System:     m.cfg.systemRoles[subject],
		Namespaces: map[string]authorization.Role{},
	}
	for namespace, role := range m.cfg.namespaceRoles[subject] {
		claims.Namespaces[namespace] = role
	}
	return claims, nil
}

type TracingAuthorizer struct {
	next authorization.Authorizer
}

func NewTracingAuthorizer(next authorization.Authorizer) *TracingAuthorizer {
	return &TracingAuthorizer{next: next}
}

func (a *TracingAuthorizer) Authorize(ctx context.Context, claims *authorization.Claims, target *authorization.CallTarget) (authorization.Result, error) {
	ctx, span := tracer.Start(ctx, "temporal.auth.authorize")
	defer span.End()
	if target != nil {
		span.SetAttributes(
			attribute.String("rpc.method", target.APIName),
			attribute.String("temporal.namespace", target.Namespace),
		)
	}
	if claims != nil {
		span.SetAttributes(
			attribute.String("spiffe.peer_id", claims.Subject),
			attribute.Int("temporal.system_role", int(claims.System)),
		)
	} else if cert := authorization.PeerCert(authorization.TLSInfoFromContext(ctx)); cert != nil {
		if id, err := x509svid.IDFromCert(cert); err == nil {
			span.SetAttributes(attribute.String("spiffe.peer_id", id.String()))
		} else {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
	}
	result, err := a.next.Authorize(ctx, claims, target)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return result, err
	}
	span.SetAttributes(
		attribute.String("temporal.authz.decision", decisionString(result.Decision)),
		attribute.String("temporal.authz.reason", result.Reason),
	)
	if result.Decision == authorization.DecisionDeny {
		span.SetStatus(codes.Error, "deny")
	}
	return result, nil
}

func UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		ctx, span := tracer.Start(ctx, "auth.spiffe.mtls.server")
		defer span.End()
		span.SetAttributes(attribute.String("rpc.method", info.FullMethod))
		if namespace, ok := req.(interface{ GetNamespace() string }); ok {
			span.SetAttributes(attribute.String("temporal.namespace", namespace.GetNamespace()))
		}
		if cert := authorization.PeerCert(authorization.TLSInfoFromContext(ctx)); cert != nil {
			if id, err := x509svid.IDFromCert(cert); err == nil {
				span.SetAttributes(attribute.String("spiffe.peer_id", id.String()))
			} else {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
			}
		} else {
			err := errors.New("missing SPIFFE peer certificate")
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		resp, err := handler(ctx, req)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		return resp, err
	}
}

func parseRole(raw string) (authorization.Role, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "admin":
		return authorization.RoleAdmin, nil
	case "writer", "write":
		return authorization.RoleWriter, nil
	case "reader", "read":
		return authorization.RoleReader, nil
	case "worker":
		return authorization.RoleWorker, nil
	default:
		return authorization.RoleUndefined, fmt.Errorf("unsupported role %q", raw)
	}
}

func decisionString(decision authorization.Decision) string {
	switch decision {
	case authorization.DecisionAllow:
		return "allow"
	case authorization.DecisionDeny:
		return "deny"
	default:
		return "unknown"
	}
}
