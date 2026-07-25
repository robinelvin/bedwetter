package web

import "embed"

//go:embed templates/*.html
var templatesFS embed.FS

// staticVersion is set at build time via -ldflags for cache busting.
var staticVersion string
