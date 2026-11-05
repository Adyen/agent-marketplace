package materialize

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// spec: materialization/check-detects-changed-linked-resource-content
func TestRunCheckDetectsLinkedResourceContent(t *testing.T) {
	for _, target := range []hcc.Target{hcc.TargetClaude, hcc.TargetFactory, hcc.TargetCursor, hcc.TargetGemini, hcc.TargetWindsurf, hcc.TargetDevin, hcc.TargetCodex, hcc.TargetOpenCode} {
		for _, kind := range []ir.ResourceKind{ir.KindSkill, ir.KindCommand, ir.KindSubagent} {
			if target == hcc.TargetWindsurf && kind == ir.KindSubagent {
				continue
			}
			for _, source := range []config.Source{config.RepoCommitted, config.UserPersonal} {
				t.Run(string(target)+"/"+kind.String()+"/"+source.String(), func(t *testing.T) {
					home, repo := t.TempDir(), t.TempDir()
					t.Setenv("HOME", home)
					t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
					t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
					t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
					t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
					resource := func(body string) ir.Resource {
						switch kind {
						case ir.KindCommand:
							return ir.NewCommand("check", "Check content", body, nil, ir.SourceRef{})
						case ir.KindSubagent:
							return ir.NewSubagent("check", "Check content", body, "", nil, "", nil, "", nil, ir.SourceRef{})
						default:
							return ir.NewSkill("check", "Check content", body, nil, nil, ir.SourceRef{})
						}
					}
					resolved := []resolver.ResolvedArtifact{{
						Entry:  catalog.Entry{Kind: kind, Name: "check", Resource: resource("original body")},
						Source: source, Harnesses: []hcc.Target{target},
					}}
					installed, err := Run(t.Context(), repo, resolved)
					if err != nil || diag.HasErrors(installed.Diags) || installed.TotalWritten() == 0 {
						t.Fatalf("install control: %+v %v", installed, err)
					}
					var output string
					for _, root := range installed.TargetReports() {
						for _, written := range root.Written {
							if filepath.Base(written) == "check" || filepath.Base(written) == "check.md" || filepath.Base(written) == "check.toml" {
								output = filepath.Join(root.OutDir, filepath.FromSlash(written))
							}
						}
					}
					info, err := os.Lstat(output)
					if err != nil {
						t.Fatal(err)
					}
					// Factory commands deliberately use regular files in symlink mode.
					if target == hcc.TargetFactory && kind == ir.KindCommand {
						if info.Mode()&os.ModeSymlink != 0 {
							t.Fatal("Factory command must remain a regular file")
						}
						return
					}
					if info.Mode()&os.ModeSymlink == 0 {
						t.Fatalf("expected linked native resource at %s", output)
					}
					cacheTarget, err := os.Readlink(output)
					if err != nil {
						t.Fatal(err)
					}
					payload := cacheTarget
					if stat, err := os.Stat(payload); err == nil && stat.IsDir() {
						payload = filepath.Join(payload, "SKILL.md")
					}
					before, mode, mtime := fileSnapshot(t, payload)
					healthy, err := Run(t.Context(), repo, resolved, WithDryRun())
					if err != nil || !healthy.InSync() {
						t.Fatalf("unchanged control: %+v %v", healthy, err)
					}
					resolved[0].Resource = resource("updated body")
					pending, err := Run(t.Context(), repo, resolved, WithDryRun())
					if err != nil || diag.HasErrors(pending.Diags) || pending.InSync() || pending.TotalWritten() != 1 {
						t.Fatalf("changed linked content must be pending: %+v %v", pending, err)
					}
					nativePathReported := false
					for _, root := range pending.TargetReports() {
						for _, written := range root.Written {
							nativePathReported = nativePathReported || filepath.Join(root.OutDir, filepath.FromSlash(written)) == output
						}
					}
					if !nativePathReported {
						t.Fatalf("pending report does not name affected native path %s: %+v", output, pending)
					}
					assertFileUnchanged(t, payload, before, mode, mtime)
					assertCommandPruneLinkSnapshot(t, output, cacheTarget, info)
					synced, err := Run(t.Context(), repo, resolved)
					if err != nil || diag.HasErrors(synced.Diags) {
						t.Fatalf("sync: %+v %v", synced, err)
					}
					if data, err := os.ReadFile(payload); err != nil || !bytes.Contains(data, []byte("updated body")) {
						t.Fatalf("real sync did not update cached content: %q %v", data, err)
					}
					assertCommandPruneLinkSnapshot(t, output, cacheTarget, info)
					repeat, err := Run(t.Context(), repo, resolved, WithDryRun())
					if err != nil || !repeat.InSync() {
						t.Fatalf("repaired control: %+v %v", repeat, err)
					}
				})
			}
		}
	}
}

// spec: materialization/check-detects-linked-support-file-drift
func TestRunCheckDetectsLinkedSupportChanges(t *testing.T) {
	for _, change := range []string{"edit", "add", "remove", "mode", "missing-cache"} {
		t.Run(change, func(t *testing.T) {
			home, repo, source := t.TempDir(), t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
			t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
			support := filepath.Join(source, "reference.txt")
			if err := os.WriteFile(support, []byte("original support"), 0o644); err != nil {
				t.Fatal(err)
			}
			resolved := []resolver.ResolvedArtifact{{
				Entry:  catalog.Entry{Kind: ir.KindSkill, Name: "check", Path: source, Resource: ir.NewSkill("check", "Support check", "body", nil, nil, ir.SourceRef{})},
				Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetClaude},
			}}
			if report, err := Run(t.Context(), repo, resolved); err != nil || diag.HasErrors(report.Diags) {
				t.Fatalf("install: %+v %v", report, err)
			}
			output := filepath.Join(repo, ".claude", "skills", "check")
			cacheTarget, err := os.Readlink(output)
			if err != nil {
				t.Fatal(err)
			}
			linkInfo, err := os.Lstat(output)
			if err != nil {
				t.Fatal(err)
			}
			before, mode, mtime := fileSnapshot(t, filepath.Join(cacheTarget, "reference.txt"))
			switch change {
			case "edit":
				err = os.WriteFile(support, []byte("updated support"), 0o644)
			case "add":
				err = os.WriteFile(filepath.Join(source, "added.txt"), []byte("added support"), 0o644)
			case "remove":
				err = os.Remove(support)
			case "mode":
				err = os.Chmod(support, 0o755)
			case "missing-cache":
				err = os.Rename(cacheTarget, filepath.Join(t.TempDir(), "retained-cache"))
			}
			if err != nil {
				t.Fatal(err)
			}
			report, err := Run(t.Context(), repo, resolved, WithDryRun())
			if err != nil || diag.HasErrors(report.Diags) || report.InSync() || report.TotalWritten() != 1 {
				t.Fatalf("support drift must be pending: %+v %v", report, err)
			}
			assertCommandPruneLinkSnapshot(t, output, cacheTarget, linkInfo)
			if change == "missing-cache" {
				if _, err := os.Lstat(cacheTarget); !os.IsNotExist(err) {
					t.Fatalf("check recreated missing cache: %v", err)
				}
			} else {
				assertFileUnchanged(t, filepath.Join(cacheTarget, "reference.txt"), before, mode, mtime)
				if _, err := os.Lstat(filepath.Join(cacheTarget, "added.txt")); !os.IsNotExist(err) {
					t.Fatalf("check copied new support: %v", err)
				}
			}
			if report, err := Run(t.Context(), repo, resolved); err != nil || diag.HasErrors(report.Diags) {
				t.Fatalf("real sync: %+v %v", report, err)
			}
			if change == "edit" {
				if data, err := os.ReadFile(filepath.Join(output, "reference.txt")); err != nil || string(data) != "updated support" {
					t.Fatalf("support was not updated: %q %v", data, err)
				}
			}
			if report, err := Run(t.Context(), repo, resolved, WithDryRun()); err != nil || !report.InSync() {
				t.Fatalf("repaired control: %+v %v", report, err)
			}
		})
	}
}
