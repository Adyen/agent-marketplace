package store_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestSanitizeDSNErrorRedactsURLAndKeyValueCredentials(t *testing.T) {
	cause := errors.New(`failed to connect to postgres://marketplace:super-secret@db.example.invalid/marketplace password=super-secret`)
	got := store.SanitizeDSNError(cause, "postgres://marketplace:super-secret@db.example.invalid/marketplace")
	if strings.Contains(got.Error(), "super-secret") {
		t.Fatalf("sanitized error leaked credential: %v", got)
	}
	if !strings.Contains(got.Error(), "redacted") {
		t.Fatalf("sanitized error does not indicate redaction: %v", got)
	}
}

func TestSanitizeDSNErrorRedactsQuotedKeyValuePassword(t *testing.T) {
	cause := errors.New(`dial failed: host=db.example.invalid user=marketplace password="a secret" dbname=marketplace`)
	got := store.SanitizeDSNError(cause, "host=db.example.invalid user=marketplace password=\"a secret\" dbname=marketplace")
	if strings.Contains(got.Error(), "a secret") {
		t.Fatalf("sanitized error leaked quoted credential: %v", got)
	}
}

func TestSanitizeDSNErrorRedactsURLQuerySecrets(t *testing.T) {
	cause := errors.New(`dial failed: postgres://marketplace@db.example.invalid/marketplace?sslmode=require&password=query-secret&token=token-secret&access_token=access-secret`)
	got := store.SanitizeDSNError(cause, "postgres://marketplace@db.example.invalid/marketplace")
	for _, secret := range []string{"query-secret", "token-secret", "access-secret"} {
		if strings.Contains(got.Error(), secret) {
			t.Fatalf("sanitized error leaked URL query secret %q: %v", secret, got)
		}
	}
	if !strings.Contains(got.Error(), "password=[redacted]") || !strings.Contains(got.Error(), "token=[redacted]") || !strings.Contains(got.Error(), "access_token=[redacted]") {
		t.Fatalf("sanitized error did not redact URL query keys: %v", got)
	}
}

func TestSanitizeDSNErrorRedactsEncodedURLQuerySecretKey(t *testing.T) {
	cause := errors.New(`dial failed: postgres://db.example.invalid/marketplace?%70assword=encoded-secret`)
	got := store.SanitizeDSNError(cause, "postgres://db.example.invalid/marketplace")
	if strings.Contains(got.Error(), "encoded-secret") {
		t.Fatalf("sanitized error leaked encoded URL query secret: %v", got)
	}
	if !strings.Contains(got.Error(), "%70assword=[redacted]") {
		t.Fatalf("sanitized error did not redact encoded URL query key: %v", got)
	}
}
