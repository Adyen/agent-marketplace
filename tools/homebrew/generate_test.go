package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var wantPlatforms = []string{"darwin-arm64", "darwin-amd64", "linux-amd64", "linux-arm64"}

func assetNameFor(tag, suffix string) string {
	return "agent-marketplace-" + tag + "-" + suffix
}

func wantIndex(suffix string) int {
	for i, p := range wantPlatforms {
		if p == suffix {
			return i
		}
	}
	return -1
}

func writeFixture(t *testing.T, mutate func(*releaseFixture)) (releaseJSON, dist string) {
	t.Helper()
	dir := t.TempDir()
	dist = filepath.Join(dir, "dist")
	if err := os.MkdirAll(dist, 0o755); err != nil {
		t.Fatal(err)
	}
	f := &releaseFixture{tag: "v1.2.3", ids: map[string]int64{}}
	for i, suffix := range wantPlatforms {
		name := assetNameFor(f.tag, suffix)
		if err := os.WriteFile(filepath.Join(dist, name), []byte("binary-"+suffix), 0o755); err != nil {
			t.Fatal(err)
		}
		f.ids[name] = int64(1000 + i)
		f.names = append(f.names, name)
	}
	if mutate != nil {
		mutate(f)
	}
	releaseJSON = filepath.Join(dir, "release.json")
	var assets []map[string]any
	for _, name := range f.names {
		assets = append(assets, map[string]any{"id": f.ids[name], "name": name})
	}
	doc := map[string]any{
		"tag_name":   f.tag,
		"draft":      f.draft,
		"prerelease": f.prerelease,
		"assets":     assets,
	}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(releaseJSON, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return releaseJSON, dist
}

type releaseFixture struct {
	tag        string
	draft      bool
	prerelease bool
	names      []string
	ids        map[string]int64
}

func outputTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// spec: homebrew-packaging/formula-selects-each-supported-release-binary
func TestGenerateSelectsEachSupportedBinary(t *testing.T) {
	for _, visibility := range []string{"public", "private", "internal"} {
		t.Run(visibility, func(t *testing.T) {
			releaseJSON, dist := writeFixture(t, nil)
			out := filepath.Join(t.TempDir(), "tap")
			if err := generate("Adyen/agent-marketplace", "v1.2.3", releaseJSON, dist, out, visibility); err != nil {
				t.Fatalf("generate: %v", err)
			}
			tree := outputTree(t, out)
			if len(tree) != 2 {
				t.Fatalf("output paths = %v, want exactly Formula/agent-marketplace.rb and VERSION", tree)
			}
			formula, ok := tree["Formula/agent-marketplace.rb"]
			if !ok {
				t.Fatalf("missing formula; got %v", tree)
			}
			if tree["VERSION"] != "1.2.3\n" {
				t.Errorf("VERSION = %q, want %q", tree["VERSION"], "1.2.3\n")
			}
			repo := "Adyen/agent-marketplace"
			for _, suffix := range wantPlatforms {
				name := assetNameFor("v1.2.3", suffix)
				if visibility == "public" {
					want := fmt.Sprintf("https://github.com/%s/releases/download/v1.2.3/%s", repo, name)
					if !strings.Contains(formula, want) {
						t.Errorf("formula missing canonical url %s", want)
					}
				} else {
					want := fmt.Sprintf("https://api.github.com/repos/%s/releases/assets/100%d", repo, wantIndex(suffix))
					if !strings.Contains(formula, want) {
						t.Errorf("formula missing asset API url %s", want)
					}
				}
			}
			out2 := filepath.Join(t.TempDir(), "tap")
			if err := generate("Adyen/agent-marketplace", "v1.2.3", releaseJSON, dist, out2, visibility); err != nil {
				t.Fatalf("regenerate: %v", err)
			}
			for p, content := range outputTree(t, out2) {
				if tree[p] != content {
					t.Errorf("repeated generation differs for %s", p)
				}
			}
		})
	}
}

// spec: homebrew-packaging/invalid-release-data-produces-no-formula
func TestGenerateInvalidInputsProduceNoFormula(t *testing.T) {
	cases := []struct {
		name       string
		repository string
		tag        string
		visibility string
		explicit   bool
		mutate     func(*releaseFixture)
		mutateFS   func(t *testing.T, releaseJSON, dist string)
	}{
		{name: "missing v prefix", tag: "1.2.3"},
		{name: "prerelease tag", tag: "v1.2.3-rc.1"},
		{name: "four-part tag", tag: "v1.2.3.4"},
		{name: "non-numeric tag", tag: "vx.y.z"},
		{name: "empty tag", explicit: true, repository: "Adyen/agent-marketplace", tag: "", visibility: "public"},
		{name: "overflow tag", tag: "v99999999999999999999.0.0"},
		{name: "empty owner", repository: "/agent-marketplace"},
		{name: "missing repo", repository: "Adyen"},
		{name: "path traversal repo", repository: "Adyen/../x"},
		{name: "dot repository", repository: "o/."},
		{name: "dotdot repository", repository: "o/.."},
		{name: "empty repository", explicit: true, repository: "", tag: "v1.2.3", visibility: "public"},
		{name: "bad visibility", visibility: "bogus"},
		{name: "tag mismatch", mutate: func(f *releaseFixture) { f.tag = "v9.9.9" }},
		{name: "draft", mutate: func(f *releaseFixture) { f.draft = true }},
		{name: "prerelease", mutate: func(f *releaseFixture) { f.prerelease = true }},
		{name: "missing asset", mutate: func(f *releaseFixture) {
			name := assetNameFor("v1.2.3", "linux-arm64")
			delete(f.ids, name)
			for i, n := range f.names {
				if n == name {
					f.names = append(f.names[:i], f.names[i+1:]...)
				}
			}
		}},
		{name: "duplicate required name", mutate: func(f *releaseFixture) {
			f.names = append(f.names, f.names[0])
		}},
		{name: "zero asset id", mutate: func(f *releaseFixture) {
			f.ids[assetNameFor("v1.2.3", "darwin-arm64")] = 0
		}},
		{name: "negative asset id", mutate: func(f *releaseFixture) {
			f.ids[assetNameFor("v1.2.3", "darwin-arm64")] = -5
		}},
		{name: "duplicate asset id", mutate: func(f *releaseFixture) {
			f.ids[assetNameFor("v1.2.3", "darwin-arm64")] = f.ids[assetNameFor("v1.2.3", "darwin-amd64")]
		}},
		{name: "unreadable release json", mutateFS: func(t *testing.T, rj, d string) {
			t.Helper()
			if err := os.Remove(rj); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "malformed release json", mutateFS: func(t *testing.T, rj, d string) {
			t.Helper()
			if err := os.WriteFile(rj, []byte("{not json"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "missing binary", mutateFS: func(t *testing.T, rj, d string) {
			t.Helper()
			if err := os.Remove(filepath.Join(d, assetNameFor("v1.2.3", "linux-amd64"))); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "nonregular binary", mutateFS: func(t *testing.T, rj, d string) {
			t.Helper()
			p := filepath.Join(d, assetNameFor("v1.2.3", "linux-amd64"))
			if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(p, 0o755); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repository := tc.repository
			tag := tc.tag
			visibility := tc.visibility
			if !tc.explicit {
				if repository == "" {
					repository = "Adyen/agent-marketplace"
				}
				if tag == "" {
					tag = "v1.2.3"
				}
				if visibility == "" {
					visibility = "public"
				}
			}
			releaseJSON, dist := writeFixture(t, tc.mutate)
			if tc.mutateFS != nil {
				tc.mutateFS(t, releaseJSON, dist)
			}
			out := filepath.Join(t.TempDir(), "tap")
			if err := os.MkdirAll(filepath.Join(out, "Formula"), 0o755); err != nil {
				t.Fatal(err)
			}
			sentinel := map[string]string{
				"VERSION":                      "0.0.0\n",
				"Formula/agent-marketplace.rb": "sentinel-formula",
				"Formula/other.rb":             "unrelated",
			}
			for rel, content := range sentinel {
				if err := os.WriteFile(filepath.Join(out, rel), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			before := outputTree(t, out)
			err := generate(repository, tag, releaseJSON, dist, out, visibility)
			if err == nil {
				t.Fatalf("generate succeeded; want failure")
			}
			after := outputTree(t, out)
			if len(after) != len(before) {
				t.Fatalf("output changed on failure: before %v after %v", before, after)
			}
			for rel, content := range sentinel {
				if after[rel] != content {
					t.Errorf("pre-existing output %s changed: %q", rel, after[rel])
				}
			}
		})
	}
}

// spec: homebrew-packaging/invalid-release-data-produces-no-formula
func TestGenerateExtraAssetsIgnored(t *testing.T) {
	releaseJSON, dist := writeFixture(t, func(f *releaseFixture) {
		extra := "agent-marketplace-v1.2.3-checksums.txt"
		f.names = append(f.names, extra)
		f.ids[extra] = 7777
	})
	out := filepath.Join(t.TempDir(), "tap")
	if err := generate("Adyen/agent-marketplace", "v1.2.3", releaseJSON, dist, out, "public"); err != nil {
		t.Fatalf("extra unrelated asset must be ignored: %v", err)
	}
	tree := outputTree(t, out)
	if len(tree) != 2 {
		t.Fatalf("output paths = %v, want exactly two owned paths", tree)
	}
	if n := strings.Count(tree["Formula/agent-marketplace.rb"], "url \"https://github.com"); n != 4 {
		t.Errorf("formula has %d platform urls, want exactly 4", n)
	}
}
