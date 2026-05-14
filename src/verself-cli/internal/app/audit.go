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
	case "api-activities":
		return c.auditAPIActivities(ctx, args[1:])
	case "exports":
		return c.runDataExports(ctx, args[1:])
	default:
		return fmt.Errorf("unknown audit command %q", args[0])
	}
}

func (c CLI) auditAPIActivities(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("audit api-activities", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	limit := fs.Int("limit", 50, "page size")
	cursor := fs.String("cursor", "", "pagination cursor")
	order := fs.String("order", "", "asc or desc")
	actorUID := fs.String("actor-uid", "", "actor uid")
	apiService := fs.String("api-service", "", "api service")
	apiOperation := fs.String("api-operation", "", "api operation")
	resourceUID := fs.String("resource-uid", "", "resource uid")
	resourceType := fs.String("resource-type", "", "resource type")
	statusID := fs.Int("status-id", 0, "OCSF status_id")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: audit api-activities [--api-service SERVICE] [--status-id 1|2] [--limit N] [--json]")
	}
	if *limit < 1 || *limit > 200 {
		return errors.New("audit api-activities requires --limit between 1 and 200")
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	page, err := client.Governance.ListAPIActivities(ctx, verself.ListGovernanceAPIActivitiesOptions{
		Limit:        *limit,
		Cursor:       *cursor,
		Order:        verself.GovernanceAPIActivityOrder(*order),
		ActorUID:     *actorUID,
		APIService:   *apiService,
		APIOperation: *apiOperation,
		ResourceUID:  *resourceUID,
		ResourceType: *resourceType,
		StatusID:     uint8(*statusID),
	})
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, page)
	}
	for _, event := range page.APIActivities {
		if err := writeAPIActivity(c.out, event); err != nil {
			return err
		}
	}
	if page.NextCursor != "" {
		return writef(c.out, "next_cursor\t%s\n", page.NextCursor)
	}
	return nil
}

func (c CLI) runDataExports(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("audit exports command is required")
	}
	switch args[0] {
	case "list", "ls":
		return c.dataExportsList(ctx, args[1:])
	case "create":
		return c.dataExportsCreate(ctx, args[1:])
	case "get":
		return c.dataExportsGet(ctx, args[1:])
	case "download":
		return c.dataExportsDownload(ctx, args[1:])
	default:
		return fmt.Errorf("unknown audit exports command %q", args[0])
	}
}

func (c CLI) dataExportsList(ctx context.Context, args []string) error {
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

func (c CLI) dataExportsCreate(ctx context.Context, args []string) error {
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
		return errors.New("usage: audit exports create [--scope identity|billing|sandbox|api_activity] [--json]")
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

func (c CLI) dataExportsGet(ctx context.Context, args []string) error {
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

func (c CLI) dataExportsDownload(ctx context.Context, args []string) error {
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

func writeAPIActivity(w interface {
	Write([]byte) (int, error)
}, event verself.GovernanceAPIActivity,
) error {
	return writef(
		w,
		"api_activity\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
		event.MetadataUID,
		event.Time.Format("2006-01-02T15:04:05Z07:00"),
		event.Status,
		event.APIService,
		event.APIOperation,
		event.ActorUID,
		event.PrimaryResourceType,
		event.PrimaryResourceUID,
		event.TraceUID,
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
