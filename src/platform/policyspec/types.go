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
	Version        int                  `yaml:"version" validate:"required"`
	EffectiveAt    string               `yaml:"effective_at" validate:"required"`
	StateMachine   []LifecycleState     `yaml:"state_machine" validate:"required,min=1"`
	Transitions    []LifecycleTransition `yaml:"transitions" validate:"required,min=1"`
	Windows        []Window             `yaml:"windows" validate:"required,min=1"`
	Export         RetentionExport      `yaml:"export" validate:"required"`
	Extensions     RetentionExtensions  `yaml:"extensions" validate:"required"`
	LegalHold      RetentionLegalHold   `yaml:"legal_hold" validate:"required"`
	Deletion       RetentionDeletion    `yaml:"deletion" validate:"required"`
	AnonymizedData RetentionAnonymized  `yaml:"anonymized_data" validate:"required"`
	Changes        RetentionChanges     `yaml:"changes" validate:"required"`
}

type LifecycleState struct {
	Key   string `yaml:"key" validate:"required"`
	Label string `yaml:"label" validate:"required"`
	Blurb string `yaml:"blurb" validate:"required"`
}

type LifecycleTransition struct {
	From    string `yaml:"from" validate:"required"`
	To      string `yaml:"to" validate:"required"`
	Trigger string `yaml:"trigger" validate:"required"`
	Days    int    `yaml:"days,omitempty"`
}

type Window struct {
	ID              string      `yaml:"id" validate:"required"`
	Label           string      `yaml:"label" validate:"required"`
	Description     string      `yaml:"description" validate:"required"`
	Source          string      `yaml:"source,omitempty"`
	Active          WindowValue `yaml:"active" validate:"required"`
	PastDue         WindowValue `yaml:"past_due" validate:"required"`
	Suspended       WindowValue `yaml:"suspended" validate:"required"`
	PendingDeletion WindowValue `yaml:"pending_deletion" validate:"required"`
}

// WindowValue is a discriminated union tagged by Kind. Each Kind uses a
// different subset of the numeric fields; the loader verifies the
// combination is legal.
type WindowValue struct {
	Kind  string `yaml:"kind" validate:"required,oneof=preserved per_user_policy delete_with_parent not_provided delete_after ttl_days retain_years"`
	Days  int    `yaml:"days,omitempty"`
	Years int    `yaml:"years,omitempty"`
}

type RetentionExport struct {
	AvailableDuring []string               `yaml:"available_during" validate:"required"`
	PostClosureDays int                    `yaml:"post_closure_days" validate:"required"`
	ResetsClock     bool                   `yaml:"resets_clock"`
	Delivery        string                 `yaml:"delivery" validate:"required"`
	Formats         []RetentionExportFormat `yaml:"formats" validate:"required,min=1"`
}

type RetentionExportFormat struct {
	Class  string `yaml:"class" validate:"required"`
	Format string `yaml:"format" validate:"required"`
}

type RetentionExtensions struct {
	AllowMultiple     bool     `yaml:"allow_multiple"`
	ClockBehavior     string   `yaml:"clock_behavior" validate:"required"`
	DeclineConditions string   `yaml:"decline_conditions" validate:"required"`
	AuditedFields     []string `yaml:"audited_fields" validate:"required,min=1"`
}

type RetentionLegalHold struct {
	Behavior string `yaml:"behavior" validate:"required"`
}

type RetentionDeletion struct {
	SoftDelete                bool     `yaml:"soft_delete"`
	RecoverableAfterExecution bool     `yaml:"recoverable_after_execution"`
	Methods                   []string `yaml:"methods" validate:"required,min=1"`
}

type RetentionAnonymized struct {
	RetainedIndefinitely bool   `yaml:"retained_indefinitely"`
	Description          string `yaml:"description" validate:"required"`
}

type RetentionChanges struct {
	NoticeDays           int    `yaml:"notice_days" validate:"required"`
	NotificationChannel  string `yaml:"notification_channel" validate:"required"`
	PriorVersionsSurface string `yaml:"prior_versions_surface" validate:"required"`
}

// ─── subprocessors ─────────────────────────────────────────────────────────

type Subprocessors struct {
	Version             int                  `yaml:"version" validate:"required"`
	EffectiveAt         string               `yaml:"effective_at" validate:"required"`
	Subprocessors       []Subprocessor       `yaml:"subprocessors" validate:"required,min=1"`
	ChangeNotification  ChangeNotification   `yaml:"change_notification" validate:"required"`
}

type Subprocessor struct {
	ID                 string   `yaml:"id" validate:"required"`
	Name               string   `yaml:"name" validate:"required"`
	Purpose            string   `yaml:"purpose" validate:"required"`
	DataCategories     []string `yaml:"data_categories" validate:"required,min=1"`
	ProcessingLocation string   `yaml:"processing_location" validate:"required"`
	DPAURL             string   `yaml:"dpa_url" validate:"required,url"`
}

type ChangeNotification struct {
	Channel       string `yaml:"channel" validate:"required"`
	LeadTimeDays  int    `yaml:"lead_time_days" validate:"required"`
}

// ─── ropa ──────────────────────────────────────────────────────────────────

type ROPA struct {
	Version              int                  `yaml:"version" validate:"required"`
	EffectiveAt          string               `yaml:"effective_at" validate:"required"`
	ProcessingActivities []ProcessingActivity `yaml:"processing_activities" validate:"required,min=1"`
}

type ProcessingActivity struct {
	ID             string   `yaml:"id" validate:"required"`
	Role           string   `yaml:"role" validate:"required,oneof=controller processor"`
	Purpose        string   `yaml:"purpose" validate:"required"`
	DataCategories []string `yaml:"data_categories" validate:"required,min=1"`
	LawfulBasis    string   `yaml:"lawful_basis" validate:"required"`
	RetentionRef   string   `yaml:"retention_ref" validate:"required"`
}

// ─── contacts ──────────────────────────────────────────────────────────────

type Contacts struct {
	Version     int                `yaml:"version" validate:"required"`
	EffectiveAt string             `yaml:"effective_at" validate:"required"`
	Mailboxes   ContactsMailboxes  `yaml:"mailboxes" validate:"required"`
	Routing     string             `yaml:"routing" validate:"required"`
}

type ContactsMailboxes struct {
	Policy   string `yaml:"policy" validate:"required"`
	Privacy  string `yaml:"privacy" validate:"required"`
	Security string `yaml:"security" validate:"required"`
	DPO      string `yaml:"dpo" validate:"required"`
	Abuse    string `yaml:"abuse" validate:"required"`
	Legal    string `yaml:"legal" validate:"required"`
}

// ─── versions ──────────────────────────────────────────────────────────────

type Versions struct {
	Entries []VersionEntry `yaml:"entries" validate:"required,min=1"`
}

type VersionEntry struct {
	Date     string   `yaml:"date" validate:"required"`
	Version  string   `yaml:"version" validate:"required"`
	Policies []string `yaml:"policies" validate:"required,min=1"`
	Summary  string   `yaml:"summary" validate:"required"`
}
