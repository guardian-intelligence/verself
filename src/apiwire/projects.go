package apiwire

import "time"

type ProjectState string

const (
	ProjectStateActive   ProjectState = "active"
	ProjectStateArchived ProjectState = "archived"
)

type ProjectEnvironmentKind string

const (
	ProjectEnvironmentKindProduction  ProjectEnvironmentKind = "production"
	ProjectEnvironmentKindPreview     ProjectEnvironmentKind = "preview"
	ProjectEnvironmentKindDevelopment ProjectEnvironmentKind = "development"
	ProjectEnvironmentKindCustom      ProjectEnvironmentKind = "custom"
)

type ProjectEnvironmentState string

const (
	ProjectEnvironmentStateActive   ProjectEnvironmentState = "active"
	ProjectEnvironmentStateArchived ProjectEnvironmentState = "archived"
)

type Project struct {
	ProjectID          string       `json:"project_id" format:"uuid"`
	OrgID              OrgID        `json:"org_id"`
	Slug               string       `json:"slug"`
	RedirectedFromSlug string       `json:"redirected_from_slug,omitempty"`
	DisplayName        string       `json:"display_name"`
	Description        string       `json:"description"`
	State              ProjectState `json:"state"`
	Version            DecimalInt64 `json:"version"`
	CreatedBy          string       `json:"created_by"`
	UpdatedBy          string       `json:"updated_by"`
	CreatedAt          time.Time    `json:"created_at"`
	UpdatedAt          time.Time    `json:"updated_at"`
	ArchivedAt         *time.Time   `json:"archived_at,omitempty"`
}

type ProjectList struct {
	Projects   []Project `json:"projects"`
	NextCursor string    `json:"next_cursor,omitempty"`
}

type CreateProjectRequest struct {
	Slug        string `json:"slug,omitempty" minLength:"1" maxLength:"80"`
	DisplayName string `json:"display_name" minLength:"1" maxLength:"120"`
	Description string `json:"description,omitempty" maxLength:"500"`
}

type UpdateProjectRequest struct {
	Version     DecimalInt64 `json:"version"`
	Slug        *string      `json:"slug,omitempty" minLength:"1" maxLength:"80"`
	DisplayName *string      `json:"display_name,omitempty" maxLength:"120"`
	Description *string      `json:"description,omitempty" maxLength:"500"`
}

type ProjectLifecycleRequest struct {
	Version DecimalInt64 `json:"version"`
}

type ProjectEnvironment struct {
	EnvironmentID    string                  `json:"environment_id" format:"uuid"`
	ProjectID        string                  `json:"project_id" format:"uuid"`
	OrgID            OrgID                   `json:"org_id"`
	Slug             string                  `json:"slug"`
	DisplayName      string                  `json:"display_name"`
	Kind             ProjectEnvironmentKind  `json:"kind"`
	State            ProjectEnvironmentState `json:"state"`
	ProtectionPolicy map[string]string       `json:"protection_policy,omitempty"`
	Version          DecimalInt64            `json:"version"`
	CreatedBy        string                  `json:"created_by"`
	UpdatedBy        string                  `json:"updated_by"`
	CreatedAt        time.Time               `json:"created_at"`
	UpdatedAt        time.Time               `json:"updated_at"`
	ArchivedAt       *time.Time              `json:"archived_at,omitempty"`
}

type ProjectEnvironmentList struct {
	Environments []ProjectEnvironment `json:"environments"`
	NextCursor   string               `json:"next_cursor,omitempty"`
}

type CreateProjectEnvironmentRequest struct {
	Slug             string                 `json:"slug" minLength:"1" maxLength:"80"`
	DisplayName      string                 `json:"display_name" minLength:"1" maxLength:"120"`
	Kind             ProjectEnvironmentKind `json:"kind"`
	ProtectionPolicy map[string]string      `json:"protection_policy,omitempty"`
}

type UpdateProjectEnvironmentRequest struct {
	Version          DecimalInt64      `json:"version"`
	DisplayName      string            `json:"display_name,omitempty" maxLength:"120"`
	ProtectionPolicy map[string]string `json:"protection_policy,omitempty"`
}

type ResolveProjectRequest struct {
	OrgID         OrgID  `json:"org_id"`
	ProjectID     string `json:"project_id,omitempty" format:"uuid"`
	Slug          string `json:"slug,omitempty" maxLength:"80"`
	RequireActive bool   `json:"require_active"`
}

type ResolveProjectResponse struct {
	Project Project `json:"project"`
}

type ResolveProjectEnvironmentRequest struct {
	OrgID         OrgID  `json:"org_id"`
	ProjectID     string `json:"project_id" format:"uuid"`
	EnvironmentID string `json:"environment_id,omitempty" format:"uuid"`
	Slug          string `json:"slug,omitempty" maxLength:"80"`
	RequireActive bool   `json:"require_active"`
}

type ResolveProjectEnvironmentResponse struct {
	Environment ProjectEnvironment `json:"environment"`
}

type ProjectEvent struct {
	EventID       string            `json:"event_id" format:"uuid"`
	OrgID         OrgID             `json:"org_id"`
	ProjectID     string            `json:"project_id" format:"uuid"`
	EnvironmentID string            `json:"environment_id,omitempty" format:"uuid"`
	EventType     string            `json:"event_type"`
	ActorID       string            `json:"actor_id"`
	Payload       map[string]string `json:"payload,omitempty"`
	TraceID       string            `json:"trace_id,omitempty"`
	CreatedAt     time.Time         `json:"created_at"`
}

type ProjectEventList struct {
	Events     []ProjectEvent `json:"events"`
	NextCursor string         `json:"next_cursor,omitempty"`
}
