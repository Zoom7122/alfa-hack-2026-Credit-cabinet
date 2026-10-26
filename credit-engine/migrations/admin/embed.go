// Package adminmigrations contains only the administrative service's schema.
package adminmigrations

import "embed"

//go:embed *.sql
var FS embed.FS
