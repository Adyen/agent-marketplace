package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var platforms = []string{"darwin-arm64", "darwin-amd64", "linux-amd64", "linux-arm64"}

var (
	tagPattern        = regexp.MustCompile(`^v([0-9]+)\.([0-9]+)\.([0-9]+)$`)
	repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})/[A-Za-z0-9._-]{1,100}$`)
)

func validateRepository(repository string) error {
	if !repositoryPattern.MatchString(repository) {
		return fmt.Errorf("invalid repository coordinate %q", repository)
	}
	if name := repository[strings.LastIndexByte(repository, '/')+1:]; name == "." || name == ".." {
		return fmt.Errorf("invalid repository coordinate %q", repository)
	}
	return nil
}

type releaseDoc struct {
	TagName    string         `json:"tag_name"`
	Draft      bool           `json:"draft"`
	Prerelease bool           `json:"prerelease"`
	Assets     []releaseAsset `json:"assets"`
}

type releaseAsset struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

func assetName(tag, platform string) string {
	return "agent-marketplace-" + tag + "-" + platform
}

func parseStableTag(tag string) ([3]uint64, error) {
	var v [3]uint64
	m := tagPattern.FindStringSubmatch(tag)
	if m == nil {
		return v, fmt.Errorf("tag %q is not an exact vMAJOR.MINOR.PATCH stable tag", tag)
	}
	for i := 0; i < 3; i++ {
		n, err := strconv.ParseUint(m[i+1], 10, 32)
		if err != nil {
			return v, fmt.Errorf("tag %q has an invalid numeric component", tag)
		}
		v[i] = n
	}
	return v, nil
}

func parseVersionNumber(s string) ([3]uint64, error) {
	v, err := parseStableTag("v" + strings.TrimRight(s, "\n"))
	if err != nil {
		return v, fmt.Errorf("version %q is not MAJOR.MINOR.PATCH", strings.TrimRight(s, "\n"))
	}
	return v, nil
}

func compareVersions(a, b [3]uint64) int {
	for i := 0; i < 3; i++ {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

type assetInfo struct {
	id     int64
	sha256 string
}

func validateRelease(tag string, doc releaseDoc) (map[string]assetInfo, error) {
	if doc.TagName != tag {
		return nil, fmt.Errorf("release tag %q does not match requested tag %q", doc.TagName, tag)
	}
	if doc.Draft {
		return nil, fmt.Errorf("release %q is a draft", tag)
	}
	if doc.Prerelease {
		return nil, fmt.Errorf("release %q is a prerelease", tag)
	}
	want := map[string]bool{}
	for _, p := range platforms {
		want[assetName(tag, p)] = true
	}
	seen := map[string]int64{}
	for _, a := range doc.Assets {
		if !want[a.Name] {
			continue
		}
		if _, dup := seen[a.Name]; dup {
			return nil, fmt.Errorf("release %q has duplicate required asset %q", tag, a.Name)
		}
		seen[a.Name] = a.ID
	}
	if len(seen) != len(platforms) {
		var missing []string
		for name := range want {
			if _, ok := seen[name]; !ok {
				missing = append(missing, name)
			}
		}
		sort.Strings(missing)
		return nil, fmt.Errorf("release %q is missing required assets: %s", tag, strings.Join(missing, ", "))
	}
	ids := map[int64]string{}
	for _, a := range doc.Assets {
		if !want[a.Name] {
			continue
		}
		if a.ID <= 0 {
			return nil, fmt.Errorf("asset %q has nonpositive id %d", a.Name, a.ID)
		}
		if other, dup := ids[a.ID]; dup {
			return nil, fmt.Errorf("assets %q and %q share id %d", other, a.Name, a.ID)
		}
		ids[a.ID] = a.Name
	}
	out := map[string]assetInfo{}
	for name, id := range seen {
		out[name] = assetInfo{id: id}
	}
	return out, nil
}

func hashBinaries(tag, distDir string, assets map[string]assetInfo) error {
	for _, p := range platforms {
		name := assetName(tag, p)
		path := filepath.Join(distDir, name)
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("release binary %s: %w", name, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("release binary %s is not a regular file", name)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read binary %s: %w", name, err)
		}
		a := assets[name]
		sum := sha256.Sum256(data)
		a.sha256 = hex.EncodeToString(sum[:])
		assets[name] = a
	}
	return nil
}

func generate(repository, tag, releaseJSONPath, distDir, outDir, visibility string) error {
	if err := validateRepository(repository); err != nil {
		return err
	}
	version, err := parseStableTag(tag)
	if err != nil {
		return err
	}
	if visibility != "public" && visibility != "private" && visibility != "internal" {
		return fmt.Errorf("invalid visibility %q", visibility)
	}
	data, err := os.ReadFile(releaseJSONPath)
	if err != nil {
		return fmt.Errorf("read release metadata: %w", err)
	}
	var doc releaseDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parse release metadata: %w", err)
	}
	assets, err := validateRelease(tag, doc)
	if err != nil {
		return err
	}
	if err := hashBinaries(tag, distDir, assets); err != nil {
		return err
	}
	versionText := fmt.Sprintf("%d.%d.%d", version[0], version[1], version[2])
	formula := renderFormula(repository, tag, versionText, visibility, assets)
	files := map[string]string{
		filepath.Join("Formula", "agent-marketplace.rb"): formula,
		"VERSION": versionText + "\n",
	}
	for rel, content := range files {
		path := filepath.Join(outDir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", rel, err)
		}
	}
	return nil
}
