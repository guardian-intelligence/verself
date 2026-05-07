package app

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	verself "github.com/verself/verself-go"
)

func (c CLI) runOrgs(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("orgs command is required")
	}
	switch args[0] {
	case "list", "ls":
		return c.orgsList(ctx, args[1:])
	case "use":
		return c.orgsUse(ctx, args[1:])
	case "inspect":
		return c.orgsInspect(ctx, args[1:])
	case "update":
		return c.orgsUpdate(ctx, args[1:])
	case "credentials", "api-credentials":
		return c.runOrgCredentials(ctx, args[1:])
	default:
		return fmt.Errorf("unknown orgs command %q", args[0])
	}
}

func (c CLI) orgsList(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("orgs list", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: orgs list [--json]")
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	orgs, err := client.IAM.ListMyOrganizations(ctx)
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, orgs)
	}
	for _, org := range orgs {
		if err := writeOrg(c.out, org); err != nil {
			return err
		}
	}
	return nil
}

func (c CLI) orgsUse(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("orgs use", c.err)
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: orgs use <org-id|slug>")
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	orgs, err := client.IAM.ListMyOrganizations(ctx)
	if err != nil {
		return err
	}
	selected := verself.OrganizationMetadata{}
	requested := strings.TrimSpace(fs.Arg(0))
	for _, org := range orgs {
		if org.OrgID == requested || org.Slug == requested {
			selected = org
			break
		}
	}
	if selected.OrgID == "" {
		return fmt.Errorf("organization %q is not available to the current profile", requested)
	}
	store, err := newStore(c.getenv)
	if err != nil {
		return err
	}
	profile, err := store.LoadProfile("")
	if err != nil {
		return err
	}
	profile.SelectedOrg = &OrgRef{
		OrgID:       selected.OrgID,
		Slug:        selected.Slug,
		DisplayName: selected.DisplayName,
	}
	if err := store.SaveProfile(profile); err != nil {
		return err
	}
	return writef(c.out, "active org %s\n", selected.Slug)
}

func (c CLI) orgsInspect(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("orgs inspect", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: orgs inspect [--json]")
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	org, err := client.IAM.GetOrganization(ctx)
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, org)
	}
	return writef(c.out, "%s\t%s\t%s\n", org.Slug, org.OrgID, org.DisplayName)
}

func (c CLI) orgsUpdate(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("orgs update", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	version := fs.String("version", "", "observed organization version")
	idempotencyKey := fs.String("idempotency-key", "", "stable mutation key")
	var displayName, slug optionalStringFlag
	fs.Var(&displayName, "display-name", "organization display name")
	fs.Var(&slug, "slug", "organization slug")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: orgs update --version VERSION [--display-name NAME] [--slug SLUG]")
	}
	if !displayName.set && !slug.set {
		return errors.New("orgs update requires --display-name or --slug")
	}
	parsedVersion, err := parseInt32Flag(*version, "version")
	if err != nil {
		return err
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	org, err := client.IAM.UpdateOrganization(ctx, verself.UpdateOrganizationInput{
		Version:        parsedVersion,
		DisplayName:    displayName.ptr(),
		Slug:           slug.ptr(),
		IdempotencyKey: *idempotencyKey,
	})
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, org)
	}
	return writef(c.out, "%s\t%s\t%s\n", org.Slug, org.OrgID, org.DisplayName)
}

func (c CLI) runOrgCredentials(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("orgs credentials command is required")
	}
	switch args[0] {
	case "list", "ls":
		return c.orgCredentialsList(ctx, args[1:])
	case "get":
		return c.orgCredentialsGet(ctx, args[1:])
	case "create":
		return c.orgCredentialsCreate(ctx, args[1:])
	case "roll":
		return c.orgCredentialsRoll(ctx, args[1:])
	case "revoke":
		return c.orgCredentialsRevoke(ctx, args[1:])
	default:
		return fmt.Errorf("unknown orgs credentials command %q", args[0])
	}
}

func (c CLI) orgCredentialsList(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("orgs credentials list", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: orgs credentials list [--json]")
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	credentials, err := client.IAM.ListAPICredentials(ctx)
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, credentials)
	}
	for _, credential := range credentials {
		if err := writeCredential(c.out, credential); err != nil {
			return err
		}
	}
	return nil
}

func (c CLI) orgCredentialsGet(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("orgs credentials get", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: orgs credentials get <credential-id> [--json]")
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	credential, err := client.IAM.GetAPICredential(ctx, fs.Arg(0))
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, credential)
	}
	return writeCredential(c.out, credential)
}

func (c CLI) orgCredentialsCreate(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("orgs credentials create", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	authMethod := fs.String("auth-method", "private_key_jwt", "private_key_jwt or client_secret")
	idempotencyKey := fs.String("idempotency-key", "", "stable mutation key")
	var permissions repeatedStringFlag
	fs.Var(&permissions, "permission", "permission to grant")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: orgs credentials create <display-name> --permission PERMISSION [--json]")
	}
	if len(permissions.values) == 0 {
		return errors.New("orgs credentials create requires at least one --permission")
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	result, err := client.IAM.CreateAPICredential(ctx, verself.CreateAPICredentialInput{
		DisplayName:    fs.Arg(0),
		AuthMethod:     verself.APICredentialAuthMethod(*authMethod),
		Permissions:    permissions.values,
		IdempotencyKey: *idempotencyKey,
	})
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, result)
	}
	if err := writeCredential(c.out, result.Credential); err != nil {
		return err
	}
	return writeIssuedMaterial(c.out, result.IssuedMaterial)
}

func (c CLI) orgCredentialsRoll(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("orgs credentials roll", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	authMethod := fs.String("auth-method", "", "private_key_jwt or client_secret")
	idempotencyKey := fs.String("idempotency-key", "", "stable mutation key")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: orgs credentials roll <credential-id> [--json]")
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	result, err := client.IAM.RollAPICredential(ctx, verself.RollAPICredentialInput{
		CredentialID:   fs.Arg(0),
		AuthMethod:     verself.APICredentialAuthMethod(*authMethod),
		IdempotencyKey: *idempotencyKey,
	})
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, result)
	}
	if err := writeCredential(c.out, result.Credential); err != nil {
		return err
	}
	return writeIssuedMaterial(c.out, result.IssuedMaterial)
}

func (c CLI) orgCredentialsRevoke(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("orgs credentials revoke", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	idempotencyKey := fs.String("idempotency-key", "", "stable mutation key")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: orgs credentials revoke <credential-id> [--json]")
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	credential, err := client.IAM.RevokeAPICredential(ctx, verself.RevokeAPICredentialInput{
		CredentialID:   fs.Arg(0),
		IdempotencyKey: *idempotencyKey,
	})
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, credential)
	}
	return writeCredential(c.out, credential)
}

func writeOrg(w interface{ Write([]byte) (int, error) }, org verself.OrganizationMetadata) error {
	return writef(w, "%s\t%s\t%s\n", org.Slug, org.OrgID, org.DisplayName)
}

func writeCredential(w interface{ Write([]byte) (int, error) }, credential verself.APICredential) error {
	return writef(w, "%s\t%s\t%s\t%s\n", credential.CredentialID, credential.Status, credential.AuthMethod, credential.DisplayName)
}

func writeIssuedMaterial(w interface{ Write([]byte) (int, error) }, material verself.APICredentialIssuedMaterial) error {
	if material.KeyContent != "" {
		return writef(w, "key_content\t%s\n", material.KeyContent)
	}
	if material.ClientSecret != "" {
		return writef(w, "client_secret\t%s\n", material.ClientSecret)
	}
	return nil
}

func parseInt32Flag(value, name string) (int32, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, fmt.Errorf("%s is required", name)
	}
	parsed, err := strconv.ParseInt(trimmed, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("%s must be an int32: %w", name, err)
	}
	return int32(parsed), nil
}

type repeatedStringFlag struct {
	values []string
}

func (f *repeatedStringFlag) Set(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return errors.New("value must be non-empty")
	}
	f.values = append(f.values, trimmed)
	return nil
}

func (f *repeatedStringFlag) String() string {
	return strings.Join(f.values, ",")
}
