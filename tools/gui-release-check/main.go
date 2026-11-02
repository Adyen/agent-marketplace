package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

var signingPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)developer[ -]?id`),
	regexp.MustCompile(`(?i)(apple[_ -]?team[_ -]?id|team[_ -]?id|development[_ -]?team)\s*[:=]\s*[A-Z0-9]{10}`),
	regexp.MustCompile(`(?i)(APPLE|MACOS|DEVELOPER|SIGNING)[A-Z0-9_]*(IDENTITY|CERTIFICATE|TEAM|SECRET|PRIVATE_KEY)`),
	regexp.MustCompile(`(?i)secrets\.[A-Z0-9_]*(APPLE|MACOS|DEVELOPER|SIGNING|CERTIFICATE|TEAM|PRIVATE_KEY)`),
	regexp.MustCompile(`(?i)(signing|developer|apple)[-_ /][^\r\n]*\.(p12|pfx|p8|mobileprovision)\b`),
	regexp.MustCompile(`(?i)(--identity|--keychain|--signing-key|--certificate)\b`),
}

var codesignIdentity = regexp.MustCompile(`(?i)codesign\b[^\n]*?(?:--sign|-s)\s+(['"]?)([^\s'"]+)`)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("expected check-matrix, audit-signing, snapshot, verify-digests, or compare-cli")
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	dir := flags.String("dir", "", "artifact directory")
	root := flags.String("root", "", "repository root")
	tag := flags.String("tag", "", "release tag")
	actual := flags.String("actual", "", "published release directory")
	expected := flags.String("expected", "", "make release output directory")
	output := flags.String("output", "", "digest snapshot output path")
	snapshot := flags.String("snapshot", "", "digest snapshot input path")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	switch args[0] {
	case "check-matrix":
		if *dir == "" || *tag == "" {
			return errors.New("check-matrix requires --dir and --tag")
		}
		return checkMatrix(*dir, *tag)
	case "audit-signing":
		if *root == "" {
			return errors.New("audit-signing requires --root")
		}
		return auditSigningMaterial(*root)
	case "snapshot":
		if *dir == "" || *output == "" {
			return errors.New("snapshot requires --dir and --output")
		}
		digests, err := artifactDigests(*dir)
		if err != nil {
			return err
		}
		data, err := json.Marshal(digests)
		if err != nil {
			return err
		}
		return os.WriteFile(*output, append(data, '\n'), 0o600)
	case "verify-digests":
		if *dir == "" || *snapshot == "" {
			return errors.New("verify-digests requires --dir and --snapshot")
		}
		data, err := os.ReadFile(*snapshot)
		if err != nil {
			return err
		}
		var expected map[string]string
		if err := json.Unmarshal(data, &expected); err != nil {
			return err
		}
		return verifyArtifactDigests(*dir, expected)
	case "compare-cli":
		if *actual == "" || *expected == "" || *tag == "" {
			return errors.New("compare-cli requires --actual, --expected, and --tag")
		}
		return compareCLIAssets(*actual, *expected, *tag)
	default:
		return fmt.Errorf("unknown check %q", args[0])
	}
}

func checkMatrix(dir, tag string) error {
	expected := []string{
		"agent-marketplace-gui-" + tag + "-macos-universal.dmg",
		"agent-marketplace-gui-" + tag + "-macos-universal.zip",
		"agent-marketplace-gui-" + tag + "-linux-amd64.rpm",
		"agent-marketplace-gui-" + tag + "-linux-amd64.deb",
		"agent-marketplace-gui-" + tag + "-linux-arm64.rpm",
		"agent-marketplace-gui-" + tag + "-linux-arm64.deb",
	}
	for _, name := range expected {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			return fmt.Errorf("required GUI release asset %q: %w", name, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("required GUI release asset %q is not a regular file", name)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "SHA256SUMS")); err == nil {
		return verifyPackageChecksums(dir, expected)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func verifyPackageChecksums(dir string, assets []string) error {
	data, err := os.ReadFile(filepath.Join(dir, "SHA256SUMS"))
	if err != nil {
		return err
	}
	expected := make(map[string]bool, len(assets))
	for _, name := range assets {
		expected[name] = true
	}
	found := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		name := strings.TrimPrefix(fields[1], "*")
		if !expected[name] {
			continue
		}
		if found[name] {
			return fmt.Errorf("duplicate GUI checksum for %q", name)
		}
		digest, err := hex.DecodeString(fields[0])
		if err != nil || len(digest) != sha256.Size {
			return fmt.Errorf("invalid GUI checksum for %q", name)
		}
		content, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		actual := sha256.Sum256(content)
		if !slices.Equal(digest, actual[:]) {
			return fmt.Errorf("GUI checksum does not match %q", name)
		}
		found[name] = true
	}
	for name := range expected {
		if !found[name] {
			return fmt.Errorf("missing GUI checksum for %q", name)
		}
	}
	return nil
}

func auditSigningMaterial(root string) error {
	paths := []string{
		filepath.Join(root, ".github", "workflows"),
		filepath.Join(root, "packaging"),
	}
	for _, path := range paths {
		if err := filepath.WalkDir(path, func(name string, entry fs.DirEntry, err error) error {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			if !signingConfigFile(name) {
				return nil
			}
			data, err := os.ReadFile(name)
			if err != nil {
				return err
			}
			if err := auditSigningText(name, string(data)); err != nil {
				return err
			}
			return nil
		}); err != nil {
			return err
		}
	}
	makefile := filepath.Join(root, "Makefile")
	if data, err := os.ReadFile(makefile); err == nil {
		if err := auditSigningText(makefile, string(data)); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	scripts := filepath.Join(root, "scripts")
	if err := filepath.WalkDir(scripts, func(name string, entry fs.DirEntry, err error) error {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasPrefix(filepath.Base(name), "gui-") || filepath.Ext(name) != ".sh" {
			return nil
		}
		data, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		return auditSigningText(name, string(data))
	}); err != nil {
		return err
	}
	return nil
}

func signingConfigFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yml", ".yaml", ".json", ".toml", ".plist", ".sh":
		return true
	default:
		return false
	}
}

func auditSigningText(path, text string) error {
	for _, pattern := range signingPatterns {
		if pattern.MatchString(text) {
			return fmt.Errorf("signing material found in %s", path)
		}
	}
	for _, match := range codesignIdentity.FindAllStringSubmatch(text, -1) {
		if len(match) > 2 && match[2] != "-" {
			return fmt.Errorf("signing identity found in %s", path)
		}
	}
	return nil
}

func artifactDigests(dir string) (map[string]string, error) {
	digests := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("artifact %q is not a regular file", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(data)
		digests[filepath.ToSlash(relative)] = hex.EncodeToString(digest[:])
		return nil
	})
	return digests, err
}

func verifyArtifactDigests(dir string, expected map[string]string) error {
	actual, err := artifactDigests(dir)
	if err != nil {
		return err
	}
	if len(actual) != len(expected) {
		return fmt.Errorf("artifact file count changed: got %d, want %d", len(actual), len(expected))
	}
	for name, digest := range expected {
		if actual[name] != digest {
			return fmt.Errorf("artifact digest changed for %q", name)
		}
	}
	return nil
}

func compareCLIAssets(actualDir, expectedDir, tag string) error {
	prefix := "agent-marketplace-" + tag + "-"
	expectedNames, err := assetNames(expectedDir, prefix)
	if err != nil {
		return err
	}
	actualNames, err := assetNames(actualDir, prefix)
	if err != nil {
		return err
	}
	if len(expectedNames) == 0 || !slices.Equal(actualNames, expectedNames) {
		return fmt.Errorf("CLI asset names differ: got %v, want %v", actualNames, expectedNames)
	}
	for _, name := range expectedNames {
		want, err := os.ReadFile(filepath.Join(expectedDir, name))
		if err != nil {
			return err
		}
		got, err := os.ReadFile(filepath.Join(actualDir, name))
		if err != nil {
			return err
		}
		if !slices.Equal(got, want) {
			return fmt.Errorf("CLI asset bytes differ for %q", name)
		}
	}
	expectedLines, err := checksumLines(filepath.Join(expectedDir, "SHA256SUMS"), prefix)
	if err != nil {
		return err
	}
	actualLines, err := checksumLines(filepath.Join(actualDir, "SHA256SUMS"), prefix)
	if err != nil {
		return err
	}
	if !slices.Equal(actualLines, expectedLines) {
		return fmt.Errorf("CLI SHA256SUMS lines differ: got %v, want %v", actualLines, expectedLines)
	}
	return nil
}

func assetNames(dir, prefix string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if entry.Type().IsRegular() && strings.HasPrefix(entry.Name(), prefix) {
			names = append(names, entry.Name())
		}
	}
	return names, nil
}

func checksumLines(path, prefix string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.HasPrefix(strings.TrimPrefix(fields[1], "*"), prefix) {
			lines = append(lines, line)
		}
	}
	return lines, nil
}
