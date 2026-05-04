package profile

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/language"
	"golang.org/x/text/unicode/norm"
)

const (
	DefaultLocale      = "en-US"
	DefaultTimezone    = "UTC"
	DefaultTimeDisplay = "utc"
	DefaultTheme       = "system"
	DefaultSurface     = "executions"
	TimeDisplayUTC     = "utc"
	TimeDisplayLocal   = "local"
	ThemeSystem        = "system"
	ThemeLight         = "light"
	ThemeDark          = "dark"

	zitadelGenericProjectRolesClaim = "urn:zitadel:iam:org:project:roles"
)

var (
	ErrInvalidInput        = errors.New("invalid input")
	ErrConflict            = errors.New("version conflict")
	ErrNotFound            = errors.New("not found")
	ErrStoreUnavailable    = errors.New("profile store unavailable")
	ErrIdentityUnavailable = errors.New("iam service unavailable")
)

type Principal struct {
	Subject string
	OrgID   string
	Email   string
	Raw     map[string]any
}

type IdentitySummary struct {
	Version     int32
	Email       string
	GivenName   string
	FamilyName  string
	DisplayName string
	SyncedAt    *time.Time
}

type Preferences struct {
	Version        int32
	Locale         string
	Timezone       string
	TimeDisplay    string
	Theme          string
	DefaultSurface string
	UpdatedAt      time.Time
	UpdatedBy      string
}

type Snapshot struct {
	SubjectID   string
	OrgID       string
	Identity    IdentitySummary
	Preferences Preferences
}

type UpdateIdentityRequest struct {
	Version     int32
	GivenName   string
	FamilyName  string
	DisplayName string
}

type PutPreferencesRequest struct {
	Version        int32
	Locale         string
	Timezone       string
	TimeDisplay    string
	Theme          string
	DefaultSurface string
}

type IdentityWriter interface {
	UpdateHumanProfile(ctx context.Context, subjectID string, input UpdateIdentityRequest, bearerToken string) (IdentitySummary, error)
}

func ValidatePrincipal(principal Principal) error {
	if strings.TrimSpace(principal.Subject) == "" {
		return fmt.Errorf("%w: subject is required", ErrInvalidInput)
	}
	if strings.TrimSpace(principal.OrgID) == "" {
		return fmt.Errorf("%w: org_id is required", ErrInvalidInput)
	}
	if credentialID, _ := principal.Raw["verself:credential_id"].(string); strings.TrimSpace(credentialID) != "" {
		return fmt.Errorf("%w: api credentials cannot mutate human profiles", ErrInvalidInput)
	}
	if !hasGenericProjectRolesClaim(principal.Raw) {
		return fmt.Errorf("%w: human token marker is required", ErrInvalidInput)
	}
	return nil
}

func hasGenericProjectRolesClaim(claims map[string]any) bool {
	// ZITADEL access tokens here omit email, so the generic roles claim is the current human-token discriminator.
	value, ok := claims[zitadelGenericProjectRolesClaim]
	if !ok {
		return false
	}
	roles, ok := value.(map[string]any)
	return ok && len(roles) > 0
}

func NormalizeIdentityInput(input UpdateIdentityRequest) (UpdateIdentityRequest, error) {
	input.GivenName = normalizeHumanText(input.GivenName)
	input.FamilyName = normalizeHumanText(input.FamilyName)
	input.DisplayName = normalizeHumanText(input.DisplayName)
	if input.Version < 0 {
		return input, fmt.Errorf("%w: version must be non-negative", ErrInvalidInput)
	}
	if err := validateHumanText("given_name", input.GivenName, 1, 100, 100); err != nil {
		return input, err
	}
	if err := validateHumanText("family_name", input.FamilyName, 1, 100, 100); err != nil {
		return input, err
	}
	if input.DisplayName == "" {
		input.DisplayName = strings.TrimSpace(input.GivenName + " " + input.FamilyName)
	}
	if err := validateHumanText("display_name", input.DisplayName, 1, 200, 200); err != nil {
		return input, err
	}
	return input, nil
}

func NormalizePreferencesInput(input PutPreferencesRequest) (PutPreferencesRequest, error) {
	input.Locale = strings.TrimSpace(input.Locale)
	input.Timezone = strings.TrimSpace(input.Timezone)
	input.TimeDisplay = strings.TrimSpace(input.TimeDisplay)
	input.Theme = strings.TrimSpace(input.Theme)
	input.DefaultSurface = strings.TrimSpace(input.DefaultSurface)
	if input.Version < 0 {
		return input, fmt.Errorf("%w: version must be non-negative", ErrInvalidInput)
	}
	if input.Locale == "" {
		input.Locale = DefaultLocale
	}
	tag, err := language.Parse(input.Locale)
	if err != nil {
		return input, fmt.Errorf("%w: locale is invalid", ErrInvalidInput)
	}
	input.Locale = tag.String()
	if input.Timezone == "" {
		input.Timezone = DefaultTimezone
	}
	if _, err := time.LoadLocation(input.Timezone); err != nil {
		return input, fmt.Errorf("%w: timezone is invalid", ErrInvalidInput)
	}
	switch input.TimeDisplay {
	case TimeDisplayUTC, TimeDisplayLocal:
	case "":
		input.TimeDisplay = DefaultTimeDisplay
	default:
		return input, fmt.Errorf("%w: time_display is invalid", ErrInvalidInput)
	}
	switch input.Theme {
	case ThemeSystem, ThemeLight, ThemeDark:
	case "":
		input.Theme = DefaultTheme
	default:
		return input, fmt.Errorf("%w: theme is invalid", ErrInvalidInput)
	}
	if input.DefaultSurface == "" {
		input.DefaultSurface = DefaultSurface
	}
	if err := validateIdentifierText("default_surface", input.DefaultSurface, 80); err != nil {
		return input, err
	}
	return input, nil
}

func DefaultPreferences(now time.Time) Preferences {
	return Preferences{
		Version:        0,
		Locale:         DefaultLocale,
		Timezone:       DefaultTimezone,
		TimeDisplay:    DefaultTimeDisplay,
		Theme:          DefaultTheme,
		DefaultSurface: DefaultSurface,
		UpdatedAt:      now.UTC(),
	}
}

func normalizeHumanText(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Join(strings.Fields(value), " ")
	return norm.NFC.String(value)
}

func validateHumanText(field, value string, minRunes, maxRunes, maxBytes int) error {
	runes := []rune(value)
	if len(runes) < minRunes {
		return fmt.Errorf("%w: %s is required", ErrInvalidInput, field)
	}
	if len(runes) > maxRunes || len(value) > maxBytes {
		return fmt.Errorf("%w: %s is too long", ErrInvalidInput, field)
	}
	for _, r := range runes {
		if unicode.IsControl(r) || isBidiOverride(r) {
			return fmt.Errorf("%w: %s contains unsupported control text", ErrInvalidInput, field)
		}
	}
	return nil
}

func validateIdentifierText(field, value string, maxBytes int) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%w: %s is required", ErrInvalidInput, field)
	}
	if len(value) > maxBytes {
		return fmt.Errorf("%w: %s is too long", ErrInvalidInput, field)
	}
	for _, r := range value {
		if r != '-' && r != '_' && r != '.' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return fmt.Errorf("%w: %s contains unsupported characters", ErrInvalidInput, field)
		}
	}
	return nil
}

func isBidiOverride(r rune) bool {
	switch r {
	case '\u202A', '\u202B', '\u202C', '\u202D', '\u202E', '\u2066', '\u2067', '\u2068', '\u2069':
		return true
	default:
		return false
	}
}
