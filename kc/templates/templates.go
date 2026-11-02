package templates

// Embed the shared assets and pages used by the login and authorization flows.

import (
	"embed"
)

//go:embed login_success.html authorize_interstitial.html login_error.html base.html brand_assets.html order_approval.html
var FS embed.FS
