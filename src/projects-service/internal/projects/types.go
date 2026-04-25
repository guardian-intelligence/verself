package projects

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

const (
	StateActive   = "active"
	StateArchived = "archived"

	EnvironmentKindProduction  = "production"
	EnvironmentKindPreview     = "preview"
	EnvironmentKindDevelopment = "development"
	EnvironmentKindCustom      = "custom"
)

var (
	ErrInvalid          = errors.New("invalid project request")
	ErrUnauthorized     = errors.New("unauthorized")
	ErrNotFound         = errors.New("project not found")
	ErrConflict         = errors.New("project conflict")
	ErrArchived         = errors.New("project archived")
	ErrStoreUnavailable = errors.New("projects store unavailable")
)

type Principal struct {
	Subject string
	OrgID   uint64
	Email   string
}

type Project struct {
	ID          uuid.UUID
	OrgID       uint64
	Slug        string
	DisplayName string
	Description string
	State       string
	Version     int64
	CreatedBy   string
	UpdatedBy   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	ArchivedAt  *time.Time
}

type Environment struct {
	ID               uuid.UUID
	ProjectID        uuid.UUID
	OrgID            uint64
	Slug             string
	DisplayName      string
	Kind             string
	State            string
	ProtectionPolicy map[string]string
	Version          int64
	CreatedBy        string
	UpdatedBy        string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	ArchivedAt       *time.Time
}

type Event struct {
	ID            uuid.UUID
	OrgID         uint64
	ProjectID     uuid.UUID
	EnvironmentID uuid.UUID
	EventType     string
	ActorID       string
	Payload       map[string]string
	TraceID       string
	Traceparent   string
	CreatedAt     time.Time
}

type CreateProjectRequest struct {
	Slug           string
	DisplayName    string
	Description    string
	IdempotencyKey string
}

type UpdateProjectRequest struct {
	ProjectID      uuid.UUID
	Version        int64
	Slug           string
	DisplayName    string
	Description    string
	IdempotencyKey string
}

type ProjectLifecycleRequest struct {
	ProjectID      uuid.UUID
	Version        int64
	IdempotencyKey string
}

type ListProjectsRequest struct {
	State  string
	Limit  int
	Cursor string
}

type CreateEnvironmentRequest struct {
	ProjectID        uuid.UUID
	Slug             string
	DisplayName      string
	Kind             string
	ProtectionPolicy map[string]string
	IdempotencyKey   string
}

type UpdateEnvironmentRequest struct {
	ProjectID        uuid.UUID
	EnvironmentID    uuid.UUID
	Version          int64
	DisplayName      string
	ProtectionPolicy map[string]string
	IdempotencyKey   string
}

type EnvironmentLifecycleRequest struct {
	ProjectID      uuid.UUID
	EnvironmentID  uuid.UUID
	Version        int64
	IdempotencyKey string
}

type ResolveProjectRequest struct {
	OrgID         uint64
	ProjectID     uuid.UUID
	Slug          string
	RequireActive bool
}

type ResolveEnvironmentRequest struct {
	OrgID         uint64
	ProjectID     uuid.UUID
	EnvironmentID uuid.UUID
	Slug          string
	RequireActive bool
}

func ValidatePrincipal(principal Principal) error {
	if strings.TrimSpace(principal.Subject) == "" {
		return fmt.Errorf("%w: subject is required", ErrInvalid)
	}
	if principal.OrgID == 0 {
		return fmt.Errorf("%w: org_id is required", ErrInvalid)
	}
	return nil
}

func NormalizeCreateProject(input CreateProjectRequest) (CreateProjectRequest, error) {
	input.DisplayName = normalizeHumanText(input.DisplayName)
	input.Description = normalizeHumanText(input.Description)
	input.Slug = normalizeSlug(input.Slug)
	if input.Slug == "" {
		input.Slug = normalizeSlug(input.DisplayName)
	}
	if err := validateSlug("slug", input.Slug); err != nil {
		return input, err
	}
	if err := validateHumanText("display_name", input.DisplayName, 1, 120, 240); err != nil {
		return input, err
	}
	if err := validateHumanText("description", input.Description, 0, 500, 1000); err != nil {
		return input, err
	}
	return input, nil
}

func NormalizeUpdateProject(input UpdateProjectRequest) (UpdateProjectRequest, error) {
	input.DisplayName = normalizeHumanText(input.DisplayName)
	input.Description = normalizeHumanText(input.Description)
	input.Slug = normalizeSlug(input.Slug)
	if input.Version < 0 {
		return input, fmt.Errorf("%w: version must be non-negative", ErrInvalid)
	}
	if input.Slug != "" {
		if err := validateSlug("slug", input.Slug); err != nil {
			return input, err
		}
	}
	if input.DisplayName != "" {
		if err := validateHumanText("display_name", input.DisplayName, 1, 120, 240); err != nil {
			return input, err
		}
	}
	if err := validateHumanText("description", input.Description, 0, 500, 1000); err != nil {
		return input, err
	}
	return input, nil
}

func NormalizeCreateEnvironment(input CreateEnvironmentRequest) (CreateEnvironmentRequest, error) {
	input.Slug = normalizeSlug(input.Slug)
	input.DisplayName = normalizeHumanText(input.DisplayName)
	if err := validateSlug("slug", input.Slug); err != nil {
		return input, err
	}
	if err := validateHumanText("display_name", input.DisplayName, 1, 120, 240); err != nil {
		return input, err
	}
	switch input.Kind {
	case EnvironmentKindProduction, EnvironmentKindPreview, EnvironmentKindDevelopment, EnvironmentKindCustom:
	case "":
		input.Kind = EnvironmentKindCustom
	default:
		return input, fmt.Errorf("%w: invalid environment kind", ErrInvalid)
	}
	for key, value := range input.ProtectionPolicy {
		if err := validatePolicyText(key, value); err != nil {
			return input, err
		}
	}
	return input, nil
}

func NormalizeUpdateEnvironment(input UpdateEnvironmentRequest) (UpdateEnvironmentRequest, error) {
	input.DisplayName = normalizeHumanText(input.DisplayName)
	if input.Version < 0 {
		return input, fmt.Errorf("%w: version must be non-negative", ErrInvalid)
	}
	if input.DisplayName != "" {
		if err := validateHumanText("display_name", input.DisplayName, 1, 120, 240); err != nil {
			return input, err
		}
	}
	for key, value := range input.ProtectionPolicy {
		if err := validatePolicyText(key, value); err != nil {
			return input, err
		}
	}
	return input, nil
}

func normalizeHumanText(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func normalizeSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	previousDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			previousDash = false
		case r == '-' || r == '_' || unicode.IsSpace(r):
			if !previousDash && b.Len() > 0 {
				b.WriteByte('-')
				previousDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func validateSlug(field, value string) error {
	if len(value) < 1 {
		return fmt.Errorf("%w: %s is required", ErrInvalid, field)
	}
	if len(value) > 80 {
		return fmt.Errorf("%w: %s is too long", ErrInvalid, field)
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return fmt.Errorf("%w: %s contains unsupported characters", ErrInvalid, field)
	}
	return nil
}

func validateHumanText(field, value string, minRunes, maxRunes, maxBytes int) error {
	runes := []rune(value)
	if len(runes) < minRunes {
		return fmt.Errorf("%w: %s is required", ErrInvalid, field)
	}
	if len(runes) > maxRunes || len(value) > maxBytes {
		return fmt.Errorf("%w: %s is too long", ErrInvalid, field)
	}
	for _, r := range runes {
		if unicode.IsControl(r) || isBidiOverride(r) {
			return fmt.Errorf("%w: %s contains unsupported control text", ErrInvalid, field)
		}
	}
	return nil
}

func validatePolicyText(key, value string) error {
	if key = strings.TrimSpace(key); key == "" || len(key) > 80 {
		return fmt.Errorf("%w: invalid protection policy key", ErrInvalid)
	}
	if len(value) > 240 {
		return fmt.Errorf("%w: protection policy value is too long", ErrInvalid)
	}
	return validateHumanText("protection_policy", value, 0, 120, 240)
}

func isBidiOverride(r rune) bool {
	return r == '\u202a' || r == '\u202b' || r == '\u202c' || r == '\u202d' || r == '\u202e' || r == '\u2066' || r == '\u2067' || r == '\u2068' || r == '\u2069'
}
