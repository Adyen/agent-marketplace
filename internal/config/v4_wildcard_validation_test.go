package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// Freeze both former validators, including direct calls that bypass general
// syntax validation. The shared kind/name validators and exact fallback stay
// unchanged by this allocation slice.
func legacyValidateV4Exclusion(value, source string) error {
	if value == "*" {
		return nil
	}
	if strings.ContainsAny(value, "?[") || strings.Contains(value, "**") {
		return v4err("v4.exclusion", "resources."+source+".exclude", 0, "wildcard form is not bounded")
	}
	if strings.HasSuffix(value, "/*") {
		base := strings.TrimSuffix(value, "/*")
		if base == "" {
			return nil
		}
		parts := strings.Split(base, "/")
		if len(parts) == 1 {
			if _, ok := v4Kinds[parts[0]]; !ok && !v4SlugRE.MatchString(parts[0]) {
				return v4err("v4.exclusion", value, 0, "invalid kind or registry wildcard")
			}
			return nil
		}
		if len(parts) == 2 && v4SlugRE.MatchString(parts[0]) {
			if _, ok := v4Kinds[parts[1]]; !ok {
				return v4err("v4.exclusion", value, 0, "invalid kind wildcard")
			}
			return nil
		}
		return v4err("v4.exclusion", value, 0, "invalid bounded wildcard")
	}
	if _, _, _, err := parseV4Locator(value, source); err != nil {
		return v4err("v4.exclusion", value, 0, "%v", err)
	}
	return nil
}

func legacyValidateV4ExclusionForSource(value string, kind V4SourceKind) error {
	if value == "*" {
		return nil
	}
	if strings.HasSuffix(value, "/*") {
		parts := strings.Split(strings.TrimSuffix(value, "/*"), "/")
		if kind != V4SourceMarketplace && (len(parts) != 1 || func() bool { _, ok := v4Kinds[parts[0]]; return !ok }()) {
			return v4err("v4.exclusion", value, 0, "git and directory wildcard exclusions require a resource kind")
		}
		if kind != V4SourceMarketplace && len(parts) != 1 {
			return v4err("v4.exclusion", value, 0, "git and directory wildcard exclusions are kind/* or *")
		}
		if kind == V4SourceMarketplace {
			if len(parts) == 1 {
				if _, isKind := v4Kinds[parts[0]]; isKind {
					return v4err("v4.exclusion", value, 0, "Marketplace wildcard exclusion must be registry/kind/*, registry/*, or *")
				}
				if !v4SlugRE.MatchString(parts[0]) {
					return v4err("v4.exclusion", value, 0, "invalid Marketplace registry wildcard")
				}
				return nil
			}
			if len(parts) != 2 || !v4SlugRE.MatchString(parts[0]) {
				return v4err("v4.exclusion", value, 0, "Marketplace wildcard exclusion must be registry/kind/*, registry/*, or *")
			}
			if _, isKind := v4Kinds[parts[1]]; !isKind {
				return v4err("v4.exclusion", value, 0, "invalid Marketplace kind wildcard")
			}
		}
		return nil
	}
	_, registry, _, err := parseV4Locator(value, string(kind))
	if err != nil {
		return v4err("v4.exclusion", value, 0, "%v", err)
	}
	if kind != V4SourceMarketplace && registry != "" {
		return v4err("v4.exclusion", value, 0, "git and directory exclusions cannot contain a registry slug")
	}
	if kind == V4SourceMarketplace && registry == "" {
		return v4err("v4.exclusion", value, 0, "Marketplace exclusions require a registry slug")
	}
	return nil
}

func checkV4WildcardValidationError(t *testing.T, got, want error) {
	t.Helper()
	if (got == nil) != (want == nil) {
		t.Fatalf("validation = %v, want %v", got, want)
	}
	if want == nil {
		return
	}
	var actual, expected *V4ValidationError
	if !errors.As(got, &actual) || !errors.As(want, &expected) || *actual != *expected {
		t.Fatalf("validation = %#v (%v), want %#v (%v)", actual, got, expected, want)
	}
}

func checkV4WildcardValidationReplay(t *testing.T, value, source string, kind V4SourceKind) {
	t.Helper()
	checkV4WildcardValidationError(t, validateV4Exclusion(value, source), legacyValidateV4Exclusion(value, source))
	checkV4WildcardValidationError(t, validateV4ExclusionForSource(value, kind), legacyValidateV4ExclusionForSource(value, kind))
}

func TestV4WildcardValidationReplay(t *testing.T) {
	values := []string{
		"", "*", "**", "/*", "/", "//", "//*", "*/*", "skill/*",
		"tools/*", "tools/skill/*", "tools/not-kind/*", "tools//skill/*",
		"tools/skill/extra/*", "tools/skill//*", "/skill/*", "tools//*",
		"tools/skill/", "tools/skill/review", "skill/review", "skill",
		" tools/*", "tools /*", "tools/* ", "tools/skill/*\n",
		"tools?/skill/*", "tools[/skill/*", "-tools/skill/*",
		"tools/skill?/*", "tools/skill[*/*", "tools/skill/**",
		"tools\\path/*", "tools\x00/*", "tools\r/*", "tools\n/*",
		"tools\t/*", "tools\x7f/*", "tools\xff/*", "tools.v2_1/skill/*",
		strings.Repeat("r", 4096) + "/skill/*",
	}
	for _, kind := range []string{"skill", "command", "subagent", "hook", "mcp", "collection"} {
		values = append(values, kind+"/*", "tools/"+kind+"/*")
	}
	for _, value := range values {
		t.Run(fmt.Sprintf("%q", value), func(t *testing.T) {
			for _, source := range []string{"", "g", " private/source "} {
				for _, kind := range []V4SourceKind{V4SourceMarketplace, V4SourceGit, V4SourceDirectory, "", "unknown"} {
					checkV4WildcardValidationReplay(t, value, source, kind)
				}
			}
		})
	}
}

func FuzzV4WildcardValidationReplay(f *testing.F) {
	for _, value := range []string{"", "*", "/*", "skill/*", "tools/*", "tools/skill/*", "tools//skill/*", "tools?/skill/*", "tools\xff/*"} {
		for _, kind := range []V4SourceKind{V4SourceMarketplace, V4SourceGit, ""} {
			f.Add(value, "g", string(kind))
		}
	}
	f.Fuzz(func(t *testing.T, value, source, kind string) {
		checkV4WildcardValidationReplay(t, value, source, V4SourceKind(kind))
	})
}

func TestV4WildcardValidationPublic(t *testing.T) {
	for _, kind := range []V4SourceKind{V4SourceGit, V4SourceDirectory, V4SourceMarketplace} {
		t.Run(string(kind), func(t *testing.T) {
			binding, kept := "git: https://example.test/tools.git, ref: main", "command/kept"
			switch kind {
			case V4SourceDirectory:
				binding = fmt.Sprintf("directory: %q", filepath.ToSlash(t.TempDir()))
			case V4SourceMarketplace:
				binding, kept = "marketplace: https://market.example", "tools/command/kept"
			}
			values := []string{"*"}
			for _, resourceKind := range []string{"skill", "command", "subagent", "hook", "mcp", "collection"} {
				value := resourceKind + "/*"
				if kind == V4SourceMarketplace {
					value = "tools/" + value
				}
				values = append(values, value)
			}
			if kind == V4SourceMarketplace {
				values = append(values, "tools/*")
			}
			for _, value := range values {
				data := fmt.Sprintf("apiVersion: agent-marketplace/v4\nsources: {g: {%s}}\nresources: {g: {required: true, include: [%q], exclude: [%q]}}\n", binding, kept, value)
				cfg, err := ParseV4([]byte(data))
				if err != nil || cfg == nil || len(cfg.Requests) != 1 || cfg.Requests[0].Locator != kept ||
					!cfg.Requests[0].Required || len(cfg.Decisions) != 2 ||
					len(cfg.Groups["g"].Exclude) != 1 || cfg.Groups["g"].Exclude[0] != value {
					t.Fatalf("wildcard %q changed protection or authored history: %#v, %v", value, cfg, err)
				}
			}
		})
	}
	for _, tc := range []struct {
		value string
		git   bool
		path  string
		msg   string
	}{
		{"tools/skill/extra/*", false, "tools/skill/extra/*", "invalid bounded wildcard"},
		{"tools/not-kind/*", false, "tools/not-kind/*", "invalid kind wildcard"},
		{"skill/*", false, "skill/*", "Marketplace wildcard exclusion must be registry/kind/*, registry/*, or *"},
		{"tools/*", true, "tools/*", "git and directory wildcard exclusions require a resource kind"},
		{"tools/skill/*", true, "tools/skill/*", "git and directory wildcard exclusions require a resource kind"},
		{"tools?/skill/*", false, "resources.g.exclude", "wildcard form is not bounded"},
		{"/*", false, "/*", "invalid Marketplace registry wildcard"},
		{"tools//skill/*", false, "tools//skill/*", "invalid bounded wildcard"},
	} {
		t.Run(fmt.Sprintf("refusal/%s/git=%t", tc.value, tc.git), func(t *testing.T) {
			binding := "marketplace: https://market.example"
			if tc.git {
				binding = "git: https://example.test/tools.git, ref: main"
			}
			data := fmt.Sprintf("apiVersion: agent-marketplace/v4\nsources: {g: {%s}}\nresources: {g: {exclude: [%q]}}\n", binding, tc.value)
			cfg, err := ParseV4([]byte(data))
			var refusal *V4ValidationError
			if cfg != nil || !errors.As(err, &refusal) || refusal.Code != "v4.exclusion" ||
				refusal.Path != tc.path || refusal.Msg != tc.msg {
				t.Fatalf("public refusal = %#v (%v), want v4.exclusion at %s: %s", refusal, err, tc.path, tc.msg)
			}
		})
	}
}

func TestV4WildcardValidationEditor(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".agents", "config.yaml")
	mustMkdirAll(t, filepath.Dir(path))
	mustWriteFile(t, path, []byte("apiVersion: agent-marketplace/v4\nsources: {g: {marketplace: https://market.example}}\nresources: {g: {include: [tools/command/kept]}}\n"))
	if err := SetUserMarketplaceExclusion(path, "https://market.example", "tools", ir.KindSkill, "*", true); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil || !bytes.Contains(before, []byte("tools/skill/*")) || !bytes.Contains(before, []byte("tools/command/kept")) {
		t.Fatalf("positive edit did not record the wildcard and preserve the selection: %s, %v", before, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ registry, name string }{{"tools", "extra/*"}, {"tools?", "*"}} {
		value := tc.registry + "/skill/" + tc.name
		err := SetUserMarketplaceExclusion(path, "https://market.example", tc.registry, ir.KindSkill, tc.name, true)
		var refusal *V4ValidationError
		if !errors.As(err, &refusal) || refusal.Code != "v4.exclusion" || refusal.Path != value ||
			refusal.Msg != "Marketplace wildcard exclusion must be registry/kind/*, registry/*, or *" {
			t.Fatalf("editor refusal = %#v (%v)", refusal, err)
		}
		after, readErr := os.ReadFile(path)
		afterInfo, statErr := os.Stat(path)
		entries, dirErr := os.ReadDir(filepath.Dir(path))
		if readErr != nil || statErr != nil || dirErr != nil || !bytes.Equal(after, before) ||
			afterInfo.Mode() != info.Mode() || !afterInfo.ModTime().Equal(info.ModTime()) ||
			len(entries) != 1 || entries[0].Name() != "config.yaml" {
			t.Fatalf("refusal changed bytes, metadata, or left residue: %v, %v, %v", readErr, statErr, dirErr)
		}
	}
}
