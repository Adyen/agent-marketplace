package windsurf

import (
	"slices"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestLoadHooksAliasDistinctNames(t *testing.T) {
	src := fstest.MapFS{"hooks.json": {Data: []byte(`{"hooks":{"pre_run_command":[{"command":"echo first"},{"command":"echo second"}]}}`)}}
	hooks, _, err := LoadHooks(src, "hooks.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(hooks) != 2 {
		t.Fatalf("hooks = %d, want 2", len(hooks))
	}
	if hooks[0].Name() != "hooks-2" || hooks[1].Name() != "hooks" {
		t.Fatalf("names = %q, %q, want hooks-2 then hooks", hooks[0].Name(), hooks[1].Name())
	}
}

func TestLoadWorkspaceHookSourcePreference(t *testing.T) {
	tests := []struct {
		name    string
		options LoadOptions
		want    string
		wantErr bool
		src     fstest.MapFS
	}{
		{
			name: "native preferred",
			src: fstest.MapFS{
				".windsurf/hooks.json":        {Data: []byte(`{"hooks":{"pre_run_command":[{"command":"echo native"}]}}`)},
				".windsurf/hooks/legacy.json": {Data: []byte(`{"command":"echo legacy","event":"pre_run_command"}`)},
			},
			want: "echo native",
		},
		{
			name:    "legacy mode directory preferred",
			options: LoadOptions{LegacyHookInventory: true},
			src: fstest.MapFS{
				".windsurf/hooks.json":        {Data: []byte(`{"hooks":{"pre_run_command":[{"command":"echo native"}]}}`)},
				".windsurf/hooks/legacy.json": {Data: []byte(`{"command":"echo legacy","event":"pre_run_command"}`)},
			},
			want: "echo legacy",
		},
		{
			name: "malformed native does not fallback",
			src: fstest.MapFS{
				".windsurf/hooks.json":        {Data: []byte(`{"hooks":`)},
				".windsurf/hooks/legacy.json": {Data: []byte(`{"command":"echo legacy","event":"pre_run_command"}`)},
			},
			wantErr: true,
		},
		{
			name: "empty native is authoritative",
			src: fstest.MapFS{
				".windsurf/hooks.json":        {Data: []byte(`{"hooks":{}}`)},
				".windsurf/hooks/legacy.json": {Data: []byte(`{"command":"echo legacy","event":"pre_run_command"}`)},
			},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bundle, err := (Ingester{}).LoadBundleWithOptions(tt.src, tt.options)
			if tt.wantErr {
				if err == nil {
					t.Fatal("LoadBundle unexpectedly succeeded")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var commands []string
			for _, resource := range bundle.Resources {
				if hook, ok := resource.(*ir.Hook); ok {
					commands = append(commands, hook.Command)
				}
			}
			if tt.want == "" {
				if len(commands) != 0 {
					t.Fatalf("hook commands = %v, want none", commands)
				}
				return
			}
			if len(commands) != 1 || commands[0] != tt.want {
				t.Fatalf("hook commands = %v, want [%q]", commands, tt.want)
			}
		})
	}
}

func TestLoadWorkspaceCurrentCombinesUserAndProjectHooks(t *testing.T) {
	project := fstest.MapFS{".windsurf/hooks.json": {Data: []byte(`{"hooks":{"pre_run_command":[{"command":"echo project"}]}}`)}}
	user := fstest.MapFS{"hooks.json": {Data: []byte(`{"hooks":{"pre_run_command":[{"command":"echo user"}]}}`)}}
	bundle, err := LoadWorkspace(project, LoadOptions{User: user})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, resource := range bundle.Resources {
		if hook, ok := resource.(*ir.Hook); ok {
			got = append(got, hook.Name()+":"+hook.Command)
		}
	}
	want := []string{"hooks:echo project", "hooks-2:echo user"}
	if !slices.Equal(got, want) {
		t.Fatalf("hooks = %v, want %v", got, want)
	}
}

func TestDistinctHookNamesAvoidsExistingSuffix(t *testing.T) {
	hooks := []*ir.Hook{
		ir.NewHook("hooks", "pre_run_command", "echo first", "command", nil, ir.SourceRef{}),
		ir.NewHook("hooks-2", "pre_run_command", "echo suffix", "command", nil, ir.SourceRef{}),
		ir.NewHook("hooks", "pre_run_command", "echo second", "command", nil, ir.SourceRef{}),
	}
	distinctHookNames(hooks)
	if hooks[0].Name() != "hooks-3" || hooks[1].Name() != "hooks-2" || hooks[2].Name() != "hooks" {
		t.Fatalf("names = %q, %q, %q", hooks[0].Name(), hooks[1].Name(), hooks[2].Name())
	}
}
