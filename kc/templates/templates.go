package templates

// Embed index.html in this package

import (
	"embed"
)

//go:embed index.html
var FS embed.FS
