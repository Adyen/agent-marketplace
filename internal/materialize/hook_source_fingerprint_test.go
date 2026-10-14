package materialize

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
)

// hookSourceFingerprintVector mirrors one independently calculated case in
// testdata/hook-source-fingerprint-v1.json. The expected digests are committed
// constants: the implementation must match them, never derive them.
type hookSourceFingerprintVector struct {
	Name           string `json:"name"`
	SourceIdentity string `json:"source_identity"`
	InstalledName  string `json:"installed_name"`
	Target         string `json:"target"`
	Scope          string `json:"scope"`
	Definition     struct {
		Path     string `json:"path"`
		BytesB64 string `json:"bytes_b64"`
	} `json:"definition"`
	Support []struct {
		Path     string `json:"path"`
		Mode     string `json:"mode"`
		BytesB64 string `json:"bytes_b64"`
	} `json:"support"`
	Digest string `json:"digest"`
}

type hookSourceFingerprintVectors struct {
	Version        int                           `json:"version"`
	Algorithm      string                        `json:"algorithm"`
	Cases          []hookSourceFingerprintVector `json:"cases"`
	InvariantPairs [][]string                    `json:"invariant_pairs"`
}

func loadHookSourceFingerprintVectors(t *testing.T) hookSourceFingerprintVectors {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "hook-source-fingerprint-v1.json"))
	if err != nil {
		t.Fatalf("read vectors: %v", err)
	}
	var v hookSourceFingerprintVectors
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("parse vectors: %v", err)
	}
	if v.Version != 1 {
		t.Fatalf("vector version = %d, want 1", v.Version)
	}
	if len(v.Cases) == 0 {
		t.Fatal("vectors contain no cases")
	}
	return v
}

// writeFingerprintSource builds a filesystem source layout for one vector and
// returns the absolute path of the hook definition. Support paths are relative
// to the definition's directory, per the spec.
func writeFingerprintSource(t *testing.T, c hookSourceFingerprintVector) (root, defAbs string) {
	t.Helper()
	root = t.TempDir()
	defAbs = filepath.Join(root, filepath.FromSlash(c.Definition.Path))
	defBytes, err := base64.StdEncoding.DecodeString(c.Definition.BytesB64)
	if err != nil {
		t.Fatalf("case %s: decode definition: %v", c.Name, err)
	}
	if err := os.MkdirAll(filepath.Dir(defAbs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(defAbs, defBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	defDir := filepath.Dir(defAbs)
	for _, s := range c.Support {
		data, err := base64.StdEncoding.DecodeString(s.BytesB64)
		if err != nil {
			t.Fatalf("case %s: decode support %s: %v", c.Name, s.Path, err)
		}
		mode, err := strconv.ParseUint(s.Mode, 8, 32)
		if err != nil {
			t.Fatalf("case %s: parse mode %q: %v", c.Name, s.Mode, err)
		}
		p := filepath.Join(defDir, filepath.FromSlash(s.Path))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, os.FileMode(mode)); err != nil {
			t.Fatal(err)
		}
	}
	return root, defAbs
}

// fingerprintVectorInput builds the call input from the source root and the
// committed relative definition path. The definition's absolute path is
// derived inside the implementation: callers never supply it.
func fingerprintVectorInput(c hookSourceFingerprintVector, sourceRoot string) HookSourceFingerprintInput {
	return HookSourceFingerprintInput{
		SourceIdentity: c.SourceIdentity,
		Name:           c.InstalledName,
		Target:         c.Target,
		Scope:          c.Scope,
		SourceRoot:     sourceRoot,
		DefinitionRel:  c.Definition.Path,
	}
}

func newFingerprintAliasSource(t *testing.T, base hookSourceFingerprintVector) (HookSourceFingerprintInput, string) {
	t.Helper()
	root, definition := writeFingerprintSource(t, base)
	return fingerprintVectorInput(base, root), filepath.Dir(definition)
}

func writeFingerprintAliasFile(t *testing.T, dir, rel string, data []byte, mode os.FileMode) string {
	t.Helper()
	fullPath := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, data, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(fullPath, mode); err != nil {
		t.Fatal(err)
	}
	return fullPath
}

func fingerprintSupportDirectoryFD(t *testing.T, dir string) int {
	t.Helper()
	fd, err := unix.Open(dir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := unix.Close(fd); err != nil {
			t.Errorf("close fingerprint support root: %v", err)
		}
	})
	return fd
}

func readFingerprintSupportCapture(dirFD int, rel string) ([]byte, os.FileMode, error) {
	file, mode, _, err := openHookSupportFileAt(dirFD, rel, 0)
	if err != nil {
		return nil, 0, err
	}
	data, readErr := io.ReadAll(file)
	closeErr := file.Close()
	if readErr != nil {
		return nil, 0, readErr
	}
	if closeErr != nil {
		return nil, 0, closeErr
	}
	return data, mode.Perm(), nil
}

func requireFingerprintAliasRefusal(t *testing.T, in HookSourceFingerprintInput, defDir, rel string) {
	t.Helper()
	fingerprint, publicErr := HookSourceFingerprint(in)
	if publicErr == nil || fingerprint != "" {
		t.Errorf("public fingerprint for %s = %q / %v, want refusal", rel, fingerprint, publicErr)
	}
	data, mode, captureErr := readFingerprintSupportCapture(fingerprintSupportDirectoryFD(t, defDir), rel)
	if captureErr == nil {
		t.Errorf("direct support capture accepted %s: %q mode=%#o", rel, data, mode.Perm())
	}
}

func requireFingerprintAliasCapture(t *testing.T, in HookSourceFingerprintInput, defDir, rel string, want []byte, wantMode os.FileMode) string {
	t.Helper()
	fingerprint, err := HookSourceFingerprint(in)
	if err != nil || fingerprint == "" {
		t.Fatalf("public fingerprint for %s: %q / %v", rel, fingerprint, err)
	}
	data, mode, err := readFingerprintSupportCapture(fingerprintSupportDirectoryFD(t, defDir), rel)
	if err != nil {
		t.Fatalf("direct support capture for %s: %v", rel, err)
	}
	if string(data) != string(want) || mode.Perm() != wantMode.Perm() {
		t.Fatalf("direct support capture for %s = %q mode=%#o, want %q mode=%#o", rel, data, mode.Perm(), want, wantMode.Perm())
	}
	return fingerprint
}

// spec: hook-native-location-tolerance/source-and-support-changes-invalidate-the-fingerprint
// spec: hook-native-location-tolerance/different-ir-provenance-keeps-the-same-fingerprint
func TestHookSourceFingerprintVectors(t *testing.T) {
	v := loadHookSourceFingerprintVectors(t)
	digests := make(map[string]string, len(v.Cases))
	for _, c := range v.Cases {
		t.Run(c.Name, func(t *testing.T) {
			root, _ := writeFingerprintSource(t, c)
			got, err := HookSourceFingerprint(fingerprintVectorInput(c, root))
			if err != nil {
				t.Fatalf("HookSourceFingerprint: %v", err)
			}
			if got != c.Digest {
				t.Fatalf("fingerprint = %s, want committed vector %s", got, c.Digest)
			}
			digests[c.Name] = got
		})
	}
	for _, pair := range v.InvariantPairs {
		if len(pair) != 2 {
			t.Fatalf("invariant pair %v must name two cases", pair)
		}
		a, b := digests[pair[0]], digests[pair[1]]
		if a == "" || b == "" {
			t.Fatalf("invariant pair %v references an unknown case", pair)
		}
		if a != b {
			t.Fatalf("cases %s and %s must share the fingerprint (IR provenance is not hashed): %s != %s", pair[0], pair[1], a, b)
		}
	}
}

// spec: hook-native-location-tolerance/source-and-support-changes-invalidate-the-fingerprint
func TestHookSourceFingerprintContainedFileAlias(t *testing.T) {
	vectors := loadHookSourceFingerprintVectors(t)
	base := vectors.Cases[0]
	const support = "contained hook support bytes"

	writeSupport := func(root string, symlink bool) string {
		t.Helper()
		defDir := filepath.Dir(filepath.Join(root, filepath.FromSlash(base.Definition.Path)))
		helperPath := filepath.Join(defDir, "helper.txt")
		if err := os.WriteFile(helperPath, []byte(support), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(helperPath, 0o755); err != nil {
			t.Fatal(err)
		}
		aliasPath := filepath.Join(defDir, "alias.txt")
		if symlink {
			if err := os.Symlink("helper.txt", aliasPath); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := os.WriteFile(aliasPath, []byte(support), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(aliasPath, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		return aliasPath
	}

	linkedRoot, _ := writeFingerprintSource(t, base)
	writeSupport(linkedRoot, true)
	linkedDefDir := filepath.Dir(filepath.Join(linkedRoot, filepath.FromSlash(base.Definition.Path)))
	directData, directMode, directErr := readFingerprintSupportCapture(fingerprintSupportDirectoryFD(t, linkedDefDir), "alias.txt")
	if directErr != nil || string(directData) != support || directMode.Perm() != 0o755 {
		t.Fatalf("direct simple alias capture = %q mode=%#o / %v", directData, directMode.Perm(), directErr)
	}
	linked, err := HookSourceFingerprint(fingerprintVectorInput(base, linkedRoot))
	if err != nil {
		t.Fatalf("contained relative file alias: %v", err)
	}
	if !strings.HasPrefix(linked, "sha256:") || len(linked) != len("sha256:")+64 {
		t.Fatalf("contained alias fingerprint = %q, want sha256 digest", linked)
	}

	regularRoot, _ := writeFingerprintSource(t, base)
	writeSupport(regularRoot, false)
	regular, err := HookSourceFingerprint(fingerprintVectorInput(base, regularRoot))
	if err != nil {
		t.Fatalf("regular alias-equivalent source: %v", err)
	}
	if linked != regular {
		t.Fatalf("contained alias fingerprint = %q, regular logical copy = %q", linked, regular)
	}
	t.Run("relative_escape_refuses", func(t *testing.T) {
		in, defDir := newFingerprintAliasSource(t, base)
		if err := os.Symlink("../../outside.txt", filepath.Join(defDir, "escape.txt")); err != nil {
			t.Fatal(err)
		}
		requireFingerprintAliasRefusal(t, in, defDir, "escape.txt")
	})
	t.Run("cycle_refuses", func(t *testing.T) {
		in, defDir := newFingerprintAliasSource(t, base)
		if err := os.Symlink("cycle-b.txt", filepath.Join(defDir, "cycle-a.txt")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("cycle-a.txt", filepath.Join(defDir, "cycle-b.txt")); err != nil {
			t.Fatal(err)
		}
		requireFingerprintAliasRefusal(t, in, defDir, "cycle-a.txt")
	})
	t.Run("missing_component_before_dotdot_refuses", func(t *testing.T) {
		in, defDir := newFingerprintAliasSource(t, base)
		writeFingerprintAliasFile(t, defDir, "helper.txt", []byte("helper payload"), 0o755)
		if err := os.Symlink("missing/../helper.txt", filepath.Join(defDir, "a-alias.txt")); err != nil {
			t.Fatal(err)
		}
		requireFingerprintAliasRefusal(t, in, defDir, "a-alias.txt")
	})
	t.Run("file_component_before_dotdot_refuses", func(t *testing.T) {
		in, defDir := newFingerprintAliasSource(t, base)
		writeFingerprintAliasFile(t, defDir, "file", []byte("not a directory"), 0o644)
		writeFingerprintAliasFile(t, defDir, "helper.txt", []byte("helper payload"), 0o755)
		if err := os.Symlink("file/../helper.txt", filepath.Join(defDir, "a-alias.txt")); err != nil {
			t.Fatal(err)
		}
		requireFingerprintAliasRefusal(t, in, defDir, "a-alias.txt")
	})
	t.Run("symlink_directory_before_dotdot_refuses_without_foreign_changes", func(t *testing.T) {
		in, defDir := newFingerprintAliasSource(t, base)
		writeFingerprintAliasFile(t, defDir, "helper.txt", []byte("helper payload"), 0o755)
		outsideDir := t.TempDir()
		foreignPath := writeFingerprintAliasFile(t, outsideDir, "foreign.txt", []byte("foreign payload"), 0o640)
		foreignBefore, foreignMode, foreignMtime := fileSnapshot(t, foreignPath)
		foreignInfo, err := os.Stat(foreignPath)
		if err != nil {
			t.Fatal(err)
		}
		outsideInfo, err := os.Stat(outsideDir)
		if err != nil {
			t.Fatal(err)
		}
		linkPath := filepath.Join(defDir, "linked-dir")
		if err := os.Symlink(outsideDir, linkPath); err != nil {
			t.Fatal(err)
		}
		linkBefore, err := os.Lstat(linkPath)
		if err != nil {
			t.Fatal(err)
		}
		linkTarget, err := os.Readlink(linkPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("linked-dir/../helper.txt", filepath.Join(defDir, "a-alias.txt")); err != nil {
			t.Fatal(err)
		}
		requireFingerprintAliasRefusal(t, in, defDir, "a-alias.txt")
		assertFileUnchanged(t, foreignPath, foreignBefore, foreignMode, foreignMtime)
		foreignAfter, err := os.Stat(foreignPath)
		if err != nil || !os.SameFile(foreignInfo, foreignAfter) {
			t.Fatalf("foreign file identity changed: %v", err)
		}
		outsideAfter, err := os.Stat(outsideDir)
		if err != nil || !os.SameFile(outsideInfo, outsideAfter) || outsideInfo.Mode() != outsideAfter.Mode() || !outsideInfo.ModTime().Equal(outsideAfter.ModTime()) {
			t.Fatalf("foreign directory identity changed: %v", err)
		}
		linkAfter, err := os.Lstat(linkPath)
		if err != nil || !os.SameFile(linkBefore, linkAfter) || linkBefore.Mode() != linkAfter.Mode() || !linkBefore.ModTime().Equal(linkAfter.ModTime()) {
			t.Fatalf("foreign directory link changed: %v", err)
		}
		linkTargetAfter, err := os.Readlink(linkPath)
		if err != nil || linkTargetAfter != linkTarget {
			t.Fatalf("foreign directory link target = %q / %v, want %q", linkTargetAfter, err, linkTarget)
		}
	})
	t.Run("nested_sub_dotdot_resolves_same_regular_payload", func(t *testing.T) {
		in, defDir := newFingerprintAliasSource(t, base)
		want := []byte("nested helper payload")
		writeFingerprintAliasFile(t, defDir, "nested/helper.txt", want, 0o751)
		if err := os.MkdirAll(filepath.Join(defDir, "nested", "sub"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("sub/../helper.txt", filepath.Join(defDir, "nested", "alias.txt")); err != nil {
			t.Fatal(err)
		}
		aliasDigest := requireFingerprintAliasCapture(t, in, defDir, "nested/alias.txt", want, 0o751)

		regularIn, regularDir := newFingerprintAliasSource(t, base)
		writeFingerprintAliasFile(t, regularDir, "nested/helper.txt", want, 0o751)
		if err := os.MkdirAll(filepath.Join(regularDir, "nested", "sub"), 0o755); err != nil {
			t.Fatal(err)
		}
		writeFingerprintAliasFile(t, regularDir, "nested/alias.txt", want, 0o751)
		regularDigest, err := HookSourceFingerprint(regularIn)
		if err != nil {
			t.Fatal(err)
		}
		if aliasDigest != regularDigest {
			t.Fatalf("normalized nested alias fingerprint = %q, regular logical copy = %q", aliasDigest, regularDigest)
		}
	})
	t.Run("sub_alias_parent_resolves_to_contained_helper", func(t *testing.T) {
		in, defDir := newFingerprintAliasSource(t, base)
		want := []byte("parent helper payload")
		writeFingerprintAliasFile(t, defDir, "helper.txt", want, 0o751)
		if err := os.MkdirAll(filepath.Join(defDir, "sub"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("../helper.txt", filepath.Join(defDir, "sub", "alias.txt")); err != nil {
			t.Fatal(err)
		}
		aliasDigest := requireFingerprintAliasCapture(t, in, defDir, "sub/alias.txt", want, 0o751)

		regularIn, regularDir := newFingerprintAliasSource(t, base)
		writeFingerprintAliasFile(t, regularDir, "helper.txt", want, 0o751)
		if err := os.MkdirAll(filepath.Join(regularDir, "sub"), 0o755); err != nil {
			t.Fatal(err)
		}
		writeFingerprintAliasFile(t, regularDir, "sub/alias.txt", want, 0o751)
		regularDigest, err := HookSourceFingerprint(regularIn)
		if err != nil {
			t.Fatal(err)
		}
		if aliasDigest != regularDigest {
			t.Fatalf("parent alias fingerprint = %q, regular logical copy = %q", aliasDigest, regularDigest)
		}
	})
	t.Run("more_than_40_active_aliases_refuse", func(t *testing.T) {
		in, defDir := newFingerprintAliasSource(t, base)
		writeFingerprintAliasFile(t, defDir, "helper.txt", []byte("chain target"), 0o644)
		for i := 0; i <= maxContainedFingerprintAliasExpansions; i++ {
			name := "chain-" + strconv.Itoa(i) + ".txt"
			target := "helper.txt"
			if i < maxContainedFingerprintAliasExpansions {
				target = "chain-" + strconv.Itoa(i+1) + ".txt"
			}
			if err := os.Symlink(target, filepath.Join(defDir, name)); err != nil {
				t.Fatal(err)
			}
		}
		requireFingerprintAliasRefusal(t, in, defDir, "chain-0.txt")
	})
}

// spec: hook-native-location-tolerance/source-and-support-changes-invalidate-the-fingerprint
func TestHookSourceFingerprintChanges(t *testing.T) {
	v := loadHookSourceFingerprintVectors(t)
	base := v.Cases[0]
	root, defAbs := writeFingerprintSource(t, base)
	baseDigest, err := HookSourceFingerprint(fingerprintVectorInput(base, root))
	if err != nil {
		t.Fatal(err)
	}

	// A different committed relative path is part of the fingerprint; place an
	// identical definition at a second source-relative path so the change is
	// resolvable under the same source root.
	otherRel := "other/" + base.Definition.Path
	otherAbs := filepath.Join(root, filepath.FromSlash(otherRel))
	if err := os.MkdirAll(filepath.Dir(otherAbs), 0o755); err != nil {
		t.Fatal(err)
	}
	defData, err := os.ReadFile(defAbs)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(otherAbs, defData, 0o644); err != nil {
		t.Fatal(err)
	}

	mutations := map[string]func(in *HookSourceFingerprintInput){
		"source_identity": func(in *HookSourceFingerprintInput) { in.SourceIdentity += "-other" },
		"installed_name":  func(in *HookSourceFingerprintInput) { in.Name += "-other" },
		"target":          func(in *HookSourceFingerprintInput) { in.Target = "cursor" },
		"scope":           func(in *HookSourceFingerprintInput) { in.Scope = "machine" },
		"definition_path": func(in *HookSourceFingerprintInput) { in.DefinitionRel = otherRel },
	}
	for name, mutate := range mutations {
		in := fingerprintVectorInput(base, root)
		mutate(&in)
		got, err := HookSourceFingerprint(in)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got == baseDigest {
			t.Fatalf("changing %s did not change the fingerprint", name)
		}
	}

	// A definition byte change invalidates the fingerprint.
	if err := os.WriteFile(defAbs, []byte(`{"hooks":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := HookSourceFingerprint(fingerprintVectorInput(base, root))
	if err != nil {
		t.Fatal(err)
	}
	if got == baseDigest {
		t.Fatal("changed definition bytes did not change the fingerprint")
	}
}

func TestHookSourceFingerprintFailsClosed(t *testing.T) {
	v := loadHookSourceFingerprintVectors(t)
	base := v.Cases[0]
	root, defAbs := writeFingerprintSource(t, base)

	cases := map[string]HookSourceFingerprintInput{}
	for name, rel := range map[string]string{
		"traversal":   "../escape/hooks.json",
		"absolute":    "/abs/hooks.json",
		"empty_rel":   "",
		"unclean_rel": "hooks/../hooks/hooks.json",
	} {
		in := fingerprintVectorInput(base, root)
		in.DefinitionRel = rel
		cases[name] = in
	}
	{
		in := fingerprintVectorInput(base, root)
		in.SourceRoot = filepath.Join(t.TempDir(), "absent")
		cases["missing_source_root"] = in
	}
	{
		in := fingerprintVectorInput(base, root)
		in.DefinitionRel = "hooks/absent.json"
		cases["missing_definition"] = in
	}
	{
		in := fingerprintVectorInput(base, root)
		in.Scope = "user"
		cases["bad_scope"] = in
	}
	{
		in := fingerprintVectorInput(base, "")
		cases["empty_source_root"] = in
	}
	for name, in := range cases {
		if got, err := HookSourceFingerprint(in); err == nil || got != "" {
			t.Fatalf("%s: fingerprint = %q err = %v, want no match", name, got, err)
		}
	}

	// A symlinked definition must refuse rather than follow.
	target := filepath.Join(t.TempDir(), "real.json")
	if err := os.WriteFile(target, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	linkRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(linkRoot, "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(linkRoot, "hooks", "hooks.json")); err != nil {
		t.Fatal(err)
	}
	in := fingerprintVectorInput(base, linkRoot)
	if got, err := HookSourceFingerprint(in); err == nil || got != "" {
		t.Fatalf("symlinked definition: fingerprint = %q err = %v, want refusal", got, err)
	}

	defDir := filepath.Dir(defAbs)
	escape := filepath.Join(t.TempDir(), "outside.sh")
	if err := os.WriteFile(escape, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(escape, filepath.Join(defDir, "escape.sh")); err != nil {
		t.Fatal(err)
	}
	in = fingerprintVectorInput(base, root)
	if got, err := HookSourceFingerprint(in); err == nil || got != "" {
		t.Fatalf("symlink in definition dir: fingerprint = %q err = %v, want refusal", got, err)
	}
}

// TestHookSourceFingerprintDescriptorTraversal covers the descriptor-level
// races and special files a path-based Lstat+ReadFile cannot defend: a
// symlinked parent directory escaping the source root, a final-component
// symlink, a FIFO that must fail rather than block, and oversized files that
// must refuse without a matching allocation.
// spec: hook-native-location-tolerance/source-and-support-changes-invalidate-the-fingerprint
func TestHookSourceFingerprintDescriptorTraversal(t *testing.T) {
	v := loadHookSourceFingerprintVectors(t)
	base := v.Cases[0]

	// A symlinked parent directory must refuse and must never read the
	// external tree.
	t.Run("parent_symlink_escape", func(t *testing.T) {
		outside := t.TempDir()
		externalBytes := []byte(`{"external":true}`)
		if err := os.MkdirAll(filepath.Join(outside, "hooks"), 0o755); err != nil {
			t.Fatal(err)
		}
		externalDef := filepath.Join(outside, "hooks", "hooks.json")
		if err := os.WriteFile(externalDef, externalBytes, 0o644); err != nil {
			t.Fatal(err)
		}
		root := t.TempDir()
		if err := os.Symlink(filepath.Join(outside, "hooks"), filepath.Join(root, "hooks")); err != nil {
			t.Fatal(err)
		}
		in := fingerprintVectorInput(base, root)
		if got, err := HookSourceFingerprint(in); err == nil || got != "" {
			t.Fatalf("parent symlink: fingerprint = %q err = %v, want refusal", got, err)
		}
		after, err := os.ReadFile(externalDef)
		if err != nil || string(after) != string(externalBytes) {
			t.Fatalf("external file mutated or unreadable: %q err=%v", after, err)
		}
	})

	// A symlink at the source root itself must refuse; opening it with
	// O_DIRECTORY alone would follow the link and hash the external tree.
	t.Run("root_symlink_escape", func(t *testing.T) {
		outside := t.TempDir()
		externalBytes := []byte(`{"external":true}`)
		if err := os.MkdirAll(filepath.Join(outside, "hooks"), 0o755); err != nil {
			t.Fatal(err)
		}
		externalDef := filepath.Join(outside, "hooks", "hooks.json")
		if err := os.WriteFile(externalDef, externalBytes, 0o644); err != nil {
			t.Fatal(err)
		}
		linkRoot := filepath.Join(t.TempDir(), "linked-root")
		if err := os.Symlink(outside, linkRoot); err != nil {
			t.Fatal(err)
		}
		in := fingerprintVectorInput(base, linkRoot)
		if got, err := HookSourceFingerprint(in); err == nil || got != "" {
			t.Fatalf("root symlink: fingerprint = %q err = %v, want refusal", got, err)
		}
		after, err := os.ReadFile(externalDef)
		if err != nil || string(after) != string(externalBytes) {
			t.Fatalf("external file mutated or unreadable: %q err=%v", after, err)
		}
	})

	// A definition directory with more regular files than the existing
	// canonical archive file budget must fail closed instead of enumerating
	// without bound.
	t.Run("excessive_entries_refused", func(t *testing.T) {
		root, _ := writeFingerprintSource(t, base)
		defDir := filepath.Join(root, filepath.FromSlash(filepath.Dir(base.Definition.Path)))
		limit := int(artifact.DefaultArchiveLimits().MaxTotalFiles)
		for i := 0; i <= limit; i++ {
			name := filepath.Join(defDir, "extra-"+strconv.Itoa(i)+".txt")
			if err := os.WriteFile(name, []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		in := fingerprintVectorInput(base, root)
		if got, err := HookSourceFingerprint(in); err == nil || got != "" {
			t.Fatalf("excessive entries: fingerprint = %q err = %v, want refusal", got, err)
		}
	})

	// A symlinked directory deeper in the support tree must refuse.
	t.Run("nested_symlink_escape", func(t *testing.T) {
		root, _ := writeFingerprintSource(t, base)
		outside := t.TempDir()
		if err := os.WriteFile(filepath.Join(outside, "x.sh"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		defDir := filepath.Join(root, filepath.FromSlash(filepath.Dir(base.Definition.Path)))
		if err := os.Symlink(outside, filepath.Join(defDir, "linked")); err != nil {
			t.Fatal(err)
		}
		in := fingerprintVectorInput(base, root)
		if got, err := HookSourceFingerprint(in); err == nil || got != "" {
			t.Fatalf("nested symlink: fingerprint = %q err = %v, want refusal", got, err)
		}
	})

	// A FIFO (or other non-regular special file) must refuse promptly rather
	// than block a reader.
	t.Run("fifo_does_not_block", func(t *testing.T) {
		root, _ := writeFingerprintSource(t, base)
		defDir := filepath.Join(root, filepath.FromSlash(filepath.Dir(base.Definition.Path)))
		if err := unix.Mkfifo(filepath.Join(defDir, "pipe.sh"), 0o644); err != nil {
			t.Skipf("cannot create fifo: %v", err)
		}
		in := fingerprintVectorInput(base, root)
		type result struct {
			got string
			err error
		}
		done := make(chan result, 1)
		go func() {
			got, err := HookSourceFingerprint(in)
			done <- result{got, err}
		}()
		select {
		case r := <-done:
			if r.err == nil || r.got != "" {
				t.Fatalf("fifo: fingerprint = %q err = %v, want refusal", r.got, r.err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("fingerprint blocked on FIFO")
		}
	})

	// A file whose grown size exceeds the ceiling must refuse without a
	// matching allocation; a sparse file makes the size cheap to declare.
	t.Run("oversized_support_refused", func(t *testing.T) {
		root, _ := writeFingerprintSource(t, base)
		defDir := filepath.Join(root, filepath.FromSlash(filepath.Dir(base.Definition.Path)))
		big := filepath.Join(defDir, "huge.bin")
		f, err := os.Create(big)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.Truncate(hookSourceFingerprintMaxBytes + 1); err != nil {
			_ = f.Close()
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		in := fingerprintVectorInput(base, root)
		if got, err := HookSourceFingerprint(in); err == nil || got != "" {
			t.Fatalf("oversized support: fingerprint = %q err = %v, want refusal", got, err)
		}
	})
}

// hookSourceMirrorFingerprintVector mirrors one independently calculated case
// in testdata/hook-source-fingerprint-mirror-v1.json. Expected digests are
// committed constants: the implementation must match them, never derive them.
type hookSourceMirrorFingerprintVector struct {
	Name          string `json:"name"`
	InstanceID    string `json:"instance_id"`
	RegistryID    string `json:"registry_id"`
	Kind          string `json:"kind"`
	ResourceName  string `json:"resource_name"`
	Digest        string `json:"digest"`
	InstalledName string `json:"installed_name"`
	Target        string `json:"target"`
	Scope         string `json:"scope"`
	Fingerprint   string `json:"fingerprint"`
	ExpectError   bool   `json:"expect_error"`
}

type hookSourceMirrorFingerprintVectors struct {
	Version   int                                 `json:"version"`
	Algorithm string                              `json:"algorithm"`
	Cases     []hookSourceMirrorFingerprintVector `json:"cases"`
	Distinct  [][]string                          `json:"distinct_pairs"`
	DomainSep struct {
		Case               string `json:"case"`
		SourceDomainDigest string `json:"source_domain_digest"`
	} `json:"domain_separation"`
}

func loadHookSourceMirrorFingerprintVectors(t *testing.T) hookSourceMirrorFingerprintVectors {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "hook-source-fingerprint-mirror-v1.json"))
	if err != nil {
		t.Fatalf("read mirror vectors: %v", err)
	}
	var v hookSourceMirrorFingerprintVectors
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("parse mirror vectors: %v", err)
	}
	if v.Version != 1 {
		t.Fatalf("mirror vector version = %d, want 1", v.Version)
	}
	if len(v.Cases) == 0 {
		t.Fatal("mirror vectors contain no cases")
	}
	return v
}

func mirrorVectorInput(c hookSourceMirrorFingerprintVector) HookSourceMirrorFingerprintInput {
	return HookSourceMirrorFingerprintInput{
		InstanceID:    c.InstanceID,
		RegistryID:    c.RegistryID,
		Kind:          c.Kind,
		ResourceName:  c.ResourceName,
		Digest:        c.Digest,
		InstalledName: c.InstalledName,
		Target:        c.Target,
		Scope:         c.Scope,
	}
}

// spec: hook-native-location-tolerance/verified-mirror-revision-uses-its-own-fingerprint
// spec: hook-native-location-tolerance/unverified-or-changed-mirror-revision-never-satisfies
func TestHookSourceMirrorFingerprintVectors(t *testing.T) {
	v := loadHookSourceMirrorFingerprintVectors(t)
	digests := make(map[string]string, len(v.Cases))
	for _, c := range v.Cases {
		t.Run(c.Name, func(t *testing.T) {
			got, err := HookSourceMirrorFingerprint(mirrorVectorInput(c))
			if c.ExpectError {
				if err == nil || got != "" {
					t.Fatalf("fingerprint = %q err = %v, want failure", got, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("HookSourceMirrorFingerprint: %v", err)
			}
			if got != c.Fingerprint {
				t.Fatalf("fingerprint = %s, want committed vector %s", got, c.Fingerprint)
			}
			digests[c.Name] = got
		})
	}
	for _, pair := range v.Distinct {
		if len(pair) != 2 {
			t.Fatalf("distinct pair %v must name two cases", pair)
		}
		a, b := digests[pair[0]], digests[pair[1]]
		if a == "" || b == "" {
			t.Fatalf("distinct pair %v references an unknown case", pair)
		}
		if a == b {
			t.Fatalf("cases %s and %s must not share the fingerprint", pair[0], pair[1])
		}
	}
	// Domain separation: hashing the identical field tuple under the
	// source-tree domain yields the committed cross-domain digest, which the
	// mirror fingerprint must never equal.
	base := digests[v.DomainSep.Case]
	if base == "" {
		t.Fatalf("domain separation references unknown case %q", v.DomainSep.Case)
	}
	if v.DomainSep.SourceDomainDigest == "" || base == v.DomainSep.SourceDomainDigest {
		t.Fatalf("mirror fingerprint %q collides with the source-domain digest %q", base, v.DomainSep.SourceDomainDigest)
	}
}

func TestHookSourceMirrorFingerprintFailsClosed(t *testing.T) {
	v := loadHookSourceMirrorFingerprintVectors(t)
	var base hookSourceMirrorFingerprintVector
	for _, c := range v.Cases {
		if !c.ExpectError {
			base = c
			break
		}
	}
	mutations := map[string]func(in *HookSourceMirrorFingerprintInput){
		"missing_instance":  func(in *HookSourceMirrorFingerprintInput) { in.InstanceID = "" },
		"missing_registry":  func(in *HookSourceMirrorFingerprintInput) { in.RegistryID = "" },
		"missing_kind":      func(in *HookSourceMirrorFingerprintInput) { in.Kind = "" },
		"missing_name":      func(in *HookSourceMirrorFingerprintInput) { in.ResourceName = "" },
		"missing_installed": func(in *HookSourceMirrorFingerprintInput) { in.InstalledName = "" },
		"missing_target":    func(in *HookSourceMirrorFingerprintInput) { in.Target = "" },
		"bad_scope":         func(in *HookSourceMirrorFingerprintInput) { in.Scope = "user" },
		"non_sha256_digest": func(in *HookSourceMirrorFingerprintInput) { in.Digest = "md5:abc" },
		"short_digest":      func(in *HookSourceMirrorFingerprintInput) { in.Digest = "sha256:ab" },
		"uppercase_digest": func(in *HookSourceMirrorFingerprintInput) {
			in.Digest = "sha256:" + strings.ToUpper("abababababababababababababababababababababababababababababababab")
		},
		"unprefixed_digest": func(in *HookSourceMirrorFingerprintInput) { in.Digest = strings.Repeat("ab", 32) },
	}
	for name, mutate := range mutations {
		in := mirrorVectorInput(base)
		mutate(&in)
		if got, err := HookSourceMirrorFingerprint(in); err == nil || got != "" {
			t.Fatalf("%s: fingerprint = %q err = %v, want failure", name, got, err)
		}
	}
}

func TestHookSourceFingerprintOnlyDigests(t *testing.T) {
	// The fingerprint is the only thing that may reach ownership state; it must
	// carry no source plaintext beyond a sha256 digest shape.
	v := loadHookSourceFingerprintVectors(t)
	base := v.Cases[0]
	root, _ := writeFingerprintSource(t, base)
	got, err := HookSourceFingerprint(fingerprintVectorInput(base, root))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "sha256:") || len(got) != len("sha256:")+64 {
		t.Fatalf("fingerprint = %q, want sha256:<64 hex>", got)
	}
	defBytes, _ := base64.StdEncoding.DecodeString(base.Definition.BytesB64)
	if strings.Contains(got, string(defBytes)) {
		t.Fatal("fingerprint leaks definition plaintext")
	}
}
