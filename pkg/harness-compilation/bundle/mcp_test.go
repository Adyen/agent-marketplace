package bundle

import (
	"encoding/json"
	"errors"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestLoadMcpServersRejectsDuplicateNestedKeys(t *testing.T) {
	fs := fstest.MapFS{".mcp.json": {Data: []byte(`{"mcpServers":{"my-server":{"env":{"TOKEN":"first","TOKEN":"second"}}}}`)}}
	if _, err := LoadMcpServers(fs, ".mcp.json"); err == nil {
		t.Fatal("LoadMcpServers accepted duplicate nested JSON key")
	}
}

func TestLoadMcpServersRejectsCaseCollidingKnownAliases(t *testing.T) {
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
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := []byte(`{"mcpServers":{"my-server":{` + tc.fields + `}}}`)
			_, err := LoadMcpServers(fstest.MapFS{".mcp.json": {Data: data}}, ".mcp.json")
			var coded *diag.CodedError
			if !errors.As(err, &coded) || coded.Code != diag.CodeJSONDuplicateKey {
				t.Fatalf("error = %v, want coded duplicate-key error", err)
			}
		})
	}
}

func TestLoadMcpServers(t *testing.T) {
	tests := []struct {
		name        string
		fs          fstest.MapFS
		path        string
		wantCount   int
		wantNames   []string
		wantCommand map[string]string
		wantArgs    map[string][]string
		wantEnv     map[string]map[string]string
		wantExtra   map[string]map[string]any
		wantErr     bool
		errContain  string
	}{
		{
			name: "single MCP server",
			fs: fstest.MapFS{
				".mcp.json": {
					Data: []byte(`{"mcpServers":{"my-server":{"command":"npx","args":["-y","@my/mcp-server"],"env":{"API_KEY":"xxx"}}}}`),
				},
			},
			path:        ".mcp.json",
			wantCount:   1,
			wantNames:   []string{"my-server"},
			wantCommand: map[string]string{"my-server": "npx"},
			wantArgs:    map[string][]string{"my-server": {"-y", "@my/mcp-server"}},
			wantEnv:     map[string]map[string]string{"my-server": {"API_KEY": "xxx"}},
		},
		{
			name: "multiple MCP servers",
			fs: fstest.MapFS{
				".mcp.json": {
					Data: []byte(`{"mcpServers":{"server-a":{"command":"node","args":["a.js"]},"server-b":{"command":"python","args":["b.py"]}}}`),
				},
			},
			path:        ".mcp.json",
			wantCount:   2,
			wantNames:   []string{"server-a", "server-b"},
			wantCommand: map[string]string{"server-a": "node", "server-b": "python"},
		},
		{
			name: "extra fields preserved",
			fs: fstest.MapFS{
				".mcp.json": {
					Data: []byte(`{"mcpServers":{"my-server":{"command":"npx","args":[],"customField":"hello","enabled":true}}}`),
				},
			},
			path:      ".mcp.json",
			wantCount: 1,
			wantExtra: map[string]map[string]any{
				"my-server": {"customField": "hello", "enabled": true},
			},
		},
		{
			name: "server without args or env",
			fs: fstest.MapFS{
				".mcp.json": {
					Data: []byte(`{"mcpServers":{"simple":{"command":"echo"}}}`),
				},
			},
			path:        ".mcp.json",
			wantCount:   1,
			wantNames:   []string{"simple"},
			wantCommand: map[string]string{"simple": "echo"},
		},
		{
			name: "empty mcpServers",
			fs: fstest.MapFS{
				".mcp.json": {
					Data: []byte(`{"mcpServers":{}}`),
				},
			},
			path:      ".mcp.json",
			wantCount: 0,
		},
		{
			name: "null mcpServers",
			fs: fstest.MapFS{
				".mcp.json": {
					Data: []byte(`{"mcpServers":null}`),
				},
			},
			path:       ".mcp.json",
			wantErr:    true,
			errContain: "null",
		},
		{
			name:       "path traversal rejected",
			fs:         fstest.MapFS{},
			path:       "../.mcp.json",
			wantErr:    true,
			errContain: "path traversal",
		},
		{
			name: "invalid JSON",
			fs: fstest.MapFS{
				".mcp.json": {
					Data: []byte(`{bad json}`),
				},
			},
			path:       ".mcp.json",
			wantErr:    true,
			errContain: "parse",
		},
		{
			name:       "missing file",
			fs:         fstest.MapFS{},
			path:       ".mcp.json",
			wantErr:    true,
			errContain: "read",
		},
		{
			name: "invalid kebab name for server uppercase",
			fs: fstest.MapFS{
				".mcp.json": {
					Data: []byte(`{"mcpServers":{"BadServer":{"command":"echo"}}}`),
				},
			},
			path:       ".mcp.json",
			wantErr:    true,
			errContain: "lowercase letter",
		},
		{
			name: "invalid kebab name for server with underscores",
			fs: fstest.MapFS{
				".mcp.json": {
					Data: []byte(`{"mcpServers":{"bad_server":{"command":"echo"}}}`),
				},
			},
			path:       ".mcp.json",
			wantErr:    true,
			errContain: "kebab-case",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			servers, err := LoadMcpServers(tt.fs, tt.path)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.errContain)
				}
				if !containsString(err.Error(), tt.errContain) {
					t.Fatalf("expected error containing %q, got %q", tt.errContain, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(servers) != tt.wantCount {
				t.Fatalf("server count: got %d, want %d", len(servers), tt.wantCount)
			}
			if tt.wantNames != nil {
				found := make(map[string]bool)
				for _, s := range servers {
					found[s.Name()] = true
				}
				for _, n := range tt.wantNames {
					if !found[n] {
						t.Errorf("missing server %q", n)
					}
				}
			}
			for _, s := range servers {
				n := s.Name()
				if want, ok := tt.wantCommand[n]; ok && s.Command != want {
					t.Errorf("server %q command: got %q, want %q", n, s.Command, want)
				}
				if want, ok := tt.wantArgs[n]; ok {
					if len(s.Args) != len(want) {
						t.Errorf("server %q args: got %v, want %v", n, s.Args, want)
					} else {
						for i, v := range want {
							if s.Args[i] != v {
								t.Errorf("server %q args[%d]: got %q, want %q", n, i, s.Args[i], v)
							}
						}
					}
				}
				if want, ok := tt.wantEnv[n]; ok {
					if len(s.Env) != len(want) {
						t.Errorf("server %q env: got %v, want %v", n, s.Env, want)
					} else {
						for k, v := range want {
							if s.Env[k] != v {
								t.Errorf("server %q env[%q]: got %q, want %q", n, k, s.Env[k], v)
							}
						}
					}
				}
				if want, ok := tt.wantExtra[n]; ok {
					if s.Extra == nil {
						t.Errorf("server %q extra: got nil, want %v", n, want)
					} else {
						for k, v := range want {
							got, exists := s.Extra[k]
							if !exists {
								t.Errorf("server %q extra key %q missing", n, k)
							} else if got != v {
								t.Errorf("server %q extra[%q]: got %v (%T), want %v (%T)", n, k, got, got, v, v)
							}
						}
					}
				}
			}
		})
	}
}

func TestLoadMcpServers_SourceRef(t *testing.T) {
	src := fstest.MapFS{
		".mcp.json": {
			Data: []byte(`{"mcpServers":{"my-server":{"command":"echo"}}}`),
		},
	}
	servers, err := LoadMcpServers(src, ".mcp.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(servers))
	}
	wantRef := ir.SourceRef{File: ".mcp.json", Line: 0}
	if servers[0].Source != wantRef {
		t.Errorf("source: got %+v, want %+v", servers[0].Source, wantRef)
	}
}

func TestMcpServerEntryUnmarshal(t *testing.T) {
	input := `{"command":"npx","args":["-y","pkg"],"env":{"KEY":"val"},"url":"https://example.com","timeout":30}`
	var e mcpServerEntry
	if err := json.Unmarshal([]byte(input), &e); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e.Command != "npx" {
		t.Errorf("command: got %q, want %q", e.Command, "npx")
	}
	if len(e.Args) != 2 || e.Args[0] != "-y" || e.Args[1] != "pkg" {
		t.Errorf("args: got %v", e.Args)
	}
	if e.Env["KEY"] != "val" {
		t.Errorf("env: got %v", e.Env)
	}
	if e.URL != "https://example.com" {
		t.Errorf("url: got %q, want %q", e.URL, "https://example.com")
	}
	if e.DisabledSet {
		t.Error("disabled set unexpectedly true")
	}
	if v, ok := e.Extra["timeout"]; !ok {
		t.Error("extra[timeout]: missing")
	} else if f, ok := v.(float64); !ok || f != 30 {
		t.Errorf("extra[timeout]: got %v (%T), want 30", v, v)
	}
}

func TestMcpServerEntryUnmarshalResetsState(t *testing.T) {
	var entry mcpServerEntry
	if err := json.Unmarshal([]byte(`{"command":"echo","disabled":true,"custom":"keep"}`), &entry); err != nil {
		t.Fatal(err)
	}
	if !entry.DisabledSet || entry.Extra["custom"] != "keep" {
		t.Fatalf("initial state: disabledSet=%v extra=%v", entry.DisabledSet, entry.Extra)
	}
	if err := json.Unmarshal([]byte(`{"command":"echo"}`), &entry); err != nil {
		t.Fatal(err)
	}
	if entry.DisabledSet || entry.Extra != nil {
		t.Fatalf("reused state: disabledSet=%v extra=%v", entry.DisabledSet, entry.Extra)
	}
}

func TestLoadMcpServers_CommentStripping(t *testing.T) {
	src := fstest.MapFS{
		"mcp.json": {
			Data: []byte(`{
				"mcpServers": {
					"my-server": {
						"command": "npx",
						"args": ["-y", "pkg"],
						"env": {
							// "COMMENTED_KEY": "should be ignored"
							"API_KEY": "xxx"
						}
					}
				}
			}`),
		},
	}
	servers, err := LoadMcpServers(src, "mcp.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(servers))
	}
	if servers[0].Command != "npx" {
		t.Errorf("command: got %q, want %q", servers[0].Command, "npx")
	}
	if servers[0].Env["API_KEY"] != "xxx" {
		t.Errorf("env API_KEY: got %q, want %q", servers[0].Env["API_KEY"], "xxx")
	}
	// The commented-out key should not appear.
	if _, ok := servers[0].Env["COMMENTED_KEY"]; ok {
		t.Error("COMMENTED_KEY should have been stripped by comment removal")
	}
}
