package session

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func localSkillFilesTestRow(root, sourcePath string, supportFiles ...string) Row {
	resource := ir.NewSkill("review", "Review code", "# Local review\n", nil, nil, ir.SourceRef{File: sourcePath})
	resource.SupportFiles = supportFiles
	return Row{Artifact: resolver.ResolvedArtifact{
		Entry: catalog.Entry{
			Kind:              ir.KindSkill,
			Name:              resource.Name(),
			Resource:          resource,
			SourceRoot:        root,
			PrimarySourcePath: sourcePath,
		},
	}}
}

func localSkillFilesTestPath(t *testing.T, root, relative string, contents []byte) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, contents, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLocalSkillSourceReadLimit(t *testing.T) {
	root := t.TempDir()
	body := []byte("bounded source bytes")
	localSkillFilesTestPath(t, root, "payload.txt", body)
	source, err := fsboundary.OpenSourceFS(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = source.Close() }()

	data, identity, err := source.ReadFileWithIdentity("payload.txt", int64(len(body)))
	if err != nil || string(data) != string(body) || identity.Size != int64(len(body)) {
		t.Fatalf("bounded source read = %q identity %d err=%v", data, identity.Size, err)
	}
	data, _, err = source.ReadFileWithIdentity("payload.txt", int64(len(body)-1))
	if !errors.Is(err, fsboundary.ErrTooLarge) || data != nil {
		t.Fatalf("oversized source read = %q err=%v, want ErrTooLarge and no bytes", data, err)
	}
}

func TestLocalSkillFilesRequiresCapturedLocalSkillSource(t *testing.T) {
	root := t.TempDir()
	primary := localSkillFilesTestPath(t, root, "review/SKILL.md", []byte("# Local review\n"))
	row := localSkillFilesTestRow(root, primary, "notes.md")
	if !LocalSkillFiles(row) {
		t.Fatal("local skill with captured source root did not expose file inspection")
	}

	marketplace := row
	marketplace.Artifact.IsMarketplace = true
	if LocalSkillFiles(marketplace) {
		t.Fatal("Marketplace row exposed local file inspection")
	}
	withoutRoot := row
	withoutRoot.Artifact.SourceRoot = ""
	if LocalSkillFiles(withoutRoot) {
		t.Fatal("local row without a source root exposed file inspection")
	}
	nilSkill := row
	nilSkill.Artifact.Resource = (*ir.Skill)(nil)
	if LocalSkillFiles(nilSkill) {
		t.Fatal("nil skill pointer exposed local file inspection")
	}
}

func TestOpenLocalSkillBundleReadsPrimaryAndContainedSupportLink(t *testing.T) {
	root := t.TempDir()
	localSkillFilesTestPath(t, root, "skills/review/SKILL.md", []byte("# Local review\n"))
	localSkillFilesTestPath(t, root, "shared/notes.md", []byte("# Shared notes\n"))
	if err := os.Symlink("../../shared/notes.md", filepath.Join(root, "skills", "review", "notes.md")); err != nil {
		t.Fatal(err)
	}
	row := localSkillFilesTestRow(root, filepath.Join(root, "skills", "review", "SKILL.md"), "notes.md")

	bundle, err := OpenLocalSkillBundle(row)
	if err != nil {
		t.Fatalf("open local skill bundle: %v", err)
	}
	defer func() { _ = bundle.Close() }()
	if bundle.PrimaryPath() != "SKILL.md" {
		t.Fatalf("primary path = %q, want SKILL.md", bundle.PrimaryPath())
	}
	files := bundle.Files()
	if len(files) != 2 || files[0].Path != "SKILL.md" || files[1].Path != "notes.md" {
		t.Fatalf("local skill files = %#v, want SKILL.md then notes.md", files)
	}
	for path, want := range map[string]string{
		"SKILL.md": "# Local review\n",
		"notes.md": "# Shared notes\n",
	} {
		data, err := bundle.ReadFile(path)
		if err != nil || string(data) != want {
			t.Fatalf("ReadFile(%q) = %q (err=%v), want %q", path, data, err, want)
		}
	}
}

func requireLocalSkillFIFORefusal(t *testing.T, row Row) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestOpenLocalSkillBundleFIFOChild$")
	cmd.Env = append(os.Environ(), "AM_LOCAL_SKILL_FIFO_ROOT="+row.Artifact.SourceRoot)
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil || err != nil {
		t.Fatalf("bounded local skill FIFO probe failed: %v (context=%v): %s", err, ctx.Err(), output)
	}
}

func TestOpenLocalSkillBundleFIFOChild(t *testing.T) {
	root := os.Getenv("AM_LOCAL_SKILL_FIFO_ROOT")
	if root == "" {
		t.Skip("bounded FIFO child only")
	}
	row := localSkillFilesTestRow(root, filepath.Join(root, "review", "SKILL.md"), "notes.md")
	bundle, err := OpenLocalSkillBundle(row)
	if bundle != nil {
		_ = bundle.Close()
	}
	if err == nil || !errors.Is(err, fsboundary.ErrUnsafeFile) {
		t.Fatalf("local skill FIFO error = %v, want ErrUnsafeFile", err)
	}
}

// spec: gui-shell/unsafe-or-missing-local-skill-source-fails-actionably
func TestOpenLocalSkillBundleRefusesUnsafeOrMissingSources(t *testing.T) {
	outside := t.TempDir()
	secret := "outside-local-skill-secret"
	localSkillFilesTestPath(t, outside, "notes.md", []byte(secret))

	for _, tc := range []struct {
		name      string
		prepare   func(t *testing.T, root string) string
		support   []string
		fifoProbe bool
		want      error
	}{
		{
			name: "escaping support symlink",
			prepare: func(t *testing.T, root string) string {
				localSkillFilesTestPath(t, root, "review/SKILL.md", []byte("# Local review\n"))
				if err := os.Symlink(filepath.Join(outside, "notes.md"), filepath.Join(root, "review", "notes.md")); err != nil {
					t.Fatal(err)
				}
				return filepath.Join(root, "review", "SKILL.md")
			},
			support: []string{"notes.md"},
			want:    fsboundary.ErrUnsafePath,
		},
		{
			name: "FIFO support",
			prepare: func(t *testing.T, root string) string {
				localSkillFilesTestPath(t, root, "review/SKILL.md", []byte("# Local review\n"))
				if err := syscall.Mkfifo(filepath.Join(root, "review", "notes.md"), 0o600); err != nil {
					t.Fatal(err)
				}
				return filepath.Join(root, "review", "SKILL.md")
			},
			support:   []string{"notes.md"},
			fifoProbe: true,
			want:      fsboundary.ErrUnsafeFile,
		},
		{
			name: "missing primary",
			prepare: func(t *testing.T, root string) string {
				return filepath.Join(root, "review", "SKILL.md")
			},
			want: fs.ErrNotExist,
		},
		{
			name: "skill directory outside source root",
			prepare: func(t *testing.T, root string) string {
				return filepath.Join(outside, "review", "SKILL.md")
			},
			want: fsboundary.ErrUnsafePath,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			primary := tc.prepare(t, root)
			row := localSkillFilesTestRow(root, primary, tc.support...)
			if tc.fifoProbe {
				requireLocalSkillFIFORefusal(t, row)
				return
			}
			bundle, err := OpenLocalSkillBundle(row)
			if err == nil || bundle != nil {
				if bundle != nil {
					_ = bundle.Close()
				}
				t.Fatalf("unsafe or missing local source opened a bundle: bundle=%v err=%v", bundle, err)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("local source error = %v, want %v", err, tc.want)
			}
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("local source error returned bytes outside the source root: %v", err)
			}
		})
	}
}

func TestLocalSkillBundleRejectsChangedMember(t *testing.T) {
	root := t.TempDir()
	localSkillFilesTestPath(t, root, "review/SKILL.md", []byte("# Local review\n"))
	localSkillFilesTestPath(t, root, "review/notes.md", []byte("# Initial notes\n"))
	row := localSkillFilesTestRow(root, filepath.Join(root, "review", "SKILL.md"), "notes.md")
	bundle, err := OpenLocalSkillBundle(row)
	if err != nil {
		t.Fatalf("open local bundle: %v", err)
	}
	replacement := filepath.Join(root, "review", "replacement.tmp")
	if err := os.WriteFile(replacement, []byte("# Replacement notes\n"), 0o644); err != nil {
		_ = bundle.Close()
		t.Fatal(err)
	}
	if err := os.Rename(replacement, filepath.Join(root, "review", "notes.md")); err != nil {
		_ = bundle.Close()
		t.Fatal(err)
	}
	if data, err := bundle.ReadFile("notes.md"); !errors.Is(err, fsboundary.ErrGenerationConflict) || data != nil {
		_ = bundle.Close()
		t.Fatalf("changed local member returned %q (err=%v), want generation conflict", data, err)
	}
	if err := bundle.Close(); err != nil {
		t.Fatal(err)
	}
}
