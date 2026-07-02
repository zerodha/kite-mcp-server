package oauth

import (
	"testing"
)

func TestValidateRedirectURI_DefaultLocalhost(t *testing.T) {
	srv := New(Config{})

	valid := []string{
		"http://localhost:3000/callback",
		"http://127.0.0.1:8080/callback",
		"https://localhost/callback",
	}
	for _, uri := range valid {
		if err := srv.ValidateRedirectURI(uri); err != nil {
			t.Fatalf("expected %q to be allowed, got error: %v", uri, err)
		}
	}

	invalid := []string{
		"https://claude.ai/api/mcp/auth_callback",
		"https://chatgpt.com/connector_platform_oauth_redirect",
	}
	for _, uri := range invalid {
		if err := srv.ValidateRedirectURI(uri); err == nil {
			t.Fatalf("expected %q to be rejected", uri)
		}
	}
}

func TestValidateRedirectURI_ExplicitHostedCallbacks(t *testing.T) {
	srv := New(Config{AllowedRedirectPatterns: []string{
		"localhost",
		"https://claude.ai/api/mcp/auth_callback",
		"https://claude.com/api/mcp/auth_callback",
		"https://chatgpt.com/connector_platform_oauth_redirect",
	}})

	valid := []string{
		"http://localhost:3000/callback",
		"https://claude.ai/api/mcp/auth_callback",
		"https://claude.com/api/mcp/auth_callback",
		"https://chatgpt.com/connector_platform_oauth_redirect",
	}
	for _, uri := range valid {
		if err := srv.ValidateRedirectURI(uri); err != nil {
			t.Fatalf("expected %q to be allowed, got error: %v", uri, err)
		}
	}

	invalid := []string{
		"https://claude.ai/other-path",
		"https://chatgpt.com/other-callback",
		"https://evil.example.com/callback",
	}
	for _, uri := range invalid {
		if err := srv.ValidateRedirectURI(uri); err == nil {
			t.Fatalf("expected %q to be rejected", uri)
		}
	}
}

func TestValidateRedirectURI_RejectsHostnameOnlyPattern(t *testing.T) {
	srv := New(Config{AllowedRedirectPatterns: []string{"claude.ai"}})

	if err := srv.ValidateRedirectURI("https://claude.ai/api/mcp/auth_callback"); err == nil {
		t.Fatalf("expected hostname-only pattern to be rejected")
	}
}

func TestValidateRedirectURI_PrefixHostedCallbacks(t *testing.T) {
	srv := New(Config{AllowedRedirectPatterns: []string{
		"prefix:https://chatgpt.com/connector/oauth/",
	}})

	valid := []string{
		"https://chatgpt.com/connector/oauth/cb_123",
		"https://chatgpt.com/connector/oauth/abc-def",
	}
	for _, uri := range valid {
		if err := srv.ValidateRedirectURI(uri); err != nil {
			t.Fatalf("expected %q to be allowed, got error: %v", uri, err)
		}
	}

	invalid := []string{
		"https://chatgpt.com/connector_platform_oauth_redirect",
		"https://chatgpt.com/connector/other/cb_123",
		"https://evil.example.com/connector/oauth/cb_123",
	}
	for _, uri := range invalid {
		if err := srv.ValidateRedirectURI(uri); err == nil {
			t.Fatalf("expected %q to be rejected", uri)
		}
	}
}
