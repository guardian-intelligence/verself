package governance

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/verself/governance-service/internal/ocsf"
	"github.com/verself/governance-service/internal/store"
	"go.opentelemetry.io/otel/attribute"
)

const zeroHMAC = "0000000000000000000000000000000000000000000000000000000000000000"

type (
	AuthorizationDecision = ocsf.AuthorizationDecision
	Status                = ocsf.Status
)

const (
	AuthorizationAllowed = ocsf.AuthorizationAllowed
	AuthorizationDenied  = ocsf.AuthorizationDenied
	StatusSuccess        = ocsf.StatusSuccess
	StatusFailure        = ocsf.StatusFailure
	StatusOther          = ocsf.StatusOther
)

type APIActivityRecord struct {
	OrgID                 string
	APIService            string
	APIOperation          string
	APIEventCode          string
	APIAction             string
	APIVersion            string
	ActorType             string
	ActorID               string
	ActorDisplayName      string
	ActorEmail            string
	CredentialID          string
	Permission            string
	Resources             []APIActivityResource
	HTTPRequest           APIActivityHTTPRequest
	HTTPResponse          APIActivityHTTPResponse
	AuthorizationDecision ocsf.AuthorizationDecision
	Status                ocsf.Status
	StatusCode            string
	StatusDetail          string
	TraceID               string
	SpanID                string
	HMACKeyID             string
	Unmapped              map[string]any
	Time                  time.Time
}

type APIActivityHTTPRequest struct {
	UID           string
	Method        string
	Route         string
	SafeParams    string
	UserAgent     string
	XForwardedFor string
	Referrer      string
	Host          string
	Scheme        string
	ClientIP      string
	SourceName    string
}

type APIActivityHTTPResponse struct {
	Code    uint16
	Message string
	Status  string
}

type APIActivityResource struct {
	Type     string
	UID      string
	Name     string
	FullName string
	Role     string
	RoleID   uint8
}

type APIActivityRow struct {
	Time                    time.Time `ch:"time"`
	EventDate               time.Time `ch:"event_date"`
	MetadataUID             uuid.UUID `ch:"metadata_uid"`
	OrgID                   string    `ch:"org_id"`
	Sequence                uint64    `ch:"sequence"`
	OCSFVersion             string    `ch:"ocsf_version"`
	CategoryUID             uint8     `ch:"category_uid"`
	CategoryName            string    `ch:"category_name"`
	ClassUID                uint16    `ch:"class_uid"`
	ClassName               string    `ch:"class_name"`
	TypeUID                 uint32    `ch:"type_uid"`
	ActivityID              uint8     `ch:"activity_id"`
	ActivityName            string    `ch:"activity_name"`
	ActionID                uint8     `ch:"action_id"`
	Action                  string    `ch:"action"`
	StatusID                uint8     `ch:"status_id"`
	Status                  string    `ch:"status"`
	StatusCode              string    `ch:"status_code"`
	SeverityID              uint8     `ch:"severity_id"`
	Severity                string    `ch:"severity"`
	APIService              string    `ch:"api_service"`
	APIOperation            string    `ch:"api_operation"`
	APIVersion              string    `ch:"api_version"`
	ActorType               string    `ch:"actor_type"`
	ActorUID                string    `ch:"actor_uid"`
	ActorName               string    `ch:"actor_name"`
	CredentialUID           string    `ch:"credential_uid"`
	PrimaryResourceType     string    `ch:"primary_resource_type"`
	PrimaryResourceUID      string    `ch:"primary_resource_uid"`
	PrimaryResourceName     string    `ch:"primary_resource_name"`
	PrimaryResourceFullName string    `ch:"primary_resource_full_name"`
	Permission              string    `ch:"permission"`
	HTTPRequestUID          string    `ch:"http_request_uid"`
	HTTPMethod              string    `ch:"http_method"`
	HTTPRoute               string    `ch:"http_route"`
	HTTPArgs                string    `ch:"http_args"`
	HTTPUserAgent           string    `ch:"http_user_agent"`
	SrcEndpointIP           string    `ch:"src_endpoint_ip"`
	SrcEndpointName         string    `ch:"src_endpoint_name"`
	HTTPResponseCode        uint16    `ch:"http_response_code"`
	TraceUID                string    `ch:"trace_uid"`
	SpanUID                 string    `ch:"span_uid"`
	OCSFSHA256              string    `ch:"ocsf_sha256"`
	PrevHMAC                string    `ch:"prev_hmac"`
	RowHMAC                 string    `ch:"row_hmac"`
	HMACKeyID               string    `ch:"hmac_key_id"`
}

type APIActivityPayload struct {
	MetadataUID uuid.UUID `ch:"metadata_uid"`
	OrgID       string    `ch:"org_id"`
	EventDate   time.Time `ch:"event_date"`
	OCSFJSON    string    `ch:"ocsf_json"`
	OCSFSHA256  string    `ch:"ocsf_sha256"`
}

type APIActivityResourceRow struct {
	MetadataUID uuid.UUID `ch:"metadata_uid"`
	OrgID       string    `ch:"org_id"`
	EventDate   time.Time `ch:"event_date"`
	Ordinal     uint8     `ch:"resource_ordinal"`
	RoleID      uint8     `ch:"resource_role_id"`
	Role        string    `ch:"resource_role"`
	Type        string    `ch:"resource_type"`
	UID         string    `ch:"resource_uid"`
	Name        string    `ch:"resource_name"`
	FullName    string    `ch:"resource_full_name"`
}

type APIActivityListFilters struct {
	Limit         int
	Cursor        string
	Order         string
	ActorUID      string
	ActorType     string
	APIService    string
	APIOperation  string
	ActivityID    uint8
	CredentialUID string
	ResourceUID   string
	ResourceType  string
	StatusID      uint8
	StatusCode    string
	TraceUID      string
}

type APIActivityListPage struct {
	Events     []APIActivityRow
	NextCursor string
	Limit      int
}

func (s *Service) RecordAPIActivity(ctx context.Context, record APIActivityRecord) (*APIActivityRow, error) {
	ctx, span := tracer.Start(ctx, "governance.ocsf.api_activity.record")
	defer span.End()
	if err := s.Validate(); err != nil {
		return nil, err
	}
	record = s.normalizeAPIActivityRecord(record)
	builder := ocsf.Builder{Product: ocsf.Product{
		Name:       ptrString("Verself Governance Service"),
		Uid:        ptrString(firstNonEmpty(s.InstallationID, "verself")),
		VendorName: ptrString("Guardian Intelligence"),
		Version:    ptrString(ocsf.SchemaVersion),
	}}
	built, err := builder.Build(ctx, ocsf.APIActivityInput{
		OrgID:        record.OrgID,
		EventUID:     uuid.New(),
		Time:         record.Time,
		Operation:    ocsf.Operation{Service: record.APIService, ID: record.APIOperation, EventCode: record.APIEventCode, Action: record.APIAction, Permission: record.Permission, Version: record.APIVersion},
		Actor:        ocsf.ActorIdentity{Type: record.ActorType, UID: record.ActorID, DisplayName: record.ActorDisplayName, Email: record.ActorEmail, CredentialUID: record.CredentialID},
		Resources:    ocsfResources(record.Resources),
		Request:      ocsf.Request{UID: record.HTTPRequest.UID, Method: record.HTTPRequest.Method, Route: record.HTTPRequest.Route, SafeParams: record.HTTPRequest.SafeParams, UserAgent: record.HTTPRequest.UserAgent, XForwardedFor: record.HTTPRequest.XForwardedFor, Referrer: record.HTTPRequest.Referrer, Host: record.HTTPRequest.Host, Scheme: record.HTTPRequest.Scheme, ClientIP: record.HTTPRequest.ClientIP, SourceName: record.HTTPRequest.SourceName},
		Response:     ocsf.Response{Code: record.HTTPResponse.Code, Message: record.HTTPResponse.Message, Status: record.HTTPResponse.Status},
		Decision:     record.AuthorizationDecision,
		Status:       record.Status,
		StatusCode:   record.StatusCode,
		StatusDetail: record.StatusDetail,
		Trace:        ocsf.TraceContext{TraceID: record.TraceID, SpanID: record.SpanID, Service: record.APIService},
		HMACKeyID:    record.HMACKeyID,
		Unmapped:     record.Unmapped,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: build OCSF API activity: %v", ErrInvalidArgument, err)
	}
	row, err := rowFromAPIActivityRecord(record, built)
	if err != nil {
		return nil, fmt.Errorf("%w: project OCSF API activity row: %v", ErrInvalidArgument, err)
	}
	if err := s.assignAPIActivitySequence(ctx, row, built.JSON, built.Resources); err != nil {
		return nil, err
	}
	if err := s.projectAPIActivity(ctx, store.New(s.PG), row, built.JSON, built.Resources); err != nil {
		return nil, err
	}
	span.SetAttributes(
		attribute.String("verself.org_id", row.OrgID),
		attribute.String("ocsf.api_service", row.APIService),
		attribute.String("ocsf.api_operation", row.APIOperation),
		attribute.String("ocsf.status", row.Status),
		attribute.Int64("verself.api_activity_sequence", int64FromUint64(row.Sequence, "api activity sequence")),
	)
	return row, nil
}

func (s *Service) ProjectPendingAPIActivities(ctx context.Context, limit int) (int, error) {
	ctx, span := tracer.Start(ctx, "governance.ocsf.api_activity.project_pending")
	defer span.End()
	if err := s.Validate(); err != nil {
		return 0, err
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, fmt.Errorf("%w: begin API activity projection tx: %v", ErrStore, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qtx := store.New(tx)
	rows, err := qtx.ClaimPendingAPIActivityRows(ctx, store.ClaimPendingAPIActivityRowsParams{LimitCount: int32(limit)})
	if err != nil {
		return 0, fmt.Errorf("%w: list pending API activities: %v", ErrStore, err)
	}

	projected := 0
	for _, pending := range rows {
		var row APIActivityRow
		if err := json.Unmarshal([]byte(pending.RowJson), &row); err != nil {
			return projected, fmt.Errorf("%w: unmarshal pending API activity row: %v", ErrStore, err)
		}
		var resources []ocsf.ActivityResource
		if err := json.Unmarshal([]byte(pending.ResourcesJson), &resources); err != nil {
			return projected, fmt.Errorf("%w: unmarshal pending API activity resources: %v", ErrStore, err)
		}
		if err := s.projectAPIActivity(ctx, qtx, &row, pending.OcsfJson, resources); err != nil {
			return projected, err
		}
		projected++
	}
	if err := tx.Commit(ctx); err != nil {
		return projected, fmt.Errorf("%w: commit API activity projection tx: %v", ErrStore, err)
	}
	span.SetAttributes(attribute.Int("verself.api_activity_projected_count", projected))
	return projected, nil
}

func (s *Service) normalizeAPIActivityRecord(record APIActivityRecord) APIActivityRecord {
	record.OrgID = strings.TrimSpace(record.OrgID)
	record.APIService = strings.TrimSpace(record.APIService)
	record.APIOperation = strings.TrimSpace(record.APIOperation)
	record.APIEventCode = strings.TrimSpace(record.APIEventCode)
	record.APIAction = strings.TrimSpace(record.APIAction)
	record.APIVersion = strings.TrimSpace(record.APIVersion)
	record.ActorType = firstNonEmpty(record.ActorType, "user")
	record.ActorID = strings.TrimSpace(record.ActorID)
	record.ActorDisplayName = strings.TrimSpace(record.ActorDisplayName)
	record.ActorEmail = strings.TrimSpace(record.ActorEmail)
	record.CredentialID = strings.TrimSpace(record.CredentialID)
	record.Permission = strings.TrimSpace(record.Permission)
	record.HMACKeyID = firstNonEmpty(record.HMACKeyID, s.HMACKeyID, "default")
	if record.Time.IsZero() {
		record.Time = time.Now().UTC()
	}
	record.Time = record.Time.UTC()
	if record.HTTPResponse.Code == 0 {
		record.HTTPResponse.Code = 500
	}
	record.Resources = s.normalizeAPIActivityResources(record.OrgID, record.Resources)
	return record
}

func (s *Service) normalizeAPIActivityResources(orgID string, resources []APIActivityResource) []APIActivityResource {
	out := make([]APIActivityResource, 0, len(resources))
	for index, resource := range resources {
		resource.Type = strings.TrimSpace(resource.Type)
		resource.UID = strings.TrimSpace(resource.UID)
		resource.Name = strings.TrimSpace(resource.Name)
		resource.FullName = strings.TrimSpace(resource.FullName)
		resource.Role = strings.TrimSpace(resource.Role)
		if resource.RoleID == 0 {
			if index == 0 {
				resource.RoleID = 1
				resource.Role = "Target"
			} else {
				resource.RoleID = 4
				resource.Role = "Related"
			}
		}
		if resource.FullName == "" {
			resource.FullName = s.resourceFullName(orgID, resource).String()
		}
		if resource.Name == "" {
			resource.Name = firstNonEmpty(resource.FullName, resource.UID, resource.Type)
		}
		if resource.Type == "" && resource.UID == "" && resource.Name == "" && resource.FullName == "" {
			continue
		}
		out = append(out, resource)
	}
	return out
}

func (s *Service) resourceFullName(orgID string, resource APIActivityResource) ResourceName {
	if s.InstallationID == "" || orgID == "" {
		return ""
	}
	switch resource.Type {
	case "organization", "api_activity":
		if resource.UID != "" && resource.Type == "api_activity" {
			return ResourceNameAPIActivity(s.InstallationID, orgID, resource.UID)
		}
		return ResourceNameOrg(s.InstallationID, orgID)
	case "data_export":
		return optionalResourceName(resource.UID, ResourceNameDataExport(s.InstallationID, orgID, resource.UID))
	case "organization_member":
		return optionalResourceName(resource.UID, ResourceNameMember(s.InstallationID, orgID, resource.UID))
	case "service_account":
		return optionalResourceName(resource.UID, ResourceNameMachinePrincipal(s.InstallationID, orgID, resource.UID))
	case "api_credential", "opaque_credential":
		return optionalResourceName(resource.UID, ResourceNameCredential(s.InstallationID, orgID, resource.UID))
	case "secret":
		return optionalResourceName(resource.UID, ResourceNameSecret(s.InstallationID, orgID, resource.UID))
	case "variable":
		return optionalResourceName(resource.UID, ResourceNameVariable(s.InstallationID, orgID, resource.UID))
	case "transit_key":
		return optionalResourceName(resource.UID, ResourceNameTransitKey(s.InstallationID, orgID, resource.UID))
	case "run", "execution":
		return optionalResourceName(resource.UID, ResourceNameRun(s.InstallationID, orgID, resource.UID))
	case "execution_schedule":
		return optionalResourceName(resource.UID, ResourceNameSchedule(s.InstallationID, orgID, resource.UID))
	case "github_installation":
		return optionalResourceName(resource.UID, ResourceNameGitHubInstallation(s.InstallationID, orgID, resource.UID))
	default:
		return ""
	}
}

func optionalResourceName(id string, name ResourceName) ResourceName {
	if id == "" {
		return ""
	}
	return name
}

func rowFromAPIActivityRecord(record APIActivityRecord, built ocsf.BuildOutput) (*APIActivityRow, error) {
	primary := primaryOCSFResource(built.Resources)
	event := built.Event
	traceUID := record.TraceID
	spanUID := record.SpanID
	categoryUID, err := uint8FromInt32(event.CategoryUid, "category_uid")
	if err != nil {
		return nil, err
	}
	classUID, err := uint16FromInt32(event.ClassUid, "class_uid")
	if err != nil {
		return nil, err
	}
	typeUID, err := uint32FromInt64(event.TypeUid, "type_uid")
	if err != nil {
		return nil, err
	}
	activityID, err := uint8FromInt32(event.ActivityId, "activity_id")
	if err != nil {
		return nil, err
	}
	actionID, err := uint8FromInt32Pointer(event.ActionId, "action_id")
	if err != nil {
		return nil, err
	}
	statusID, err := uint8FromInt32Pointer(event.StatusId, "status_id")
	if err != nil {
		return nil, err
	}
	severityID, err := uint8FromInt32(event.SeverityId, "severity_id")
	if err != nil {
		return nil, err
	}
	return &APIActivityRow{
		Time:                    record.Time,
		EventDate:               truncateDate(record.Time),
		MetadataUID:             parseMetadataUID(stringValue(built.Event.Metadata.Uid)),
		OrgID:                   record.OrgID,
		OCSFVersion:             event.Metadata.Version,
		CategoryUID:             categoryUID,
		CategoryName:            stringValue(event.CategoryName),
		ClassUID:                classUID,
		ClassName:               stringValue(event.ClassName),
		TypeUID:                 typeUID,
		ActivityID:              activityID,
		ActivityName:            stringValue(event.ActivityName),
		ActionID:                actionID,
		Action:                  stringValue(event.Action),
		StatusID:                statusID,
		Status:                  stringValue(event.Status),
		StatusCode:              stringValue(event.StatusCode),
		SeverityID:              severityID,
		Severity:                stringValue(event.Severity),
		APIService:              record.APIService,
		APIOperation:            record.APIOperation,
		APIVersion:              record.APIVersion,
		ActorType:               record.ActorType,
		ActorUID:                record.ActorID,
		ActorName:               record.ActorDisplayName,
		CredentialUID:           record.CredentialID,
		PrimaryResourceType:     primary.Type,
		PrimaryResourceUID:      primary.UID,
		PrimaryResourceName:     primary.Name,
		PrimaryResourceFullName: primary.FullName,
		Permission:              record.Permission,
		HTTPRequestUID:          record.HTTPRequest.UID,
		HTTPMethod:              record.HTTPRequest.Method,
		HTTPRoute:               record.HTTPRequest.Route,
		HTTPArgs:                record.HTTPRequest.SafeParams,
		HTTPUserAgent:           record.HTTPRequest.UserAgent,
		SrcEndpointIP:           record.HTTPRequest.ClientIP,
		SrcEndpointName:         record.HTTPRequest.SourceName,
		HTTPResponseCode:        record.HTTPResponse.Code,
		TraceUID:                traceUID,
		SpanUID:                 spanUID,
		OCSFSHA256:              built.SHA256Hex,
		HMACKeyID:               record.HMACKeyID,
	}, nil
}

func parseMetadataUID(value string) uuid.UUID {
	id, err := uuid.Parse(value)
	if err != nil {
		return uuid.New()
	}
	return id
}

func ptrString(value string) *string {
	return &value
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func uint8FromInt32Pointer(value *int32, field string) (uint8, error) {
	if value == nil {
		return 0, nil
	}
	return uint8FromInt32(*value, field)
}

func uint8FromInt32(value int32, field string) (uint8, error) {
	if value < 0 || value > 255 {
		return 0, fmt.Errorf("%s out of UInt8 range: %d", field, value)
	}
	return uint8(value), nil
}

func uint16FromInt32(value int32, field string) (uint16, error) {
	if value < 0 || value > 65535 {
		return 0, fmt.Errorf("%s out of UInt16 range: %d", field, value)
	}
	return uint16(value), nil
}

func uint32FromInt64(value int64, field string) (uint32, error) {
	if value < 0 || value > 4294967295 {
		return 0, fmt.Errorf("%s out of UInt32 range: %d", field, value)
	}
	return uint32(value), nil
}

func (s *Service) assignAPIActivitySequence(ctx context.Context, row *APIActivityRow, ocsfJSON string, resources []ocsf.ActivityResource) error {
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("%w: begin API activity chain tx: %v", ErrStore, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qtx := store.New(tx)
	if err := qtx.InitializeAPIActivityChainState(ctx, store.InitializeAPIActivityChainStateParams{
		OrgID:   row.OrgID,
		RowHmac: zeroHMAC,
	}); err != nil {
		return fmt.Errorf("%w: initialize API activity chain: %v", ErrStore, err)
	}

	chain, err := qtx.LockAPIActivityChainState(ctx, store.LockAPIActivityChainStateParams{OrgID: row.OrgID})
	if err != nil {
		return fmt.Errorf("%w: lock API activity chain: %v", ErrStore, err)
	}
	if chain.Sequence < 0 || chain.Sequence == math.MaxInt64 {
		return fmt.Errorf("%w: API activity chain sequence out of range", ErrStore)
	}
	row.Sequence = uint64(chain.Sequence) + 1
	row.PrevHMAC = chain.RowHmac
	row.RowHMAC = s.computeAPIActivityHMAC(row)
	sequence, err := apiActivitySequenceInt64(row.Sequence)
	if err != nil {
		return err
	}
	if err := qtx.AdvanceAPIActivityChainState(ctx, store.AdvanceAPIActivityChainStateParams{
		OrgID:    row.OrgID,
		Sequence: sequence,
		RowHmac:  row.RowHMAC,
	}); err != nil {
		return fmt.Errorf("%w: advance API activity chain: %v", ErrStore, err)
	}
	rowJSON, err := json.Marshal(row)
	if err != nil {
		return fmt.Errorf("%w: marshal API activity row: %v", ErrStore, err)
	}
	resourcesJSON, err := json.Marshal(resources)
	if err != nil {
		return fmt.Errorf("%w: marshal API activity resources: %v", ErrStore, err)
	}
	if err := qtx.InsertAPIActivity(ctx, store.InsertAPIActivityParams{
		OrgID:         row.OrgID,
		Sequence:      sequence,
		MetadataUid:   row.MetadataUID,
		ObservedAt:    pgtype.Timestamptz{Time: row.Time, Valid: true},
		EventDate:     row.EventDate,
		IngestedAt:    pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
		OcsfVersion:   row.OCSFVersion,
		OcsfJson:      ocsfJSON,
		OcsfSha256:    row.OCSFSHA256,
		RowJson:       string(rowJSON),
		ResourcesJson: string(resourcesJSON),
		PrevHmac:      row.PrevHMAC,
		RowHmac:       row.RowHMAC,
		HmacKeyID:     row.HMACKeyID,
	}); err != nil {
		return fmt.Errorf("%w: stage API activity: %v", ErrStore, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("%w: commit API activity chain: %v", ErrStore, err)
	}
	return nil
}

func (s *Service) projectAPIActivity(ctx context.Context, q *store.Queries, row *APIActivityRow, ocsfJSON string, resources []ocsf.ActivityResource) error {
	projected, err := s.apiActivityProjected(ctx, row.MetadataUID)
	if err != nil {
		return err
	}
	if !projected {
		if err := s.insertAPIActivityClickHouse(ctx, row); err != nil {
			return err
		}
	}
	payloadProjected, err := s.apiActivityPayloadProjected(ctx, row.MetadataUID)
	if err != nil {
		return err
	}
	if !payloadProjected {
		if err := s.insertAPIActivityPayloadClickHouse(ctx, row, ocsfJSON); err != nil {
			return err
		}
	}
	resourcesProjected, err := s.apiActivityResourcesProjected(ctx, row.MetadataUID)
	if err != nil {
		return err
	}
	if !resourcesProjected {
		if err := s.insertAPIActivityResourcesClickHouse(ctx, row, resources); err != nil {
			return err
		}
	}
	sequence, err := apiActivitySequenceInt64(row.Sequence)
	if err != nil {
		return err
	}
	return s.markAPIActivityProjected(ctx, q, row.OrgID, sequence)
}

func (s *Service) insertAPIActivityClickHouse(ctx context.Context, row *APIActivityRow) error {
	batch, err := s.CH.PrepareBatch(ctx, "INSERT INTO verself.api_activity_events")
	if err != nil {
		return fmt.Errorf("%w: prepare API activity insert: %v", ErrStore, err)
	}
	if err := batch.AppendStruct(row); err != nil {
		return fmt.Errorf("%w: append API activity: %v", ErrStore, err)
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("%w: send API activity: %v", ErrStore, err)
	}
	return nil
}

func (s *Service) insertAPIActivityPayloadClickHouse(ctx context.Context, row *APIActivityRow, ocsfJSON string) error {
	payload := APIActivityPayload{
		MetadataUID: row.MetadataUID,
		OrgID:       row.OrgID,
		EventDate:   row.EventDate,
		OCSFJSON:    ocsfJSON,
		OCSFSHA256:  row.OCSFSHA256,
	}
	batch, err := s.CH.PrepareBatch(ctx, "INSERT INTO verself.api_activity_payloads")
	if err != nil {
		return fmt.Errorf("%w: prepare API activity payload insert: %v", ErrStore, err)
	}
	if err := batch.AppendStruct(&payload); err != nil {
		return fmt.Errorf("%w: append API activity payload: %v", ErrStore, err)
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("%w: send API activity payload: %v", ErrStore, err)
	}
	return nil
}

func (s *Service) insertAPIActivityResourcesClickHouse(ctx context.Context, row *APIActivityRow, resources []ocsf.ActivityResource) error {
	if len(resources) == 0 {
		return nil
	}
	batch, err := s.CH.PrepareBatch(ctx, "INSERT INTO verself.api_activity_resources")
	if err != nil {
		return fmt.Errorf("%w: prepare API activity resource insert: %v", ErrStore, err)
	}
	for index, resource := range resources {
		if index > math.MaxUint8 {
			return fmt.Errorf("%w: API activity has too many resources", ErrInvalidArgument)
		}
		resourceRow := APIActivityResourceRow{
			MetadataUID: row.MetadataUID,
			OrgID:       row.OrgID,
			EventDate:   row.EventDate,
			Ordinal:     uint8(index),
			RoleID:      resource.RoleID,
			Role:        resource.Role,
			Type:        resource.Type,
			UID:         resource.UID,
			Name:        resource.Name,
			FullName:    resource.FullName,
		}
		if err := batch.AppendStruct(&resourceRow); err != nil {
			return fmt.Errorf("%w: append API activity resource: %v", ErrStore, err)
		}
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("%w: send API activity resources: %v", ErrStore, err)
	}
	return nil
}

func (s *Service) apiActivityProjected(ctx context.Context, metadataUID uuid.UUID) (bool, error) {
	var found uint8
	err := s.CH.QueryRow(ctx, `
		SELECT 1
		FROM verself.api_activity_events
		WHERE metadata_uid = $1
		LIMIT 1
	`, metadataUID).Scan(&found)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("%w: check API activity projection: %v", ErrStore, err)
	}
	return true, nil
}

func (s *Service) apiActivityPayloadProjected(ctx context.Context, metadataUID uuid.UUID) (bool, error) {
	var found uint8
	err := s.CH.QueryRow(ctx, `
		SELECT 1
		FROM verself.api_activity_payloads
		WHERE metadata_uid = $1
		LIMIT 1
	`, metadataUID).Scan(&found)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("%w: check API activity payload projection: %v", ErrStore, err)
	}
	return true, nil
}

func (s *Service) apiActivityResourcesProjected(ctx context.Context, metadataUID uuid.UUID) (bool, error) {
	var count uint64
	if err := s.CH.QueryRow(ctx, `
		SELECT count()
		FROM verself.api_activity_resources
		WHERE metadata_uid = $1
	`, metadataUID).Scan(&count); err != nil {
		return false, fmt.Errorf("%w: check API activity resources projection: %v", ErrStore, err)
	}
	return count > 0, nil
}

func (s *Service) markAPIActivityProjected(ctx context.Context, q *store.Queries, orgID string, sequence int64) error {
	affected, err := q.MarkAPIActivityProjected(ctx, store.MarkAPIActivityProjectedParams{
		OrgID:    orgID,
		Sequence: sequence,
	})
	if err != nil {
		return fmt.Errorf("%w: mark API activity projection: %v", ErrStore, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: mark API activity projection: missing staging row", ErrStore)
	}
	return nil
}

func (s *Service) ListAPIActivities(ctx context.Context, principal Principal, filters APIActivityListFilters) (APIActivityListPage, error) {
	ctx, span := tracer.Start(ctx, "governance.ocsf.api_activity.list")
	defer span.End()
	if err := s.Validate(); err != nil {
		return APIActivityListPage{}, err
	}
	orgID := strings.TrimSpace(principal.OrgID)
	if orgID == "" {
		return APIActivityListPage{}, fmt.Errorf("%w: org id is required", ErrInvalidArgument)
	}
	limit := filters.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	cursor := apiActivityCursor{Time: time.Unix(0, 0).UTC()}
	cursorEnabled := uint8(0)
	if filters.Cursor != "" {
		parsedCursor, err := parseAPIActivityCursor(filters.Cursor)
		if err != nil {
			return APIActivityListPage{}, err
		}
		cursor = parsedCursor
		cursorEnabled = 1
	}
	ascending := strings.EqualFold(strings.TrimSpace(filters.Order), "asc")
	query := apiActivitySelectSQL() + `
		FROM verself.api_activity_events
		WHERE org_id = $1
		  AND ($2 = '' OR api_service = $2)
		  AND ($3 = '' OR api_operation = $3)
		  AND ($4 = 0 OR activity_id = $4)
		  AND ($5 = 0 OR status_id = $5)
		  AND ($6 = '' OR status_code = $6)
		  AND ($7 = '' OR actor_uid = $7)
		  AND ($8 = '' OR actor_type = $8)
		  AND ($9 = '' OR credential_uid = $9)
		  AND ($10 = '' OR trace_uid = $10)
		  AND ($11 = '' OR primary_resource_uid = $11 OR metadata_uid IN (SELECT metadata_uid FROM verself.api_activity_resources WHERE org_id = $1 AND resource_uid = $11))
		  AND ($12 = '' OR primary_resource_type = $12 OR metadata_uid IN (SELECT metadata_uid FROM verself.api_activity_resources WHERE org_id = $1 AND resource_type = $12))
	`
	if ascending {
		query += `
		  AND ($13 = 0 OR (time, sequence) > ($14, $15))
		ORDER BY time ASC, sequence ASC
		LIMIT $16
	`
	} else {
		query += `
		  AND ($13 = 0 OR (time, sequence) < ($14, $15))
		ORDER BY time DESC, sequence DESC
		LIMIT $16
	`
	}
	rows, err := s.CH.Query(ctx, query,
		orgID,
		filters.APIService,
		filters.APIOperation,
		filters.ActivityID,
		filters.StatusID,
		filters.StatusCode,
		filters.ActorUID,
		filters.ActorType,
		filters.CredentialUID,
		filters.TraceUID,
		filters.ResourceUID,
		filters.ResourceType,
		cursorEnabled,
		cursor.Time,
		cursor.Sequence,
		limit+1,
	)
	if err != nil {
		return APIActivityListPage{}, fmt.Errorf("%w: list API activities: %v", ErrStore, err)
	}
	defer func() { _ = rows.Close() }()
	events := make([]APIActivityRow, 0, limit)
	for rows.Next() {
		var row APIActivityRow
		if err := rows.ScanStruct(&row); err != nil {
			return APIActivityListPage{}, fmt.Errorf("%w: scan API activity: %v", ErrStore, err)
		}
		events = append(events, row)
	}
	if err := rows.Err(); err != nil {
		return APIActivityListPage{}, fmt.Errorf("%w: API activity rows: %v", ErrStore, err)
	}
	nextCursor := ""
	if len(events) > limit {
		last := events[limit-1]
		nextCursor = makeAPIActivityCursor(last.Time, last.Sequence)
		events = events[:limit]
	}
	span.SetAttributes(attribute.String("verself.org_id", orgID), attribute.Int("verself.api_activity_count", len(events)))
	return APIActivityListPage{Events: events, NextCursor: nextCursor, Limit: limit}, nil
}

func apiActivitySelectSQL() string {
	return `
		SELECT
			time, event_date, metadata_uid, org_id, sequence, ocsf_version,
			category_uid, category_name, class_uid, class_name, type_uid,
			activity_id, activity_name, action_id, action, status_id, status, status_code,
			severity_id, severity, api_service, api_operation, api_version,
			actor_type, actor_uid, actor_name, credential_uid,
			primary_resource_type, primary_resource_uid, primary_resource_name, primary_resource_full_name,
			permission, http_request_uid, http_method, http_route, http_args, http_user_agent,
			src_endpoint_ip, src_endpoint_name, http_response_code, trace_uid, span_uid,
			ocsf_sha256, prev_hmac, row_hmac, hmac_key_id
	`
}

func (s *Service) computeAPIActivityHMAC(row *APIActivityRow) string {
	mac := hmac.New(sha256.New, s.HMACKey)
	_, _ = fmt.Fprintf(mac, "%s\n%s\n%s\n%d\n%s\n%s\n%s\n%s\n%d\n%d\n%s\n%s\n%s\n%s\n",
		row.OCSFVersion,
		row.OrgID,
		row.MetadataUID.String(),
		row.Sequence,
		row.PrevHMAC,
		row.Time.Format(time.RFC3339Nano),
		row.APIService,
		row.APIOperation,
		row.ActionID,
		row.StatusID,
		row.PrimaryResourceFullName,
		row.OCSFSHA256,
		row.TraceUID,
		row.SpanUID,
	)
	return hex.EncodeToString(mac.Sum(nil))
}

func apiActivitySequenceInt64(sequence uint64) (int64, error) {
	if sequence > uint64(math.MaxInt64) {
		return 0, fmt.Errorf("%w: API activity sequence out of range", ErrStore)
	}
	return int64(sequence), nil
}

func ocsfResources(resources []APIActivityResource) []ocsf.Resource {
	out := make([]ocsf.Resource, 0, len(resources))
	for _, resource := range resources {
		out = append(out, ocsf.Resource{
			Type:     resource.Type,
			UID:      resource.UID,
			Name:     resource.Name,
			FullName: resource.FullName,
			Role:     resource.Role,
			RoleID:   resource.RoleID,
		})
	}
	return out
}

func primaryOCSFResource(resources []ocsf.ActivityResource) ocsf.ActivityResource {
	for _, resource := range resources {
		if resource.RoleID == 1 {
			return resource
		}
	}
	if len(resources) == 0 {
		return ocsf.ActivityResource{}
	}
	return resources[0]
}

func truncateDate(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func hashText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return hashTextRaw(value)
}

func hashTextRaw(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

type apiActivityCursor struct {
	Time     time.Time
	Sequence uint64
}

func makeAPIActivityCursor(observedAt time.Time, sequence uint64) string {
	return observedAt.UTC().Format(time.RFC3339Nano) + "." + strconv.FormatUint(sequence, 10)
}

func parseAPIActivityCursor(value string) (apiActivityCursor, error) {
	sep := strings.LastIndex(value, ".")
	if sep <= 0 || sep == len(value)-1 {
		return apiActivityCursor{}, fmt.Errorf("%w: invalid cursor", ErrInvalidArgument)
	}
	observedAt, err := time.Parse(time.RFC3339Nano, value[:sep])
	if err != nil {
		return apiActivityCursor{}, fmt.Errorf("%w: invalid cursor", ErrInvalidArgument)
	}
	sequence, err := strconv.ParseUint(value[sep+1:], 10, 64)
	if err != nil {
		return apiActivityCursor{}, fmt.Errorf("%w: invalid cursor", ErrInvalidArgument)
	}
	if observedAt.IsZero() {
		return apiActivityCursor{}, fmt.Errorf("%w: invalid cursor", ErrInvalidArgument)
	}
	return apiActivityCursor{Time: observedAt.UTC(), Sequence: sequence}, nil
}
