package dto

import "time"

type ProfileSnapshot struct {
	SubjectID   string             `json:"subject_id" doc:"Zitadel human subject ID."`
	OrgID       string             `json:"org_id" doc:"Active organization ID from the validated token."`
	Identity    ProfileIdentity    `json:"identity"`
	Preferences ProfilePreferences `json:"preferences"`
}

type ProfileIdentity struct {
	Version     int32      `json:"version" minimum:"0" maximum:"2147483647"`
	Email       string     `json:"email"`
	GivenName   string     `json:"given_name"`
	FamilyName  string     `json:"family_name"`
	DisplayName string     `json:"display_name"`
	SyncedAt    *time.Time `json:"synced_at,omitempty"`
}

type ProfilePreferences struct {
	Version        int32     `json:"version" minimum:"0" maximum:"2147483647"`
	Locale         string    `json:"locale"`
	Timezone       string    `json:"timezone"`
	TimeDisplay    string    `json:"time_display" enum:"utc,local"`
	Theme          string    `json:"theme" enum:"system,light,dark"`
	DefaultSurface string    `json:"default_surface,omitempty"`
	UpdatedAt      time.Time `json:"updated_at"`
	UpdatedBy      string    `json:"updated_by"`
}

type ProfileUpdateIdentityRequest struct {
	Version     int32  `json:"version" minimum:"0" maximum:"2147483647"`
	GivenName   string `json:"given_name" required:"true" maxLength:"100"`
	FamilyName  string `json:"family_name" required:"true" maxLength:"100"`
	DisplayName string `json:"display_name,omitempty" maxLength:"200"`
}

type ProfilePutPreferencesRequest struct {
	Version        int32  `json:"version" minimum:"0" maximum:"2147483647"`
	Locale         string `json:"locale" required:"true" maxLength:"35"`
	Timezone       string `json:"timezone" required:"true" maxLength:"64"`
	TimeDisplay    string `json:"time_display" required:"true" enum:"utc,local"`
	Theme          string `json:"theme" required:"true" enum:"system,light,dark"`
	DefaultSurface string `json:"default_surface,omitempty" maxLength:"80"`
}

type ProfileDataRightsRequest struct {
	RequestID   string    `json:"request_id" required:"true" maxLength:"128"`
	RequestedAt time.Time `json:"requested_at" required:"true"`
	RequestedBy string    `json:"requested_by" required:"true" maxLength:"200"`
	Traceparent string    `json:"traceparent,omitempty" maxLength:"200"`
	OrgID       string    `json:"org_id,omitempty" maxLength:"200"`
	SubjectID   string    `json:"subject_id,omitempty" maxLength:"200"`
}

type ProfileDataRightsManifest struct {
	RequestID          string                              `json:"request_id"`
	RequestType        string                              `json:"request_type"`
	Status             string                              `json:"status"`
	OrgID              string                              `json:"org_id,omitempty"`
	SubjectID          string                              `json:"subject_id,omitempty"`
	Artifacts          []ProfileDataRightsArtifact         `json:"artifacts"`
	ErasureActions     []ProfileDataRightsErasureAction    `json:"erasure_actions"`
	RetainedCategories []ProfileDataRightsRetainedCategory `json:"retained_categories"`
	RecordCounts       map[string]string                   `json:"record_counts,omitempty"`
	CompletedAt        time.Time                           `json:"completed_at"`
}

type ProfileDataRightsArtifact struct {
	Path        string `json:"path"`
	ContentType string `json:"content_type"`
	Rows        string `json:"rows"`
	Bytes       string `json:"bytes"`
	SHA256      string `json:"sha256"`
}

type ProfileDataRightsErasureAction struct {
	Name        string `json:"name"`
	Rows        string `json:"rows"`
	Description string `json:"description,omitempty"`
}

type ProfileDataRightsRetainedCategory struct {
	Category string `json:"category"`
	Reason   string `json:"reason"`
}

type ProfileDataRightsRequestStatus struct {
	RequestID string                    `json:"request_id"`
	Manifest  ProfileDataRightsManifest `json:"manifest"`
}
