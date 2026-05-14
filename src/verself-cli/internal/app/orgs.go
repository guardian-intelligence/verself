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
	page, err := client.IAM.ListOrganizations(ctx, verself.ListOrganizationsOptions{})
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, page.Organizations)
	}
	for _, org := range page.Organizations {
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
	page, err := client.IAM.ListOrganizations(ctx, verself.ListOrganizationsOptions{})
	if err != nil {
		return err
	}
	selected := verself.Organization{}
	requested := strings.TrimSpace(fs.Arg(0))
	for _, org := range page.Organizations {
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
	profile.SelectedOrg = orgRefFromSDK(selected)
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
	client, profile, err := c.serviceClientWithProfile(*serviceFlags)
	if err != nil {
		return err
	}
	org, err := selectedOrganization(ctx, client, profile)
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, org)
	}
	return writeOrg(c.out, org)
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
	client, profile, err := c.serviceClientWithProfile(*serviceFlags)
	if err != nil {
		return err
	}
	org, err := selectedOrganization(ctx, client, profile)
	if err != nil {
		return err
	}
	updated, err := client.IAM.UpdateOrganization(ctx, verself.UpdateOrganizationInput{
		OrgID:          org.OrgID,
		Version:        int64(parsedVersion),
		DisplayName:    displayName.ptr(),
		Slug:           slug.ptr(),
		IdempotencyKey: *idempotencyKey,
	})
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, updated)
	}
	return writeOrg(c.out, updated)
}

func selectedOrganization(ctx context.Context, client *verself.Client, profile *ProfileRecord) (verself.Organization, error) {
	if profile != nil && profile.SelectedOrg != nil && strings.TrimSpace(profile.SelectedOrg.OrgID) != "" {
		return client.IAM.GetOrganization(ctx, profile.SelectedOrg.OrgID)
	}
	page, err := client.IAM.ListOrganizations(ctx, verself.ListOrganizationsOptions{PageSize: 1})
	if err != nil {
		return verself.Organization{}, err
	}
	if len(page.Organizations) == 0 {
		return verself.Organization{}, errors.New("current profile has no available organizations")
	}
	return page.Organizations[0], nil
}

func orgRefFromSDK(org verself.Organization) *OrgRef {
	return &OrgRef{
		OrgID:       org.OrgID,
		Slug:        org.Slug,
		DisplayName: org.DisplayName,
	}
}

func writeOrg(w interface{ Write([]byte) (int, error) }, org verself.Organization) error {
	return writef(w, "%s\t%s\t%s\n", org.Slug, org.OrgID, org.DisplayName)
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
