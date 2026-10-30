package templates

// Embed login_success.html, status.html, authorize_interstitial.html, and base.html in this package

import (
	"embed"
)

//go:embed login_success.html status.html authorize_interstitial.html login_error.html base.html brand_assets.html
var FS embed.FS
