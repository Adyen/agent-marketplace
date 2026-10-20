package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A native hook file with the wrong envelope must refuse only its own target
// with a coded error and keep its bytes, mode and mtime, while a separately
// selected valid target still reconciles. Repairing the file lets the hook
// install, proving the refusal came from the envelope.
// spec: hook-native-location-tolerance/malformed-native-file-leaves-both-locations-unchanged
func TestHookInvariantMalformedNativeEnvelopeRefusesOnlyItsTarget(t *testing.T) {
	const command = "echo am-malformed-envelope"
	// Targets with a native-location compatibility check refuse with
	// hook.native-tolerance-refused; Devin CLI and Codex, which never had a
	// legacy location, refuse through their aggregate writers' own codes.
	for _, tc := range []struct{ target, sibling, envelope, code string }{
		{"claude", "factory", `{"hooks": ["not an event map"]}`, "hook.native-tolerance-refused"},
		{"factory", "claude", `{"PreToolUse": {"not": "a group list"}}`, "hook.native-tolerance-refused"},
		{"cursor", "claude", `{"version": 1, "hooks": ["not an event map"]}`, "hook.native-tolerance-refused"},
		{"gemini", "claude", `{"hooks": ["not an event map"]}`, "hook.native-tolerance-refused"},
		{"windsurf", "claude", `{"hooks": ["not an event map"]}`, "hook.native-tolerance-refused"},
		{"devin", "claude", `{"PreToolUse": {"not": "a group list"}}`, "materialize.devin.aggregate-shape"},
		{"codex", "claude", `{"hooks": ["not an event map"]}`, "materialize.codex.aggregate-malformed"},
	} {
		t.Run(tc.target, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t)
			loaded := hookInvariantLoadedFile(t, tc.target)
			e.WriteFile("/workspace/"+loaded, tc.envelope+"\n")
			path := filepath.Join(e.workspace, filepath.FromSlash(loaded))
			before, err := pathFingerprint(path)
			if err != nil {
				t.Fatal(err)
			}
			registry := hookInvariantRegistry(e, "envelope", hookInvariantDocument(hookInvariantGroup("Bash", command)))
			e.WriteRepoConfig(hookInvariantV4Config(tc.target+", "+tc.sibling, registry, "hook/PreToolUse-command"))
			e.InitGitRepo()

			for _, args := range [][]string{{"sync"}, {"sync", "--check"}} {
				r := e.RunAM(args...)
				// Devin and Codex name the target through their code namespace
				// and every target names the refused file, so the error must
				// name the malformed file and never the sibling's.
				if r.ExitCode == 0 || !hookInvariantErrorMentions(r.Combined, "/"+loaded+":", tc.code+": ") {
					t.Fatalf("%v exited %d without a %s error naming %s:\n%s", args, r.ExitCode, tc.code, loaded, r.Combined)
				}
				if sibling := hookInvariantLoadedFile(t, tc.sibling); hookInvariantErrorMentions(r.Combined, "/"+sibling) {
					t.Fatalf("%v refused the valid sibling target %s:\n%s", args, tc.sibling, r.Combined)
				}
				if outside := hookInvariantOutsideReview(r.Combined); strings.Contains(outside, command) {
					t.Fatalf("%v quotes the hook payload outside the hook review:\n%s", args, outside)
				}
				if after, err := pathFingerprint(path); err != nil || after != before {
					t.Fatalf("%v changed the malformed %s: %v", args, loaded, err)
				}
			}
			if got := hookInvariantLoadedCount(t, tc.sibling, e.workspace, false, command); got != 1 {
				t.Fatalf("valid sibling %s loads %d copies, want 1", tc.sibling, got)
			}

			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			e.MustRunAM("sync")
			if got := hookInvariantLoadedCount(t, tc.target, e.workspace, false, command); got != 1 {
				t.Fatalf("after repair %s loads %d copies, want 1", tc.target, got)
			}
		})
	}
}

// A source hook whose handler spells a modeled field twice in different
// letter case is ambiguous. The registry fails to load with
// json.duplicate-key, so sync and check refuse before materializing anything:
// no payload is quoted and every installed hook file, including the hook the
// registry installed before the ambiguity and a hook from another registry,
// keeps its bytes. Correcting the source installs the corrected hook.
// spec: hook-resources/ambiguous-hook-sources-refuse-without-changing-output
func TestHookInvariantAmbiguousSourceAliasRefusesWithoutChangingOutput(t *testing.T) {
	const (
		original  = "echo am-alias-original"
		sibling   = "echo am-alias-sibling"
		corrected = "echo am-alias-corrected"
	)
	for _, target := range hookInvariantNativeTargets {
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t)
			ambiguous := hookInvariantRegistry(e, "ambiguous", hookInvariantDocument(hookInvariantGroup("Bash", original)))
			// The sibling selects its second handler, so its hook name differs
			// from the ambiguous registry's even on OpenCode (#806).
			other := hookInvariantRegistry(e, "sibling", hookInvariantDocument(hookInvariantGroup("Bash", "echo am-alias-unselected"), hookInvariantGroup("Bash", sibling)))
			e.WriteRepoConfig("apiVersion: agent-marketplace/v4\nharnesses: [" + target + "]\nsources:\n  ambiguous: {directory: '" + ambiguous + "'}\n  sibling: {directory: '" + other + "'}\nresources:\n  ambiguous: {include: [hook/PreToolUse-command]}\n  sibling: {include: [hook/PreToolUse-command-2]}\n")
			e.InitGitRepo()
			e.MustRunAM("sync")
			for _, command := range []string{original, sibling} {
				if got := hookInvariantLoadedCount(t, target, e.workspace, false, command); got != 1 {
					t.Fatalf("before the ambiguity %q loads %d times, want 1", command, got)
				}
			}
			roots := []string{filepath.Join(e.workspace, "."+target), filepath.Join(e.workspace, ".devin"), filepath.Join(e.workspace, ".agents")}
			before := map[string]string{}
			for _, root := range roots {
				if fingerprint, err := pathFingerprint(root); err == nil {
					before[root] = fingerprint
				}
			}

			e.WriteFile("/repos/ambiguous/hooks/hooks.json", `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo am-alias-lower","Command":"echo am-alias-upper"}]}]}}`)
			for _, args := range [][]string{{"sync"}, {"sync", "--check"}} {
				r := e.RunAM(args...)
				if r.ExitCode == 0 || !strings.Contains(r.Combined, "json.duplicate-key") {
					t.Fatalf("%v exited %d without json.duplicate-key:\n%s", args, r.ExitCode, r.Combined)
				}
				outside := hookInvariantOutsideReview(r.Combined)
				for _, payload := range []string{"am-alias-lower", "am-alias-upper", original, sibling} {
					if strings.Contains(outside, payload) {
						t.Fatalf("%v quotes %q outside the hook review:\n%s", args, payload, outside)
					}
				}
				for root, want := range before {
					if got, err := pathFingerprint(root); err != nil || got != want {
						t.Fatalf("%v changed %s: %v", args, root, err)
					}
				}
			}

			e.WriteFile("/repos/ambiguous/hooks/hooks.json", hookInvariantDocument(hookInvariantGroup("Bash", corrected)))
			e.MustRunAM("sync")
			for command, want := range map[string]int{original: 0, corrected: 1, sibling: 1} {
				if got := hookInvariantLoadedCount(t, target, e.workspace, false, command); got != want {
					t.Fatalf("after correction %q loads %d times, want %d", command, got, want)
				}
			}
		})
	}
}

// A timeout that one target represents exactly and another cannot must be
// emitted exactly where it fits and refused only where it does not, while a
// sibling hook with an ordinary timeout installs on both targets.
// spec: hook-resources/timeouts-beyond-the-exact-integer-range-are-refused
func TestHookInvariantTimeoutBeyondOneTargetsRangeRefusesOnlyThere(t *testing.T) {
	e := newEnv(t)
	const large, ordinary = "echo am-timeout-large", "echo am-timeout-ordinary"
	registry := hookInvariantRegistry(e, "timeouts", `{"hooks":{"PreToolUse":[`+
		`{"matcher":"Bash","hooks":[{"type":"command","command":"`+large+`","timeout":9007199254741}]},`+
		`{"matcher":"Bash","hooks":[{"type":"command","command":"`+ordinary+`","timeout":30}]}]}}`)
	e.WriteRepoConfig(hookInvariantV4Config("claude, gemini", registry, "hook/PreToolUse-command", "hook/PreToolUse-command-2"))
	e.InitGitRepo()
	r := e.RunAM("sync")
	if r.ExitCode == 0 || !hookInvariantErrorMentions(r.Combined, "(target gemini,", "hook.timeout-unsupported") {
		t.Fatalf("Gemini did not refuse the out-of-range timeout (exit %d):\n%s", r.ExitCode, r.Combined)
	}
	if hookInvariantErrorMentions(r.Combined, "(target claude,") {
		t.Fatalf("Claude refused a timeout it represents exactly:\n%s", r.Combined)
	}
	if outside := hookInvariantOutsideReview(r.Combined); strings.Contains(outside, "9007199254741") || strings.Contains(outside, large) {
		t.Fatalf("refusal quotes the timeout or command outside the hook review:\n%s", outside)
	}
	for target, want := range map[string]map[string]int{"claude": {large: 1, ordinary: 1}, "gemini": {large: 0, ordinary: 1}} {
		for command, count := range want {
			if got := hookInvariantLoadedCount(t, target, e.workspace, false, command); got != count {
				t.Errorf("%s loads %q %d times, want %d", target, command, got, count)
			}
		}
	}
	settings := e.ReadFile("/workspace/.claude/settings.json")
	if !strings.Contains(settings, `"timeout": 9007199254741`) && !strings.Contains(settings, `"timeout":9007199254741`) {
		t.Errorf("Claude timeout was not emitted exactly:\n%s", settings)
	}
	if gemini := e.ReadFile("/workspace/.gemini/settings.json"); !strings.Contains(gemini, "30000") {
		t.Errorf("Gemini sibling timeout was not converted to milliseconds:\n%s", gemini)
	}
}

// hookInvariantErrorMentions reports whether an error diagnostic in output, a
// top-level "error:" line or an indented line grouped beneath one, contains
// token and every given code.
func hookInvariantErrorMentions(output, token string, codes ...string) bool {
	inError := false
	for _, line := range strings.Split(output, "\n") {
		head := strings.TrimPrefix(line, "Error: ")
		switch {
		case strings.HasPrefix(head, "error: "):
			inError = true
		case strings.HasPrefix(line, "  ") && inError:
		default:
			inError = false
			continue
		}
		matched := strings.Contains(line, token)
		for _, code := range codes {
			matched = matched && strings.Contains(line, code)
		}
		if matched {
			return true
		}
	}
	return false
}

// hookInvariantOutsideReview drops the "Hook changes" listing, whose rows may
// legitimately quote reviewed commands, from rendered output.
func hookInvariantOutsideReview(output string) string {
	var kept []string
	inReview := false
	for _, line := range strings.Split(output, "\n") {
		switch {
		case strings.HasPrefix(line, "Hook changes"):
			inReview = true
		case inReview && strings.HasPrefix(line, "  "):
		default:
			inReview = false
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

// When a registry fails to load, sync refuses without pruning its hooks, so
// sync --check must not predict that a later sync removes them.
func TestHookInvariantCheckPredictsNoRemovalForAnUnloadableRegistry(t *testing.T) {
	t.Skip("known defect: https://github.com/Adyen/agent-marketplace/issues/814")
	e := newEnv(t)
	const installed = "echo am-unloadable-installed"
	registry := hookInvariantRegistry(e, "unloadable", hookInvariantDocument(hookInvariantGroup("Bash", installed)))
	e.WriteRepoConfig(hookInvariantV4Config("factory", registry, "hook/PreToolUse-command"))
	e.InitGitRepo()
	e.MustRunAM("sync")
	e.WriteFile("/repos/unloadable/hooks/hooks.json", `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo x","Command":"echo y"}]}]}}`)
	check := e.RunAM("sync", "--check")
	if strings.Contains(check.Combined, `- command: "`+installed+`"`) || strings.Contains(check.Combined, "Deselecting hooks removes") {
		t.Fatalf("check predicts removing a hook that sync keeps while its registry cannot load:\n%s", check.Combined)
	}
	e.RunAM("sync")
	if got := hookInvariantLoadedCount(t, "factory", e.workspace, false, installed); got != 1 {
		t.Fatalf("control: sync kept %d copies, want the unpruned 1", got)
	}
}
