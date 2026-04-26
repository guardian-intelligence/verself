package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/verself/billing-service/internal/billing/ledger"
	"github.com/verself/billing-service/internal/store"
)

const ledgerCommandMaxAttempts = 12

type ledgerAccountRegistryRow struct {
	AccountKey string
	AccountID  ledger.ID
	Ledger     uint32
	Code       uint16
	Flags      uint16
}

type ledgerCommandRow struct {
	CommandID     string
	Operation     string
	AggregateType string
	AggregateID   string
	OrgID         OrgID
	ProductID     string
	Payload       ledger.CommandPayload
	Attempts      int
}

func (c *Client) requireLedger() (*ledger.Client, error) {
	if c.ledger == nil {
		return nil, fmt.Errorf("%w: tigerbeetle client is not configured", ledger.ErrUnavailable)
	}
	return c.ledger, nil
}

func (c *Client) EnsureLedgerBootstrapped(ctx context.Context) error {
	ledgerClient, err := c.requireLedger()
	if err != nil {
		return err
	}
	defs := ledger.OperatorAccountDefinitions()
	payload := ledger.CommandPayload{Accounts: make([]ledger.AccountPayload, 0, len(defs))}
	err = c.WithTx(ctx, "billing.ledger.bootstrap_accounts.pg", func(ctx context.Context, tx pgx.Tx, q *store.Queries) error {
		for _, def := range defs {
			row, err := c.ensureLedgerAccountTx(ctx, q, def)
			if err != nil {
				return err
			}
			payload.Accounts = append(payload.Accounts, ledger.OperatorAccount(row.AccountID, def))
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err := ledgerClient.Dispatch(ctx, "bootstrap_operator_accounts", payload); err != nil {
		return err
	}
	return c.WithTx(ctx, "billing.ledger.bootstrap_accounts.event", func(ctx context.Context, tx pgx.Tx, q *store.Queries) error {
		return appendEvent(ctx, tx, q, eventFact{
			EventType:     "ledger_operator_accounts_bootstrapped",
			AggregateType: "ledger",
			AggregateID:   "operator_accounts",
			OccurredAt:    time.Now().UTC(),
			Payload:       map[string]any{"account_count": len(payload.Accounts)},
		})
	})
}

func (c *Client) ensureLedgerAccountTx(ctx context.Context, q *store.Queries, def ledger.AccountDefinition) (ledgerAccountRegistryRow, error) {
	generated := ledger.NewID()
	if err := q.InsertLedgerAccount(ctx, store.InsertLedgerAccountParams{
		AccountKey:  def.Key,
		AccountID:   generated.Bytes(),
		Ledger:      int32(ledger.DefaultLedger),
		Code:        int32(def.Code),
		Flags:       int32(def.Flags),
		AccountKind: def.Kind,
		Description: def.Description,
	}); err != nil {
		return ledgerAccountRegistryRow{}, fmt.Errorf("insert ledger account registry %s: %w", def.Key, err)
	}
	stored, err := q.GetLedgerAccount(ctx, store.GetLedgerAccountParams{AccountKey: def.Key})
	if err != nil {
		return ledgerAccountRegistryRow{}, fmt.Errorf("load ledger account registry %s: %w", def.Key, err)
	}
	accountID, err := ledger.IDFromBytes(stored.AccountID)
	if err != nil {
		return ledgerAccountRegistryRow{}, err
	}
	row := ledgerAccountRegistryRow{
		AccountKey: stored.AccountKey,
		AccountID:  accountID,
		Ledger:     uint32(stored.Ledger),
		Code:       uint16(stored.Code),
		Flags:      uint16(stored.Flags),
	}
	if row.Ledger != ledger.DefaultLedger || row.Code != def.Code || row.Flags != def.Flags {
		return ledgerAccountRegistryRow{}, fmt.Errorf("ledger account registry drift for %s: got ledger=%d code=%d flags=%d expected ledger=%d code=%d flags=%d", def.Key, row.Ledger, row.Code, row.Flags, ledger.DefaultLedger, def.Code, def.Flags)
	}
	return row, nil
}

func (c *Client) operatorLedgerAccountsTx(ctx context.Context, tx pgx.Tx) (map[string]ledger.ID, error) {
	rows, err := c.queries.WithTx(tx).ListOperatorLedgerAccounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("query operator ledger accounts: %w", err)
	}
	out := map[string]ledger.ID{}
	for _, row := range rows {
		id, err := ledger.IDFromBytes(row.AccountID)
		if err != nil {
			return nil, fmt.Errorf("parse operator ledger account %s: %w", row.AccountKey, err)
		}
		out[row.AccountKey] = id
	}
	for _, def := range ledger.OperatorAccountDefinitions() {
		if _, ok := out[def.Key]; !ok {
			return nil, fmt.Errorf("operator ledger account %s is not bootstrapped", def.Key)
		}
	}
	return out, nil
}

func (c *Client) createLedgerCommandTx(ctx context.Context, tx pgx.Tx, operation, aggregateType, aggregateID string, orgID OrgID, productID, idempotencyKey string, payload ledger.CommandPayload) (string, ledger.CommandPayload, error) {
	if operation == "" || aggregateType == "" || aggregateID == "" || idempotencyKey == "" {
		return "", ledger.CommandPayload{}, fmt.Errorf("%w: ledger command identity is incomplete", ledger.ErrInvalidCommand)
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", ledger.CommandPayload{}, fmt.Errorf("marshal ledger command payload: %w", err)
	}
	commandID := textID("ledger_cmd", idempotencyKey)
	q := c.queries.WithTx(tx)
	if err := q.InsertLedgerCommand(ctx, store.InsertLedgerCommandParams{
		CommandID:      commandID,
		Operation:      operation,
		AggregateType:  aggregateType,
		AggregateID:    aggregateID,
		OrgID:          orgIDText(orgID),
		ProductID:      productID,
		IdempotencyKey: idempotencyKey,
		Payload:        payloadBytes,
	}); err != nil {
		return "", ledger.CommandPayload{}, fmt.Errorf("insert ledger command: %w", err)
	}
	stored, err := q.GetLedgerCommandByIdempotencyKey(ctx, store.GetLedgerCommandByIdempotencyKeyParams{IdempotencyKey: idempotencyKey})
	if err != nil {
		return "", ledger.CommandPayload{}, fmt.Errorf("load ledger command %s: %w", idempotencyKey, err)
	}
	var persisted ledger.CommandPayload
	if err := json.Unmarshal(stored.Payload, &persisted); err != nil {
		return "", ledger.CommandPayload{}, fmt.Errorf("unmarshal persisted ledger command %s: %w", stored.CommandID, err)
	}
	return stored.CommandID, persisted, nil
}

func (c *Client) dispatchLedgerCommand(ctx context.Context, commandID string) error {
	ledgerClient, err := c.requireLedger()
	if err != nil {
		return err
	}
	command, ok, err := c.leaseLedgerCommand(ctx, commandID)
	if err != nil || !ok {
		return err
	}
	if err := ledgerClient.Dispatch(ctx, command.Operation, command.Payload); err != nil {
		_ = c.markLedgerCommandFailed(ctx, command, err)
		return err
	}
	return c.markLedgerCommandPosted(ctx, command)
}

func (c *Client) loadLedgerCommand(ctx context.Context, commandID string) (ledgerCommandRow, error) {
	row, err := c.queries.GetLedgerCommand(ctx, store.GetLedgerCommandParams{CommandID: commandID})
	if err != nil {
		return ledgerCommandRow{}, fmt.Errorf("load ledger command %s: %w", commandID, err)
	}
	return ledgerCommandFromStore(row.CommandID, row.Operation, row.AggregateType, row.AggregateID, row.OrgID, row.ProductID, row.Payload, row.Attempts)
}

func (c *Client) completePostedLedgerCommand(ctx context.Context, commandID string) error {
	command, err := c.loadLedgerCommand(ctx, commandID)
	if err != nil {
		return err
	}
	switch command.Operation {
	case "grant_deposit":
		return c.markGrantLedgerPostingPosted(ctx, command.AggregateID)
	case "settle_window":
		return c.markWindowSettlementPosted(ctx, command.AggregateID)
	default:
		return nil
	}
}

func (c *Client) leaseLedgerCommand(ctx context.Context, commandID string) (ledgerCommandRow, bool, error) {
	var row store.LeaseLedgerCommandRow
	err := c.WithTx(ctx, "billing.ledger.command.lease", func(ctx context.Context, tx pgx.Tx, q *store.Queries) error {
		leased, err := q.LeaseLedgerCommand(ctx, store.LeaseLedgerCommandParams{CommandID: commandID, LastAttemptID: ledger.NewID().String()})
		if err != nil {
			return err
		}
		row = leased
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ledgerCommandRow{}, false, nil
	}
	if err != nil {
		return ledgerCommandRow{}, false, err
	}
	command, err := ledgerCommandFromStore(row.CommandID, row.Operation, row.AggregateType, row.AggregateID, row.OrgID, row.ProductID, row.Payload, row.Attempts)
	if err != nil {
		return ledgerCommandRow{}, false, fmt.Errorf("unmarshal ledger command payload %s: %w", commandID, err)
	}
	return command, true, nil
}

func (c *Client) markLedgerCommandPosted(ctx context.Context, command ledgerCommandRow) error {
	return c.WithTx(ctx, "billing.ledger.command.posted", func(ctx context.Context, tx pgx.Tx, q *store.Queries) error {
		if err := q.MarkLedgerCommandPosted(ctx, store.MarkLedgerCommandPostedParams{CommandID: command.CommandID}); err != nil {
			return fmt.Errorf("mark ledger command posted: %w", err)
		}
		return appendEvent(ctx, tx, q, eventFact{
			EventType:     "ledger_command_posted",
			AggregateType: command.AggregateType,
			AggregateID:   command.AggregateID,
			OrgID:         command.OrgID,
			ProductID:     command.ProductID,
			OccurredAt:    time.Now().UTC(),
			Payload:       map[string]any{"command_id": command.CommandID, "operation": command.Operation, "attempts": command.Attempts, "transfer_count": len(command.Payload.Transfers), "account_count": len(command.Payload.Accounts)},
		})
	})
}

func (c *Client) markLedgerCommandFailed(ctx context.Context, command ledgerCommandRow, cause error) error {
	if cause == nil {
		cause = fmt.Errorf("unknown ledger command failure")
	}
	state := "retryable_failed"
	nextAttempt := time.Now().UTC().Add(time.Second)
	deadLetteredAt := any(nil)
	deadLetterReason := ""
	if command.Attempts >= ledgerCommandMaxAttempts {
		state = "dead_letter"
		deadLetteredAt = time.Now().UTC()
		deadLetterReason = cause.Error()
	}
	return c.WithTx(ctx, "billing.ledger.command.failed", func(ctx context.Context, tx pgx.Tx, q *store.Queries) error {
		if err := q.MarkLedgerCommandFailed(ctx, store.MarkLedgerCommandFailedParams{
			CommandID: command.CommandID,
			State:     state,
			Column3:   timestamptz(nextAttempt),
			LastError: cause.Error(),
			Column5:   timestamptzValue(deadLetteredAt),
			Column6:   deadLetterReason,
		}); err != nil {
			return fmt.Errorf("mark ledger command failed: %w", err)
		}
		return appendEvent(ctx, tx, q, eventFact{
			EventType:     "ledger_command_failed",
			AggregateType: command.AggregateType,
			AggregateID:   command.AggregateID,
			OrgID:         command.OrgID,
			ProductID:     command.ProductID,
			OccurredAt:    time.Now().UTC(),
			Payload:       map[string]any{"command_id": command.CommandID, "operation": command.Operation, "attempts": command.Attempts, "state": state, "error": cause.Error()},
		})
	})
}

func (c *Client) DispatchPendingLedgerCommands(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		limit = 100
	}
	commands, err := c.queries.ListPendingLedgerCommands(ctx, store.ListPendingLedgerCommandsParams{Limit: int32(limit)})
	if err != nil {
		return 0, fmt.Errorf("query pending ledger commands: %w", err)
	}
	posted := 0
	for _, command := range commands {
		if command.State == "posted" {
			if err := c.completePostedLedgerCommand(ctx, command.CommandID); err != nil {
				return posted, err
			}
			posted++
			continue
		}
		if err := c.dispatchLedgerCommand(ctx, command.CommandID); err != nil {
			return posted, err
		}
		if err := c.completePostedLedgerCommand(ctx, command.CommandID); err != nil {
			return posted, err
		}
		posted++
	}
	return posted, nil
}

func ledgerCommandFromStore(commandID, operation, aggregateType, aggregateID, orgText, productID string, rawPayload []byte, attempts int32) (ledgerCommandRow, error) {
	command := ledgerCommandRow{
		CommandID:     commandID,
		Operation:     operation,
		AggregateType: aggregateType,
		AggregateID:   aggregateID,
		ProductID:     productID,
		Attempts:      int(attempts),
	}
	if orgText != "" {
		org, err := parseOrgID(orgText)
		if err != nil {
			return ledgerCommandRow{}, err
		}
		command.OrgID = org
	}
	if err := json.Unmarshal(rawPayload, &command.Payload); err != nil {
		return ledgerCommandRow{}, err
	}
	return command, nil
}
