package harnesscompile_test

import (
	"errors"
	"fmt"
	"io/fs"
	"testing"
	"testing/fstest"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest/cursor"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest/factory"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest/gemini"
)

func TestPublicMCPLoadSingletonAliases(t *testing.T) {
	loaders := []struct {
		name string
		path string
		load func(fs.FS, string) ([]*hcc.McpServer, error)
	}{
		{name: "claude", path: ".mcp.json", load: hcc.LoadMcpServers},
		{name: "factory", path: ".mcp.json", load: factory.LoadMcpServers},
		{name: "cursor", path: ".mcp.json", load: cursor.LoadMcpServers},
		{name: "gemini", path: "settings.json", load: func(src fs.FS, path string) ([]*hcc.McpServer, error) {
			servers, _, err := gemini.LoadMcpServers(src, path)
			return servers, err
		}},
	}
	keys := []struct {
		name        string
		commandKey  string
		argsKey     string
		disabledKey string
	}{
		{name: "canonical", commandKey: "command", argsKey: "args", disabledKey: "disabled"},
		{name: "capitalized", commandKey: "Command", argsKey: "Args", disabledKey: "Disabled"},
	}
	for _, loader := range loaders {
		for _, key := range keys {
			t.Run(loader.name+"/"+key.name, func(t *testing.T) {
				data := fmt.Sprintf(`{
	// preserve comments
	"mcpServers": {
		"first": {"%s":"echo","%s":["one"],"%s":true,"env":{"Foo":"one","foo":"two"},"custom":"keep"},
		"second": {"command":"second"}
	}
}`, key.commandKey, key.argsKey, key.disabledKey)
				path := loader.path
				file := path
				servers, err := loader.load(fstest.MapFS{file: {Data: []byte(data)}}, path)
				if err != nil {
					t.Fatalf("load: %v", err)
				}
				if len(servers) != 2 {
					t.Fatalf("server count = %d, want 2", len(servers))
				}
				byName := make(map[string]*hcc.McpServer, len(servers))
				for _, server := range servers {
					byName[server.Name()] = server
				}
				first, ok := byName["first"]
				if !ok {
					t.Fatal("missing lowercase first server")
				}
				secondServer, ok := byName["second"]
				if !ok {
					t.Fatal("missing second server")
				}
				if secondServer.Command != "second" {
					t.Fatalf("second server command = %q, want second", secondServer.Command)
				}
				if first.Command != "echo" || len(first.Args) != 1 || first.Args[0] != "one" || !first.Disabled || !first.DisabledSet {
					t.Fatalf("typed fields = %#v", first)
				}
				if first.Env["Foo"] != "one" || first.Env["foo"] != "two" {
					t.Fatalf("environment = %#v", first.Env)
				}
				if len(first.Extra) != 1 || first.Extra["custom"] != "keep" {
					t.Fatalf("extra = %#v", first.Extra)
				}
			})
		}
	}
}

func TestPublicMCPLoadRejectsCaseCollidingAliases(t *testing.T) {
	loaders := []struct {
		name string
		path string
		load func(fs.FS, string) ([]*hcc.McpServer, error)
	}{
		{name: "bundle", path: ".mcp.json", load: hcc.LoadMcpServers},
		{name: "gemini", path: "settings.json", load: func(src fs.FS, path string) ([]*hcc.McpServer, error) {
			servers, _, err := gemini.LoadMcpServers(src, path)
			return servers, err
		}},
	}
	cases := []struct {
		name   string
		fields string
	}{
		{name: "command-first", fields: `"command":"safe","Command":"secret"`},
		{name: "command-second", fields: `"Command":"secret","command":"safe"`},
		{name: "args-first", fields: `"args":["safe"],"Args":["secret"]`},
		{name: "args-second", fields: `"Args":["secret"],"args":["safe"]`},
		{name: "disabled-first", fields: `"disabled":false,"Disabled":true`},
		{name: "disabled-second", fields: `"Disabled":true,"disabled":false`},
	}
	for _, loader := range loaders {
		for _, tc := range cases {
			t.Run(loader.name+"/"+tc.name, func(t *testing.T) {
				data := []byte(`{"mcpServers":{"first":{` + tc.fields + `}}}`)
				_, err := loader.load(fstest.MapFS{loader.path: {Data: data}}, loader.path)
				var coded *diag.CodedError
				if !errors.As(err, &coded) || coded.Code != diag.CodeJSONDuplicateKey {
					t.Fatalf("error = %v, want coded duplicate-key error", err)
				}
			})
		}
	}
}
