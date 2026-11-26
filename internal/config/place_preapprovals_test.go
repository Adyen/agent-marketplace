package config

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/remedy"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"
)

const (
	placePreapprovalProfileDigest    = "sha256:" + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	placePreapprovalExecutableDigest = "sha256:" + "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func TestV4OrganizationPlacePreapprovals(t *testing.T) {
	data := placePreapprovalTestYAML(
		fmt.Sprintf(`{canonical_path: "/usr/bin/invented app", sha256: %s}`, placePreapprovalExecutableDigest),
		"[version_probe, initial-load]",
	)
	layer, err := readRootLayerIntent("organization.yaml", []byte(data), organizationLayer)
	if err != nil {
		t.Fatalf("read organization preapproval root: %v", err)
	}
	preapprovals := layer.PlacePreapprovals()
	if len(preapprovals) != 1 {
		t.Fatalf("organization preapprovals = %#v, want one", preapprovals)
	}
	binding := targetprofile.ApprovalBinding{
		OS: "linux", Profile: targetprofile.Identity{
			TargetID: "invented-app", RevisionDigest: placePreapprovalProfileDigest,
		},
		CanonicalRoot:      "/home/alice/.config/invented-app",
		CanonicalStateRoot: "/home/alice/.local/state/agent-marketplace",
		CommandRoles:       []string{"initial-load", "version_probe"},
		Executable: targetprofile.ExecutableBinding{
			CanonicalPath: "/usr/bin/invented app", SHA256: placePreapprovalExecutableDigest,
		},
	}
	if preapprovals[0].Match(binding) != PlacePreapprovalExact {
		t.Fatal("exact organization preapproval did not match binding with reordered roles")
	}
	for _, test := range []struct {
		name   string
		change func(*targetprofile.ApprovalBinding)
	}{
		{name: "target ID", change: func(b *targetprofile.ApprovalBinding) { b.Profile.TargetID += "-other" }},
		{name: "target root", change: func(b *targetprofile.ApprovalBinding) { b.CanonicalRoot += "/other" }},
		{name: "profile digest", change: func(b *targetprofile.ApprovalBinding) { b.Profile.RevisionDigest = "sha256:" + strings.Repeat("f", 64) }},
		{name: "executable digest", change: func(b *targetprofile.ApprovalBinding) { b.Executable.SHA256 = "sha256:" + strings.Repeat("f", 64) }},
		{name: "command role set", change: func(b *targetprofile.ApprovalBinding) { b.CommandRoles = []string{"initial-load"} }},
		{name: "OS", change: func(b *targetprofile.ApprovalBinding) { b.OS = "macos" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			mismatched := binding
			mismatched.CommandRoles = append([]string(nil), binding.CommandRoles...)
			test.change(&mismatched)
			for _, stateRoot := range []string{binding.CanonicalStateRoot, binding.CanonicalStateRoot + "/other"} {
				mismatched.CanonicalStateRoot = stateRoot
				if got := preapprovals[0].Match(mismatched); got != PlacePreapprovalUnrelated {
					t.Fatalf("changed %s at state root %s: match = %v, want unrelated", test.name, stateRoot, got)
				}
			}
		})
	}
	preapprovals[0].CommandRoles[0] = "mutated"
	if got := layer.PlacePreapprovals()[0].CommandRoles; got[0] != "initial-load" {
		t.Fatalf("accessor leaked mutable role slice: %v", got)
	}

	noExecution, err := readRootLayerIntent(
		"organization.yaml",
		[]byte(placePreapprovalTestYAML("no-execution", "[]")),
		organizationLayer,
	)
	if err != nil {
		t.Fatalf("read no-execution organization preapproval: %v", err)
	}
	noExecutionBinding := binding
	noExecutionBinding.CommandRoles = nil
	noExecutionBinding.Executable = targetprofile.ExecutableBinding{}
	if got := noExecution.PlacePreapprovals(); len(got) != 1 || got[0].Match(noExecutionBinding) != PlacePreapprovalExact {
		t.Fatalf("no-execution preapproval = %#v, want exact no-execution match", got)
	}

	for _, test := range []struct {
		name      string
		data      string
		wantEntry bool
	}{
		{name: "no-execution with roles", data: placePreapprovalTestYAML("no-execution", "[initial-load]"), wantEntry: true},
		{name: "wildcard path", data: strings.Replace(data, "/home/alice/.config/invented-app", "/home/*/invented-app", 1), wantEntry: true},
		{name: "unknown executable field", data: placePreapprovalTestYAML(fmt.Sprintf(
			`{canonical_path: "/usr/bin/invented-app", sha256: %s, unexpected: true}`, placePreapprovalExecutableDigest,
		), "[initial-load]"), wantEntry: true},
		{name: "null list", data: "apiVersion: agent-marketplace/v4\ntarget_preapprovals: null\n"},
		{name: "duplicate roles", data: placePreapprovalTestYAML(
			fmt.Sprintf(`{canonical_path: "/usr/bin/invented-app", sha256: %s}`, placePreapprovalExecutableDigest),
			"[initial-load, initial-load]",
		), wantEntry: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := readRootLayerIntent("organization.yaml", []byte(test.data), organizationLayer)
			requirePlacePreapprovalError(t, err, "v4.target-preapprovals")
			if test.wantEntry && !strings.Contains(err.Error(), "entry 0") {
				t.Fatalf("error = %v, want list entry index", err)
			}
		})
	}
	duplicateField := strings.Replace(data, "  - os: linux\n", "  - os: linux\n    os: macos\n", 1)
	_, err = readRootLayerIntent("organization.yaml", []byte(duplicateField), organizationLayer)
	requirePlacePreapprovalError(t, err, "v4.duplicate-key")

	if entries := (LayerIntent{Source: UserPersonal, v4: layer.v4}).PlacePreapprovals(); entries != nil {
		t.Fatalf("user layer exposed organization preapprovals: %#v", entries)
	}
}

func TestV4OrganizationPlacePreapprovalsScope(t *testing.T) {
	checkScopeError := func(t *testing.T, err error, action, location string) {
		t.Helper()
		requirePlacePreapprovalError(t, err, "v4.target-preapprovals-scope")
		hint := remedy.For(err, "sync")
		if !strings.HasPrefix(hint, action+" target_preapprovals from ") || !strings.Contains(hint, location) || !strings.Contains(hint, "only in the organization root configuration") {
			t.Fatalf("scope remediation = %q, want removal from %s and organization-root-only grant guidance", hint, location)
		}
	}
	fragment := []byte("target_preapprovals: []\n")
	if _, err := parseV4Document(fragment, "included.yaml", false, organizationLayer); err == nil {
		t.Fatal("organization include accepted target_preapprovals")
	} else {
		checkScopeError(t, err, "Ask your administrator to remove", "included.yaml:1")
	}
	for _, kind := range []layerKind{userLayer, repoLayer, localLayer} {
		t.Run(kind.name(), func(t *testing.T) {
			data := []byte("apiVersion: agent-marketplace/v4\ntarget_preapprovals: []\n")
			_, err := readRootLayerIntent("authored.yaml", data, kind)
			checkScopeError(t, err, "Remove", "authored.yaml:2")
		})
	}
}

func placePreapprovalTestYAML(executable, roles string) string {
	return fmt.Sprintf(`apiVersion: agent-marketplace/v4
target_preapprovals:
  - os: linux
    target_id: invented-app
    profile_digest: %s
    canonical_root: /home/alice/.config/invented-app
    canonical_state_root: /home/alice/.local/state/agent-marketplace
    executable: %s
    command_roles: %s
`, placePreapprovalProfileDigest, executable, roles)
}

func requirePlacePreapprovalError(t *testing.T, err error, code string) {
	t.Helper()
	var validation *V4ValidationError
	if !errors.As(err, &validation) || validation.Code != code {
		t.Fatalf("error = %v, want %s", err, code)
	}
}
