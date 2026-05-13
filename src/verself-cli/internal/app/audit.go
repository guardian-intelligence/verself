package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	verself "github.com/verself/verself-go"
)

func (c CLI) runAudit(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("audit command is required")
	}
	switch args[0] {
	case "events":
		return c.auditEvents(ctx, args[1:])
	case "exports":
		return c.runAuditExports(ctx, args[1:])
	default:
		return fmt.Errorf("unknown audit command %q", args[0])
	}
}

func (c CLI) auditEvents(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("audit events", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	limit := fs.Int("limit", 50, "page size")
	cursor := fs.String("cursor", "", "pagination cursor")
	order := fs.String("order", "", "asc or desc")
	actorID := fs.String("actor-id", "", "actor id")
	auditEvent := fs.String("audit-event", "", "audit event name")
	credentialID := fs.String("credential-id", "", "credential id")
	eventName := fs.String("event-name", "", "operation event name")
	eventSource := fs.String("event-source", "", "recording service or system")
	outcome := fs.String("outcome", "", "allowed, denied, or error")
	targetID := fs.String("target-id", "", "target id")
	targetType := fs.String("target-type", "", "target type")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: audit events [--event-source SOURCE] [--outcome allowed|denied|error] [--limit N] [--json]")
	}
	if *limit < 1 || *limit > 200 {
		return errors.New("audit events requires --limit between 1 and 200")
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	page, err := client.Governance.ListAuditEvents(ctx, verself.ListGovernanceAuditEventsOptions{
		Limit:        *limit,
		Cursor:       *cursor,
		Order:        verself.GovernanceAuditOrder(*order),
		ActorID:      *actorID,
		AuditEvent:   *auditEvent,
		CredentialID: *credentialID,
		EventName:    *eventName,
		EventSource:  *eventSource,
		Outcome:      verself.GovernanceAuditOutcome(*outcome),
		TargetID:     *targetID,
		TargetType:   *targetType,
	})
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, page)
	}
	for _, event := range page.Events {
		if err := writeAuditEvent(c.out, event); err != nil {
			return err
		}
	}
	if page.NextCursor != "" {
		return writef(c.out, "next_cursor\t%s\n", page.NextCursor)
	}
	return nil
}

func (c CLI) runAuditExports(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("audit exports command is required")
	}
	switch args[0] {
	case "list", "ls":
		return c.auditExportsList(ctx, args[1:])
	case "create":
		return c.auditExportsCreate(ctx, args[1:])
	case "get":
		return c.auditExportsGet(ctx, args[1:])
	case "download":
		return c.auditExportsDownload(ctx, args[1:])
	default:
		return fmt.Errorf("unknown audit exports command %q", args[0])
	}
}

func (c CLI) auditExportsList(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("audit exports list", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: audit exports list [--json]")
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	exports, err := client.Governance.ListDataExports(ctx)
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, exports)
	}
	for _, export := range exports {
		if err := writeGovernanceExport(c.out, export); err != nil {
			return err
		}
	}
	return nil
}

func (c CLI) auditExportsCreate(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("audit exports create", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	includeLogs := fs.Bool("include-logs", false, "include high-volume sandbox logs")
	idempotencyKey := fs.String("idempotency-key", "", "stable mutation key")
	var scopes repeatedStringFlag
	fs.Var(&scopes, "scope", "export scope")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: audit exports create [--scope identity|billing|sandbox|audit] [--json]")
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	scopeValues := make([]verself.GovernanceExportScope, 0, len(scopes.values))
	for _, scope := range scopes.values {
		scopeValues = append(scopeValues, verself.GovernanceExportScope(scope))
	}
	export, err := client.Governance.CreateDataExport(ctx, verself.CreateGovernanceExportInput{
		Scopes:         scopeValues,
		IncludeLogs:    *includeLogs,
		IdempotencyKey: *idempotencyKey,
	})
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, export)
	}
	return writeGovernanceExport(c.out, export)
}

func (c CLI) auditExportsGet(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("audit exports get", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: audit exports get <export-id> [--json]")
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	export, err := client.Governance.GetDataExport(ctx, fs.Arg(0))
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, export)
	}
	return writeGovernanceExport(c.out, export)
}

func (c CLI) auditExportsDownload(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("audit exports download", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() < 1 || fs.NArg() > 2 {
		return errors.New("usage: audit exports download <export-id> [file] [--json]")
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	artifact, err := client.Governance.DownloadDataExport(ctx, fs.Arg(0))
	if err != nil {
		return err
	}
	path := artifact.FileName
	if fs.NArg() == 2 {
		path = fs.Arg(1)
	}
	if strings.TrimSpace(path) == "" {
		return errors.New("audit exports download destination is empty")
	}
	if err := os.WriteFile(path, artifact.Body, 0o600); err != nil {
		return err
	}
	metadata := struct {
		ExportID    string `json:"export_id"`
		FileName    string `json:"file_name"`
		ContentType string `json:"content_type"`
		Bytes       int    `json:"bytes"`
		Path        string `json:"path"`
	}{
		ExportID:    artifact.ExportID,
		FileName:    artifact.FileName,
		ContentType: artifact.ContentType,
		Bytes:       len(artifact.Body),
		Path:        path,
	}
	if *jsonOut {
		return writeJSON(c.out, metadata)
	}
	return writef(c.out, "download\t%s\t%s\t%d\t%s\n", artifact.ExportID, artifact.FileName, len(artifact.Body), filepath.Clean(path))
}

func writeAuditEvent(w interface {
	Write([]byte) (int, error)
}, event verself.GovernanceAuditEvent,
) error {
	return writef(
		w,
		"event\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
		event.EventID,
		event.RecordedAt.Format("2006-01-02T15:04:05Z07:00"),
		event.Outcome,
		event.EventSource,
		event.EventName,
		event.ActorID,
		event.TargetType,
		event.TargetID,
		event.TraceID,
	)
}

func writeGovernanceExport(w interface {
	Write([]byte) (int, error)
}, export verself.GovernanceExportJob,
) error {
	return writef(
		w,
		"export\t%s\t%s\t%s\t%d\t%s\n",
		export.State,
		export.ExportID,
		strings.Join(export.Scopes, ","),
		export.ArtifactBytes,
		export.DownloadURL,
	)
}
