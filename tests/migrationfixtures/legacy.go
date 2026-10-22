// Package migrationfixtures contains immutable snapshots of the previous SQL runner.
package migrationfixtures

import "embed"

//go:embed legacy/*/*.sql
var Files embed.FS
