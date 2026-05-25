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
	case "create":
		return c.orgsCreate(ctx, args[1:])
	case "use":
		return c.orgsUse(ctx, args[1:])
	case "inspect":
		return c.orgsInspect(ctx, args[1:])
	case "update":
		return c.orgsUpdate(ctx, args[1:])
	case "members":
		return c.orgsMembers(ctx, args[1:])
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

func (c CLI) orgsCreate(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("orgs create", c.err)
	displayName := fs.String("display-name", "", "organization display name")
	slug := fs.String("slug", "", "organization slug")
	idempotencyKey := fs.String("idempotency-key", "", "stable mutation key")
	jsonOut := fs.Bool("json", false, "json output")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: orgs create --display-name NAME [--slug SLUG]")
	}
	if strings.TrimSpace(*displayName) == "" {
		return errors.New("orgs create requires --display-name")
	}
	client, profile, err := c.serviceClientWithProfile(*serviceFlags)
	if err != nil {
		return err
	}
	var slugPtr *string
	if strings.TrimSpace(*slug) != "" {
		value := strings.TrimSpace(*slug)
		slugPtr = &value
	}
	org, err := client.IAM.CreateOrganization(ctx, verself.CreateOrganizationInput{
		DisplayName:    strings.TrimSpace(*displayName),
		Slug:           slugPtr,
		IdempotencyKey: *idempotencyKey,
	})
	if err != nil {
		return err
	}
	if profile != nil {
		if profile.Account == nil {
			return errors.New("active auth profile has no selected account; run `verself auth accounts use`")
		}
		profile.Account.SelectedOrg = orgRefFromSDK(org)
		store, err := newStore(c.getenv)
		if err != nil {
			return err
		}
		if err := store.SaveAccount(*profile.Account); err != nil {
			return err
		}
	}
	if *jsonOut {
		return writeJSON(c.out, org)
	}
	return writeOrg(c.out, org)
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
	if strings.TrimSpace(profile.ActiveAccount) == "" {
		return errors.New("active auth profile has no selected account; run `verself auth accounts use`")
	}
	account, err := store.LoadAccount(profile.Name, profile.ActiveAccount)
	if err != nil {
		return err
	}
	account.SelectedOrg = orgRefFromSDK(selected)
	if err := store.SaveAccount(account); err != nil {
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

func (c CLI) orgsMembers(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("orgs members command is required")
	}
	switch args[0] {
	case "list", "ls":
		return c.orgsMembersList(ctx, args[1:])
	case "invite":
		return c.orgsMembersInvite(ctx, args[1:])
	default:
		return fmt.Errorf("unknown orgs members command %q", args[0])
	}
}

func (c CLI) orgsMembersList(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("orgs members list", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: orgs members list [--json]")
	}
	client, profile, err := c.serviceClientWithProfile(*serviceFlags)
	if err != nil {
		return err
	}
	org, err := selectedOrganization(ctx, client, profile)
	if err != nil {
		return err
	}
	members, err := client.IAM.ListMembers(ctx, verself.ListMembersOptions{OrgID: org.OrgID})
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, members.Members)
	}
	for _, member := range members.Members {
		if err := writef(c.out, "%s\t%s\t%s\n", member.Email, member.MemberID, member.DisplayName); err != nil {
			return err
		}
	}
	return nil
}

func (c CLI) orgsMembersInvite(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("orgs members invite", c.err)
	var roles repeatedStringFlag
	fs.Var(&roles, "role", "IAM role to grant")
	givenName := fs.String("given-name", "", "given name")
	familyName := fs.String("family-name", "", "family name")
	idempotencyKey := fs.String("idempotency-key", "", "stable mutation key")
	jsonOut := fs.Bool("json", false, "json output")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: orgs members invite <email> [--role ROLE]")
	}
	client, profile, err := c.serviceClientWithProfile(*serviceFlags)
	if err != nil {
		return err
	}
	org, err := selectedOrganization(ctx, client, profile)
	if err != nil {
		return err
	}
	var givenNamePtr, familyNamePtr *string
	if strings.TrimSpace(*givenName) != "" {
		value := strings.TrimSpace(*givenName)
		givenNamePtr = &value
	}
	if strings.TrimSpace(*familyName) != "" {
		value := strings.TrimSpace(*familyName)
		familyNamePtr = &value
	}
	roleNames := make([]verself.IAMRoleName, 0, len(roles.values))
	for _, role := range roles.values {
		roleNames = append(roleNames, verself.IAMRoleName(role))
	}
	invitation, err := client.IAM.InviteMember(ctx, verself.InviteMemberInput{
		OrgID:          org.OrgID,
		Email:          fs.Arg(0),
		GivenName:      givenNamePtr,
		FamilyName:     familyNamePtr,
		Roles:          roleNames,
		IdempotencyKey: *idempotencyKey,
	})
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, invitation)
	}
	return writef(c.out, "%s\t%s\t%s\n", invitation.Email, invitation.MemberID, invitation.Status)
}

func selectedOrganization(ctx context.Context, client *verself.Client, profile *ProfileRecord) (verself.Organization, error) {
	if profile != nil && profile.Account != nil && profile.Account.SelectedOrg != nil && strings.TrimSpace(profile.Account.SelectedOrg.OrgID) != "" {
		return client.IAM.GetOrganization(ctx, profile.Account.SelectedOrg.OrgID)
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
