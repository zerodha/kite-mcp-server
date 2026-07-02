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
		"https://chatgpt.com/connector_platform_oauth_redirect",
	}})

	valid := []string{
		"http://localhost:3000/callback",
		"https://claude.ai/api/mcp/auth_callback",
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
