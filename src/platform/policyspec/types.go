// Package policyspec loads the machine-readable policy source at
// src/platform/policies/*.yml into typed Go structs. The same files are
// consumed by the TypeScript render path in the platform app, so a shape
// mismatch breaks the build on both sides.
//
// Callers use Load() to get a Spec, inspect .Retention / .Subprocessors /
// .ROPA, and optionally EmitBootSpan() to record a policy-load event in
// ClickHouse at service startup. That span is how we satisfy the agent
// contract's "ClickHouse traces are the only admitted proof" rule for
// compliance work: the span records the windows that were in effect on the
// box at a particular moment.
package policyspec

import "time"

// Spec is the parsed, validated collection of all policy YAML files.
type Spec struct {
	Retention     Retention
	Subprocessors Subprocessors
	ROPA          ROPA
	Contacts      Contacts
	Versions      Versions
	LoadedAt      time.Time
	SourceDir     string
}

// ─── retention ─────────────────────────────────────────────────────────────

type Retention struct {
	Version        int                   `yaml:"version"`
	EffectiveAt    string                `yaml:"effective_at"`
	StateMachine   []LifecycleState      `yaml:"state_machine"`
	Transitions    []LifecycleTransition `yaml:"transitions"`
	Windows        []Window              `yaml:"windows"`
	Export         RetentionExport       `yaml:"export"`
	Extensions     RetentionExtensions   `yaml:"extensions"`
	LegalHold      RetentionLegalHold    `yaml:"legal_hold"`
	Deletion       RetentionDeletion     `yaml:"deletion"`
	AnonymizedData RetentionAnonymized   `yaml:"anonymized_data"`
	Changes        RetentionChanges      `yaml:"changes"`
}

type LifecycleState struct {
	Key   string `yaml:"key"`
	Label string `yaml:"label"`
	Blurb string `yaml:"blurb"`
}

type LifecycleTransition struct {
	From    string `yaml:"from"`
	To      string `yaml:"to"`
	Trigger string `yaml:"trigger"`
	Days    int    `yaml:"days,omitempty"`
}

type Window struct {
	ID              string      `yaml:"id"`
	Label           string      `yaml:"label"`
	Description     string      `yaml:"description"`
	Source          string      `yaml:"source,omitempty"`
	Active          WindowValue `yaml:"active"`
	PastDue         WindowValue `yaml:"past_due"`
	Suspended       WindowValue `yaml:"suspended"`
	PendingDeletion WindowValue `yaml:"pending_deletion"`
}

// WindowValue is a discriminated union tagged by Kind. Each Kind uses a
// different subset of the numeric fields; validateWindowValue enforces the
// legal combinations.
type WindowValue struct {
	Kind  string `yaml:"kind"`
	Days  int    `yaml:"days,omitempty"`
	Years int    `yaml:"years,omitempty"`
}

type RetentionExport struct {
	AvailableDuring []string                `yaml:"available_during"`
	PostClosureDays int                     `yaml:"post_closure_days"`
	ResetsClock     bool                    `yaml:"resets_clock"`
	Delivery        string                  `yaml:"delivery"`
	Formats         []RetentionExportFormat `yaml:"formats"`
}

type RetentionExportFormat struct {
	Class  string `yaml:"class"`
	Format string `yaml:"format"`
}

type RetentionExtensions struct {
	AllowMultiple     bool     `yaml:"allow_multiple"`
	ClockBehavior     string   `yaml:"clock_behavior"`
	DeclineConditions string   `yaml:"decline_conditions"`
	AuditedFields     []string `yaml:"audited_fields"`
}

type RetentionLegalHold struct {
	Behavior string `yaml:"behavior"`
}

type RetentionDeletion struct {
	SoftDelete                bool     `yaml:"soft_delete"`
	RecoverableAfterExecution bool     `yaml:"recoverable_after_execution"`
	Methods                   []string `yaml:"methods"`
}

type RetentionAnonymized struct {
	RetainedIndefinitely bool   `yaml:"retained_indefinitely"`
	Description          string `yaml:"description"`
}

type RetentionChanges struct {
	NoticeDays           int    `yaml:"notice_days"`
	NotificationChannel  string `yaml:"notification_channel"`
	PriorVersionsSurface string `yaml:"prior_versions_surface"`
}

// ─── subprocessors ─────────────────────────────────────────────────────────

type Subprocessors struct {
	Version            int                `yaml:"version"`
	EffectiveAt        string             `yaml:"effective_at"`
	Subprocessors      []Subprocessor     `yaml:"subprocessors"`
	ChangeNotification ChangeNotification `yaml:"change_notification"`
}

type Subprocessor struct {
	ID                 string   `yaml:"id"`
	Name               string   `yaml:"name"`
	Purpose            string   `yaml:"purpose"`
	DataCategories     []string `yaml:"data_categories"`
	ProcessingLocation string   `yaml:"processing_location"`
	DPAURL             string   `yaml:"dpa_url"`
}

type ChangeNotification struct {
	Channel      string `yaml:"channel"`
	LeadTimeDays int    `yaml:"lead_time_days"`
}

// ─── ropa ──────────────────────────────────────────────────────────────────

type ROPA struct {
	Version              int                  `yaml:"version"`
	EffectiveAt          string               `yaml:"effective_at"`
	ProcessingActivities []ProcessingActivity `yaml:"processing_activities"`
}

type ProcessingActivity struct {
	ID             string   `yaml:"id"`
	Role           string   `yaml:"role"`
	Purpose        string   `yaml:"purpose"`
	DataCategories []string `yaml:"data_categories"`
	LawfulBasis    string   `yaml:"lawful_basis"`
	RetentionRef   string   `yaml:"retention_ref"`
}

// ─── contacts ──────────────────────────────────────────────────────────────

type Contacts struct {
	Version     int               `yaml:"version"`
	EffectiveAt string            `yaml:"effective_at"`
	Mailboxes   ContactsMailboxes `yaml:"mailboxes"`
	Routing     string            `yaml:"routing"`
}

type ContactsMailboxes struct {
	Policy   string `yaml:"policy"`
	Privacy  string `yaml:"privacy"`
	Security string `yaml:"security"`
	DPO      string `yaml:"dpo"`
	Abuse    string `yaml:"abuse"`
	Legal    string `yaml:"legal"`
}

// ─── versions ──────────────────────────────────────────────────────────────

type Versions struct {
	Entries []VersionEntry `yaml:"entries"`
}

type VersionEntry struct {
	Date     string   `yaml:"date"`
	Version  string   `yaml:"version"`
	Policies []string `yaml:"policies"`
	Summary  string   `yaml:"summary"`
}
