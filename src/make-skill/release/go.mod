module github.com/verself/make-skill/release

go 1.25.8

require (
	github.com/opencontainers/image-spec v1.1.1
	github.com/verself/releaseattest v0.0.0
	oras.land/oras-go/v2 v2.6.1
)

require (
	github.com/opencontainers/go-digest v1.0.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
)

replace github.com/verself/releaseattest => ../../tools/releaseattest
