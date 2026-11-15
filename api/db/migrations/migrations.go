package migrations

import "embed"

// FS contém todas as migrations SQL gerenciadas pelo goose.
//
//go:embed *.sql
var FS embed.FS
