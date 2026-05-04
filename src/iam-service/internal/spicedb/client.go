package spicedb

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	authzed "github.com/authzed/authzed-go/v1"
	v1 "github.com/authzed/authzed-go/proto/authzed/api/v1"
	"github.com/authzed/grpcutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/structpb"
)

const defaultRequestTimeout = 2 * time.Second

type Config struct {
	Endpoint       string
	PresharedKey   string
	RequestTimeout time.Duration
}

type Client struct {
	client         *authzed.Client
	requestTimeout time.Duration
}

type ResourceRef struct {
	Type string
	ID   string
}

type SubjectRef struct {
	Type     string
	ID       string
	Relation string
}

type Relationship struct {
	Resource ResourceRef
	Relation string
	Subject  SubjectRef
}

func New(_ context.Context, cfg Config) (*Client, error) {
	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" {
		return nil, errors.New("spicedb endpoint is required")
	}
	presharedKey := strings.TrimSpace(cfg.PresharedKey)
	if presharedKey == "" {
		return nil, errors.New("spicedb preshared key is required")
	}
	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = defaultRequestTimeout
	}
	client, err := authzed.NewClient(
		endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpcutil.WithInsecureBearerToken(presharedKey),
	)
	if err != nil {
		return nil, fmt.Errorf("create spicedb client: %w", err)
	}
	return &Client{client: client, requestTimeout: timeout}, nil
}

func (c *Client) Close() error {
	if c == nil || c.client == nil {
		return nil
	}
	return c.client.Close()
}

func (c *Client) WriteSchema(ctx context.Context, schema string) (string, error) {
	if c == nil || c.client == nil {
		return "", errors.New("spicedb client is unavailable")
	}
	schema = strings.TrimSpace(schema)
	if schema == "" {
		return "", errors.New("spicedb schema is empty")
	}
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	resp, err := c.client.WriteSchema(ctx, &v1.WriteSchemaRequest{Schema: schema})
	if err != nil {
		return "", fmt.Errorf("write spicedb schema: %w", err)
	}
	return zedTokenString(resp.GetWrittenAt()), nil
}

func (c *Client) Check(ctx context.Context, resource ResourceRef, permission string, subject SubjectRef, minZedToken string) (bool, string, error) {
	if c == nil || c.client == nil {
		return false, "", errors.New("spicedb client is unavailable")
	}
	permission = strings.TrimSpace(permission)
	if permission == "" {
		return false, "", errors.New("spicedb permission is required")
	}
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	resp, err := c.client.CheckPermission(ctx, &v1.CheckPermissionRequest{
		Resource:    objectReference(resource),
		Permission:  permission,
		Subject:     subjectReference(subject),
		Consistency: consistency(minZedToken),
	})
	if err != nil {
		return false, "", fmt.Errorf("check spicedb permission: %w", err)
	}
	return resp.GetPermissionship() == v1.CheckPermissionResponse_PERMISSIONSHIP_HAS_PERMISSION, zedTokenString(resp.GetCheckedAt()), nil
}

func (c *Client) TestPermissions(ctx context.Context, resource ResourceRef, permissions []string, subject SubjectRef, minZedToken string) ([]string, string, error) {
	allowed := make([]string, 0, len(permissions))
	checkedAt := ""
	for _, permission := range compactSorted(permissions) {
		ok, token, err := c.Check(ctx, resource, permission, subject, minZedToken)
		if err != nil {
			return nil, "", err
		}
		if token != "" {
			checkedAt = token
		}
		if ok {
			allowed = append(allowed, permission)
		}
	}
	return allowed, checkedAt, nil
}

func (c *Client) ReadResourceRelationships(ctx context.Context, resource ResourceRef, relations map[string]struct{}) ([]Relationship, string, error) {
	if c == nil || c.client == nil {
		return nil, "", errors.New("spicedb client is unavailable")
	}
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	stream, err := c.client.ReadRelationships(ctx, &v1.ReadRelationshipsRequest{
		Consistency: fullyConsistent(),
		RelationshipFilter: &v1.RelationshipFilter{
			ResourceType:       strings.TrimSpace(resource.Type),
			OptionalResourceId: strings.TrimSpace(resource.ID),
		},
	})
	if err != nil {
		return nil, "", fmt.Errorf("read spicedb relationships: %w", err)
	}
	out := []Relationship{}
	readAt := ""
	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, "", fmt.Errorf("read spicedb relationships stream: %w", err)
		}
		if resp.GetReadAt() != nil {
			readAt = zedTokenString(resp.GetReadAt())
		}
		rel := relationshipFromProto(resp.GetRelationship())
		if rel.Resource.Type == "" || rel.Relation == "" || rel.Subject.Type == "" {
			continue
		}
		if len(relations) > 0 {
			if _, ok := relations[rel.Relation]; !ok {
				continue
			}
		}
		out = append(out, rel)
	}
	return out, readAt, nil
}

func (c *Client) ReplaceResourceRelationships(ctx context.Context, current []Relationship, desired []Relationship, metadata map[string]any) (string, error) {
	if c == nil || c.client == nil {
		return "", errors.New("spicedb client is unavailable")
	}
	desiredSet := relationshipSet(desired)
	updates := make([]*v1.RelationshipUpdate, 0, len(current)+len(desired))
	for _, rel := range current {
		if _, keep := desiredSet[relationshipKey(rel)]; keep {
			continue
		}
		updates = append(updates, &v1.RelationshipUpdate{
			Operation:    v1.RelationshipUpdate_OPERATION_DELETE,
			Relationship: relationshipProto(rel),
		})
	}
	for _, rel := range desired {
		updates = append(updates, &v1.RelationshipUpdate{
			Operation:    v1.RelationshipUpdate_OPERATION_TOUCH,
			Relationship: relationshipProto(rel),
		})
	}
	if len(updates) == 0 {
		return "", nil
	}
	txMetadata, err := structpb.NewStruct(metadata)
	if err != nil {
		return "", fmt.Errorf("encode spicedb transaction metadata: %w", err)
	}
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	resp, err := c.client.WriteRelationships(ctx, &v1.WriteRelationshipsRequest{
		Updates:                     updates,
		OptionalTransactionMetadata: txMetadata,
	})
	if err != nil {
		return "", fmt.Errorf("write spicedb relationships: %w", err)
	}
	return zedTokenString(resp.GetWrittenAt()), nil
}

func (c *Client) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(ctx, c.requestTimeout)
}

func objectReference(ref ResourceRef) *v1.ObjectReference {
	return &v1.ObjectReference{
		ObjectType: strings.TrimSpace(ref.Type),
		ObjectId:   strings.TrimSpace(ref.ID),
	}
}

func subjectReference(ref SubjectRef) *v1.SubjectReference {
	out := &v1.SubjectReference{Object: objectReference(ResourceRef{Type: ref.Type, ID: ref.ID})}
	if relation := strings.TrimSpace(ref.Relation); relation != "" {
		out.OptionalRelation = relation
	}
	return out
}

func consistency(minZedToken string) *v1.Consistency {
	minZedToken = strings.TrimSpace(minZedToken)
	if minZedToken == "" {
		return fullyConsistent()
	}
	return &v1.Consistency{Requirement: &v1.Consistency_AtLeastAsFresh{AtLeastAsFresh: &v1.ZedToken{Token: minZedToken}}}
}

func fullyConsistent() *v1.Consistency {
	return &v1.Consistency{Requirement: &v1.Consistency_FullyConsistent{FullyConsistent: true}}
}

func relationshipProto(rel Relationship) *v1.Relationship {
	return &v1.Relationship{
		Resource: objectReference(rel.Resource),
		Relation: strings.TrimSpace(rel.Relation),
		Subject:  subjectReference(rel.Subject),
	}
}

func relationshipFromProto(rel *v1.Relationship) Relationship {
	if rel == nil {
		return Relationship{}
	}
	subject := rel.GetSubject()
	subjectObject := subject.GetObject()
	return Relationship{
		Resource: ResourceRef{
			Type: rel.GetResource().GetObjectType(),
			ID:   rel.GetResource().GetObjectId(),
		},
		Relation: rel.GetRelation(),
		Subject: SubjectRef{
			Type:     subjectObject.GetObjectType(),
			ID:       subjectObject.GetObjectId(),
			Relation: subject.GetOptionalRelation(),
		},
	}
}

func zedTokenString(token *v1.ZedToken) string {
	if token == nil {
		return ""
	}
	return token.GetToken()
}

func relationshipSet(relationships []Relationship) map[string]struct{} {
	out := make(map[string]struct{}, len(relationships))
	for _, rel := range relationships {
		out[relationshipKey(rel)] = struct{}{}
	}
	return out
}

func relationshipKey(rel Relationship) string {
	return strings.Join([]string{
		strings.TrimSpace(rel.Resource.Type),
		strings.TrimSpace(rel.Resource.ID),
		strings.TrimSpace(rel.Relation),
		strings.TrimSpace(rel.Subject.Type),
		strings.TrimSpace(rel.Subject.ID),
		strings.TrimSpace(rel.Subject.Relation),
	}, "\x00")
}

func compactSorted(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
