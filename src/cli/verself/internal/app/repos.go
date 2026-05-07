package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	verself "github.com/verself/verself-go"
)

func (c CLI) runRepos(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("repos command is required")
	}
	switch args[0] {
	case "list", "ls":
		return c.reposList(ctx, args[1:])
	case "get":
		return c.reposGet(ctx, args[1:])
	case "create":
		return c.reposCreate(ctx, args[1:])
	case "credentials", "creds":
		return c.runRepoCredentials(ctx, args[1:])
	case "refs":
		return c.reposRefs(ctx, args[1:])
	case "tree":
		return c.reposTree(ctx, args[1:])
	case "blob":
		return c.reposBlob(ctx, args[1:])
	case "checkout-grants", "checkouts":
		return c.runRepoCheckoutGrants(ctx, args[1:])
	case "workflow-runs", "workflows":
		return c.runRepoWorkflowRuns(ctx, args[1:])
	default:
		return fmt.Errorf("unknown repos command %q", args[0])
	}
}

func (c CLI) reposList(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("repos list", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	projectID := fs.String("project-id", "", "project id")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: repos list [--project-id PROJECT_ID] [--json]")
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	repositories, err := client.Source.ListRepositories(ctx, verself.ListSourceRepositoriesOptions{
		ProjectID: *projectID,
	})
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, repositories)
	}
	for _, repo := range repositories.Repositories {
		if err := writeSourceRepository(c.out, repo); err != nil {
			return err
		}
	}
	return nil
}

func (c CLI) reposCreate(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("repos create", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	description := fs.String("description", "", "repository description")
	defaultBranch := fs.String("default-branch", "", "default branch")
	idempotencyKey := fs.String("idempotency-key", "", "stable mutation key")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: repos create <project-id> [--description TEXT] [--default-branch BRANCH] [--json]")
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	repo, err := client.Source.CreateRepository(ctx, verself.CreateSourceRepositoryInput{
		ProjectID:      fs.Arg(0),
		Description:    *description,
		DefaultBranch:  *defaultBranch,
		IdempotencyKey: *idempotencyKey,
	})
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, repo)
	}
	return writeSourceRepository(c.out, repo)
}

func (c CLI) reposGet(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("repos get", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: repos get <repo-id> [--json]")
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	repo, err := client.Source.GetRepository(ctx, fs.Arg(0))
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, repo)
	}
	return writeSourceRepository(c.out, repo)
}

func (c CLI) runRepoCredentials(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("repos credentials command is required")
	}
	switch args[0] {
	case "create":
		return c.repoCredentialsCreate(ctx, args[1:])
	default:
		return fmt.Errorf("unknown repos credentials command %q", args[0])
	}
}

func (c CLI) repoCredentialsCreate(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("repos credentials create", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	label := fs.String("label", "", "credential label")
	expiresInSeconds := fs.Int64("expires-in-seconds", 0, "credential lifetime in seconds")
	idempotencyKey := fs.String("idempotency-key", "", "stable mutation key")
	var scopes repeatedStringFlag
	fs.Var(&scopes, "scope", "credential scope")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: repos credentials create --scope SCOPE [--label LABEL] [--json]")
	}
	if len(scopes.values) == 0 {
		return errors.New("repos credentials create requires at least one --scope")
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	credential, err := client.Source.CreateGitCredential(ctx, verself.CreateSourceGitCredentialInput{
		Label:            *label,
		ExpiresInSeconds: *expiresInSeconds,
		Scopes:           scopes.values,
		IdempotencyKey:   *idempotencyKey,
	})
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, credential)
	}
	return writeSourceGitCredential(c.out, credential)
}

func (c CLI) reposRefs(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("repos refs", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: repos refs <repo-id> [--json]")
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	refs, err := client.Source.ListRefs(ctx, fs.Arg(0))
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, refs)
	}
	for _, ref := range refs.Refs {
		if err := writef(c.out, "%s\t%s\n", ref.Name, ref.Commit); err != nil {
			return err
		}
	}
	return nil
}

func (c CLI) reposTree(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("repos tree", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	ref := fs.String("ref", "", "git ref")
	path := fs.String("path", "", "tree path")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: repos tree <repo-id> [--ref REF] [--path PATH] [--json]")
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	tree, err := client.Source.GetTree(ctx, verself.GetSourceTreeOptions{
		RepoID: fs.Arg(0),
		Ref:    *ref,
		Path:   *path,
	})
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, tree)
	}
	for _, entry := range tree.Entries {
		if err := writef(c.out, "%s\t%s\t%s\t%d\n", entry.Type, entry.Sha, entry.Path, entry.Size); err != nil {
			return err
		}
	}
	return nil
}

func (c CLI) reposBlob(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("repos blob", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	ref := fs.String("ref", "", "git ref")
	path := fs.String("path", "", "blob path")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: repos blob <repo-id> --path PATH [--ref REF] [--json]")
	}
	if strings.TrimSpace(*path) == "" {
		return errors.New("repos blob requires --path")
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	blob, err := client.Source.GetBlob(ctx, verself.GetSourceBlobOptions{
		RepoID: fs.Arg(0),
		Ref:    *ref,
		Path:   *path,
	})
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, blob)
	}
	return writeSourceBlob(c.out, blob)
}

func (c CLI) runRepoCheckoutGrants(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("repos checkout-grants command is required")
	}
	switch args[0] {
	case "create":
		return c.repoCheckoutGrantsCreate(ctx, args[1:])
	default:
		return fmt.Errorf("unknown repos checkout-grants command %q", args[0])
	}
}

func (c CLI) repoCheckoutGrantsCreate(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("repos checkout-grants create", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	ref := fs.String("ref", "", "git ref")
	pathPrefix := fs.String("path-prefix", "", "allowed checkout path prefix")
	idempotencyKey := fs.String("idempotency-key", "", "stable mutation key")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: repos checkout-grants create <repo-id> [--ref REF] [--json]")
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	grant, err := client.Source.CreateCheckoutGrant(ctx, verself.CreateSourceCheckoutGrantInput{
		RepoID:         fs.Arg(0),
		Ref:            *ref,
		PathPrefix:     *pathPrefix,
		IdempotencyKey: *idempotencyKey,
	})
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, grant)
	}
	return writeSourceCheckoutGrant(c.out, grant)
}

func (c CLI) runRepoWorkflowRuns(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("repos workflow-runs command is required")
	}
	switch args[0] {
	case "list", "ls":
		return c.repoWorkflowRunsList(ctx, args[1:])
	case "get":
		return c.repoWorkflowRunsGet(ctx, args[1:])
	case "dispatch", "create":
		return c.repoWorkflowRunsDispatch(ctx, args[1:])
	default:
		return fmt.Errorf("unknown repos workflow-runs command %q", args[0])
	}
}

func (c CLI) repoWorkflowRunsList(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("repos workflow-runs list", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: repos workflow-runs list <repo-id> [--json]")
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	runs, err := client.Source.ListWorkflowRuns(ctx, fs.Arg(0))
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, runs)
	}
	for _, run := range runs.WorkflowRuns {
		if err := writeSourceWorkflowRun(c.out, run); err != nil {
			return err
		}
	}
	return nil
}

func (c CLI) repoWorkflowRunsGet(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("repos workflow-runs get", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: repos workflow-runs get <workflow-run-id> [--json]")
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	run, err := client.Source.GetWorkflowRun(ctx, fs.Arg(0))
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, run)
	}
	return writeSourceWorkflowRun(c.out, run)
}

func (c CLI) repoWorkflowRunsDispatch(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("repos workflow-runs dispatch", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	projectID := fs.String("project-id", "", "project id")
	workflowPath := fs.String("workflow-path", "", "workflow path")
	ref := fs.String("ref", "", "git ref")
	idempotencyKey := fs.String("idempotency-key", "", "stable mutation key")
	var inputs sourceInputFlag
	fs.Var(&inputs, "input", "workflow input key=value")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: repos workflow-runs dispatch <repo-id> --project-id PROJECT_ID --workflow-path PATH [--json]")
	}
	if strings.TrimSpace(*projectID) == "" || strings.TrimSpace(*workflowPath) == "" {
		return errors.New("repos workflow-runs dispatch requires --project-id and --workflow-path")
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	run, err := client.Source.CreateWorkflowRun(ctx, verself.CreateSourceWorkflowRunInput{
		RepoID:         fs.Arg(0),
		ProjectID:      *projectID,
		WorkflowPath:   *workflowPath,
		Ref:            *ref,
		Inputs:         inputs.mapOrNil(),
		IdempotencyKey: *idempotencyKey,
	})
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, run)
	}
	return writeSourceWorkflowRun(c.out, run)
}

type sourceInputFlag struct {
	values map[string]string
	set    bool
}

func (f *sourceInputFlag) Set(value string) error {
	key, val, ok := strings.Cut(value, "=")
	key = strings.TrimSpace(key)
	if !ok || key == "" {
		return fmt.Errorf("input values must use key=value")
	}
	if f.values == nil {
		f.values = map[string]string{}
	}
	f.values[key] = val
	f.set = true
	return nil
}

func (f *sourceInputFlag) String() string {
	if len(f.values) == 0 {
		return ""
	}
	parts := make([]string, 0, len(f.values))
	for key, value := range f.values {
		parts = append(parts, key+"="+value)
	}
	return strings.Join(parts, ",")
}

func (f *sourceInputFlag) mapOrNil() map[string]string {
	if !f.set {
		return nil
	}
	out := make(map[string]string, len(f.values))
	for key, value := range f.values {
		out[key] = value
	}
	return out
}

func writeSourceRepository(w io.Writer, repo verself.SourceRepository) error {
	return writef(w, "%s/%s\t%s\t%s\t%s\n", repo.ProjectSlug, repo.Name, repo.RepoID, repo.State, repo.GitHTTPURL)
}

func writeSourceGitCredential(w io.Writer, credential verself.SourceGitCredential) error {
	if err := writef(w, "credential_id\t%s\n", credential.CredentialID); err != nil {
		return err
	}
	if err := writef(w, "username\t%s\n", credential.Username); err != nil {
		return err
	}
	if err := writef(w, "token\t%s\n", credential.Token); err != nil {
		return err
	}
	return writef(w, "scopes\t%s\n", strings.Join(credential.Scopes, ","))
}

func writeSourceCheckoutGrant(w io.Writer, grant verself.SourceCheckoutGrant) error {
	if err := writef(w, "grant_id\t%s\n", grant.GrantID); err != nil {
		return err
	}
	if err := writef(w, "repo_id\t%s\n", grant.RepoID); err != nil {
		return err
	}
	if err := writef(w, "ref\t%s\n", grant.Ref); err != nil {
		return err
	}
	return writef(w, "token\t%s\n", grant.Token)
}

func writeSourceWorkflowRun(w io.Writer, run verself.SourceWorkflowRun) error {
	return writef(w, "%s\t%s\t%s\t%s\n", run.WorkflowRunID, run.RepoID, run.State, run.WorkflowPath)
}

func writeSourceBlob(w io.Writer, blob verself.SourceBlob) error {
	if err := writef(w, "%s\t%s\t%d\t%s\n", blob.Path, blob.Sha, blob.Size, blob.Encoding); err != nil {
		return err
	}
	if blob.Content == "" {
		return nil
	}
	return writef(w, "%s\n", blob.Content)
}
