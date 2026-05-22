package governance

import (
	"strings"
)

const resourceNamePrefix = "urn:verself:"

type ResourceName string

func (r ResourceName) String() string {
	return string(r)
}

type ResourcePathSegment struct {
	Collection string
	ID         string
}

func FormatResourceName(installationID string, segments ...ResourcePathSegment) ResourceName {
	installationID = strings.TrimSpace(installationID)
	parts := make([]string, 0, len(segments)*2)
	for _, segment := range segments {
		parts = append(parts, strings.TrimSpace(segment.Collection), strings.TrimSpace(segment.ID))
	}
	return ResourceName(resourceNamePrefix + installationID + ":" + strings.Join(parts, "/"))
}

func ResourceNameOrg(installationID, orgID string) ResourceName {
	return FormatResourceName(installationID, ResourcePathSegment{Collection: "orgs", ID: orgID})
}

func ResourceNameAPIActivity(installationID, orgID, metadataUID string) ResourceName {
	return FormatResourceName(installationID,
		ResourcePathSegment{Collection: "orgs", ID: orgID},
		ResourcePathSegment{Collection: "apiActivities", ID: metadataUID},
	)
}

func ResourceNameDataExport(installationID, orgID, exportID string) ResourceName {
	return FormatResourceName(installationID,
		ResourcePathSegment{Collection: "orgs", ID: orgID},
		ResourcePathSegment{Collection: "dataExports", ID: exportID},
	)
}

func ResourceNameMember(installationID, orgID, memberID string) ResourceName {
	return FormatResourceName(installationID,
		ResourcePathSegment{Collection: "orgs", ID: orgID},
		ResourcePathSegment{Collection: "members", ID: memberID},
	)
}

func ResourceNameMachinePrincipal(installationID, orgID, principalID string) ResourceName {
	return FormatResourceName(installationID,
		ResourcePathSegment{Collection: "orgs", ID: orgID},
		ResourcePathSegment{Collection: "machinePrincipals", ID: principalID},
	)
}

func ResourceNameCredential(installationID, orgID, credentialID string) ResourceName {
	return FormatResourceName(installationID,
		ResourcePathSegment{Collection: "orgs", ID: orgID},
		ResourcePathSegment{Collection: "credentials", ID: credentialID},
	)
}

func ResourceNameSecret(installationID, orgID, secretID string) ResourceName {
	return FormatResourceName(installationID,
		ResourcePathSegment{Collection: "orgs", ID: orgID},
		ResourcePathSegment{Collection: "secrets", ID: secretID},
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

func ResourceNameRun(installationID, orgID, runID string) ResourceName {
	return FormatResourceName(installationID,
		ResourcePathSegment{Collection: "orgs", ID: orgID},
		ResourcePathSegment{Collection: "runs", ID: runID},
	)
}

func ResourceNameSchedule(installationID, orgID, scheduleID string) ResourceName {
	return FormatResourceName(installationID,
		ResourcePathSegment{Collection: "orgs", ID: orgID},
		ResourcePathSegment{Collection: "schedules", ID: scheduleID},
	)
}
