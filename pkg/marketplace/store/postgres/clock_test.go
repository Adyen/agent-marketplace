package postgres

import (
	"strings"
	"testing"
)

func TestDatabaseExpiryClockExpressionIsServerAuthoritative(t *testing.T) {
	if !strings.Contains(dbNowMicro, "CURRENT_TIMESTAMP") {
		t.Fatalf("database expiry clock = %q, want PostgreSQL CURRENT_TIMESTAMP", dbNowMicro)
	}
	if strings.Contains(dbNowMicro, "time.Now") {
		t.Fatalf("database expiry clock unexpectedly depends on client time: %q", dbNowMicro)
	}
}
