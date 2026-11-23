package targetprofile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

const validProfileYAML = `apiVersion: agent-marketplace.io/target-profile/v1
id: invented-app
label: Invented App
platforms: [macos, linux]
roots: {user: {override: INVENTED_APP_HOME, default: {macos: {base: home, path: .config/invented-app}, linux: {base: xdg-config, path: invented-app}}, allowed_bases: [home, xdg-config]}}
host: {executable: invented-app-bin, version_probe: {argv: [--version], pattern: '(\d+\.\d+\.\d+)'}, verified: [{range: ">=1.0.0 <2.0.0", date: 2026-11-12, evidence: fixture}]}
checks: {initial-load: {scope: eager-package-load, argv: [check, "{subject}"], subject: {under: mods, component: package-name}}}
kinds: {mod: {deliver: place, into: [mods/], verify: initial-load}}
`

func profileIdentity(data []byte, targetID string) Identity {
	digest := sha256.Sum256(data)
	return Identity{TargetID: targetID, SourceID: "builtin:agent-marketplace", RevisionDigest: "sha256:" + hex.EncodeToString(digest[:])}
}

func replaceProfileText(t *testing.T, before, after string) []byte {
	t.Helper()
	if strings.Count(validProfileYAML, before) != 1 {
		t.Fatalf("fixture has %d occurrences of %q, want one", strings.Count(validProfileYAML, before), before)
	}
	return []byte(strings.Replace(validProfileYAML, before, after, 1))
}

func profileWithoutCommandRoles() []byte {
	data := strings.Replace(validProfileYAML, "\nhost: ", "\n# host: ", 1)
	data = strings.Replace(data, "\nchecks: ", "\n# checks: ", 1)
	return []byte(strings.Replace(data, "verify: initial-load", "verify: ''", 1))
}

func TestTargetProfileParse(t *testing.T) {
	data := []byte(validProfileYAML)
	profile, err := Parse(data, profileIdentity(data, "invented-app"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if profile.APIVersion != "agent-marketplace.io/target-profile/v1" || profile.ID != "invented-app" ||
		profile.Host.Executable != "invented-app-bin" || profile.Roots.User.Default["linux"] != (RootDefault{Base: "xdg-config", Path: "invented-app"}) ||
		profile.Kinds["mod"].Deliver != "place" || len(profile.Kinds["mod"].Into) != 1 ||
		profile.Kinds["mod"].Into[0] != "mods/" || profile.Kinds["mod"].Verify != "initial-load" {
		t.Fatalf("Parse() returned unexpected profile fields: %#v", profile)
	}
	changed := replaceProfileText(t, "evidence: fixture", "evidence: updated")
	if _, err := Parse(changed, profileIdentity(data, "invented-app")); err == nil || !strings.HasPrefix(err.Error(), profileDigestMismatchCode+":") {
		t.Fatalf("Parse() error = %v, want stale revision refusal", err)
	}
	if _, err := Parse(changed, profileIdentity(changed, "invented-app")); err != nil {
		t.Fatalf("Parse() rejected changed exact bytes with their digest: %v", err)
	}
	oversized := append([]byte(validProfileYAML), bytes.Repeat([]byte{'#'}, maxProfileBytes)...)
	if _, err := Parse(oversized, profileIdentity(oversized, "invented-app")); err == nil {
		t.Fatal("Parse() accepted input larger than the profile limit")
	}
}

func TestTargetProfileParseAllowsProfilesWithoutCommands(t *testing.T) {
	data := profileWithoutCommandRoles()
	profile, err := Parse(data, profileIdentity(data, "invented-app"))
	if err != nil {
		t.Fatalf("Parse() rejected a profile without command roles: %v", err)
	}
	if profile.Host.Executable != "" || profile.Host.VersionProbe != nil || len(profile.Checks) != 0 || profile.Kinds["mod"].Verify != "" {
		t.Fatalf("Parse() returned command roles for a profile without them: %#v", profile)
	}
}

func TestTargetProfileParseAllowsSinglePlatform(t *testing.T) {
	data := replaceProfileText(t, "platforms: [macos, linux]", "platforms: [linux]")
	if _, err := Parse(data, profileIdentity(data, "invented-app")); err != nil {
		t.Fatalf("Parse() rejected a single supported platform: %v", err)
	}
}

func TestTargetProfileParseRefusesInvalidInput(t *testing.T) {
	singlePlatformMissingRoot := replaceProfileText(t, "platforms: [macos, linux]", "platforms: [linux]")
	singlePlatformMissingRoot = []byte(strings.Replace(string(singlePlatformMissingRoot),
		"default: {macos: {base: home, path: .config/invented-app}, linux:", "default: {linux:", 1))
	withoutHost := []byte(strings.Replace(validProfileYAML, "\nhost: ", "\n# host: ", 1))
	tests := []struct {
		name   string
		data   []byte
		target string
	}{
		{"compiled target", replaceProfileText(t, "id: invented-app", "id: claude"), "claude"},
		{"profile identity mismatch", replaceProfileText(t, "id: invented-app", "id: another-app"), "invented-app"},
		{"absolute default", replaceProfileText(t, "path: invented-app", "path: /tmp/invented-app"), "invented-app"},
		{"unadmitted default base", replaceProfileText(t, "base: home", "base: xdg-data"), "invented-app"},
		{"traversing destination", replaceProfileText(t, "into: [mods/]", "into: [../outside/]"), "invented-app"},
		{"invalid override variable", replaceProfileText(t, "override: INVENTED_APP_HOME", "override: bad-name"), "invented-app"},
		{"unsupported platform", replaceProfileText(t, "platforms: [macos, linux]", "platforms: [macos, windows]"), "invented-app"},
		{"duplicate platform", replaceProfileText(t, "platforms: [macos, linux]", "platforms: [macos, macos]"), "invented-app"},
		{"single platform still requires both root defaults", singlePlatformMissingRoot, "invented-app"},
		{"unknown modeled key", append([]byte(validProfileYAML), []byte("unexpected: true\n")...), "invented-app"},
		{"unknown nested key", replaceProfileText(t, "executable:", "executables:"), "invented-app"},
		{"check requires executable", withoutHost, "invented-app"},
		{"numeric executable scalar", replaceProfileText(t, "executable: invented-app-bin", "executable: 17"), "invented-app"},
		{"numeric version probe pattern scalar", replaceProfileText(t, "pattern: '(\\d+\\.\\d+\\.\\d+)'", "pattern: 17"), "invented-app"},
		{"numeric version probe argv scalar", replaceProfileText(t, "argv: [--version]", "argv: [--version, 17]"), "invented-app"},
		{"boolean check argv scalar", replaceProfileText(t, "argv: [check, \"{subject}\"]", "argv: [check, false, \"{subject}\"]"), "invented-app"},
		{"case-variant modeled key", replaceProfileText(t, "apiVersion:", "ApiVersion:"), "invented-app"},
		{"nested case-variant modeled key", replaceProfileText(t, "component: package-name", "Component: package-name"), "invented-app"},
		{"duplicate modeled key", append([]byte(validProfileYAML), []byte("id: duplicate\n")...), "invented-app"},
		{"duplicate nested key", replaceProfileText(t, "component: package-name", "component: package-name, component: other"), "invented-app"},
		{"alias", replaceProfileText(t, "kinds: {mod: {deliver: place, into: [mods/], verify: initial-load}}", "kinds: {mod: {deliver: place, into: &paths [mods/], verify: initial-load}, other: {deliver: place, into: *paths}}"), "invented-app"},
		{"merge key", replaceProfileText(t, "roots: {user: {override: INVENTED_APP_HOME, ", "roots: {user: {<<: {override: INVENTED_APP_HOME}, "), "invented-app"},
		{"trailing document", append([]byte(validProfileYAML), []byte("---\nid: extra\n")...), "invented-app"},
		{"null mapping", replaceProfileText(t, "kinds: {mod: {deliver: place, into: [mods/], verify: initial-load}}", "kinds: null"), "invented-app"},
		{"bad check reference", replaceProfileText(t, "verify: initial-load", "verify: missing-check"), "invented-app"},
		{"subject in command role", replaceProfileText(t, "argv: [check, \"{subject}\"]", "argv: [\"{subject}\", check]"), "invented-app"},
		{"embedded subject", replaceProfileText(t, "argv: [check, \"{subject}\"]", "argv: [check, \"prefix-{subject}\"]"), "invented-app"},
		{"subject in version probe", replaceProfileText(t, "argv: [--version]", "argv: [--version, \"{subject}\"]"), "invented-app"},
		{"invalid version probe pattern", replaceProfileText(t, "pattern: '(\\d+\\.\\d+\\.\\d+)'", "pattern: '['"), "invented-app"},
		{"invalid local kind", replaceProfileText(t, "kinds: {mod:", "kinds: {Mod:"), "invented-app"},
		{"unsupported delivery", replaceProfileText(t, "deliver: place", "deliver: register"), "invented-app"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Parse(tt.data, profileIdentity(tt.data, tt.target)); err == nil || !strings.HasPrefix(err.Error(), "marketplace.profile.") {
				t.Fatalf("Parse() error = %v, want a profile diagnostic", err)
			}
		})
	}
}
