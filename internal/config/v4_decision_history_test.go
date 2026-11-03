package config

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestV4DecisionHistoryResetAndOwnership(t *testing.T) {
	var absent *V4Config
	absent.rebuildRequests()
	for _, groups := range []map[string]V4ResourceGroup{
		nil, {},
		{"g": {}},
		{"g": {Decisions: []V4SelectionDecision{}}},
		{"g": {Include: []V4ResourceRequest{{Locator: "skill/fallback"}}}},
	} {
		cfg := &V4Config{
			Groups: groups, Decisions: []V4SelectionDecision{{SourceName: "stale"}},
			Requests: []V4ResourceRequest{{Locator: "skill/stale"}},
		}
		for range 2 {
			cfg.rebuildRequests()
			if cfg.Decisions != nil || !reflect.DeepEqual(cfg.Requests, groups["g"].Include) {
				t.Fatalf("empty history must stay nil and clear previous output: %#v", cfg)
			}
		}
	}

	cfg := v4DecisionHistoryFixture(2, 2, false)
	group := cfg.Groups["g0001"]
	group.Decisions = append(group.Decisions, V4SelectionDecision{
		SourceName: "g0001", Action: V4SelectionTags,
		Provenance: V4Provenance{File: "tags.yaml", Line: 17, Precedence: 99},
	})
	cfg.Groups["g0001"] = group
	wantRequests, wantDecisions := interpretV4RequestsByLocator(cfg.Groups)
	for range 2 {
		cfg.rebuildRequests()
		if !reflect.DeepEqual(cfg.Requests, wantRequests) || !reflect.DeepEqual(cfg.Decisions, wantDecisions) {
			t.Fatal("rebuilding changed ordered history, nil requests, or selected values")
		}
		for i, name := range []string{"g0000", "g0000", "g0001", "g0001", "g0001"} {
			index := i
			if i >= 2 {
				index -= 2
			}
			if cfg.Decisions[i].Request != cfg.Groups[name].Decisions[index].Request {
				t.Fatal("history no longer refers to the original request")
			}
		}
	}
	source := cfg.Groups["g0000"]
	before := source.Decisions[0]
	cfg.Decisions[0].Provenance.Line = 12345
	if source.Decisions[0] != before {
		t.Fatal("output history aliases the input decision slice")
	}
	cfg.rebuildRequests()
	source.Decisions[0].Exclusion = "changed-input"
	if cfg.Decisions[0].Exclusion != "" {
		t.Fatal("input history aliases the output decision slice")
	}
}

func TestV4DecisionHistoryPublic(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "config.yaml")
	data := []byte(`apiVersion: agent-marketplace/v4
sources:
  z: {git: https://example.test/z.git, ref: main}
  a: {git: https://example.test/a.git, ref: main}
resources:
  z:
    include:
      - command/a: {as: first}
      - skill/shared: {as: winner}
    exclude: [skill/missing]
  a:
    include: [command/a]
    exclude: [command/a]
    required: true
`)
	if err := os.WriteFile(file, data, 0o640); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	for _, consumer := range []struct {
		name string
		read func() (*V4Config, error)
	}{
		{"parse", func() (*V4Config, error) { return ParseV4Config(data, file, true) }},
		{"load", func() (*V4Config, error) { return LoadV4ConfigFromPath(file) }},
	} {
		t.Run(consumer.name, func(t *testing.T) {
			cfg, err := consumer.read()
			if err != nil {
				t.Fatal(err)
			}
			expected := []struct {
				source, locator, alias string
				action                 V4SelectionReason
				line, index            int
				required               bool
			}{
				{"a", "command/a", "a", V4SelectionExplicit, 12, 0, true},
				{"a", "command/a", "", V4SelectionExclude, 13, 1, false},
				{"z", "command/a", "first", V4SelectionExplicit, 8, 0, false},
				{"z", "skill/shared", "winner", V4SelectionExplicit, 9, 1, false},
				{"z", "skill/missing", "", V4SelectionExclude, 10, 2, false},
			}
			if len(cfg.Decisions) != len(expected) || len(cfg.Requests) != 3 {
				t.Fatalf("history/selection count changed: %#v", cfg)
			}
			for i, want := range expected {
				got := cfg.Decisions[i]
				original := cfg.Groups[want.source].Decisions[want.index]
				if got.SourceName != want.source || got.Action != want.action ||
					got.Provenance.File != file || got.Provenance.Line != want.line ||
					got.Provenance != original.Provenance || got.Request != original.Request {
					t.Fatalf("history[%d] lost authored source order, provenance, or request identity: %#v", i, got)
				}
				if want.action == V4SelectionExclude {
					if got.Exclusion != want.locator || got.Request != nil {
						t.Fatalf("history[%d] changed an exclusion: %#v", i, got)
					}
				} else if got.Request == nil || got.Locator != want.locator ||
					got.Request.Options.As != want.alias || got.Request.Required != want.required {
					t.Fatalf("history[%d] changed an include occurrence: %#v, request %#v", i, got, got.Request)
				}
			}
			if cfg.Requests[0].SourceName != "a" || cfg.Requests[0].Locator != "command/a" ||
				!cfg.Requests[0].Required || cfg.Requests[1].SourceName != "z" ||
				cfg.Requests[1].Locator != "command/a" || cfg.Requests[1].Options.As != "first" ||
				cfg.Requests[2].SourceName != "z" || cfg.Requests[2].Locator != "skill/shared" ||
				cfg.Requests[2].Options.As != "winner" {
				t.Fatalf("required protection, source binding, or output order changed: %#v", cfg.Requests)
			}
		})
	}
	after, readErr := os.ReadFile(file)
	afterInfo, statErr := os.Stat(file)
	entries, dirErr := os.ReadDir(dir)
	if readErr != nil || statErr != nil || dirErr != nil || !bytes.Equal(after, data) ||
		info.Mode() != afterInfo.Mode() || !info.ModTime().Equal(afterInfo.ModTime()) || len(entries) != 1 {
		t.Fatalf("reading history changed authored bytes, metadata, or directory contents: %v/%v/%v", readErr, statErr, dirErr)
	}
}

func TestV4DecisionHistoryPublicOwnership(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "config.yaml")
	data := v4DecisionHistoryDocument(t, 2, false)
	if err := os.WriteFile(file, data, 0o640); err != nil {
		t.Fatal(err)
	}
	for _, read := range []func() (*V4Config, error){
		func() (*V4Config, error) { return ParseV4Config(data, file, true) },
		func() (*V4Config, error) { return LoadV4ConfigFromPath(file) },
	} {
		cfg, err := read()
		if err != nil {
			t.Fatal(err)
		}
		source := cfg.Groups["g"].Decisions
		if len(cfg.Decisions) != 2 || len(source) != 2 || cfg.Decisions[0].Request != source[0].Request {
			t.Fatal("history changed request identity or length")
		}
		before := source[0]
		cfg.Decisions[0].Provenance.Line = 12345
		if source[0] != before {
			t.Fatal("public output aliases the source decision slice")
		}
		before = cfg.Decisions[1]
		source[1].Provenance.Line = 12345
		if cfg.Decisions[1] != before {
			t.Fatal("public source aliases the output decision slice")
		}
	}
}
