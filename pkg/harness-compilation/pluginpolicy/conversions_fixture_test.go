package pluginpolicy_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	harnesscompile "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/plugindecode"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/pluginpolicy"
	"gopkg.in/yaml.v3"
)

// pluginCase is one hand-authored plugin conversion fixture: a package file
// map, a format, a harness target, a delivery mode, and the expected
// projection and diagnostics.
type pluginCase struct {
	Name        string            `yaml:"name"`
	Format      string            `yaml:"format"`
	Target      string            `yaml:"target"`
	Delivery    string            `yaml:"delivery"`
	Roots       pluginRoots       `yaml:"roots"`
	Files       map[string]string `yaml:"files"`
	WantBlocked bool              `yaml:"want_blocked"`
	WantMembers []wantMember      `yaml:"want_members"`
}

type pluginRoots struct {
	Package string `yaml:"package"`
	Data    string `yaml:"data"`
}

type wantMember struct {
	Key              string   `yaml:"key"`
	Blocked          bool     `yaml:"blocked"`
	Artifacts        []string `yaml:"artifacts"`
	ArtifactContains []string `yaml:"artifact_contains"`
	DiagnosticCodes  []string `yaml:"diagnostic_codes"`
}

type pluginFixtures struct {
	Cases []pluginCase `yaml:"cases"`
}

// TestPluginConversionFixtures runs the independently authored plugin
// conversion fixtures through the public package compile path: each fixture
// decodes with plugindecode.Decode from a temp dir and projects through
// harnesscompile.CompilePackage.
func TestPluginConversionFixtures(t *testing.T) {
	for _, c := range loadPluginFixtures(t).Cases {
		t.Run(c.Name, func(t *testing.T) {
			runPluginCase(t, c)
		})
	}
}

func loadPluginFixtures(t *testing.T) pluginFixtures {
	t.Helper()
	raw, err := os.ReadFile("testdata/conversions.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures pluginFixtures
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures.Cases) == 0 {
		t.Fatal("conversions.yaml carries no cases")
	}
	return fixtures
}

type caseT interface {
	Helper()
	Fatal(args ...any)
	Fatalf(format string, args ...any)
	TempDir() string
}

func runPluginCase(t caseT, c pluginCase) *pluginpolicy.Projection {
	t.Helper()
	root := t.TempDir()
	for name, content := range c.Files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	plugin, diags, err := plugindecode.Decode(root, c.Format)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, d := range diags {
		if d != nil && d.IsError() {
			t.Fatalf("decode returned an error diagnostic: %+v", d)
		}
	}
	proj, err := harnesscompile.CompilePackage(plugin, harnesscompile.Target(c.Target),
		pluginpolicy.Delivery(c.Delivery), harnesscompile.ScopeProject,
		pluginpolicy.Roots{Package: c.Roots.Package, Data: c.Roots.Data})
	if err != nil {
		t.Fatalf("CompilePackage: %v", err)
	}
	if proj.Blocked != c.WantBlocked {
		t.Fatalf("fixture %s: projection blocked = %v, want %v", c.Name, proj.Blocked, c.WantBlocked)
	}
	if len(proj.Members) != len(c.WantMembers) {
		t.Fatalf("fixture %s: members = %v, want %d members", c.Name, memberKeysOf(proj), len(c.WantMembers))
	}
	for i, want := range c.WantMembers {
		got := proj.Members[i]
		if got.Key != want.Key {
			t.Fatalf("fixture %s: member[%d].Key = %q, want %q", c.Name, i, got.Key, want.Key)
		}
		if got.Blocked != want.Blocked {
			t.Fatalf("fixture %s: member %s blocked = %v, want %v", c.Name, want.Key, got.Blocked, want.Blocked)
		}
		var paths []string
		var body strings.Builder
		for _, a := range got.Artifacts {
			paths = append(paths, a.Path)
			body.Write(a.Bytes)
		}
		if strings.Join(paths, ",") != strings.Join(want.Artifacts, ",") {
			t.Fatalf("fixture %s: member %s artifacts = %v, want %v", c.Name, want.Key, paths, want.Artifacts)
		}
		for _, wantText := range want.ArtifactContains {
			if !strings.Contains(body.String(), wantText) {
				t.Fatalf("fixture %s: member %s artifacts must contain %q, got %s", c.Name, want.Key, wantText, body.String())
			}
		}
		for _, code := range want.DiagnosticCodes {
			found := false
			for _, d := range got.Diagnostics {
				if d.Code == code {
					found = true
				}
			}
			if !found {
				t.Fatalf("fixture %s: member %s wants diagnostic %q, got %v", c.Name, want.Key, code, got.Diagnostics)
			}
		}
	}
	return proj
}

func memberKeysOf(p *pluginpolicy.Projection) []string {
	var keys []string
	for _, m := range p.Members {
		keys = append(keys, m.Key)
	}
	return keys
}

// spec: plugin-policy-catalogs/a-plugin-fixture-mismatch-fails-the-check
func TestPluginFixtureMismatchFailsTheCheck(t *testing.T) {
	fixtures := loadPluginFixtures(t)
	if len(fixtures.Cases) == 0 {
		t.Fatal("no fixtures")
	}
	c := fixtures.Cases[0]
	c.WantMembers = append([]wantMember{}, c.WantMembers...)
	c.WantMembers[0].Artifacts = []string{"deliberately/wrong.txt"}
	fakeT := &recordingT{TB: t}
	func() {
		defer func() {
			if r := recover(); r != nil && r != errFixtureAbort {
				panic(r)
			}
		}()
		runPluginCase(fakeT, c)
	}()
	if len(fakeT.failures) == 0 {
		t.Fatal("a mismatched expectation must fail the check")
	}
	joined := strings.Join(fakeT.failures, "\n")
	if !strings.Contains(joined, c.Name) {
		t.Fatalf("the failure must name the fixture, got %q", joined)
	}
}

// recordingT captures Fatalf output so the mismatch test can prove a wrong
// expectation fails and names its fixture without failing the real test.
type recordingT struct {
	testing.TB
	failures []string
}

func (r *recordingT) Helper() { r.TB.Helper() }

func (r *recordingT) Fatal(args ...any) {
	r.failures = append(r.failures, fmt.Sprint(args...))
	panic(errFixtureAbort)
}

func (r *recordingT) Fatalf(format string, args ...any) {
	r.failures = append(r.failures, fmt.Sprintf(format, args...))
	panic(errFixtureAbort)
}

var errFixtureAbort = errors.New("fixture expectation aborted")
