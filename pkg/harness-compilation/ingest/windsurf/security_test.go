package windsurf

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestLoadMcpServersDiagnosticsDoNotExposeSensitiveValues(t *testing.T) {
	secret := "do_not_log_this_secret"
	src := fstest.MapFS{"mcp.json": {Data: []byte(`{"mcpServers":{"remote":{"command":"npx","env":{"TOKEN":"${env:` + secret + `}"}}}}`)}}
	_, diags, err := LoadMcpServers(src, "mcp.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) == 0 {
		t.Fatal("diagnostics = 0, want at least one so the redaction case is not vacuous")
	}
	for _, d := range diags {
		if strings.Contains(d.Message, secret) || strings.Contains(d.Hint, secret) {
			t.Fatalf("diagnostic leaked secret: %+v", d)
		}
	}
}
