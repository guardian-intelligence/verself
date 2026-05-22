package routecatalog

import (
	_ "embed"

	runtimecatalog "github.com/verself/service-runtime/routecatalog"
)

//go:embed github-integration.route-catalog.json
var publicCatalogJSON []byte

//go:embed github-integration-internal.route-catalog.json
var internalCatalogJSON []byte

var Public = runtimecatalog.MustParse(publicCatalogJSON)

var Internal = runtimecatalog.MustParse(internalCatalogJSON)
