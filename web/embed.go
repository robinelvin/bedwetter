package web

import (
	"embed"
	"strings"
)

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed VERSION.txt
var versionTXT string

// Version is displayed in the UI header.
var Version = strings.TrimSpace(versionTXT)

// staticVersion is used for cache busting.
var staticVersion = Version
