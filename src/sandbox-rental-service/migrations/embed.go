package migrations

import "embed"

//go:embed *.up.sql
var Files embed.FS
