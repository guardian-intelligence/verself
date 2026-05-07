package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	verself "github.com/verself/verself-go"
)

type serviceClientFlags struct {
	tokenFile   string
	projectsURL string
	traceparent string
}

func (c CLI) runProjects(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("projects command is required")
	}
	switch args[0] {
	case "list", "ls":
		return c.projectsList(ctx, args[1:])
	case "create":
		return c.projectsCreate(ctx, args[1:])
	default:
		return fmt.Errorf("unknown projects command %q", args[0])
	}
}

func (c CLI) projectsList(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("projects list", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	state := fs.String("state", "active", "project state")
	limit := fs.Int("limit", 100, "page size")
	cursor := fs.String("cursor", "", "pagination cursor")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: projects list [--state active|archived] [--json]")
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	page, err := client.Projects.List(ctx, verself.ListProjectsOptions{
		State:  verself.ProjectState(*state),
		Limit:  *limit,
		Cursor: *cursor,
	})
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, page)
	}
	for _, project := range page.Projects {
		if err := writef(c.out, "%s\t%s\t%s\n", project.Slug, project.ProjectID, project.DisplayName); err != nil {
			return err
		}
	}
	if page.NextCursor != "" {
		return writef(c.out, "next_cursor\t%s\n", page.NextCursor)
	}
	return nil
}

func (c CLI) projectsCreate(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("projects create", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	slug := fs.String("slug", "", "project slug")
	description := fs.String("description", "", "project description")
	idempotencyKey := fs.String("idempotency-key", "", "stable mutation key")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: projects create <display-name> [--slug SLUG] [--description TEXT] [--json]")
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	project, err := client.Projects.Create(ctx, verself.CreateProjectInput{
		Slug:           *slug,
		DisplayName:    fs.Arg(0),
		Description:    *description,
		IdempotencyKey: *idempotencyKey,
	})
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, project)
	}
	return writef(c.out, "%s\t%s\t%s\n", project.Slug, project.ProjectID, project.DisplayName)
}

func serviceFlagSet(name string, stderr io.Writer) (*flag.FlagSet, *serviceClientFlags) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	flags := &serviceClientFlags{}
	fs.StringVar(&flags.tokenFile, "token-file", "", "read bearer token from owner-only file")
	fs.StringVar(&flags.projectsURL, "projects-url", "", "projects service base URL")
	fs.StringVar(&flags.traceparent, "traceparent", "", "trace context to join")
	return fs, flags
}

func (c CLI) serviceClient(flags serviceClientFlags) (*verself.Client, error) {
	token, err := c.bearerToken(flags.tokenFile)
	if err != nil {
		return nil, err
	}
	projectsURL := strings.TrimSpace(flags.projectsURL)
	if projectsURL == "" {
		projectsURL = strings.TrimSpace(c.getenv("VERSELF_PROJECTS_API_URL"))
	}
	return verself.New(verself.Options{
		BearerToken: token,
		ProjectsURL: projectsURL,
		Traceparent: flags.traceparent,
	})
}

func (c CLI) bearerToken(tokenFile string) (string, error) {
	if strings.TrimSpace(tokenFile) != "" {
		return readTokenFile(tokenFile)
	}
	if envFile := strings.TrimSpace(c.getenv("VERSELF_TOKEN_FILE")); envFile != "" {
		return readTokenFile(envFile)
	}
	token := strings.TrimSpace(c.getenv("VERSELF_TOKEN"))
	if token == "" {
		return "", errors.New("VERSELF_TOKEN or --token-file is required")
	}
	return token, nil
}

func readTokenFile(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("token file %s must be a regular file", path)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("token file %s must be owner-only", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", fmt.Errorf("token file %s is empty", path)
	}
	return token, nil
}
