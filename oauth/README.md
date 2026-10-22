# OAuth authorization

The server protects `/mcp` using Authorization Code with mandatory S256 PKCE.
Public clients register through RFC 7591; client credentials, refresh-token grants,
and token introspection are not implemented. Access tokens are signed HS256 JWTs.
Kite credentials stay on the server and are never returned to an MCP client.

## Endpoints

| Path | Method | Purpose |
| --- | --- | --- |
| `/register` | POST | Register callbacks and an optional client name |
| `/authorize` | GET | Display per-client consent |
| `/authorize` | POST | Approve or decline the browser-bound consent form |
| `/callback` | GET | Complete Kite login after approval in the same browser |
| `/token` | POST | Exchange a one-use authorization code with its PKCE verifier |
| `/.well-known/oauth-authorization-server` | GET | RFC 8414 server metadata |
| `/.well-known/oauth-protected-resource` | GET | RFC 9728 resource metadata |

Registration is limited to 10 requests/minute and 100/hour per peer IP or IPv6 /64,
per instance. Configure `TRUSTED_PROXY_CIDRS` only for trusted reverse proxies.
Authorization, token exchange, and callback endpoints also have rate limits.

## Client registration and consent

Clients register `redirect_uris` (HTTPS, or HTTP loopback for local clients) and may
supply `client_name`. The server signs the callback hashes and name into a durable
client ID; registration survives restarts with the same `JWT_SECRET` and issuer.
Names are supplied by clients and are not identity verification. Authorization
requires an exact registered callback; changing callbacks requires registration.

Every authorization flow asks for consent before forwarding to Kite. The form
shows the client name, complete callback, and default scope: access through all
enabled tools. The server does not remember approval across flows. A form CSRF
cookie protects approval; a separate state cookie is created only after approval.
The callback must carry that browser's matching state cookie and signed session
parameter. The pending consent record is short-lived and consumed once.

Cookies are host-only, `Secure`, `HttpOnly`, `SameSite=Lax`, and use `__Host-` names
on HTTPS. HTTP loopback development uses unprefixed cookies without `Secure`.
Consent and callback responses prevent caching, referrer leakage, and framing.

## Token and resource binding

Clients should send `resource=<OAUTH_ISSUER>/mcp` in authorization and token
requests, as required for MCP clients by the authorization specification. This
single-resource server defaults omitted indicators to that resource for older
clients, and rejects foreign or duplicate resource indicators. JWT validation
requires the configured issuer, `/mcp` audience, signature, and expiration, plus
an active backing grant. Previously issued tokens without this audience require
reauthentication after upgrading.

Codes expire after ten minutes, are consumed on exchange, and are stored in a
bounded in-memory map with lazy expiry cleanup. Consent, codes, and grants are
process-local: server restart requires login again, and multiple instances need
sticky routing or a shared session store. Durable registration alone does not
make an in-progress login portable across instances.

Application grants last at most seven days. `OAUTH_TOKEN_TTL` may shorten token
life but cannot exceed the grant lifetime. Kite's daily access is renewed only
when a refresh token is available; this is separate from an OAuth refresh grant.

## Configuration and verification

Set `KITE_API_KEY`, `KITE_API_SECRET`, and a random `JWT_SECRET` of at least 32
bytes. Set `OAUTH_ISSUER` to the external HTTPS base URL in production, and
configure the Kite app callback as `<OAUTH_ISSUER>/callback`.

Protocol requirements checked against MCP 2025-11-25:

- [Authorization and audience binding](https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization)
- [Per-client consent and callback state](https://modelcontextprotocol.io/docs/2025-11-25/tutorials/security/security_best_practices#confused-deputy-problem)
- [Streamable HTTP origin validation](https://modelcontextprotocol.io/specification/2025-11-25/basic/transports#security-warning)

Run `go test -tags=testing ./...` and `go test -race -tags=testing ./...`.
Tests use local mock HTTP servers; no live trading account is needed.

For the browser CSP regression, run with a local Chromium executable (headless):

```sh
KITE_TEST_CHROMIUM=/path/to/chromium go test ./oauth -run TestConsentBrowserRedirects -v
```

This submits the production consent form in Chromium for both approval and denial.
Kite and the client destination are represented by local mock pages. The test
reproduces the old `form-action 'self'` failure and verifies the redirects while
retaining the CSRF, callback-state, and framing protections. No live account is used.
