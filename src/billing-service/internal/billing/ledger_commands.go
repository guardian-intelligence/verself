package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/forge-metal/billing-service/internal/billing/ledger"
	"github.com/forge-metal/billing-service/internal/store"
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
	err = c.WithTx(ctx, "billing.ledger.bootstrap_accounts.pg", func(ctx context.Context, tx pgx.Tx, _ *store.Queries) error {
		for _, def := range defs {
			row, err := c.ensureLedgerAccountTx(ctx, tx, def)
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

func (c *Client) ensureLedgerAccountTx(ctx context.Context, tx pgx.Tx, def ledger.AccountDefinition) (ledgerAccountRegistryRow, error) {
	generated := ledger.NewID()
	_, err := tx.Exec(ctx, `
		INSERT INTO billing_ledger_accounts (account_key, account_id, ledger, code, flags, account_kind, description)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (account_key) DO NOTHING
	`, def.Key, generated.Bytes(), int64(ledger.DefaultLedger), int64(def.Code), int64(def.Flags), def.Kind, def.Description)
	if err != nil {
		return ledgerAccountRegistryRow{}, fmt.Errorf("insert ledger account registry %s: %w", def.Key, err)
	}
	var rawID []byte
	var row ledgerAccountRegistryRow
	var ledgerValue, code, flags int64
	err = tx.QueryRow(ctx, `
		SELECT account_key, account_id, ledger, code, flags
		FROM billing_ledger_accounts
		WHERE account_key = $1
	`, def.Key).Scan(&row.AccountKey, &rawID, &ledgerValue, &code, &flags)
	if err != nil {
		return ledgerAccountRegistryRow{}, fmt.Errorf("load ledger account registry %s: %w", def.Key, err)
	}
	accountID, err := ledger.IDFromBytes(rawID)
	if err != nil {
		return ledgerAccountRegistryRow{}, err
	}
	row.AccountID = accountID
	row.Ledger = uint32(ledgerValue)
	row.Code = uint16(code)
	row.Flags = uint16(flags)
	if row.Ledger != ledger.DefaultLedger || row.Code != def.Code || row.Flags != def.Flags {
		return ledgerAccountRegistryRow{}, fmt.Errorf("ledger account registry drift for %s: got ledger=%d code=%d flags=%d expected ledger=%d code=%d flags=%d", def.Key, row.Ledger, row.Code, row.Flags, ledger.DefaultLedger, def.Code, def.Flags)
	}
	return row, nil
}

func (c *Client) operatorLedgerAccountsTx(ctx context.Context, tx pgx.Tx) (map[string]ledger.ID, error) {
	rows, err := tx.Query(ctx, `
		SELECT account_key, account_id
		FROM billing_ledger_accounts
		WHERE account_kind = 'operator'
	`)
	if err != nil {
		return nil, fmt.Errorf("query operator ledger accounts: %w", err)
	}
	defer rows.Close()
	out := map[string]ledger.ID{}
	for rows.Next() {
		var key string
		var raw []byte
		if err := rows.Scan(&key, &raw); err != nil {
			return nil, fmt.Errorf("scan operator ledger account: %w", err)
		}
		id, err := ledger.IDFromBytes(raw)
		if err != nil {
			return nil, fmt.Errorf("parse operator ledger account %s: %w", key, err)
		}
		out[key] = id
	}
	if err := rows.Err(); err != nil {
		return nil, err
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
	_, err = tx.Exec(ctx, `
		INSERT INTO billing_ledger_commands (
			command_id, operation, aggregate_type, aggregate_id, org_id, product_id, idempotency_key, payload, state
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'pending')
		ON CONFLICT (idempotency_key) DO NOTHING
	`, commandID, operation, aggregateType, aggregateID, orgIDText(orgID), productID, idempotencyKey, payloadBytes)
	if err != nil {
		return "", ledger.CommandPayload{}, fmt.Errorf("insert ledger command: %w", err)
	}
	var rawPayload []byte
	var existingID string
	err = tx.QueryRow(ctx, `
		SELECT command_id, payload
		FROM billing_ledger_commands
		WHERE idempotency_key = $1
	`, idempotencyKey).Scan(&existingID, &rawPayload)
	if err != nil {
		return "", ledger.CommandPayload{}, fmt.Errorf("load ledger command %s: %w", idempotencyKey, err)
	}
	var persisted ledger.CommandPayload
	if err := json.Unmarshal(rawPayload, &persisted); err != nil {
		return "", ledger.CommandPayload{}, fmt.Errorf("unmarshal persisted ledger command %s: %w", existingID, err)
	}
	return existingID, persisted, nil
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
	var command ledgerCommandRow
	var rawPayload []byte
	var orgText string
	err := c.pg.QueryRow(ctx, `
		SELECT command_id, operation, aggregate_type, aggregate_id, org_id, product_id, payload, attempts
		FROM billing_ledger_commands
		WHERE command_id = $1
	`, commandID).Scan(&command.CommandID, &command.Operation, &command.AggregateType, &command.AggregateID, &orgText, &command.ProductID, &rawPayload, &command.Attempts)
	if err != nil {
		return ledgerCommandRow{}, fmt.Errorf("load ledger command %s: %w", commandID, err)
	}
	if orgText != "" {
		org, err := parseOrgID(orgText)
		if err != nil {
			return ledgerCommandRow{}, err
		}
		command.OrgID = org
	}
	if err := json.Unmarshal(rawPayload, &command.Payload); err != nil {
		return ledgerCommandRow{}, fmt.Errorf("unmarshal ledger command payload %s: %w", commandID, err)
	}
	return command, nil
}

func (c *Client) completePostedLedgerCommand(ctx context.Context, commandID string) error {
	command, err := c.loadLedgerCommand(ctx, commandID)
	if err != nil {
		return err
	}
	switch command.Operation {
	case "grant_deposit":
		return c.markGrantLedgerPostingPosted(ctx, command.AggregateID)
	case "reserve_window":
		window, err := c.loadWindow(ctx, command.AggregateID)
		if err != nil {
			return err
		}
		return c.markWindowReservationPosted(ctx, window)
	case "settle_window":
		return c.markWindowSettlementPosted(ctx, command.AggregateID)
	case "void_window":
		return c.markWindowVoidPosted(ctx, command.AggregateID)
	default:
		return nil
	}
}

func (c *Client) leaseLedgerCommand(ctx context.Context, commandID string) (ledgerCommandRow, bool, error) {
	var command ledgerCommandRow
	var rawPayload []byte
	var orgText string
	err := c.WithTx(ctx, "billing.ledger.command.lease", func(ctx context.Context, tx pgx.Tx, _ *store.Queries) error {
		row := tx.QueryRow(ctx, `
			UPDATE billing_ledger_commands
			SET state = 'in_progress',
			    attempts = attempts + 1,
			    last_attempt_at = now(),
			    lease_expires_at = now() + interval '30 seconds',
			    leased_by = 'billing-service',
			    last_attempt_id = $2
			WHERE command_id = $1
			  AND state IN ('pending','retryable_failed','in_progress')
			  AND (state <> 'in_progress' OR lease_expires_at IS NULL OR lease_expires_at < now())
			RETURNING command_id, operation, aggregate_type, aggregate_id, org_id, product_id, payload, attempts
		`, commandID, ledger.NewID().String())
		if err := row.Scan(&command.CommandID, &command.Operation, &command.AggregateType, &command.AggregateID, &orgText, &command.ProductID, &rawPayload, &command.Attempts); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return pgx.ErrNoRows
			}
			return fmt.Errorf("lease ledger command %s: %w", commandID, err)
		}
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ledgerCommandRow{}, false, nil
	}
	if err != nil {
		return ledgerCommandRow{}, false, err
	}
	if orgText != "" {
		org, err := parseOrgID(orgText)
		if err != nil {
			return ledgerCommandRow{}, false, err
		}
		command.OrgID = org
	}
	if err := json.Unmarshal(rawPayload, &command.Payload); err != nil {
		return ledgerCommandRow{}, false, fmt.Errorf("unmarshal ledger command payload %s: %w", commandID, err)
	}
	return command, true, nil
}

func (c *Client) markLedgerCommandPosted(ctx context.Context, command ledgerCommandRow) error {
	return c.WithTx(ctx, "billing.ledger.command.posted", func(ctx context.Context, tx pgx.Tx, q *store.Queries) error {
		_, err := tx.Exec(ctx, `
			UPDATE billing_ledger_commands
			SET state = 'posted', posted_at = now(), last_error = '', lease_expires_at = NULL, leased_by = ''
			WHERE command_id = $1
		`, command.CommandID)
		if err != nil {
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
		_, err := tx.Exec(ctx, `
			UPDATE billing_ledger_commands
			SET state = $2, next_attempt_at = $3, last_error = $4, lease_expires_at = NULL, leased_by = '',
			    dead_lettered_at = COALESCE($5, dead_lettered_at), dead_letter_reason = COALESCE(NULLIF($6,''), dead_letter_reason)
			WHERE command_id = $1
		`, command.CommandID, state, nextAttempt, cause.Error(), deadLetteredAt, deadLetterReason)
		if err != nil {
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
	rows, err := c.pg.Query(ctx, `
		SELECT command_id, state
		FROM billing_ledger_commands
		WHERE (
		    state IN ('pending','retryable_failed')
		    AND next_attempt_at <= now()
		  )
		  OR (
		    state = 'in_progress'
		    AND (lease_expires_at IS NULL OR lease_expires_at < now())
		  )
		  OR (
		    state = 'posted'
		    AND (
		      (operation = 'grant_deposit' AND EXISTS (
		        SELECT 1 FROM credit_grants g
		        WHERE g.grant_id = aggregate_id AND g.ledger_posting_state <> 'posted'
		      ))
		      OR (operation = 'reserve_window' AND EXISTS (
		        SELECT 1 FROM billing_windows w
		        WHERE w.window_id = aggregate_id AND w.state = 'reserving'
		      ))
		      OR (operation = 'settle_window' AND EXISTS (
		        SELECT 1 FROM billing_windows w
		        WHERE w.window_id = aggregate_id AND w.state = 'settling'
		      ))
		      OR (operation = 'void_window' AND EXISTS (
		        SELECT 1 FROM billing_windows w
		        WHERE w.window_id = aggregate_id AND w.state = 'voiding'
		      ))
		    )
		  )
		ORDER BY next_attempt_at, created_at, command_id
		LIMIT $1
	`, limit)
	if err != nil {
		return 0, fmt.Errorf("query pending ledger commands: %w", err)
	}
	defer rows.Close()
	type pendingCommand struct {
		commandID string
		state     string
	}
	commands := []pendingCommand{}
	for rows.Next() {
		var command pendingCommand
		if err := rows.Scan(&command.commandID, &command.state); err != nil {
			return 0, fmt.Errorf("scan pending ledger command: %w", err)
		}
		commands = append(commands, command)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	posted := 0
	for _, command := range commands {
		if command.state == "posted" {
			if err := c.completePostedLedgerCommand(ctx, command.commandID); err != nil {
				return posted, err
			}
			posted++
			continue
		}
		if err := c.dispatchLedgerCommand(ctx, command.commandID); err != nil {
			return posted, err
		}
		if err := c.completePostedLedgerCommand(ctx, command.commandID); err != nil {
			return posted, err
		}
		posted++
	}
	return posted, nil
}
