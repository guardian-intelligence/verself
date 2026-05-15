package routecatalog

import (
	_ "embed"

	runtimecatalog "github.com/verself/service-runtime/routecatalog"
)

//go:embed profile.route-catalog.json
var publicCatalogJSON []byte

//go:embed profile-internal.route-catalog.json
var internalCatalogJSON []byte

var Public = runtimecatalog.MustParse(publicCatalogJSON)

var Internal = runtimecatalog.MustParse(internalCatalogJSON)
