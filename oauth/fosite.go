package oauth

import (
	"github.com/ory/fosite"
	"github.com/ory/fosite/compose"
	"github.com/ory/fosite/handler/oauth2"
	"github.com/ory/fosite/handler/pkce"
	"golang.org/x/crypto/bcrypt"
)

// FositeStore defines the methods our storage needs to implement.
// It is composed of the individual storage interfaces required by the handlers.
type FositeStore interface {
	fosite.ClientManager
	oauth2.AuthorizeCodeStorage
	oauth2.AccessTokenStorage
	oauth2.RefreshTokenStorage
	oauth2.TokenRevocationStorage
	pkce.PKCERequestStorage
}

// NewFositeProvider creates and configures the Fosite OAuth2 provider.
func NewFositeProvider(store FositeStore, config *fosite.Config) fosite.OAuth2Provider {
	// This is the strategy for creating and validating tokens.
	// We are using the HMAC-SHA256 strategy, which creates opaque, non-JWT tokens.
	strategy := &compose.CommonStrategy{
		CoreStrategy: compose.NewOAuth2HMACStrategy(config),
	}

	// The compose.Compose function creates the OAuth2 provider by wiring up the handlers.
	return compose.Compose(
		config,
		store,
		strategy,
		// nil, // We are not using a custom hasher, so this is nil.

		// --- Handler Factories ---
		// These are the features we want to support.
		compose.OAuth2AuthorizeExplicitFactory,
		compose.OAuth2ClientCredentialsGrantFactory,
		compose.OAuth2RefreshTokenGrantFactory,
		compose.OAuth2TokenIntrospectionFactory,
		compose.OAuth2TokenRevocationFactory,
		compose.OAuth2PKCEFactory,
	)
}

// HashSecret hashes a client secret using bcrypt.
func HashSecret(secret string) ([]byte, error) {
	return bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
}
