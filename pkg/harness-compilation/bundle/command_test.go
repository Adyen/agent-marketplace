package bundle

import (
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestLoadCommand(t *testing.T) {
	tests := []struct {
		name       string
		fs         fstest.MapFS
		path       string
		wantName   string
		wantDesc   string
		wantBody   string
		wantExtra  map[string]any
		wantErr    bool
		errContain string
	}{
		{
			name: "basic command with all fields",
			fs: fstest.MapFS{
				"commands/my-cmd.md": {
					Data: []byte("---\nname: my-cmd\ndescription: A command\n---\nCommand body\n"),
				},
			},
			path:     "commands/my-cmd.md",
			wantName: "my-cmd",
			wantDesc: "A command",
			wantBody: "Command body\n",
		},
		{
			name: "name from filename when not in frontmatter",
			fs: fstest.MapFS{
				"commands/auto-name.md": {
					Data: []byte("---\ndescription: Auto named\n---\nBody\n"),
				},
			},
			path:     "commands/auto-name.md",
			wantName: "auto-name",
			wantDesc: "Auto named",
			wantBody: "Body\n",
		},
		{
			name: "extra fields preserved",
			fs: fstest.MapFS{
				"commands/my-cmd.md": {
					Data: []byte("---\nname: my-cmd\ndescription: Cmd\ncustom: val\n---\nBody\n"),
				},
			},
			path:      "commands/my-cmd.md",
			wantName:  "my-cmd",
			wantDesc:  "Cmd",
			wantBody:  "Body\n",
			wantExtra: map[string]any{"custom": "val"},
		},
		{
			name:       "path traversal rejected",
			fs:         fstest.MapFS{},
			path:       "../etc/cmd.md",
			wantErr:    true,
			errContain: "path traversal",
		},
		{
			name: "invalid kebab name uppercase",
			fs: fstest.MapFS{
				"commands/BadCmd.md": {
					Data: []byte("---\nname: BadCmd\n---\nBody\n"),
				},
			},
			path:       "commands/BadCmd.md",
			wantErr:    true,
			errContain: "lowercase letter",
		},
		{
			name:       "missing file",
			fs:         fstest.MapFS{},
			path:       "commands/missing.md",
			wantErr:    true,
			errContain: "open",
		},
		{
			name: "no frontmatter uses whole content as body",
			fs: fstest.MapFS{
				"commands/plain.md": {
					Data: []byte("Plain content\nNo frontmatter\n"),
				},
			},
			path:     "commands/plain.md",
			wantName: "plain",
			wantBody: "Plain content\nNo frontmatter\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := LoadCommand(tt.fs, tt.path)
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
			if got.Name() != tt.wantName {
				t.Errorf("name: got %q, want %q", got.Name(), tt.wantName)
			}
			if got.Description != tt.wantDesc {
				t.Errorf("description: got %q, want %q", got.Description, tt.wantDesc)
			}
			if got.Body != tt.wantBody {
				t.Errorf("body: got %q, want %q", got.Body, tt.wantBody)
			}
			if tt.wantExtra != nil {
				for k, v := range tt.wantExtra {
					gv, ok := got.Extra[k]
					if !ok {
						t.Errorf("extra key %q missing", k)
					} else if gv != v {
						t.Errorf("extra[%q]: got %v, want %v", k, gv, v)
					}
				}
			} else if len(got.Extra) > 0 {
				t.Errorf("extra: got %v, want nil/empty", got.Extra)
			}
		})
	}
}

func TestLoadCommand_SourceRef(t *testing.T) {
	src := fstest.MapFS{
		"commands/my-cmd.md": {
			Data: []byte("---\nname: my-cmd\n---\nBody\n"),
		},
	}
	cmd, err := LoadCommand(src, "commands/my-cmd.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantRef := ir.SourceRef{File: "commands/my-cmd.md", Line: 1}
	if cmd.Source != wantRef {
		t.Errorf("source: got %+v, want %+v", cmd.Source, wantRef)
	}
}
