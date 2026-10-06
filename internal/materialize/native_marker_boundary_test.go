package materialize

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"golang.org/x/sys/unix"
)

// spec: materialization/native-ownership-marker-refusal-preserves-output
func TestRunNativeOwnershipMarkerRejectsHardLink(t *testing.T) {
	testNativeOwnershipMarkerRefusal(t, false)
}

func TestRunNativeOwnershipMarkerRejectsFIFO(t *testing.T) {
	if os.Getenv("AM_NATIVE_MARKER_FIFO_TEST") != t.Name() {
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^"+regexp.QuoteMeta(t.Name())+"$", "-test.timeout=8s", "-test.v")
		cmd.Env = append(os.Environ(), "AM_NATIVE_MARKER_FIFO_TEST="+t.Name(), "TMPDIR="+t.TempDir())
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("native marker FIFO consumer failed or blocked: %v\n%s", err, output)
		}
		if !bytes.Contains(output, []byte("--- PASS: "+t.Name()+" ")) {
			t.Fatalf("native marker FIFO witness did not run: %s", output)
		}
		return
	}
	testNativeOwnershipMarkerRefusal(t, true)
}

func testNativeOwnershipMarkerRefusal(t *testing.T, fifo bool) {
	home, repo := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	resolved := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "marker-probe", Resource: ir.NewCommand("marker-probe", "native marker", "original inert body", nil, ir.SourceRef{})}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}}}
	initial, err := Run(t.Context(), repo, resolved)
	if err != nil || diag.HasErrors(initial.Diags) {
		t.Fatalf("native marker install control failed: %+v %v", initial, err)
	}
	leaf := filepath.Join(repo, ".factory", "commands", "marker-probe.md")
	before, err := os.ReadFile(leaf)
	if err != nil || !bytes.Contains(before, []byte("original inert body")) || !bytes.Contains(before, []byte("_managed_by: agent-marketplace")) {
		t.Fatalf("native marker control missing: %q %v", before, err)
	}
	resolved[0].Resource = ir.NewCommand("marker-probe", "native marker", "valid updated inert body", nil, ir.SourceRef{})
	updated, err := Run(t.Context(), repo, resolved)
	if err != nil || diag.HasErrors(updated.Diags) {
		t.Fatalf("valid native marker update failed: %+v %v", updated, err)
	}
	before, err = os.ReadFile(leaf)
	if err != nil || !bytes.Contains(before, []byte("valid updated inert body")) {
		t.Fatalf("valid marker update did not occur: %q %v", before, err)
	}
	beforeInfo, err := os.Stat(leaf)
	if err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(t.TempDir(), "retained.md")
	if fifo {
		if err := os.Rename(leaf, foreign); err != nil {
			t.Fatal(err)
		}
		if err := unix.Mkfifo(leaf, 0o600); err != nil {
			t.Fatal(err)
		}
	} else if err := os.Link(leaf, foreign); err != nil {
		t.Fatal(err)
	}
	resolved[0].Resource = ir.NewCommand("marker-probe", "native marker", "changed inert body", nil, ir.SourceRef{})
	report, err := Run(t.Context(), repo, resolved)
	if err != nil || !hasDiagCode(report.Diags, "materialize.collision.user-owned") {
		t.Fatalf("multiply linked marker still proved ownership: %+v %v", report, err)
	}
	paths := []string{leaf, foreign}
	if fifo {
		paths = []string{foreign}
		if info, err := os.Lstat(leaf); err != nil || info.Mode()&os.ModeNamedPipe == 0 {
			t.Fatalf("marker FIFO object changed: %v", err)
		}
	}
	for _, path := range paths {
		if after, err := os.ReadFile(path); err != nil || !bytes.Equal(after, before) {
			t.Fatalf("marker refusal changed %s: %q %v", path, after, err)
		}
		if after, err := os.Stat(path); err != nil || !os.SameFile(beforeInfo, after) || beforeInfo.Mode() != after.Mode() || !beforeInfo.ModTime().Equal(after.ModTime()) {
			t.Fatalf("marker refusal changed %s metadata: %v", path, err)
		}
	}
}
