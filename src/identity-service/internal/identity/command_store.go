package identity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	identitystore "github.com/verself/identity-service/internal/store"
)

const (
	domainLedgerSchemaVersion = "2026-04-24.v1"
	domainLedgerServiceName   = "identity-service"

	memberRolesAcceptedEvent = "identity.organization.member.roles.write.accepted"
	memberRolesRejectedEvent = "identity.organization.member.roles.write.rejected"
	memberRolesNoopEvent     = "identity.organization.member.roles.write.noop"

	orgACLAggregateKind         = "organization_acl"
	memberRolesConflictPolicy   = "strict_observed_role_set"
	memberRolesChangedFieldRole = "role_keys"
)

var tracer = otel.Tracer("identity-service/internal/identity")

type commandResultRow struct {
	CommandID        uuid.UUID
	RequestHash      string
	Result           string
	Reason           string
	AggregateVersion int32
	TargetUserID     string
	RequestedRoles   []string
	ExpectedRoles    []string
	ActualRoles      []string
}

type domainLedgerProjection struct {
	RecordedAt         time.Time `ch:"recorded_at"`
	OccurredAt         time.Time `ch:"occurred_at"`
	SchemaVersion      string    `ch:"schema_version"`
	EventID            uuid.UUID `ch:"event_id"`
	EventType          string    `ch:"event_type"`
	ServiceName        string    `ch:"service_name"`
	OrgID              string    `ch:"org_id"`
	ActorID            string    `ch:"actor_id"`
	OperationID        string    `ch:"operation_id"`
	CommandID          uuid.UUID `ch:"command_id"`
	IdempotencyKeyHash string    `ch:"idempotency_key_hash"`
	AggregateKind      string    `ch:"aggregate_kind"`
	AggregateID        string    `ch:"aggregate_id"`
	AggregateVersion   uint32    `ch:"aggregate_version"`
	TargetKind         string    `ch:"target_kind"`
	TargetID           string    `ch:"target_id"`
	Result             string    `ch:"result"`
	Reason             string    `ch:"reason"`
	ConflictPolicy     string    `ch:"conflict_policy"`
	ExpectedVersion    uint32    `ch:"expected_version"`
	ActualVersion      uint32    `ch:"actual_version"`
	ExpectedHash       string    `ch:"expected_hash"`
	ActualHash         string    `ch:"actual_hash"`
	RequestedHash      string    `ch:"requested_hash"`
	ChangedFields      []string  `ch:"changed_fields"`
	PayloadJSON        string    `ch:"payload_json"`
	TraceID            string    `ch:"trace_id"`
	SpanID             string    `ch:"span_id"`
	Traceparent        string    `ch:"traceparent"`
}

func (s SQLStore) GetOrgACLState(ctx context.Context, orgID, actor string) (OrgACLState, error) {
	if s.PG == nil {
		return OrgACLState{}, ErrStoreUnavailable
	}
	row, err := s.q().GetOrgACLState(ctx, identitystore.GetOrgACLStateParams{OrgID: orgID})
	if errors.Is(err, pgx.ErrNoRows) {
		return OrgACLState{OrgID: orgID, Version: 1, UpdatedAt: time.Now().UTC(), UpdatedBy: actor}, nil
	}
	if err != nil {
		return OrgACLState{}, fmt.Errorf("get identity org acl state: %w", err)
	}
	return orgACLStateFromRow(row)
}

func (s SQLStore) UpdateMemberRolesCommand(ctx context.Context, command UpdateMemberRolesCommand, directory Directory, projectID string) (out UpdateMemberRolesResult, err error) {
	ctx, span := tracer.Start(ctx, "identity.member_roles.command")
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()
	if s.PG == nil {
		return UpdateMemberRolesResult{}, ErrStoreUnavailable
	}
	if directory == nil {
		return UpdateMemberRolesResult{}, ErrZitadelUnavailable
	}
	command.RoleKeys = normalizeRoleKeys(command.RoleKeys)
	command.ExpectedRoleKeys = normalizeRoleKeys(command.ExpectedRoleKeys)
	commandID := deterministicCommandID(command)
	idempotencyKeyHash := sha256Hex(command.IdempotencyKey)
	requestHash := commandRequestHash(command)
	span.SetAttributes(
		attribute.String("identity.command_id", commandID.String()),
		attribute.String("verself.org_id", command.OrgID),
		attribute.String("verself.subject_id", command.ActorID),
		attribute.String("identity.target_user_id", command.UserID),
		attribute.Int("identity.expected_org_acl_version", int(command.ExpectedOrgACLVersion)),
		attribute.String("identity.conflict_policy", memberRolesConflictPolicy),
	)

	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return UpdateMemberRolesResult{}, fmt.Errorf("begin identity member roles command: %w", err)
	}
	defer rollback(ctx, tx)
	q := identitystore.New(tx)

	existing, exists, err := lookupCommandResultTx(ctx, q, commandID)
	if err != nil {
		return UpdateMemberRolesResult{}, err
	}
	if exists {
		if existing.RequestHash != requestHash {
			return UpdateMemberRolesResult{}, fmt.Errorf("%w: idempotency key reused with a different member role command", ErrInvalidInput)
		}
		result, replayErr := replayMemberRolesResult(ctx, directory, command, projectID, existing)
		if replayErr != nil {
			return UpdateMemberRolesResult{}, replayErr
		}
		if err := tx.Commit(ctx); err != nil {
			return UpdateMemberRolesResult{}, fmt.Errorf("commit replay identity member roles command: %w", err)
		}
		span.SetAttributes(attribute.String("identity.result", "replayed"), attribute.Int("identity.actual_org_acl_version", int(result.OrgACLState.Version)))
		return result, nil
	}

	state, err := ensureOrgACLStateForUpdateTx(ctx, q, command.OrgID, command.ActorID)
	if err != nil {
		return UpdateMemberRolesResult{}, err
	}
	if state.Version != command.ExpectedOrgACLVersion {
		result, writeErr := writeMemberRolesCommandOutcomeTx(ctx, q, memberRolesOutcomeInput{
			Command:             command,
			CommandID:           commandID,
			IdempotencyKeyHash:  idempotencyKeyHash,
			RequestHash:         requestHash,
			EventType:           memberRolesRejectedEvent,
			Result:              "rejected",
			Reason:              "stale_org_acl_version",
			ExpectedVersion:     command.ExpectedOrgACLVersion,
			ActualVersion:       state.Version,
			ExpectedRoleKeys:    command.ExpectedRoleKeys,
			RequestedRoleKeys:   command.RoleKeys,
			ActualRoleKeys:      []string{},
			AggregateVersion:    state.Version,
			Traceparent:         traceparentFromContext(ctx),
			OccurredAt:          time.Now().UTC(),
			ChangedFields:       []string{memberRolesChangedFieldRole},
			DomainPayloadDetail: "org_acl_version",
		})
		if writeErr != nil {
			return UpdateMemberRolesResult{}, writeErr
		}
		if err := tx.Commit(ctx); err != nil {
			return UpdateMemberRolesResult{}, fmt.Errorf("commit rejected identity member roles command: %w", err)
		}
		span.SetAttributes(attribute.String("identity.result", "rejected"), attribute.String("identity.reason", "stale_org_acl_version"), attribute.Int("identity.actual_org_acl_version", int(state.Version)))
		return result, fmt.Errorf("%w: organization ACL version changed", ErrOrgACLConflict)
	}

	// Zitadel remains the membership source of truth, so this lock intentionally
	// spans the read and write until identity-service owns the member projection.
	member, currentRoles, err := currentAssignableMember(ctx, directory, command.OrgID, projectID, command.UserID)
	if err != nil {
		return UpdateMemberRolesResult{}, err
	}
	if !stringSlicesEqual(currentRoles, command.ExpectedRoleKeys) {
		result, writeErr := writeMemberRolesCommandOutcomeTx(ctx, q, memberRolesOutcomeInput{
			Command:             command,
			CommandID:           commandID,
			IdempotencyKeyHash:  idempotencyKeyHash,
			RequestHash:         requestHash,
			EventType:           memberRolesRejectedEvent,
			Result:              "rejected",
			Reason:              "stale_member_roles",
			ExpectedVersion:     command.ExpectedOrgACLVersion,
			ActualVersion:       state.Version,
			ExpectedRoleKeys:    command.ExpectedRoleKeys,
			RequestedRoleKeys:   command.RoleKeys,
			ActualRoleKeys:      currentRoles,
			AggregateVersion:    state.Version,
			Traceparent:         traceparentFromContext(ctx),
			OccurredAt:          time.Now().UTC(),
			ChangedFields:       []string{memberRolesChangedFieldRole},
			DomainPayloadDetail: "role_keys",
		})
		if writeErr != nil {
			return UpdateMemberRolesResult{}, writeErr
		}
		if err := tx.Commit(ctx); err != nil {
			return UpdateMemberRolesResult{}, fmt.Errorf("commit rejected identity member roles command: %w", err)
		}
		span.SetAttributes(attribute.String("identity.result", "rejected"), attribute.String("identity.reason", "stale_member_roles"))
		return result, fmt.Errorf("%w: member roles changed", ErrOrgACLConflict)
	}

	nextVersion := state.Version
	eventType := memberRolesNoopEvent
	if !stringSlicesEqual(currentRoles, command.RoleKeys) {
		member, err = directory.UpdateMemberRoles(ctx, command.OrgID, projectID, command.UserID, command.RoleKeys)
		if err != nil {
			return UpdateMemberRolesResult{}, fmt.Errorf("update zitadel member roles: %w", err)
		}
		nextVersion = state.Version + 1
		eventType = memberRolesAcceptedEvent
		if err := q.UpdateOrgACLState(ctx, identitystore.UpdateOrgACLStateParams{
			OrgID:     command.OrgID,
			Version:   nextVersion,
			UpdatedAt: timestamptz(time.Now().UTC()),
			UpdatedBy: command.ActorID,
		}); err != nil {
			return UpdateMemberRolesResult{}, fmt.Errorf("update identity org acl version: %w", err)
		}
	}
	member.RoleKeys = normalizeRoleKeys(member.RoleKeys)
	if len(member.RoleKeys) == 0 {
		member.RoleKeys = append([]string(nil), command.RoleKeys...)
	}
	result, err := writeMemberRolesCommandOutcomeTx(ctx, q, memberRolesOutcomeInput{
		Command:             command,
		CommandID:           commandID,
		IdempotencyKeyHash:  idempotencyKeyHash,
		RequestHash:         requestHash,
		EventType:           eventType,
		Result:              "accepted",
		Reason:              "observed_state_matched",
		ExpectedVersion:     command.ExpectedOrgACLVersion,
		ActualVersion:       nextVersion,
		ExpectedRoleKeys:    command.ExpectedRoleKeys,
		RequestedRoleKeys:   command.RoleKeys,
		ActualRoleKeys:      member.RoleKeys,
		AggregateVersion:    nextVersion,
		Traceparent:         traceparentFromContext(ctx),
		OccurredAt:          time.Now().UTC(),
		ChangedFields:       []string{memberRolesChangedFieldRole},
		DomainPayloadDetail: "role_keys",
		Member:              member,
	})
	if err != nil {
		return UpdateMemberRolesResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return UpdateMemberRolesResult{}, fmt.Errorf("commit accepted identity member roles command: %w", err)
	}
	span.SetAttributes(attribute.String("identity.result", "accepted"), attribute.Int("identity.actual_org_acl_version", int(nextVersion)))
	return result, nil
}

type memberRolesOutcomeInput struct {
	Command             UpdateMemberRolesCommand
	CommandID           uuid.UUID
	IdempotencyKeyHash  string
	RequestHash         string
	EventType           string
	Result              string
	Reason              string
	ExpectedVersion     int32
	ActualVersion       int32
	ExpectedRoleKeys    []string
	RequestedRoleKeys   []string
	ActualRoleKeys      []string
	AggregateVersion    int32
	Traceparent         string
	OccurredAt          time.Time
	ChangedFields       []string
	DomainPayloadDetail string
	Member              Member
}

func writeMemberRolesCommandOutcomeTx(ctx context.Context, q *identitystore.Queries, input memberRolesOutcomeInput) (UpdateMemberRolesResult, error) {
	if input.OccurredAt.IsZero() {
		input.OccurredAt = time.Now().UTC()
	}
	result := UpdateMemberRolesResult{
		Member: input.Member,
		OrgACLState: OrgACLState{
			OrgID:     input.Command.OrgID,
			Version:   input.AggregateVersion,
			UpdatedAt: input.OccurredAt,
			UpdatedBy: input.Command.ActorID,
		},
	}
	payload := map[string]any{
		"target_user_id":        input.Command.UserID,
		"expected_version":      input.ExpectedVersion,
		"actual_version":        input.ActualVersion,
		"expected_role_hash":    roleSetHash(input.ExpectedRoleKeys),
		"requested_role_hash":   roleSetHash(input.RequestedRoleKeys),
		"actual_role_hash":      roleSetHash(input.ActualRoleKeys),
		"domain_payload_detail": input.DomainPayloadDetail,
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return UpdateMemberRolesResult{}, fmt.Errorf("marshal identity command payload: %w", err)
	}
	if err := q.InsertCommandResult(ctx, identitystore.InsertCommandResultParams{
		CommandID:          input.CommandID,
		OrgID:              input.Command.OrgID,
		ActorID:            input.Command.ActorID,
		OperationID:        input.Command.OperationID,
		IdempotencyKeyHash: input.IdempotencyKeyHash,
		RequestHash:        input.RequestHash,
		Result:             input.Result,
		Reason:             input.Reason,
		AggregateKind:      orgACLAggregateKind,
		AggregateID:        input.Command.OrgID,
		AggregateVersion:   input.AggregateVersion,
		TargetUserID:       input.Command.UserID,
		RequestedRoleKeys:  nonNilStringSlice(input.RequestedRoleKeys),
		ExpectedRoleKeys:   nonNilStringSlice(input.ExpectedRoleKeys),
		ActualRoleKeys:     nonNilStringSlice(input.ActualRoleKeys),
		CreatedAt:          timestamptz(input.OccurredAt),
	}); err != nil {
		return UpdateMemberRolesResult{}, fmt.Errorf("insert identity command result: %w", err)
	}
	eventID := deterministicEventID(input.CommandID, input.EventType)
	if err := q.InsertDomainEventOutbox(ctx, identitystore.InsertDomainEventOutboxParams{
		EventID:            eventID,
		CommandID:          input.CommandID,
		EventType:          input.EventType,
		OrgID:              input.Command.OrgID,
		ActorID:            input.Command.ActorID,
		OperationID:        input.Command.OperationID,
		IdempotencyKeyHash: input.IdempotencyKeyHash,
		AggregateKind:      orgACLAggregateKind,
		AggregateID:        input.Command.OrgID,
		AggregateVersion:   input.AggregateVersion,
		TargetKind:         "organization_member",
		TargetID:           input.Command.UserID,
		Result:             input.Result,
		Reason:             input.Reason,
		ConflictPolicy:     memberRolesConflictPolicy,
		ExpectedVersion:    input.ExpectedVersion,
		ActualVersion:      input.ActualVersion,
		ExpectedHash:       roleSetHash(input.ExpectedRoleKeys),
		ActualHash:         roleSetHash(input.ActualRoleKeys),
		RequestedHash:      roleSetHash(input.RequestedRoleKeys),
		ChangedFields:      nonNilStringSlice(input.ChangedFields),
		Payload:            payloadJSON,
		Traceparent:        input.Traceparent,
		OccurredAt:         timestamptz(input.OccurredAt),
	}); err != nil {
		return UpdateMemberRolesResult{}, fmt.Errorf("insert identity domain event outbox: %w", err)
	}
	return result, nil
}

func ensureOrgACLStateForUpdateTx(ctx context.Context, q *identitystore.Queries, orgID, actor string) (OrgACLState, error) {
	now := time.Now().UTC()
	if err := q.EnsureOrgACLState(ctx, identitystore.EnsureOrgACLStateParams{
		OrgID:     orgID,
		UpdatedAt: timestamptz(now),
		UpdatedBy: actor,
	}); err != nil {
		return OrgACLState{}, fmt.Errorf("ensure identity org acl state: %w", err)
	}
	row, err := q.GetOrgACLStateForUpdate(ctx, identitystore.GetOrgACLStateForUpdateParams{OrgID: orgID})
	if err != nil {
		return OrgACLState{}, fmt.Errorf("lock identity org acl state: %w", err)
	}
	return orgACLStateFromRow(row)
}

func orgACLStateFromRow(row identitystore.IdentityOrgAclState) (OrgACLState, error) {
	updatedAt, err := requiredTime(row.UpdatedAt, "identity_org_acl_state.updated_at")
	if err != nil {
		return OrgACLState{}, err
	}
	return OrgACLState{
		OrgID:     row.OrgID,
		Version:   row.Version,
		UpdatedAt: updatedAt,
		UpdatedBy: row.UpdatedBy,
	}, nil
}

func lookupCommandResultTx(ctx context.Context, q *identitystore.Queries, commandID uuid.UUID) (commandResultRow, bool, error) {
	row, err := q.LookupCommandResultForUpdate(ctx, identitystore.LookupCommandResultForUpdateParams{CommandID: commandID})
	if errors.Is(err, pgx.ErrNoRows) {
		return commandResultRow{}, false, nil
	}
	if err != nil {
		return commandResultRow{}, false, fmt.Errorf("lookup identity command result: %w", err)
	}
	return commandResultRow{
		CommandID:        row.CommandID,
		RequestHash:      row.RequestHash,
		Result:           row.Result,
		Reason:           row.Reason,
		AggregateVersion: row.AggregateVersion,
		TargetUserID:     row.TargetUserID,
		RequestedRoles:   append([]string(nil), row.RequestedRoleKeys...),
		ExpectedRoles:    append([]string(nil), row.ExpectedRoleKeys...),
		ActualRoles:      append([]string(nil), row.ActualRoleKeys...),
	}, true, nil
}

func replayMemberRolesResult(ctx context.Context, directory Directory, command UpdateMemberRolesCommand, projectID string, existing commandResultRow) (UpdateMemberRolesResult, error) {
	if existing.Result != "accepted" {
		return UpdateMemberRolesResult{}, fmt.Errorf("%w: previously rejected member role command", ErrOrgACLConflict)
	}
	member, _, err := currentAssignableMember(ctx, directory, command.OrgID, projectID, command.UserID)
	if err != nil {
		member = Member{UserID: command.UserID, RoleKeys: append([]string(nil), existing.ActualRoles...)}
	}
	return UpdateMemberRolesResult{
		Member: member,
		OrgACLState: OrgACLState{
			OrgID:   command.OrgID,
			Version: existing.AggregateVersion,
		},
	}, nil
}

func nonNilStringSlice(values []string) []string {
	if values == nil {
		return []string{}
	}
	return append([]string(nil), values...)
}

func currentAssignableMember(ctx context.Context, directory Directory, orgID, projectID, userID string) (Member, []string, error) {
	members, err := directory.ListMembers(ctx, orgID, projectID)
	if err != nil {
		return Member{}, nil, fmt.Errorf("list identity members for observed role check: %w", err)
	}
	for _, member := range members {
		if member.UserID != userID {
			continue
		}
		if member.Type == MemberTypeMachine || containsRole(member.RoleKeys, RoleOwner) {
			return Member{}, nil, fmt.Errorf("%w: member cannot be assigned through the standard role path", ErrInvalidInput)
		}
		currentRoles := normalizeRoleKeys(member.RoleKeys)
		member.RoleKeys = currentRoles
		return member, currentRoles, nil
	}
	return Member{}, nil, ErrMemberMissing
}

func deterministicCommandID(command UpdateMemberRolesCommand) uuid.UUID {
	seed := strings.Join([]string{
		command.OrgID,
		command.ActorID,
		command.OperationID,
		sha256Hex(command.IdempotencyKey),
	}, "|")
	return uuid.NewHash(sha256.New(), uuid.Nil, []byte(seed), 5)
}

func deterministicEventID(commandID uuid.UUID, eventType string) uuid.UUID {
	return uuid.NewHash(sha256.New(), uuid.Nil, []byte(commandID.String()+"|"+eventType), 5)
}

func commandRequestHash(command UpdateMemberRolesCommand) string {
	payload := struct {
		OrgID                 string   `json:"org_id"`
		ActorID               string   `json:"actor_id"`
		UserID                string   `json:"user_id"`
		RoleKeys              []string `json:"role_keys"`
		ExpectedRoleKeys      []string `json:"expected_role_keys"`
		ExpectedOrgACLVersion int32    `json:"expected_org_acl_version"`
		OperationID           string   `json:"operation_id"`
	}{
		OrgID:                 command.OrgID,
		ActorID:               command.ActorID,
		UserID:                command.UserID,
		RoleKeys:              normalizeRoleKeys(command.RoleKeys),
		ExpectedRoleKeys:      normalizeRoleKeys(command.ExpectedRoleKeys),
		ExpectedOrgACLVersion: command.ExpectedOrgACLVersion,
		OperationID:           command.OperationID,
	}
	raw, _ := json.Marshal(payload)
	return sha256Hex(string(raw))
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func roleSetHash(roleKeys []string) string {
	roleKeys = normalizeRoleKeys(roleKeys)
	return sha256Hex(strings.Join(roleKeys, "\x00"))
}

func traceparentFromContext(ctx context.Context) string {
	carrier := propagation.MapCarrier{}
	propagation.TraceContext{}.Inject(ctx, carrier)
	return carrier.Get("traceparent")
}

func stringSlicesEqual(left, right []string) bool {
	left = normalizeRoleKeys(left)
	right = normalizeRoleKeys(right)
	if len(left) != len(right) {
		return false
	}
	for idx := range left {
		if left[idx] != right[idx] {
			return false
		}
	}
	return true
}

func (s SQLStore) ProjectPendingDomainLedger(ctx context.Context, limit int) (projected int, err error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	if s.PG == nil {
		return 0, ErrStoreUnavailable
	}
	if s.CH == nil {
		return 0, fmt.Errorf("%w: clickhouse unavailable", ErrStoreUnavailable)
	}
	ctx, span := tracer.Start(ctx, "identity.domain_ledger.project_pending")
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.SetAttributes(attribute.Int("identity.projected_count", projected))
		span.End()
	}()
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, fmt.Errorf("begin identity domain ledger projection: %w", err)
	}
	defer rollback(ctx, tx)
	q := identitystore.New(tx)
	rows, err := q.ClaimPendingDomainLedgerEvents(ctx, identitystore.ClaimPendingDomainLedgerEventsParams{LimitCount: int32(limit)})
	if err != nil {
		return 0, fmt.Errorf("select identity domain ledger outbox: %w", err)
	}
	claimed := make([]domainLedgerProjection, 0, len(rows))
	for _, outbox := range rows {
		occurredAt, err := requiredTime(outbox.OccurredAt, "identity_domain_event_outbox.occurred_at")
		if err != nil {
			return 0, err
		}
		row := domainLedgerProjection{
			RecordedAt:         time.Now().UTC(),
			OccurredAt:         occurredAt,
			SchemaVersion:      domainLedgerSchemaVersion,
			EventID:            outbox.EventID,
			EventType:          outbox.EventType,
			ServiceName:        domainLedgerServiceName,
			OrgID:              outbox.OrgID,
			ActorID:            outbox.ActorID,
			OperationID:        outbox.OperationID,
			CommandID:          outbox.CommandID,
			IdempotencyKeyHash: outbox.IdempotencyKeyHash,
			AggregateKind:      outbox.AggregateKind,
			AggregateID:        outbox.AggregateID,
			AggregateVersion:   uint32(maxInt32(outbox.AggregateVersion, 0)),
			TargetKind:         outbox.TargetKind,
			TargetID:           outbox.TargetID,
			Result:             outbox.Result,
			Reason:             outbox.Reason,
			ConflictPolicy:     outbox.ConflictPolicy,
			ExpectedVersion:    uint32(maxInt32(outbox.ExpectedVersion, 0)),
			ActualVersion:      uint32(maxInt32(outbox.ActualVersion, 0)),
			ExpectedHash:       outbox.ExpectedHash,
			ActualHash:         outbox.ActualHash,
			RequestedHash:      outbox.RequestedHash,
			ChangedFields:      append([]string(nil), outbox.ChangedFields...),
			PayloadJSON:        outbox.PayloadJson,
			Traceparent:        outbox.Traceparent,
		}
		sort.Strings(row.ChangedFields)
		claimed = append(claimed, row)
	}
	if len(claimed) == 0 {
		return 0, tx.Commit(ctx)
	}
	for _, row := range claimed {
		rowCtx := ctx
		if row.Traceparent != "" {
			rowCtx = propagation.TraceContext{}.Extract(ctx, propagation.MapCarrier{"traceparent": row.Traceparent})
		}
		rowCtx, rowSpan := tracer.Start(rowCtx, "identity.domain_ledger.project_event")
		if sc := trace.SpanContextFromContext(rowCtx); sc.HasTraceID() {
			row.TraceID = sc.TraceID().String()
			row.SpanID = sc.SpanID().String()
		}
		if done, checkErr := s.domainLedgerEventProjected(rowCtx, row.EventID); checkErr != nil {
			rowSpan.RecordError(checkErr)
			rowSpan.SetStatus(codes.Error, checkErr.Error())
			rowSpan.End()
			return projected, checkErr
		} else if !done {
			if insertErr := s.insertDomainLedgerClickHouse(rowCtx, row); insertErr != nil {
				rowSpan.RecordError(insertErr)
				rowSpan.SetStatus(codes.Error, insertErr.Error())
				rowSpan.End()
				_ = markDomainLedgerProjectionFailed(ctx, q, row.EventID, insertErr)
				_ = tx.Commit(ctx)
				return projected, insertErr
			}
		}
		rowSpan.SetAttributes(
			attribute.String("identity.event_id", row.EventID.String()),
			attribute.String("identity.event_type", row.EventType),
			attribute.String("identity.result", row.Result),
		)
		rowSpan.End()
		if err := q.MarkDomainLedgerProjected(ctx, identitystore.MarkDomainLedgerProjectedParams{EventID: row.EventID}); err != nil {
			return projected, fmt.Errorf("mark identity domain ledger projected: %w", err)
		}
		projected++
	}
	return projected, tx.Commit(ctx)
}

func markDomainLedgerProjectionFailed(ctx context.Context, q *identitystore.Queries, eventID uuid.UUID, cause error) error {
	message := cause.Error()
	if len(message) > 1000 {
		message = message[:1000]
	}
	return q.MarkDomainLedgerProjectionFailed(ctx, identitystore.MarkDomainLedgerProjectionFailedParams{
		EventID:   eventID,
		LastError: message,
	})
}

func (s SQLStore) domainLedgerEventProjected(ctx context.Context, eventID uuid.UUID) (bool, error) {
	var count uint64
	err := s.CH.QueryRow(ctx, `
SELECT count()
FROM verself.domain_update_ledger
WHERE event_id = $1`, eventID).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("%w: check identity domain ledger projection: %v", ErrStoreUnavailable, err)
	}
	return count > 0, nil
}

func (s SQLStore) insertDomainLedgerClickHouse(ctx context.Context, row domainLedgerProjection) error {
	batch, err := s.CH.PrepareBatch(ctx, `
INSERT INTO verself.domain_update_ledger (
    recorded_at, occurred_at, schema_version, event_id, event_type, service_name,
    org_id, actor_id, operation_id, command_id, idempotency_key_hash, aggregate_kind,
    aggregate_id, aggregate_version, target_kind, target_id, result, reason,
    conflict_policy, expected_version, actual_version, expected_hash, actual_hash,
    requested_hash, changed_fields, payload_json, trace_id, span_id, traceparent
)`)
	if err != nil {
		return fmt.Errorf("%w: prepare identity domain ledger insert: %v", ErrStoreUnavailable, err)
	}
	if err := batch.AppendStruct(&row); err != nil {
		return fmt.Errorf("%w: append identity domain ledger event: %v", ErrStoreUnavailable, err)
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("%w: send identity domain ledger event: %v", ErrStoreUnavailable, err)
	}
	return nil
}

func maxInt32(value, floor int32) int32 {
	if value < floor {
		return floor
	}
	return value
}
