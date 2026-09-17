package harnesscompile_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestPrivateMCPCompileModes(t *testing.T) {
	server := ir.NewMcpServer("private", "echo", []string{"private"}, map[string]string{"TOKEN": "dummy"}, nil, ir.SourceRef{})
	for _, definition := range hcc.TargetDefinitions() {
		if !definition.Compiler {
			continue
		}
		t.Run(string(definition.Target), func(t *testing.T) {
			result, err := hcc.Compile(server, hcc.Target(definition.Target), hcc.WithManagedBy("_managed_by", "agent-marketplace"))
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			if len(result.Artifacts) == 0 {
				t.Fatalf("artifacts = 0, diagnostics = %#v", result.Diagnostics)
			}
			for _, artifact := range result.Artifacts {
				if artifact.Mode != 0o600 {
					t.Fatalf("artifact %q mode = %o, want 600", artifact.Path, artifact.Mode)
				}
			}
		})
	}
}

func TestOrdinaryCompileModesRemainUnspecified(t *testing.T) {
	for _, target := range []hcc.Target{hcc.TargetClaude, hcc.TargetFactory, hcc.TargetCursor, hcc.TargetWindsurf, hcc.TargetGemini, hcc.TargetDevin, hcc.TargetCodex} {
		t.Run(string(target), func(t *testing.T) {
			for _, resource := range []hcc.Resource{
				ir.NewSkill("ordinary", "ordinary", "body", nil, nil, ir.SourceRef{}),
				ir.NewCommand("ordinary-command", "ordinary", "body", nil, ir.SourceRef{}),
			} {
				result, err := hcc.Compile(resource, target)
				if err != nil {
					t.Fatalf("compile %s: %v", resource.Name(), err)
				}
				for _, artifact := range result.Artifacts {
					if artifact.Mode != 0 {
						t.Fatalf("%s artifact %q mode = %o, want unspecified", resource.Name(), artifact.Path, artifact.Mode)
					}
				}
			}
		})
	}
}

func TestPrivateMCPFragmentMergeRetainsMode(t *testing.T) {
	first := ir.NewMcpServer("first", "echo", []string{"first"}, nil, nil, ir.SourceRef{})
	second := ir.NewMcpServer("second", "echo", []string{"second"}, nil, nil, ir.SourceRef{})
	one, err := hcc.Compile(first, hcc.TargetClaude, hcc.WithManagedBy("_managed_by", "agent-marketplace"))
	if err != nil {
		t.Fatal(err)
	}
	two, err := hcc.Compile(second, hcc.TargetClaude, hcc.WithManagedBy("_managed_by", "agent-marketplace"))
	if err != nil {
		t.Fatal(err)
	}
	result := &hcc.Result{Artifacts: append(one.Artifacts, two.Artifacts...)}
	outDir := t.TempDir()
	if _, err := hcc.Materialize(result, outDir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(outDir, ".mcp.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("merged mode = %o, want 600", info.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"first"`)) || !bytes.Contains(data, []byte(`"second"`)) {
		t.Fatalf("merged MCP data = %s", data)
	}
}

func TestMaterializePrivateMCPModes(t *testing.T) {
	server := ir.NewMcpServer("private", "echo", []string{"private"}, map[string]string{"TOKEN": "dummy"}, nil, ir.SourceRef{})
	result, err := hcc.Compile(server, hcc.TargetClaude, hcc.WithManagedBy("_managed_by", "agent-marketplace"))
	if err != nil {
		t.Fatal(err)
	}

	t.Run("fresh", func(t *testing.T) {
		outDir := t.TempDir()
		if _, err := hcc.Materialize(result, outDir); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(filepath.Join(outDir, ".mcp.json"))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("fresh mode = %o, want 600", info.Mode().Perm())
		}
	})

	for _, mode := range []os.FileMode{0o600, 0o640} {
		t.Run(modeString(mode), func(t *testing.T) {
			outDir := t.TempDir()
			path := filepath.Join(outDir, ".mcp.json")
			if err := os.WriteFile(path, []byte(`{"mcpServers":{"handwritten":{"command":"manual"}}}`), mode); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, mode); err != nil {
				t.Fatal(err)
			}
			before, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := hcc.Materialize(result, outDir, hcc.WithWriteMode(hcc.WriteModeOverwrite)); err != nil {
				t.Fatal(err)
			}
			after, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if after.Mode().Perm() != mode.Perm() {
				t.Fatalf("mode = %o, want %o", after.Mode().Perm(), mode.Perm())
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(data, []byte(`"handwritten"`)) || !bytes.Contains(data, []byte(`"private"`)) {
				t.Fatalf("merged data = %s", data)
			}
			if mode == 0o600 {
				before = after
				if _, err := hcc.Materialize(result, outDir, hcc.WithWriteMode(hcc.WriteModeOverwrite)); err != nil {
					t.Fatal(err)
				}
				repeat, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				if !before.ModTime().Equal(repeat.ModTime()) {
					t.Fatalf("same bytes rewrote file: before=%v after=%v", before.ModTime(), repeat.ModTime())
				}
			}
		})
	}
}

func TestMaterializePrivateAggregateUmaskChild(t *testing.T) {
	if os.Getenv("PRIVATE_MATERIALIZE_UMASK_CHILD") == "1" {
		old := syscall.Umask(0o077)
		defer syscall.Umask(old)
		server := ir.NewMcpServer("private", "echo", []string{"updated"}, nil, nil, ir.SourceRef{})
		result, err := hcc.Compile(server, hcc.TargetClaude, hcc.WithManagedBy("_managed_by", "agent-marketplace"))
		if err != nil {
			t.Fatal(err)
		}
		outDir := t.TempDir()
		path := filepath.Join(outDir, ".mcp.json")
		if err := os.WriteFile(path, []byte(`{"mcpServers":{"handwritten":{"command":"manual"}}}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o640); err != nil {
			t.Fatal(err)
		}
		if _, err := hcc.Materialize(result, outDir, hcc.WithWriteMode(hcc.WriteModeOverwrite)); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o640 {
			t.Fatalf("mode = %v, %v", info, err)
		}
		data, err := os.ReadFile(path)
		if err != nil || !bytes.Contains(data, []byte(`"handwritten"`)) || !bytes.Contains(data, []byte(`"updated"`)) {
			t.Fatalf("merged data = %s, err=%v", data, err)
		}
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMaterializePrivateAggregateUmaskChild$", "-test.v")
	cmd.Env = append(os.Environ(), "PRIVATE_MATERIALIZE_UMASK_CHILD=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child: %v\n%s", err, output)
	}
}

func TestMaterializePlainOverwritePreservesPrivateMode(t *testing.T) {
	outDir := t.TempDir()
	path := filepath.Join(outDir, "plain.json")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	result := &hcc.Result{Artifacts: []hcc.Artifact{{Path: "plain.json", Bytes: []byte("new"), Mode: 0o600}}}
	if _, err := hcc.Materialize(result, outDir, hcc.WithWriteMode(hcc.WriteModeOverwrite)); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 600", info.Mode().Perm())
	}
}

func TestMaterializeFreshDevinMCPIsPrivate(t *testing.T) {
	server := ir.NewMcpServer("private", "echo", []string{"private"}, nil, nil, ir.SourceRef{})
	result, err := hcc.Compile(server, hcc.TargetDevin)
	if err != nil {
		t.Fatal(err)
	}
	outDir := t.TempDir()
	if _, err := hcc.Materialize(result, outDir); err != nil {
		t.Fatal(err)
	}
	if len(result.Artifacts) != 1 {
		t.Fatalf("artifacts = %d", len(result.Artifacts))
	}
	info, err := os.Stat(filepath.Join(outDir, filepath.FromSlash(result.Artifacts[0].Path)))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 600", info.Mode().Perm())
	}
}

func modeString(mode os.FileMode) string {
	return mode.Perm().String()
}

func BenchmarkMaterializePrivateAggregateNoop(b *testing.B) {
	server := ir.NewMcpServer("benchmark", "echo", []string{"private"}, map[string]string{"TOKEN": "dummy"}, nil, ir.SourceRef{})
	result, err := hcc.Compile(server, hcc.TargetClaude, hcc.WithManagedBy("_managed_by", "agent-marketplace"))
	if err != nil {
		b.Fatal(err)
	}
	outDir := b.TempDir()
	if _, err := hcc.Materialize(result, outDir, hcc.WithWriteMode(hcc.WriteModeOverwrite)); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := hcc.Materialize(result, outDir, hcc.WithWriteMode(hcc.WriteModeOverwrite)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCompilePrivateMCP(b *testing.B) {
	server := ir.NewMcpServer("benchmark", "echo", []string{"private"}, map[string]string{"TOKEN": "dummy"}, nil, ir.SourceRef{})
	b.ReportAllocs()
	for b.Loop() {
		result, err := hcc.Compile(server, hcc.TargetClaude)
		if err != nil || len(result.Artifacts) == 0 {
			b.Fatalf("compile result: %v", err)
		}
	}
}
