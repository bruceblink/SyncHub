package migrations

import "embed"

// FS contains the PostgreSQL migration files shipped with the API binary.
//
//go:embed *.sql
var FS embed.FS
