package main

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/devincli"
	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
)

// devinMcpArtifact builds one selected Devin MCP resource.
func devinMcpArtifact(name, command string) resolver.ResolvedArtifact {
	return resolver.ResolvedArtifact{
		Entry: catalog.Entry{Name: name,
			Resource: ir.NewMcpServer(name, command, nil, nil, nil, ir.SourceRef{})},
		Source:    config.RepoCommitted,
		Harnesses: []hcc.Target{hcc.TargetDevin},
	}
}

// prepareTwoDevinConflicts writes an mcp_config.json holding TWO hand-written
// entries whose keys match two selected MCP servers but whose values differ.
// It is the fixture every "one answer must authorize exactly one entry" attack
// below needs.
func prepareTwoDevinConflicts(t *testing.T, repoRoot string) (mcpPath string, selection []resolver.ResolvedArtifact) {
	t.Helper()
	outDir := filepath.Join(repoRoot, ".devin")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mcpPath = filepath.Join(outDir, harness.DevinMcpConfigFile)
	body := "{\n  \"mcpServers\": {\n" +
		"    \"aaa-first\": { \"command\": \"/usr/local/bin/first\", \"env\": { \"TOKEN\": \"" + conflictSecret + "-A\" } },\n" +
		"    \"zzz-second\": { \"command\": \"/usr/local/bin/second\", \"env\": { \"TOKEN\": \"" + conflictSecret + "-B\" } }\n" +
		"  }\n}\n"
	if err := os.WriteFile(mcpPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	selection = []resolver.ResolvedArtifact{
		devinMcpArtifact("aaa-first", "generated-first"),
		devinMcpArtifact("zzz-second", "generated-second"),
	}
	if _, err := materialize.Run(context.Background(), repoRoot, selection); err != nil {
		t.Fatalf("seed sync: %v", err)
	}
	return mcpPath, selection
}

// TestAdversarialOneYesThenEOFReplacesExactlyOneEntry attacks the per-entry
// consent rule with the cheapest real-world stdin: a user who types y once and
// then hits Ctrl-D (or a heredoc with a single line). The second entry must
// survive untouched — an EOF is not consent, and a "y" already spent on entry A
// is not consent for entry B.
func TestAdversarialOneYesThenEOFReplacesExactlyOneEntry(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repoRoot, _ := devinDoctorEnv(t)
	mcpPath, selection := prepareTwoDevinConflicts(t, repoRoot)

	var out bytes.Buffer
	if err := runDoctor(doctorOptions{
		out:     &out,
		in:      strings.NewReader("y\n"),
		isTTY:   true,
		repair:  true,
		probe:   staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
		resolve: staticResolve(selection),
	}); err != nil {
		t.Fatalf("one confirmation plus EOF must exit 0: %v\n%s", err, out.String())
	}
	after, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatal(err)
	}
	// Candidates are sorted by identity, so "aaa-first" is the one the single
	// "y" authorized.
	if strings.Contains(string(after), conflictSecret+"-A") {
		t.Fatalf("the confirmed entry was not replaced:\n%s", after)
	}
	if !strings.Contains(string(after), conflictSecret+"-B") {
		t.Fatalf("EOF destroyed a second entry the user never confirmed:\n%s", after)
	}
}

// TestAdversarialYesToAdoptionDoesNotSweepAConflict verifies that plan consent
// adopts exact matches without authorizing a conflicting replacement.
// spec: doctor-repair-planning/repair-plan-lists-every-step-before-confirmation
func TestAdversarialYesToAdoptionDoesNotSweepAConflict(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, tc := range []struct {
		name            string
		answers         string
		replaceConflict bool
	}{
		{name: "EOF declines conflict", answers: "y\n"},
		{name: "explicit decline", answers: "y\nn\n"},
		{name: "explicit destructive consent", answers: "y\ny\n", replaceConflict: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repoRoot, _ := devinDoctorEnv(t)
			outDir := filepath.Join(repoRoot, ".devin")
			if err := os.MkdirAll(outDir, 0o755); err != nil {
				t.Fatal(err)
			}
			mcpPath := filepath.Join(outDir, harness.DevinMcpConfigFile)

			// Seed generated entries, add a user conflict, then remove ownership.
			adoptable := devinMcpArtifact("adoptable", "adoptable-binary")
			secondAdoptable := devinMcpArtifact("second-adoptable", "second-adoptable-binary")
			if _, err := materialize.Run(context.Background(), repoRoot, []resolver.ResolvedArtifact{adoptable, secondAdoptable}); err != nil {
				t.Fatalf("seed sync: %v", err)
			}
			generated, err := os.ReadFile(mcpPath)
			if err != nil {
				t.Fatal(err)
			}
			withConflict := strings.Replace(string(generated), "\"mcpServers\": {",
				"\"mcpServers\": {\n    \"conflicting\": { \"command\": \"/usr/local/bin/hand\", \"env\": { \"TOKEN\": \""+conflictSecret+"\" } },", 1)
			if withConflict == string(generated) {
				t.Fatalf("fixture did not inject a conflicting entry:\n%s", generated)
			}
			if err := os.WriteFile(mcpPath, []byte(withConflict), 0o644); err != nil {
				t.Fatal(err)
			}
			// Wipe ownership state so both exact matches are adoption candidates
			// rather than clean owned entries.
			store, err := ownership.Open(outDir)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(store.StateFile()); err != nil {
				t.Fatalf("drop ownership state: %v", err)
			}

			selection := []resolver.ResolvedArtifact{adoptable, secondAdoptable, devinMcpArtifact("conflicting", "generated-binary")}
			preview := devinRepairPreview(t, repoRoot, selection)
			if len(preview.Files) != 1 || len(preview.Files[0].Adoptions) != 2 || len(preview.Files[0].Conflicts) != 1 {
				t.Fatalf("fixture must have two adoptions and one conflict: %#v", preview.Files)
			}
			backupsBefore, err := store.SafetyBackups(harness.DevinMcpConfigFile)
			if err != nil {
				t.Fatalf("read safety backups before repair: %v", err)
			}
			var out bytes.Buffer
			if err := runDoctor(doctorOptions{
				out:     &out,
				in:      strings.NewReader(tc.answers),
				isTTY:   true,
				repair:  true,
				probe:   staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
				resolve: staticResolve(selection),
			}); err != nil {
				t.Fatalf("repair must exit 0: %v\n%s", err, out.String())
			}
			output := out.String()
			planPrompt := "Repair the safe steps above? [y/N]"
			conflictPrompt := "replace mcp-server \"conflicting\" in mcp_config.json with the entry generated"
			planIndex := strings.Index(output, planPrompt)
			conflictIndex := strings.Index(output, conflictPrompt)
			if strings.Count(output, planPrompt) != 1 || strings.Count(output, conflictPrompt) != 1 ||
				strings.Count(output, "[y/N]") != 2 || planIndex < 0 || conflictIndex <= planIndex {
				t.Fatalf("want only the safe plan prompt followed by the destructive replacement prompt, with no per-entry adoption confirmations:\n%s", output)
			}
			for _, action := range []string{`adopt mcp-server "adoptable"`, `adopt mcp-server "second-adoptable"`, `replace mcp-server "conflicting"`} {
				if index := strings.Index(output, action); index < 0 || index >= planIndex {
					t.Fatalf("candidate %q was not listed before plan confirmation:\n%s", action, output)
				}
			}
			if !strings.Contains(output, "adopted 2 entr(y/ies) in mcp_config.json") {
				t.Fatalf("one plan confirmation must adopt both exact matches:\n%s", output)
			}
			if tc.replaceConflict != strings.Contains(output, "replaced mcp-server \"conflicting\" in mcp_config.json") {
				t.Fatalf("conflict replacement output does not match explicit consent:\n%s", output)
			}
			preview = devinRepairPreview(t, repoRoot, selection)
			wantConflicts := 1
			if tc.replaceConflict {
				wantConflicts = 0
			}
			if len(preview.Files) != 1 || len(preview.Files[0].Adoptions) != 0 || len(preview.Files[0].Conflicts) != wantConflicts {
				t.Fatalf("both adoptions must be recorded and conflict state must match consent: %#v", preview.Files)
			}
			after, err := os.ReadFile(mcpPath)
			if err != nil {
				t.Fatal(err)
			}
			if tc.replaceConflict {
				if bytes.Equal([]byte(withConflict), after) || bytes.Contains(after, []byte(conflictSecret)) ||
					!bytes.Contains(after, []byte("generated-binary")) {
					t.Fatalf("explicit destructive consent did not replace only the conflicting value:\n%s", after)
				}
				for _, want := range []string{"adoptable-binary", "second-adoptable-binary"} {
					if !bytes.Contains(after, []byte(want)) {
						t.Fatalf("replacement removed adopted entry %q:\n%s", want, after)
					}
				}
				backupsAfter, err := store.SafetyBackups(harness.DevinMcpConfigFile)
				if err != nil || len(backupsAfter) <= len(backupsBefore) {
					t.Fatalf("confirmed replacement did not create a fresh safety backup: before=%d after=%d err=%v", len(backupsBefore), len(backupsAfter), err)
				}
				foundBackup := false
				for _, backup := range backupsAfter[len(backupsBefore):] {
					raw, err := backup.RawBytes()
					if err != nil {
						t.Fatalf("read fresh safety backup: %v", err)
					}
					if bytes.Equal(raw, []byte(withConflict)) {
						foundBackup = true
					}
				}
				if !foundBackup {
					t.Fatal("no fresh safety backup contains the pre-replacement aggregate")
				}
			} else if !bytes.Equal([]byte(withConflict), after) || !bytes.Contains(after, []byte(conflictSecret)) {
				t.Fatalf("plan consent changed aggregate bytes or destroyed the conflict value:\n%s", after)
			}
		})
	}
}

// TestAdversarialNilStdinOnATTYDeclinesInsteadOfPanicking attacks the seam a
// caller can reach without a terminal: doctorOptions.in left nil while isTTY is
// set. runDoctor defaults opts.out but not opts.in, and confirmPrompt's
// "reader == nil" guard is unreachable because bufio.NewReader(nil) returns a
// non-nil reader wrapping a nil io.Reader. A prompt then dereferences nil.
//
// Whatever the answer is, it must be "no", and it must not be a panic.
func TestAdversarialNilStdinOnATTYDeclinesInsteadOfPanicking(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repoRoot, _ := devinDoctorEnv(t)
	mcpPath, selection := prepareDevinConflictFixture(t, repoRoot)
	before, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	err = runDoctor(doctorOptions{
		out:     &out,
		in:      nil,
		isTTY:   true,
		repair:  true,
		probe:   staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
		resolve: staticResolve(selection),
	})
	if err != nil {
		t.Fatalf("a nil stdin must decline, not fail: %v\n%s", err, out.String())
	}
	after, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("a nil stdin authorized a destructive replacement:\n%s", after)
	}
}

// TestAdversarialWholeRepairFlowLeaksNoSecretAtDebugLevel attacks the leak rule
// across EVERY sink the command touches at once — stdout and slog at Debug —
// with the secret placed in the three parts of an MCP entry that actually carry
// credentials: an env value, a header value, and a URL query string.
//
// Debug matters because it is the level a user turns on when a repair goes
// wrong, which is exactly when the aggregate is most likely to hold a token.
func TestAdversarialWholeRepairFlowLeaksNoSecretAtDebugLevel(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repoRoot, _ := devinDoctorEnv(t)
	outDir := filepath.Join(repoRoot, ".devin")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mcpPath := filepath.Join(outDir, harness.DevinMcpConfigFile)
	body := "{\n  \"mcpServers\": {\n    \"secretive\": {\n" +
		"      \"url\": \"https://example.invalid/mcp?token=" + conflictSecret + "-url\",\n" +
		"      \"headers\": { \"Authorization\": \"Bearer " + conflictSecret + "-hdr\" },\n" +
		"      \"env\": { \"API_KEY\": \"" + conflictSecret + "-env\" }\n" +
		"    }\n  }\n}\n"
	if err := os.WriteFile(mcpPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	selection := []resolver.ResolvedArtifact{devinMcpArtifact("secretive", "generated-binary")}
	if _, err := materialize.Run(context.Background(), repoRoot, selection); err != nil {
		t.Fatalf("seed sync: %v", err)
	}

	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	var out bytes.Buffer
	if err := runDoctor(doctorOptions{
		out:     &out,
		in:      strings.NewReader("y\n"),
		isTTY:   true,
		repair:  true,
		probe:   staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
		resolve: staticResolve(selection),
	}); err != nil {
		t.Fatalf("repair: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "replaced mcp-server \"secretive\"") {
		t.Fatalf("the fixture did not exercise the destructive path:\n%s", out.String())
	}
	combined := out.String() + "\n" + logs.String()
	for _, forbidden := range []string{
		conflictSecret, "Bearer", "Authorization", "example.invalid", "API_KEY", "generated-binary",
	} {
		if strings.Contains(combined, forbidden) {
			t.Fatalf("the repair flow leaked %q at debug level", forbidden)
		}
	}
}

// TestAdversarialCorruptOwnershipStateNeverReplaces attacks the fail-closed
// rule from the other side: with the durable record CORRUPT, every existing
// entry is unmanaged, so the conflict class nominates entries it must never be
// allowed to act on. Confirming must fail loudly and change nothing — a repair
// may not become the one way to mutate a file while state is corrupt.
func TestAdversarialCorruptOwnershipStateNeverReplaces(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repoRoot, _ := devinDoctorEnv(t)
	outDir := filepath.Join(repoRoot, ".devin")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mcpPath := filepath.Join(outDir, harness.DevinMcpConfigFile)
	body := "{\n  \"mcpServers\": {\n    \"conflicting\": { \"command\": \"/usr/local/bin/hand\", \"env\": { \"TOKEN\": \"" +
		conflictSecret + "\" } }\n  }\n}\n"
	if err := os.WriteFile(mcpPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	// "managed" syncs cleanly, which is what makes a durable record exist at
	// all; "conflicting" then collides forever.
	selection := []resolver.ResolvedArtifact{
		devinMcpArtifact("managed", "managed-binary"),
		devinMcpArtifact("conflicting", "generated-binary"),
	}
	if _, err := materialize.Run(context.Background(), repoRoot, selection); err != nil {
		t.Fatalf("seed sync: %v", err)
	}
	before, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatal(err)
	}

	stateDir := filepath.Join(os.Getenv("XDG_STATE_HOME"), "agent-marketplace", "ownership")
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		t.Fatalf("read ownership state dir: %v", err)
	}
	wrote := false
	for _, e := range entries {
		state := filepath.Join(stateDir, e.Name(), "state.json")
		if _, statErr := os.Stat(state); statErr != nil {
			continue
		}
		if err := os.WriteFile(state, []byte("{ this is not json"), 0o600); err != nil {
			t.Fatal(err)
		}
		wrote = true
	}
	if !wrote {
		t.Fatalf("fixture produced no ownership state file under %s; the corrupt-state scenario cannot be set up", stateDir)
	}

	var out bytes.Buffer
	err = runDoctor(doctorOptions{
		out:     &out,
		in:      strings.NewReader("y\ny\ny\n"),
		isTTY:   true,
		repair:  true,
		probe:   staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
		resolve: staticResolve(selection),
	})
	if err == nil {
		t.Fatalf("corrupt ownership state must exit 1:\n%s", out.String())
	}
	after, readErr := os.ReadFile(mcpPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("a repair mutated an aggregate while ownership state was corrupt:\n%s", after)
	}
}
