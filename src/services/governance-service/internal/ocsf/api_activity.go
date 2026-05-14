package ocsf

import (
	"encoding/json"

	schema "github.com/telophasehq/go-ocsf/ocsf/v1_5_0"
)

const (
	SchemaVersion = "1.5.0"

	CategoryUIDApplicationActivity = 6
	CategoryNameApplication        = "Application Activity"

	ClassUIDAPIActivity  = 6003
	ClassNameAPIActivity = "API Activity"

	ActivityIDCreate = 1
	ActivityIDRead   = 2
	ActivityIDUpdate = 3
	ActivityIDDelete = 4
	ActivityIDOther  = 99

	ActionIDAllowed = 1
	ActionIDDenied  = 2

	SeverityIDInformational = 1
	SeverityInformational   = "Informational"

	StatusIDSuccess = 1
	StatusIDFailure = 2
	StatusIDOther   = 99
)

type (
	APIActivity            = schema.APIActivity
	Actor                  = schema.Actor
	API                    = schema.API
	HTTPRequest            = schema.HTTPRequest
	HTTPResponse           = schema.HTTPResponse
	Metadata               = schema.Metadata
	NetworkEndpoint        = schema.NetworkEndpoint
	Product                = schema.Product
	RequestElements        = schema.RequestElements
	ResourceDetails        = schema.ResourceDetails
	ResponseElements       = schema.ResponseElements
	Service                = schema.Service
	UniformResourceLocator = schema.UniformResourceLocator
	User                   = schema.User
)

type ActivityResource struct {
	Type     string
	UID      string
	Name     string
	FullName string
	Role     string
	RoleID   uint8
}

type BuildOutput struct {
	Event     APIActivity
	JSON      string
	SHA256Hex string
	Resources []ActivityResource
}

func CanonicalJSON(value any) ([]byte, error) {
	return json.Marshal(value)
}
