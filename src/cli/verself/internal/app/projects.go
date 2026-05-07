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
	iamURL      string
	projectsURL string
	billingURL  string
	sandboxURL  string
	secretsURL  string
	sourceURL   string
	traceparent string
}

func (c CLI) runProjects(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("projects command is required")
	}
	switch args[0] {
	case "list", "ls":
		return c.projectsList(ctx, args[1:])
	case "get":
		return c.projectsGet(ctx, args[1:])
	case "create":
		return c.projectsCreate(ctx, args[1:])
	case "update", "patch":
		return c.projectsUpdate(ctx, args[1:])
	case "archive":
		return c.projectsArchive(ctx, args[1:])
	case "restore":
		return c.projectsRestore(ctx, args[1:])
	case "environments", "envs":
		return c.runProjectEnvironments(ctx, args[1:])
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
		if err := writeProject(c.out, project); err != nil {
			return err
		}
	}
	if page.NextCursor != "" {
		return writef(c.out, "next_cursor\t%s\n", page.NextCursor)
	}
	return nil
}

func (c CLI) projectsGet(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("projects get", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: projects get <project-id> [--json]")
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	project, err := client.Projects.Get(ctx, fs.Arg(0))
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, project)
	}
	return writeProject(c.out, project)
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
	return writeProject(c.out, project)
}

func (c CLI) projectsUpdate(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("projects update", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	version := fs.String("version", "", "observed project version")
	idempotencyKey := fs.String("idempotency-key", "", "stable mutation key")
	var slug, displayName, description optionalStringFlag
	fs.Var(&slug, "slug", "project slug")
	fs.Var(&displayName, "display-name", "project display name")
	fs.Var(&description, "description", "project description")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: projects update <project-id> --version VERSION [--slug SLUG] [--display-name NAME] [--description TEXT] [--json]")
	}
	if !slug.set && !displayName.set && !description.set {
		return errors.New("projects update requires at least one of --slug, --display-name, or --description")
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	project, err := client.Projects.Update(ctx, verself.UpdateProjectInput{
		ProjectID:      fs.Arg(0),
		Version:        *version,
		Slug:           slug.ptr(),
		DisplayName:    displayName.ptr(),
		Description:    description.ptr(),
		IdempotencyKey: *idempotencyKey,
	})
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, project)
	}
	return writeProject(c.out, project)
}

func (c CLI) projectsArchive(ctx context.Context, args []string) error {
	return c.projectsLifecycle(ctx, "projects archive", "archive", args)
}

func (c CLI) projectsRestore(ctx context.Context, args []string) error {
	return c.projectsLifecycle(ctx, "projects restore", "restore", args)
}

func (c CLI) projectsLifecycle(ctx context.Context, command, action string, args []string) error {
	fs, serviceFlags := serviceFlagSet(command, c.err)
	jsonOut := fs.Bool("json", false, "json output")
	version := fs.String("version", "", "observed project version")
	idempotencyKey := fs.String("idempotency-key", "", "stable mutation key")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: projects %s <project-id> --version VERSION [--json]", action)
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	input := verself.ProjectLifecycleInput{
		ProjectID:      fs.Arg(0),
		Version:        *version,
		IdempotencyKey: *idempotencyKey,
	}
	var project verself.Project
	switch action {
	case "archive":
		project, err = client.Projects.Archive(ctx, input)
	case "restore":
		project, err = client.Projects.Restore(ctx, input)
	default:
		return fmt.Errorf("unknown project lifecycle action %q", action)
	}
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, project)
	}
	return writeProject(c.out, project)
}

func (c CLI) runProjectEnvironments(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("projects environments command is required")
	}
	switch args[0] {
	case "list", "ls":
		return c.projectEnvironmentsList(ctx, args[1:])
	case "create":
		return c.projectEnvironmentsCreate(ctx, args[1:])
	case "update", "patch":
		return c.projectEnvironmentsUpdate(ctx, args[1:])
	case "archive":
		return c.projectEnvironmentsArchive(ctx, args[1:])
	default:
		return fmt.Errorf("unknown projects environments command %q", args[0])
	}
}

func (c CLI) projectEnvironmentsList(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("projects environments list", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: projects environments list <project-id> [--json]")
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	page, err := client.Projects.ListEnvironments(ctx, fs.Arg(0))
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, page)
	}
	for _, environment := range page.Environments {
		if err := writeEnvironment(c.out, environment); err != nil {
			return err
		}
	}
	if page.NextCursor != "" {
		return writef(c.out, "next_cursor\t%s\n", page.NextCursor)
	}
	return nil
}

func (c CLI) projectEnvironmentsCreate(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("projects environments create", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	slug := fs.String("slug", "", "environment slug")
	kind := fs.String("kind", "", "environment kind")
	idempotencyKey := fs.String("idempotency-key", "", "stable mutation key")
	var policy keyValueFlag
	fs.Var(&policy, "policy", "protection policy key=value")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return errors.New("usage: projects environments create <project-id> <display-name> --slug SLUG --kind KIND [--json]")
	}
	if strings.TrimSpace(*slug) == "" || strings.TrimSpace(*kind) == "" {
		return errors.New("projects environments create requires --slug and --kind")
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	environment, err := client.Projects.CreateEnvironment(ctx, verself.CreateProjectEnvironmentInput{
		ProjectID:        fs.Arg(0),
		Slug:             *slug,
		DisplayName:      fs.Arg(1),
		Kind:             verself.ProjectEnvironmentKind(*kind),
		ProtectionPolicy: policy.mapOrNil(),
		IdempotencyKey:   *idempotencyKey,
	})
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, environment)
	}
	return writeEnvironment(c.out, environment)
}

func (c CLI) projectEnvironmentsUpdate(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("projects environments update", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	version := fs.String("version", "", "observed environment version")
	idempotencyKey := fs.String("idempotency-key", "", "stable mutation key")
	var displayName optionalStringFlag
	var policy keyValueFlag
	fs.Var(&displayName, "display-name", "environment display name")
	fs.Var(&policy, "policy", "protection policy key=value")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return errors.New("usage: projects environments update <project-id> <environment-id> --version VERSION [--display-name NAME] [--json]")
	}
	if !displayName.set && !policy.set {
		return errors.New("projects environments update requires --display-name or --policy")
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	environment, err := client.Projects.UpdateEnvironment(ctx, verself.UpdateProjectEnvironmentInput{
		ProjectID:        fs.Arg(0),
		EnvironmentID:    fs.Arg(1),
		Version:          *version,
		DisplayName:      displayName.ptr(),
		ProtectionPolicy: policy.mapOrNil(),
		IdempotencyKey:   *idempotencyKey,
	})
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, environment)
	}
	return writeEnvironment(c.out, environment)
}

func (c CLI) projectEnvironmentsArchive(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("projects environments archive", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	version := fs.String("version", "", "observed environment version")
	idempotencyKey := fs.String("idempotency-key", "", "stable mutation key")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return errors.New("usage: projects environments archive <project-id> <environment-id> --version VERSION [--json]")
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	environment, err := client.Projects.ArchiveEnvironment(ctx, verself.ProjectEnvironmentLifecycleInput{
		ProjectID:      fs.Arg(0),
		EnvironmentID:  fs.Arg(1),
		Version:        *version,
		IdempotencyKey: *idempotencyKey,
	})
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, environment)
	}
	return writeEnvironment(c.out, environment)
}

func serviceFlagSet(name string, stderr io.Writer) (*flag.FlagSet, *serviceClientFlags) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	flags := &serviceClientFlags{}
	fs.StringVar(&flags.tokenFile, "token-file", "", "read bearer token from owner-only file")
	fs.StringVar(&flags.iamURL, "iam-url", "", "IAM service base URL")
	fs.StringVar(&flags.projectsURL, "projects-url", "", "projects service base URL")
	fs.StringVar(&flags.billingURL, "billing-url", "", "billing service base URL")
	fs.StringVar(&flags.sandboxURL, "sandbox-url", "", "sandbox rental service base URL")
	fs.StringVar(&flags.secretsURL, "secrets-url", "", "secrets service base URL")
	fs.StringVar(&flags.sourceURL, "source-url", "", "source service base URL")
	fs.StringVar(&flags.traceparent, "traceparent", "", "trace context to join")
	return fs, flags
}

func (c CLI) serviceClient(flags serviceClientFlags) (*verself.Client, error) {
	token, profile, err := c.bearerTokenWithProfile(flags.tokenFile)
	if err != nil {
		return nil, err
	}
	iamURL := strings.TrimSpace(flags.iamURL)
	if iamURL == "" {
		iamURL = strings.TrimSpace(c.getenv("VERSELF_IAM_API_URL"))
	}
	if iamURL == "" && profile != nil {
		iamURL = profile.IAMURL
	}
	projectsURL := strings.TrimSpace(flags.projectsURL)
	if projectsURL == "" {
		projectsURL = strings.TrimSpace(c.getenv("VERSELF_PROJECTS_API_URL"))
	}
	if projectsURL == "" && profile != nil {
		projectsURL = profile.ProjectsURL
	}
	billingURL := strings.TrimSpace(flags.billingURL)
	if billingURL == "" {
		billingURL = strings.TrimSpace(c.getenv("VERSELF_BILLING_API_URL"))
	}
	if billingURL == "" && profile != nil {
		billingURL = profile.BillingURL
	}
	sandboxURL := strings.TrimSpace(flags.sandboxURL)
	if sandboxURL == "" {
		sandboxURL = strings.TrimSpace(c.getenv("VERSELF_SANDBOX_API_URL"))
	}
	if sandboxURL == "" && profile != nil {
		sandboxURL = profile.SandboxURL
	}
	secretsURL := strings.TrimSpace(flags.secretsURL)
	if secretsURL == "" {
		secretsURL = strings.TrimSpace(c.getenv("VERSELF_SECRETS_API_URL"))
	}
	if secretsURL == "" && profile != nil {
		secretsURL = profile.SecretsURL
	}
	sourceURL := strings.TrimSpace(flags.sourceURL)
	if sourceURL == "" {
		sourceURL = strings.TrimSpace(c.getenv("VERSELF_SOURCE_API_URL"))
	}
	if sourceURL == "" && profile != nil {
		sourceURL = profile.SourceURL
	}
	return verself.New(verself.Options{
		BearerToken: token,
		IAMURL:      iamURL,
		ProjectsURL: projectsURL,
		BillingURL:  billingURL,
		SandboxURL:  sandboxURL,
		SecretsURL:  secretsURL,
		SourceURL:   sourceURL,
		Traceparent: flags.traceparent,
	})
}

func (c CLI) bearerToken(tokenFile string) (string, error) {
	token, _, err := c.bearerTokenWithProfile(tokenFile)
	return token, err
}

func (c CLI) bearerTokenWithProfile(tokenFile string) (string, *ProfileRecord, error) {
	if strings.TrimSpace(tokenFile) != "" {
		token, err := readTokenFile(tokenFile)
		return token, nil, err
	}
	if envFile := strings.TrimSpace(c.getenv("VERSELF_TOKEN_FILE")); envFile != "" {
		token, err := readTokenFile(envFile)
		return token, nil, err
	}
	token := strings.TrimSpace(c.getenv("VERSELF_TOKEN"))
	if token != "" {
		return token, nil, nil
	}
	store, err := newStore(c.getenv)
	if err != nil {
		return "", nil, err
	}
	profile, err := store.LoadProfile("")
	if err != nil {
		return "", nil, errors.New("VERSELF_TOKEN, VERSELF_TOKEN_FILE, --token-file, or auth profile is required")
	}
	tokenValue, err := store.ReadCredential(profile.TokenRef)
	if err != nil {
		return "", nil, err
	}
	tokenValue = strings.TrimSpace(tokenValue)
	if credential, ok := parseStoredAuthCredential(tokenValue); ok {
		tokenValue = strings.TrimSpace(credential.AccessToken)
	}
	if tokenValue == "" {
		return "", nil, errors.New("active auth profile token is empty")
	}
	return tokenValue, &profile, nil
}

type optionalStringFlag struct {
	value string
	set   bool
}

func (f *optionalStringFlag) Set(value string) error {
	f.value = value
	f.set = true
	return nil
}

func (f *optionalStringFlag) String() string {
	return f.value
}

func (f *optionalStringFlag) ptr() *string {
	if !f.set {
		return nil
	}
	value := f.value
	return &value
}

type keyValueFlag struct {
	values map[string]string
	set    bool
}

func (f *keyValueFlag) Set(value string) error {
	key, val, ok := strings.Cut(value, "=")
	key = strings.TrimSpace(key)
	if !ok || key == "" {
		return fmt.Errorf("policy values must use key=value")
	}
	if f.values == nil {
		f.values = map[string]string{}
	}
	f.values[key] = val
	f.set = true
	return nil
}

func (f *keyValueFlag) String() string {
	if len(f.values) == 0 {
		return ""
	}
	parts := make([]string, 0, len(f.values))
	for key, value := range f.values {
		parts = append(parts, key+"="+value)
	}
	return strings.Join(parts, ",")
}

func (f *keyValueFlag) mapOrNil() map[string]string {
	if !f.set {
		return nil
	}
	out := make(map[string]string, len(f.values))
	for key, value := range f.values {
		out[key] = value
	}
	return out
}

func writeProject(w io.Writer, project verself.Project) error {
	return writef(w, "%s\t%s\t%s\n", project.Slug, project.ProjectID, project.DisplayName)
}

func writeEnvironment(w io.Writer, environment verself.ProjectEnvironment) error {
	return writef(w, "%s\t%s\t%s\t%s\n", environment.Slug, environment.EnvironmentID, environment.Kind, environment.DisplayName)
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
