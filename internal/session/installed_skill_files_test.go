package session

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func installedTestSkill(supportFiles ...string) *ir.Skill {
	skill := ir.NewSkill("review", "Review code", "# Installed review\n", []string{"user"}, nil, ir.SourceRef{})
	skill.SupportFiles = append([]string(nil), supportFiles...)
	return skill
}

func installedTestRow(path string, skill *ir.Skill, archiveDigest string) Row {
	name := "review"
	if skill != nil {
		name = skill.Name()
	}
	row := Row{Artifact: resolver.ResolvedArtifact{
		Entry:                    catalog.Entry{Kind: ir.KindSkill, Name: name, Path: path},
		IsMarketplace:            true,
		MarketplaceResourceName:  name,
		MarketplaceArchiveDigest: archiveDigest,
	}}
	row.Artifact.Resource = skill
	return row
}

func writeVerifiedInstalledArtifact(t *testing.T, resource ir.Resource, support map[string]hccartifact.SupportFile, assets, licenses map[string][]byte) (string, string) {
	t.Helper()
	canonical, err := hccartifact.FromIR(resource)
	if err != nil {
		t.Fatalf("FromIR: %v", err)
	}
	canonical.Manifest.RegistryID = "src"
	canonical.Manifest.RegistrySlug = "source"
	canonical.Manifest.Revision = canonical.Manifest.ContentDigest
	canonical.Manifest.Provenance.Generator = "test"
	canonical.Manifest.SupportedSchemas = []string{hccartifact.CanonicalArtifactSchemaV1}
	canonical.Support = support
	canonical.Assets = assets
	canonical.Licenses = licenses
	raw, archiveDigest, err := hccartifact.Encode(canonical)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	dir := filepath.Join(t.TempDir(), "artifacts", strings.ReplaceAll(archiveDigest, ":", "_"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(dir, "artifact.tar.gz")
	if err := os.WriteFile(archivePath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "verified.marker"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	return archivePath, archiveDigest
}

func writeInstalledSupport(t *testing.T, root string, skill *ir.Skill) map[string][]byte {
	t.Helper()
	content := map[string][]byte{}
	for _, path := range skill.SupportFiles {
		body := []byte("# " + filepath.Base(path) + "\n")
		if strings.HasSuffix(path, ".sh") {
			body = []byte("printf \"review\"\n")
		}
		target := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		mode := os.FileMode(0o644)
		if strings.HasSuffix(path, ".sh") {
			mode = 0o755
		}
		if err := os.WriteFile(target, body, mode); err != nil {
			t.Fatal(err)
		}
		content[path] = body
	}
	return content
}

func writeInstalledTestFile(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
}

func expectedInstalledContentIdentity(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func expectedInstalledStageIdentity(t *testing.T, root, path string) string {
	t.Helper()
	dir, err := fsboundary.OpenDir(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dir.Close() }()
	file, identity, err := dir.OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	identityJSON, err := json.Marshal(identity)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(identityJSON)
	return hex.EncodeToString(sum[:])
}

func installedFileByPath(files []InstalledSkillFile, path string) (InstalledSkillFile, bool) {
	for _, file := range files {
		if file.Path == path {
			return file, true
		}
	}
	return InstalledSkillFile{}, false
}

func requireInstalledSkillFIFORefusal(t *testing.T, root string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestInstalledSkillBundleFIFOChild$")
	cmd.Env = append(os.Environ(), "AM_WORK37_FIFO_PROBE=1", "AM_WORK37_FIFO_ROOT="+root)
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil || err != nil {
		t.Fatalf("bounded FIFO probe failed: %v (context=%v): %s", err, ctx.Err(), output)
	}
}

func TestInstalledSkillBundleFIFOChild(t *testing.T) {
	if os.Getenv("AM_WORK37_FIFO_PROBE") != "1" {
		t.Skip("bounded FIFO child only")
	}
	row := installedTestRow(os.Getenv("AM_WORK37_FIFO_ROOT"), installedTestSkill("notes.md"), "")
	bundle, err := OpenInstalledSkillBundle(row)
	if err == nil || bundle != nil {
		if bundle != nil {
			_ = bundle.Close()
		}
		t.Fatalf("FIFO support opened as a regular member: bundle=%v err=%v", bundle, err)
	}
	if !errors.Is(err, fsboundary.ErrUnsafeFile) {
		t.Fatalf("FIFO support error = %v, want ErrUnsafeFile", err)
	}
}

func TestMarketplaceInstalledSkillFiles(t *testing.T) {
	skill := installedTestSkill("notes.md")
	row := installedTestRow("", skill, "")
	if !MarketplaceInstalledSkillFiles(row) {
		t.Fatal("installed Marketplace skill without a current source path was hidden")
	}

	row.Artifact.IsMarketplace = false
	if MarketplaceInstalledSkillFiles(row) {
		t.Fatal("local skill exposed Marketplace installed-file capability")
	}
	row = installedTestRow("", skill, "")
	row.Artifact.Kind = ir.KindCommand
	if MarketplaceInstalledSkillFiles(row) {
		t.Fatal("non-skill Marketplace resource exposed installed-file capability")
	}
	row = installedTestRow("", skill, "")
	row.MarketplaceDiscovery = &MarketplaceDiscovery{}
	if MarketplaceInstalledSkillFiles(row) {
		t.Fatal("uninstalled discovery skill exposed installed-file capability")
	}
	row = installedTestRow("", skill, "")
	row.PendingVerification = true
	if MarketplaceInstalledSkillFiles(row) {
		t.Fatal("pending Marketplace verification exposed installed-file capability")
	}
}

func TestOpenInstalledSkillBundleArchivePrimaryAndMembers(t *testing.T) {
	skill := installedTestSkill("notes.md", "scripts/helper.sh")
	support := map[string]hccartifact.SupportFile{
		"notes.md":          {Data: []byte("# Supporting review\n")},
		"scripts/helper.sh": {Data: []byte("printf \"review\"\n"), Executable: true},
	}
	assets := map[string][]byte{"logo.png": []byte("\x89PNG\r\n\x1a\nfake-image")}
	licenses := map[string][]byte{"NOTICE.txt": []byte("Test license\n")}
	archivePath, archiveDigest := writeVerifiedInstalledArtifact(t, skill, support, assets, licenses)
	bundle, err := OpenInstalledSkillBundle(installedTestRow(archivePath, skill, archiveDigest))
	if err != nil {
		t.Fatalf("open verified installed archive: %v", err)
	}
	defer func() { _ = bundle.Close() }()
	if bundle.PrimaryPath() != "SKILL.md" {
		t.Fatalf("archive primary path = %q, want SKILL.md", bundle.PrimaryPath())
	}
	for _, tc := range []struct {
		name string
		row  Row
	}{
		{name: "attached resource name fallback", row: func() Row {
			row := installedTestRow(archivePath, skill, archiveDigest)
			row.Artifact.MarketplaceResourceName = ""
			row.Artifact.Name = "different"
			return row
		}()},
		{name: "artifact name fallback", row: func() Row {
			row := installedTestRow(archivePath, skill, archiveDigest)
			row.Artifact.MarketplaceResourceName = ""
			row.Artifact.Resource = nil
			return row
		}()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fallback, err := OpenInstalledSkillBundle(tc.row)
			if err != nil {
				t.Fatalf("open canonical archive with expected-name fallback: %v", err)
			}
			if err := fallback.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
	files := bundle.Files()
	for _, path := range []string{hccartifact.CanonicalManifestPath, "assets/logo.png", "licenses/NOTICE.txt", "SKILL.md", "notes.md", "scripts/helper.sh"} {
		file, ok := installedFileByPath(files, path)
		if !ok || file.Identity == "" {
			t.Fatalf("verified installed archive member %q missing or unidentified in %#v", path, files)
		}
	}
	for _, path := range []string{"artifact.tar.gz", "verified.marker", ".cache", "cache-root"} {
		if _, ok := installedFileByPath(files, path); ok {
			t.Fatalf("cache implementation file %q was exposed in %#v", path, files)
		}
	}
	notesFile, ok := installedFileByPath(files, "notes.md")
	if !ok || notesFile.Identity != expectedInstalledContentIdentity(support["notes.md"].Data) {
		t.Fatalf("archive support identity = %+v", notesFile)
	}
	image, ok := installedFileByPath(files, "assets/logo.png")
	if !ok || image.InspectionClass != "image" || image.MediaType != "image/png" {
		t.Fatalf("raster member metadata = %+v", image)
	}
	script, ok := installedFileByPath(files, "scripts/helper.sh")
	if !ok || script.InspectionClass != "text" {
		t.Fatalf("script support metadata = %+v", script)
	}
	originalPath := files[0].Path
	files[0].Path = "mutated"
	if bundle.Files()[0].Path != originalPath {
		t.Fatal("Files returned the bundle-owned member slice")
	}

	primary, err := bundle.ReadFile("SKILL.md")
	primaryFile, primaryListed := installedFileByPath(bundle.Files(), "SKILL.md")
	if err != nil || !bytes.Contains(primary, []byte("# Installed review")) || bytes.Contains(primary, []byte(`"Body"`)) || bytes.Contains(primary, []byte(`"SupportFiles"`)) || !primaryListed || primaryFile.Identity != expectedInstalledContentIdentity(primary) {
		t.Fatalf("archive primary = %q (err=%v), listed=%+v; want inert Factory Markdown and its byte identity", primary, err, primaryFile)
	}
	for path, want := range map[string][]byte{
		"notes.md":            support["notes.md"].Data,
		"scripts/helper.sh":   support["scripts/helper.sh"].Data,
		"assets/logo.png":     assets["logo.png"],
		"licenses/NOTICE.txt": licenses["NOTICE.txt"],
	} {
		got, err := bundle.ReadFile(path)
		if path == "assets/logo.png" {
			if !errors.Is(err, ErrInstalledSkillFileUnsupported) || got != nil {
				t.Fatalf("unsupported member %q returned %q, %v; want no bytes", path, got, err)
			}
			continue
		}
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("archive member %q = %q (err=%v), want %q", path, got, err, want)
		}
	}
	if err := bundle.Close(); err != nil {
		t.Fatal(err)
	}
	if data, err := bundle.ReadFile("notes.md"); err != nil || !bytes.Equal(data, support["notes.md"].Data) {
		t.Fatalf("archive Close should be a no-op: data=%q err=%v", data, err)
	}
	if data, err := bundle.ReadFile("artifact.tar.gz"); err == nil || data != nil {
		t.Fatalf("archive container member returned data=%q err=%v", data, err)
	}
}

func TestOpenInstalledSkillBundleStagedVirtualPrimary(t *testing.T) {
	skill := installedTestSkill("notes.md", "scripts/helper.sh")
	stage := t.TempDir()
	support := writeInstalledSupport(t, stage, skill)
	for path, body := range map[string][]byte{
		"artifact.tar.gz":     []byte("private archive container"),
		"verified.marker":     []byte("private marker"),
		".cache/private.json": []byte("private cache metadata"),
	} {
		target := filepath.Join(stage, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	beforeNotes, err := os.ReadFile(filepath.Join(stage, "notes.md"))
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := OpenInstalledSkillBundle(installedTestRow(stage, skill, ""))
	if err != nil {
		t.Fatalf("open declared staged skill files: %v", err)
	}
	defer func() { _ = bundle.Close() }()
	if bundle.PrimaryPath() != "SKILL.md" {
		t.Fatalf("staged primary path = %q, want SKILL.md", bundle.PrimaryPath())
	}
	files := bundle.Files()
	for _, path := range []string{"SKILL.md", "notes.md", "scripts/helper.sh"} {
		if _, ok := installedFileByPath(files, path); !ok {
			t.Fatalf("staged member %q missing from %#v", path, files)
		}
	}
	notesFile, ok := installedFileByPath(files, "notes.md")
	if !ok || notesFile.Identity != expectedInstalledStageIdentity(t, stage, "notes.md") {
		t.Fatalf("staged identity does not bind the full descriptor state: %+v", notesFile)
	}
	for _, path := range []string{"artifact.tar.gz", "verified.marker", ".cache/private.json"} {
		if _, ok := installedFileByPath(files, path); ok {
			t.Fatalf("undeclared cache implementation file %q was exposed in %#v", path, files)
		}
	}
	if data, err := bundle.ReadFile(".cache/private.json"); err == nil || data != nil {
		t.Fatalf("undeclared private cache member returned data=%q err=%v", data, err)
	}
	primary, err := bundle.ReadFile("SKILL.md")
	primaryFile, primaryListed := installedFileByPath(bundle.Files(), "SKILL.md")
	if err != nil || !bytes.Contains(primary, []byte("# Installed review")) || bytes.Contains(primary, []byte(`"Body"`)) || bytes.Contains(primary, []byte(`"SupportFiles"`)) || !primaryListed || primaryFile.Identity != expectedInstalledContentIdentity(primary) {
		t.Fatalf("virtual primary = %q (err=%v), listed=%+v; want inert Factory Markdown and its byte identity", primary, err, primaryFile)
	}
	for path, want := range support {
		got, err := bundle.ReadFile(path)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("staged support %q = %q (err=%v), want %q", path, got, err, want)
		}
	}
	afterNotes, err := os.ReadFile(filepath.Join(stage, "notes.md"))
	if err != nil || !bytes.Equal(afterNotes, beforeNotes) {
		t.Fatalf("staged read changed supporting bytes: %q (err=%v)", afterNotes, err)
	}
	for path, want := range support {
		got, err := os.ReadFile(filepath.Join(stage, filepath.FromSlash(path)))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("staged read changed %q: %q (err=%v)", path, got, err)
		}
	}
	for path, want := range map[string][]byte{
		"artifact.tar.gz":     []byte("private archive container"),
		"verified.marker":     []byte("private marker"),
		".cache/private.json": []byte("private cache metadata"),
	} {
		got, err := os.ReadFile(filepath.Join(stage, filepath.FromSlash(path)))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("staged read changed private file %q: %q (err=%v)", path, got, err)
		}
	}
	if _, err := os.Stat(filepath.Join(stage, "SKILL.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("virtual primary was materialized to disk: %v", err)
	}

	existingStage := t.TempDir()
	actualPrimary := []byte("# Existing staged primary\n")
	writeInstalledTestFile(t, filepath.Join(existingStage, "SKILL.md"), actualPrimary, 0o644)
	writeInstalledSupport(t, existingStage, skill)
	existingBundle, err := OpenInstalledSkillBundle(installedTestRow(existingStage, skill, ""))
	if err != nil {
		t.Fatalf("open staged skill with regular primary: %v", err)
	}
	defer func() { _ = existingBundle.Close() }()
	gotPrimary, err := existingBundle.ReadFile("SKILL.md")
	if err != nil || !bytes.Equal(gotPrimary, actualPrimary) {
		t.Fatalf("regular staged primary = %q (err=%v), want exact file bytes", gotPrimary, err)
	}

	t.Run("invalid UTF-8 support", func(t *testing.T) {
		invalidSkill := installedTestSkill("notes.md")
		invalidRoot := t.TempDir()
		writeInstalledTestFile(t, filepath.Join(invalidRoot, "notes.md"), []byte{0xff, 0xfe}, 0o644)
		invalidBundle, err := OpenInstalledSkillBundle(installedTestRow(invalidRoot, invalidSkill, ""))
		if err != nil {
			t.Fatalf("open staged invalid-UTF8 bundle: %v", err)
		}
		defer func() { _ = invalidBundle.Close() }()
		data, err := invalidBundle.ReadFile("notes.md")
		if !errors.Is(err, ErrInstalledSkillFileUnsupported) || data != nil {
			t.Fatalf("invalid UTF-8 support returned data=%q err=%v, want metadata-only refusal", data, err)
		}
	})
}

func TestOpenInstalledSkillBundleFailsClosed(t *testing.T) {
	skill := installedTestSkill("notes.md")
	archivePath, archiveDigest := writeVerifiedInstalledArtifact(t, skill, map[string]hccartifact.SupportFile{"notes.md": {Data: []byte("# Notes\n")}}, nil, nil)

	cases := []struct {
		name string
		row  Row
	}{
		{name: "missing path", row: installedTestRow("", skill, "")},
		{name: "wrong kind", row: func() Row {
			row := installedTestRow(archivePath, skill, archiveDigest)
			row.Artifact.Kind = ir.KindCommand
			return row
		}()},
		{name: "not Marketplace", row: func() Row {
			row := installedTestRow(archivePath, skill, archiveDigest)
			row.Artifact.IsMarketplace = false
			return row
		}()},
		{name: "discovery row", row: func() Row {
			row := installedTestRow(archivePath, skill, archiveDigest)
			row.MarketplaceDiscovery = &MarketplaceDiscovery{}
			return row
		}()},
		{name: "pending verification", row: func() Row {
			row := installedTestRow(archivePath, skill, archiveDigest)
			row.PendingVerification = true
			return row
		}()},
		{name: "wrong name", row: func() Row {
			row := installedTestRow(archivePath, skill, archiveDigest)
			row.Artifact.MarketplaceResourceName = "other"
			return row
		}()},
		{name: "wrong archive digest", row: func() Row {
			row := installedTestRow(archivePath, skill, archiveDigest)
			row.Artifact.MarketplaceArchiveDigest = "sha256:" + strings.Repeat("f", 64)
			return row
		}()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bundle, err := OpenInstalledSkillBundle(tc.row)
			if err == nil || bundle != nil {
				if bundle != nil {
					_ = bundle.Close()
				}
				t.Fatalf("invalid installed skill opened a bundle: bundle=%v err=%v", bundle, err)
			}
		})
	}

	validRaw, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	unmarkedPath := filepath.Join(t.TempDir(), "artifact.tar.gz")
	writeInstalledTestFile(t, unmarkedPath, validRaw, 0o644)

	for _, tc := range []struct {
		name    string
		archive string
	}{
		{name: "missing verified marker", archive: unmarkedPath},
		{name: "corrupt archive", archive: func() string {
			dir := t.TempDir()
			path := filepath.Join(dir, "artifact.tar.gz")
			writeInstalledTestFile(t, path, []byte("not a canonical archive"), 0o644)
			writeInstalledTestFile(t, filepath.Join(dir, "verified.marker"), []byte("ok"), 0o644)
			return path
		}()},
		{name: "archive symlink", archive: func() string {
			dir := t.TempDir()
			if err := os.Symlink(archivePath, filepath.Join(dir, "artifact.tar.gz")); err != nil {
				t.Fatal(err)
			}
			writeInstalledTestFile(t, filepath.Join(dir, "verified.marker"), []byte("ok"), 0o644)
			return filepath.Join(dir, "artifact.tar.gz")
		}()},
		{name: "marker symlink", archive: func() string {
			dir := t.TempDir()
			path := filepath.Join(dir, "artifact.tar.gz")
			writeInstalledTestFile(t, path, validRaw, 0o644)
			if err := os.Symlink(filepath.Join(filepath.Dir(archivePath), "verified.marker"), filepath.Join(dir, "verified.marker")); err != nil {
				t.Fatal(err)
			}
			return path
		}()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := installedTestRow(tc.archive, skill, archiveDigest)
			bundle, err := OpenInstalledSkillBundle(row)
			if err == nil || bundle != nil {
				if bundle != nil {
					_ = bundle.Close()
				}
				t.Fatalf("invalid archive opened a bundle: bundle=%v err=%v", bundle, err)
			}
		})
	}

	wrongKind := ir.NewCommand("review", "Review code", "# command\n", nil, ir.SourceRef{})
	wrongKindPath, wrongKindDigest := writeVerifiedInstalledArtifact(t, wrongKind, nil, nil, nil)
	if bundle, err := OpenInstalledSkillBundle(installedTestRow(wrongKindPath, skill, wrongKindDigest)); err == nil || bundle != nil {
		t.Fatalf("wrong archive kind opened a bundle: bundle=%v err=%v", bundle, err)
	}
	for name, support := range map[string]map[string]hccartifact.SupportFile{
		"missing declared member": nil,
		"undeclared member":       {"other.md": {Data: []byte("# Other\n")}},
	} {
		path, digest := writeVerifiedInstalledArtifact(t, skill, support, nil, nil)
		if bundle, err := OpenInstalledSkillBundle(installedTestRow(path, skill, digest)); err == nil || bundle != nil {
			t.Fatalf("%s archive opened a bundle: bundle=%v err=%v", name, bundle, err)
		}
	}

	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "sentinel.txt")
	if err := os.WriteFile(outside, []byte("protected bytes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rootLink := filepath.Join(t.TempDir(), "stage-link")
	if err := os.Symlink(outsideDir, rootLink); err != nil {
		t.Fatal(err)
	}
	if bundle, err := OpenInstalledSkillBundle(installedTestRow(rootLink, skill, "")); err == nil || bundle != nil {
		t.Fatalf("symlinked stage root opened a bundle: bundle=%v err=%v", bundle, err)
	}
	if got, err := os.ReadFile(outside); err != nil || string(got) != "protected bytes\n" {
		t.Fatalf("symlinked root changed outside sentinel: %q (err=%v)", got, err)
	}

	outsideSupport := filepath.Join(outsideDir, "support-sentinel.txt")
	writeInstalledTestFile(t, outsideSupport, []byte("protected support bytes\n"), 0o600)
	for _, tc := range []struct {
		name     string
		prepare  func(t *testing.T, root string, skill *ir.Skill)
		resource *ir.Skill
		fifo     bool
		wantErr  error
	}{
		{name: "nil verified IR", prepare: func(t *testing.T, root string, _ *ir.Skill) {
			writeInstalledTestFile(t, filepath.Join(root, "notes.md"), []byte("# Notes\n"), 0o644)
		}},
		{name: "missing declared support", prepare: func(t *testing.T, _ string, _ *ir.Skill) {}, resource: skill, wantErr: fs.ErrNotExist},
		{name: "traversal declared support", prepare: func(t *testing.T, _ string, _ *ir.Skill) {}, resource: installedTestSkill("../outside.txt")},
		{name: "support symlink", prepare: func(t *testing.T, root string, _ *ir.Skill) {
			if err := os.Symlink(outsideSupport, filepath.Join(root, "notes.md")); err != nil {
				t.Fatal(err)
			}
		}, resource: skill, wantErr: fsboundary.ErrUnsafePath},
		{name: "hard-linked support", prepare: func(t *testing.T, root string, _ *ir.Skill) {
			if err := os.Link(outsideSupport, filepath.Join(root, "notes.md")); err != nil {
				t.Fatal(err)
			}
		}, resource: skill, wantErr: fsboundary.ErrUnsafeFile},
		{name: "FIFO support", prepare: func(t *testing.T, root string, _ *ir.Skill) {
			if err := syscall.Mkfifo(filepath.Join(root, "notes.md"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, resource: skill, fifo: true},
		{name: "symlink primary", prepare: func(t *testing.T, root string, skill *ir.Skill) {
			writeInstalledSupport(t, root, skill)
			if err := os.Symlink(outside, filepath.Join(root, "SKILL.md")); err != nil {
				t.Fatal(err)
			}
		}, resource: skill, wantErr: fsboundary.ErrUnsafePath},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if tc.prepare != nil {
				tc.prepare(t, root, tc.resource)
			}
			if tc.fifo {
				requireInstalledSkillFIFORefusal(t, root)
				return
			}
			row := installedTestRow(root, tc.resource, "")
			bundle, err := OpenInstalledSkillBundle(row)
			if err == nil || bundle != nil {
				if bundle != nil {
					_ = bundle.Close()
				}
				t.Fatalf("invalid staged bundle opened: bundle=%v err=%v", bundle, err)
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("invalid staged bundle error = %v, want %v", err, tc.wantErr)
			}
		})
	}
	if got, err := os.ReadFile(outside); err != nil || string(got) != "protected bytes\n" {
		t.Fatalf("staged symlink probe changed outside primary sentinel: %q (err=%v)", got, err)
	}
	if got, err := os.ReadFile(outsideSupport); err != nil || string(got) != "protected support bytes\n" {
		t.Fatalf("staged symlink probe changed outside support sentinel: %q (err=%v)", got, err)
	}
}

func TestInstalledSkillBundleRejectsChangedMember(t *testing.T) {
	skill := installedTestSkill("notes.md")
	root := t.TempDir()
	writeInstalledSupport(t, root, skill)
	row := installedTestRow(root, skill, "")
	bundle, err := OpenInstalledSkillBundle(row)
	if err != nil {
		t.Fatalf("open staged bundle: %v", err)
	}
	listed, ok := installedFileByPath(bundle.Files(), "notes.md")
	if !ok {
		_ = bundle.Close()
		t.Fatal("listed staged member missing")
	}
	replacement := filepath.Join(root, "replacement.tmp")
	if err := os.WriteFile(replacement, []byte("replacement notes\n"), 0o644); err != nil {
		_ = bundle.Close()
		t.Fatal(err)
	}
	if err := os.Rename(replacement, filepath.Join(root, "notes.md")); err != nil {
		_ = bundle.Close()
		t.Fatal(err)
	}
	if data, err := bundle.ReadFile("notes.md"); !errors.Is(err, fsboundary.ErrGenerationConflict) || data != nil {
		_ = bundle.Close()
		t.Fatalf("changed member returned stale/new data=%q err=%v, want generation conflict", data, err)
	}
	if err := bundle.Close(); err != nil {
		t.Fatal(err)
	}
	fresh, err := OpenInstalledSkillBundle(row)
	if err != nil {
		t.Fatalf("reopen updated staged bundle: %v", err)
	}
	defer func() { _ = fresh.Close() }()
	freshFile, ok := installedFileByPath(fresh.Files(), "notes.md")
	if !ok || freshFile.Identity == listed.Identity {
		t.Fatalf("fresh member identity did not advance: before=%+v after=%+v", listed, freshFile)
	}
	data, err := fresh.ReadFile("notes.md")
	if err != nil || string(data) != "replacement notes\n" {
		t.Fatalf("fresh member read = %q (err=%v)", data, err)
	}
}
