package publicurl

import "testing"

func TestValidateRejectsCredentialAndSignatureQueryKeys(t *testing.T) {
	for _, key := range []string{"oauth_token", "X-Amz-Security-Token", "signature", "password", "credential", "auth", "api_key", "aws_access_key_id"} {
		t.Run(key, func(t *testing.T) {
			if err := Validate("https://example.com/resource?" + key + "=value"); err == nil {
				t.Fatalf("Validate accepted unsafe query key %q", key)
			}
		})
	}
}

func TestValidateRejectsUserinfoFragmentsAndNonHTTPHosts(t *testing.T) {
	for _, raw := range []string{
		"https://user:secret@example.com/resource",
		"https://example.com/resource#fragment",
		"https://example.com/resource#",
		"ftp://example.com/resource",
		"https:///resource",
	} {
		t.Run(raw, func(t *testing.T) {
			if err := Validate(raw); err == nil {
				t.Fatalf("Validate accepted unsafe URL %q", raw)
			}
		})
	}
}

func TestSanitizeRedactsLegacyURLCredentials(t *testing.T) {
	got := Sanitize("https://user:secret@example.com/resource?oauth_token=secret&ref=main#private")
	want := "https://example.com/resource?ref=main"
	if got != want {
		t.Fatalf("Sanitize = %q, want %q", got, want)
	}
	if Redact("git@github.com:private/repo.git") != "[redacted origin]" {
		t.Fatalf("Redact did not fail closed for legacy non-HTTP origin")
	}
}
