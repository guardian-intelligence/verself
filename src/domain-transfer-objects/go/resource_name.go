package dto

import (
	"errors"
	"fmt"
	"strings"
)

const (
	ResourceNamePrefix    = "urn:verself:"
	ResourceNameNamespace = "verself"
)

type ResourceName string

func (r ResourceName) String() string {
	return string(r)
}

type ResourcePathSegment struct {
	Collection string
	ID         string
}

type ParsedResourceName struct {
	InstallationID string
	Segments       []ResourcePathSegment
}

func (p ParsedResourceName) Leaf() ResourcePathSegment {
	if len(p.Segments) == 0 {
		return ResourcePathSegment{}
	}
	return p.Segments[len(p.Segments)-1]
}

func (p ParsedResourceName) IDForCollection(collection string) (string, bool) {
	collection = strings.TrimSpace(collection)
	for _, segment := range p.Segments {
		if segment.Collection == collection {
			return segment.ID, true
		}
	}
	return "", false
}

func (p ParsedResourceName) String() string {
	return FormatResourceName(p.InstallationID, p.Segments...).String()
}

func FormatResourceName(installationID string, segments ...ResourcePathSegment) ResourceName {
	installationID = strings.TrimSpace(installationID)
	parts := make([]string, 0, len(segments)*2)
	for _, segment := range segments {
		parts = append(parts, strings.TrimSpace(segment.Collection), strings.TrimSpace(segment.ID))
	}
	return ResourceName(ResourceNamePrefix + installationID + ":" + strings.Join(parts, "/"))
}

func ParseResourceName(value string) (ParsedResourceName, error) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, ResourceNamePrefix) {
		return ParsedResourceName{}, errors.New("resource name must start with urn:verself")
	}
	rest := strings.TrimPrefix(value, ResourceNamePrefix)
	installationID, path, ok := strings.Cut(rest, ":")
	if !ok {
		return ParsedResourceName{}, errors.New("resource name must include installation id and resource path")
	}
	installationID = strings.TrimSpace(installationID)
	if err := validateResourceNameAtom("installation id", installationID, false); err != nil {
		return ParsedResourceName{}, err
	}
	raw := strings.Split(path, "/")
	if len(raw) == 0 || len(raw)%2 != 0 {
		return ParsedResourceName{}, errors.New("resource path must alternate collection and resource id")
	}
	segments := make([]ResourcePathSegment, 0, len(raw)/2)
	for i := 0; i < len(raw); i += 2 {
		collection := strings.TrimSpace(raw[i])
		id := strings.TrimSpace(raw[i+1])
		if err := validateResourceNameAtom("collection", collection, true); err != nil {
			return ParsedResourceName{}, err
		}
		if err := validateResourceNameAtom("resource id", id, false); err != nil {
			return ParsedResourceName{}, err
		}
		segments = append(segments, ResourcePathSegment{Collection: collection, ID: id})
	}
	return ParsedResourceName{InstallationID: installationID, Segments: segments}, nil
}

func ValidateResourceName(value string) error {
	_, err := ParseResourceName(value)
	return err
}

func RequireResourceName(value string) ResourceName {
	name := ResourceName(strings.TrimSpace(value))
	if err := ValidateResourceName(name.String()); err != nil {
		panic(fmt.Sprintf("invalid Verself resource name %q: %v", value, err))
	}
	return name
}

func ResourceNameOrg(installationID, orgID string) ResourceName {
	return FormatResourceName(installationID, ResourcePathSegment{Collection: "orgs", ID: orgID})
}

func ResourceNameMember(installationID, orgID, memberID string) ResourceName {
	return FormatResourceName(installationID,
		ResourcePathSegment{Collection: "orgs", ID: orgID},
		ResourcePathSegment{Collection: "members", ID: memberID},
	)
}

func ResourceNameCredential(installationID, orgID, credentialID string) ResourceName {
	return FormatResourceName(installationID,
		ResourcePathSegment{Collection: "orgs", ID: orgID},
		ResourcePathSegment{Collection: "credentials", ID: credentialID},
	)
}

func ResourceNameMachinePrincipal(installationID, orgID, principalID string) ResourceName {
	return FormatResourceName(installationID,
		ResourcePathSegment{Collection: "orgs", ID: orgID},
		ResourcePathSegment{Collection: "machinePrincipals", ID: principalID},
	)
}

func ResourceNameProject(installationID, orgID, projectID string) ResourceName {
	return FormatResourceName(installationID,
		ResourcePathSegment{Collection: "orgs", ID: orgID},
		ResourcePathSegment{Collection: "projects", ID: projectID},
	)
}

func ResourceNameEnvironment(installationID, orgID, projectID, environmentID string) ResourceName {
	return FormatResourceName(installationID,
		ResourcePathSegment{Collection: "orgs", ID: orgID},
		ResourcePathSegment{Collection: "projects", ID: projectID},
		ResourcePathSegment{Collection: "environments", ID: environmentID},
	)
}

func ResourceNameRepository(installationID, orgID, projectID, repositoryID string) ResourceName {
	return FormatResourceName(installationID,
		ResourcePathSegment{Collection: "orgs", ID: orgID},
		ResourcePathSegment{Collection: "projects", ID: projectID},
		ResourcePathSegment{Collection: "repositories", ID: repositoryID},
	)
}

func ResourceNameSourceWorkflowRun(installationID, orgID, projectID, repositoryID, workflowRunID string) ResourceName {
	return FormatResourceName(installationID,
		ResourcePathSegment{Collection: "orgs", ID: orgID},
		ResourcePathSegment{Collection: "projects", ID: projectID},
		ResourcePathSegment{Collection: "repositories", ID: repositoryID},
		ResourcePathSegment{Collection: "workflowRuns", ID: workflowRunID},
	)
}

func ResourceNameCheckoutGrant(installationID, orgID, grantID string) ResourceName {
	return FormatResourceName(installationID,
		ResourcePathSegment{Collection: "orgs", ID: orgID},
		ResourcePathSegment{Collection: "checkoutGrants", ID: grantID},
	)
}

func ResourceNameRun(installationID, orgID, runID string) ResourceName {
	return FormatResourceName(installationID,
		ResourcePathSegment{Collection: "orgs", ID: orgID},
		ResourcePathSegment{Collection: "runs", ID: runID},
	)
}

func ResourceNameAttempt(installationID, orgID, runID, attemptID string) ResourceName {
	return FormatResourceName(installationID,
		ResourcePathSegment{Collection: "orgs", ID: orgID},
		ResourcePathSegment{Collection: "runs", ID: runID},
		ResourcePathSegment{Collection: "attempts", ID: attemptID},
	)
}

func ResourceNameSchedule(installationID, orgID, scheduleID string) ResourceName {
	return FormatResourceName(installationID,
		ResourcePathSegment{Collection: "orgs", ID: orgID},
		ResourcePathSegment{Collection: "schedules", ID: scheduleID},
	)
}

func ResourceNameSecret(installationID, orgID, secretID string) ResourceName {
	return FormatResourceName(installationID,
		ResourcePathSegment{Collection: "orgs", ID: orgID},
		ResourcePathSegment{Collection: "secrets", ID: secretID},
	)
}

func ResourceNameSecretVersion(installationID, orgID, secretID, versionID string) ResourceName {
	return FormatResourceName(installationID,
		ResourcePathSegment{Collection: "orgs", ID: orgID},
		ResourcePathSegment{Collection: "secrets", ID: secretID},
		ResourcePathSegment{Collection: "versions", ID: versionID},
	)
}

func ResourceNameVariable(installationID, orgID, variableID string) ResourceName {
	return FormatResourceName(installationID,
		ResourcePathSegment{Collection: "orgs", ID: orgID},
		ResourcePathSegment{Collection: "variables", ID: variableID},
	)
}

func ResourceNameTransitKey(installationID, orgID, keyID string) ResourceName {
	return FormatResourceName(installationID,
		ResourcePathSegment{Collection: "orgs", ID: orgID},
		ResourcePathSegment{Collection: "transitKeys", ID: keyID},
	)
}

func ResourceNameAuditExport(installationID, orgID, exportID string) ResourceName {
	return FormatResourceName(installationID,
		ResourcePathSegment{Collection: "orgs", ID: orgID},
		ResourcePathSegment{Collection: "auditExports", ID: exportID},
	)
}

func ResourceNameInvoice(installationID, orgID, invoiceID string) ResourceName {
	return FormatResourceName(installationID,
		ResourcePathSegment{Collection: "orgs", ID: orgID},
		ResourcePathSegment{Collection: "invoices", ID: invoiceID},
	)
}

func ResourceNameBillingDocument(installationID, orgID, documentID string) ResourceName {
	return FormatResourceName(installationID,
		ResourcePathSegment{Collection: "orgs", ID: orgID},
		ResourcePathSegment{Collection: "billingDocuments", ID: documentID},
	)
}

func ResourceNameGitHubInstallation(installationID, orgID, githubInstallationID string) ResourceName {
	return FormatResourceName(installationID,
		ResourcePathSegment{Collection: "orgs", ID: orgID},
		ResourcePathSegment{Collection: "githubInstallations", ID: githubInstallationID},
	)
}

func ResourceNameOpaqueCredential(installationID, orgID, credentialID string) ResourceName {
	return FormatResourceName(installationID,
		ResourcePathSegment{Collection: "orgs", ID: orgID},
		ResourcePathSegment{Collection: "credentials", ID: credentialID},
	)
}

func ResourceNameNotification(installationID, orgID, notificationID string) ResourceName {
	return FormatResourceName(installationID,
		ResourcePathSegment{Collection: "orgs", ID: orgID},
		ResourcePathSegment{Collection: "notifications", ID: notificationID},
	)
}

func ResourceNameAnalyticsDataset(installationID, orgID, projectID, datasetID string) ResourceName {
	return FormatResourceName(installationID,
		ResourcePathSegment{Collection: "orgs", ID: orgID},
		ResourcePathSegment{Collection: "projects", ID: projectID},
		ResourcePathSegment{Collection: "analyticsDatasets", ID: datasetID},
	)
}

func validateResourceNameAtom(label, value string, collection bool) error {
	if value == "" {
		return fmt.Errorf("%s is required", label)
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' {
			continue
		}
		if r >= 'A' && r <= 'Z' {
			continue
		}
		if r >= '0' && r <= '9' {
			continue
		}
		switch r {
		case '-', '_', '.', '~':
			continue
		}
		return fmt.Errorf("%s contains unsupported character %q", label, r)
	}
	if collection {
		first := value[0]
		if first < 'a' || first > 'z' {
			return fmt.Errorf("%s must start with a lowercase ASCII letter", label)
		}
	}
	return nil
}
