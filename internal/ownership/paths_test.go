package ownership

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestStateRootHonoursXDGStateHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	got, err := StateRoot()
	if err != nil {
		t.Fatalf("StateRoot error: %v", err)
	}
	want := filepath.Join(dir, "agent-marketplace")
	if got != want {
		t.Fatalf("StateRoot() = %q, want %q", got, want)
	}
}

func TestStateRootDefaultsToLocalStateAndIgnoresRelativeXDG(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, xdg := range []string{"", "relative/state"} {
		t.Setenv("XDG_STATE_HOME", xdg)
		got, err := StateRoot()
		if err != nil {
			t.Fatalf("StateRoot error: %v", err)
		}
		want := filepath.Join(home, ".local", "state", "agent-marketplace")
		if got != want {
			t.Fatalf("StateRoot() with XDG_STATE_HOME=%q = %q, want %q", xdg, got, want)
		}
	}
}

// The state key must be identical for every spelling of the SAME directory and
// different for every distinct directory.
func TestCanonicalRootCollapsesEquivalentSpellings(t *testing.T) {
	base := t.TempDir()
	realRoot := filepath.Join(base, "repo", ".devin")
	if err := os.MkdirAll(realRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(filepath.Join(base, "repo"), link); err != nil {
		t.Fatal(err)
	}
	t.Chdir(filepath.Join(base, "repo"))

	canonical, err := CanonicalRoot(realRoot)
	if err != nil {
		t.Fatalf("CanonicalRoot error: %v", err)
	}
	wantKey := RootKey(canonical)

	equivalents := []struct {
		name string
		in   string
	}{
		{"absolute", realRoot},
		{"trailing separator", realRoot + string(filepath.Separator)},
		{"double trailing separator", realRoot + string(filepath.Separator) + string(filepath.Separator)},
		{"dot segment", filepath.Join(base, "repo", ".", ".devin")},
		{"dotdot segment", filepath.Join(base, "repo", "nested", "..", ".devin")},
		{"relative to cwd", ".devin"},
		{"relative with dot prefix", filepath.Join(".", ".devin")},
		{"through a symlinked parent", filepath.Join(link, ".devin")},
	}
	for _, tt := range equivalents {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CanonicalRoot(tt.in)
			if err != nil {
				t.Fatalf("CanonicalRoot(%q) error: %v", tt.in, err)
			}
			if got != canonical {
				t.Fatalf("CanonicalRoot(%q) = %q, want %q", tt.in, got, canonical)
			}
			if key := RootKey(got); key != wantKey {
				t.Fatalf("RootKey for %q = %q, want %q", tt.in, key, wantKey)
			}
		})
	}
}

func TestCanonicalRootResolvesSymlinkedRootItself(t *testing.T) {
	base := t.TempDir()
	realRoot := filepath.Join(base, "real")
	if err := os.MkdirAll(realRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "aliased")
	if err := os.Symlink(realRoot, link); err != nil {
		t.Fatal(err)
	}
	a, err := CanonicalRoot(realRoot)
	if err != nil {
		t.Fatal(err)
	}
	b, err := CanonicalRoot(link)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("a symlinked root canonicalized to %q, want %q", b, a)
	}
}

func TestCanonicalRootWorksForNotYetCreatedRoots(t *testing.T) {
	base := t.TempDir()
	// The existing prefix is resolved; the missing suffix is appended verbatim.
	missing := filepath.Join(base, "does", "not", "exist", ".devin")
	got, err := CanonicalRoot(missing)
	if err != nil {
		t.Fatalf("CanonicalRoot error: %v", err)
	}
	resolvedBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(resolvedBase, "does", "not", "exist", ".devin")
	if got != want {
		t.Fatalf("CanonicalRoot = %q, want %q", got, want)
	}
	// And once the directory is created, the same key must be derived.
	if err := os.MkdirAll(missing, 0o755); err != nil {
		t.Fatal(err)
	}
	after, err := CanonicalRoot(missing)
	if err != nil {
		t.Fatal(err)
	}
	if RootKey(after) != RootKey(got) {
		t.Fatalf("root key changed once the directory existed: %q vs %q", RootKey(after), RootKey(got))
	}
}

func TestRootKeyDoesNotCollideForDistinctRoots(t *testing.T) {
	// Roots designed to defeat a naive concatenating key scheme: the same
	// characters split differently across the path, and prefix relationships.
	roots := []string{
		"/a/bc",
		"/ab/c",
		"/abc",
		"/a/b/c",
		"/a/b",
		"/a/b ",
		"/a/b\n",
		"/A/b",
		"/a/B",
	}
	seen := make(map[string]string, len(roots))
	for _, root := range roots {
		key := RootKey(root)
		if len(key) != 64 {
			t.Fatalf("RootKey(%q) = %q, want 64 hex characters", root, key)
		}
		if prev, dup := seen[key]; dup {
			t.Fatalf("RootKey collision: %q and %q both hash to %q", prev, root, key)
		}
		seen[key] = root
	}
}

// Case is deliberately never folded. This test pins the SAFE direction: two
// spellings that differ only in case get different keys, so a case-sensitive
// filesystem can never have two distinct directories share one record.
func TestRootKeyNeverFoldsCase(t *testing.T) {
	if RootKey("/Users/x/Repo") == RootKey("/Users/x/repo") {
		t.Fatal("RootKey folded case: two distinct directories on a case-sensitive filesystem would alias")
	}
}

func TestCanonicalRootRejectsEmpty(t *testing.T) {
	if _, err := CanonicalRoot("   "); err == nil {
		t.Fatal("CanonicalRoot accepted an empty root")
	}
}

func TestCanonicalRootReportsSymlinkLoop(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink loops are not portable on windows")
	}
	base := t.TempDir()
	a := filepath.Join(base, "a")
	b := filepath.Join(base, "b")
	if err := os.Symlink(b, a); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(a, b); err != nil {
		t.Fatal(err)
	}
	// A loop must NOT silently fall back to the unresolved path: two roots could
	// then share a key.
	if _, err := CanonicalRoot(filepath.Join(a, "child")); err == nil {
		t.Fatal("CanonicalRoot accepted a symlink loop")
	}
}

func TestDigestIsContentDigestNotContent(t *testing.T) {
	secret := "NOT-A-REAL-SECRET-c0ffee"
	got := Digest([]byte(secret))
	if strings.Contains(got, secret) {
		t.Fatalf("Digest(%q) = %q contains the content", secret, got)
	}
	if !digestPattern.MatchString(got) {
		t.Fatalf("Digest = %q, want sha256:<64 hex>", got)
	}
	if got != Digest([]byte(secret)) {
		t.Fatal("Digest is not deterministic")
	}
	if got == Digest([]byte(secret+"x")) {
		t.Fatal("Digest collided for different content")
	}
}

func TestFileSlugIsStableAndPathSafe(t *testing.T) {
	tests := []struct {
		rel  string
		base string
	}{
		{"hooks.v1.json", "hooks.v1.json"},
		{"mcp_config.json", "mcp_config.json"},
		{"nested/dir/config.json", "config.json"},
	}
	seen := make(map[string]string)
	for _, tt := range tests {
		slug := fileSlug(tt.rel)
		if !strings.HasSuffix(slug, "-"+tt.base) {
			t.Fatalf("fileSlug(%q) = %q, want it to end with %q", tt.rel, slug, "-"+tt.base)
		}
		if strings.ContainsAny(slug, "/\\") {
			t.Fatalf("fileSlug(%q) = %q contains a path separator", tt.rel, slug)
		}
		if prev, dup := seen[slug]; dup {
			t.Fatalf("fileSlug collision between %q and %q", prev, tt.rel)
		}
		seen[slug] = tt.rel
		if slug != fileSlug(tt.rel) {
			t.Fatal("fileSlug is not deterministic")
		}
	}
	// Same base name in different directories must not share a directory.
	if fileSlug("a/config.json") == fileSlug("b/config.json") {
		t.Fatal("fileSlug ignored the directory component")
	}
}
