package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	toml "github.com/pelletier/go-toml/v2"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

func TestConvertHelpListsCodex(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	cmd := newConvertCmd()
	for _, name := range []string{"from", "to"} {
		flag := cmd.Flags().Lookup(name)
		if flag == nil {
			t.Fatalf("convert --%s flag is missing", name)
		}
		if !strings.Contains(flag.Usage, "codex") {
			t.Errorf("convert --%s help = %q, want codex", name, flag.Usage)
		}
	}
}

func TestConvertCommandConvertsClaudeSkillToFactory(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	srcDir := t.TempDir()
	writeClaudePluginSkill(t, srcDir, "demo-skill")

	outDir := filepath.Join(t.TempDir(), "factory-out")
	output, err := executeCommand("convert", "--from", "claude", "--to", "factory", "--out", outDir, srcDir)
	if err != nil {
		t.Fatalf("expected convert command to succeed: %v\n%s", err, output)
	}

	if !strings.Contains(output, "Converted claude -> factory") {
		t.Fatalf("expected conversion summary, got:\n%s", output)
	}

	assertFileExists(t, filepath.Join(outDir, "skills", "demo-skill", "SKILL.md"))
}

func TestConvertCommandRejectsMissingFrom(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	output, err := executeCommand("convert", "--to", "factory", t.TempDir())
	if err == nil {
		t.Fatalf("expected convert command to fail without --from, got output:\n%s", output)
	}
	if !strings.Contains(output, "--from is required") {
		t.Fatalf("expected missing --from error, got:\n%s", output)
	}
}

func TestConvertCommandCanonicalizesHarnessFlags(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	srcDir := t.TempDir()
	writeClaudePluginSkill(t, srcDir, "demo-skill")

	outDir := filepath.Join(t.TempDir(), "factory-out")
	output, err := executeCommand("convert", "--from", " claude ", "--to", " factory ", "--out", outDir, srcDir)
	if err != nil {
		t.Fatalf("expected padded harness flags to succeed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "Converted claude -> factory") {
		t.Fatalf("expected canonical conversion summary, got:\n%s", output)
	}
	assertFileExists(t, filepath.Join(outDir, "skills", "demo-skill", "SKILL.md"))
}

func TestConvertCommandRejectsCanonicalSourceOutputOverlap(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	tests := []struct {
		name  string
		sync  bool
		setup func(t *testing.T) (source, output string)
	}{
		{
			name: "equal",
			setup: func(t *testing.T) (string, string) {
				source := filepath.Join(t.TempDir(), "source")
				writeClaudePluginSkill(t, source, "must-not-change")
				return source, source
			},
		},
		{
			name: "output nested under source",
			sync: true,
			setup: func(t *testing.T) (string, string) {
				source := filepath.Join(t.TempDir(), "source")
				writeClaudePluginSkill(t, source, "must-not-change")
				return source, filepath.Join(source, "nested-output")
			},
		},
		{
			name: "source nested under output",
			setup: func(t *testing.T) (string, string) {
				output := filepath.Join(t.TempDir(), "output")
				source := filepath.Join(output, "nested-source")
				writeClaudePluginSkill(t, source, "must-not-change")
				return source, output
			},
		},
		{
			name: "symlink alias",
			setup: func(t *testing.T) (string, string) {
				root := t.TempDir()
				source := filepath.Join(root, "source")
				writeClaudePluginSkill(t, source, "must-not-change")
				output := filepath.Join(root, "output-alias")
				if err := os.Symlink(source, output); err != nil {
					t.Fatalf("symlink output: %v", err)
				}
				return source, output
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source, output := tt.setup(t)
			before, readErr := os.ReadFile(filepath.Join(source, "skills", "must-not-change", "SKILL.md"))
			if readErr != nil {
				t.Fatalf("read source before conversion: %v", readErr)
			}
			args := []string{"convert", "--from", "claude", "--to", "factory", "--out", output}
			if tt.sync {
				args = append(args, "--sync")
			}
			args = append(args, source)
			result, err := executeCommand(args...)
			if err == nil || !strings.Contains(result+err.Error(), "source and output paths overlap") {
				t.Fatalf("expected canonical overlap refusal, output=%q err=%v", result, err)
			}
			after, readErr := os.ReadFile(filepath.Join(source, "skills", "must-not-change", "SKILL.md"))
			if readErr != nil {
				t.Fatalf("read source after conversion: %v", readErr)
			}
			if string(after) != string(before) {
				t.Fatalf("source changed despite overlap refusal: before=%q after=%q", before, after)
			}
		})
	}
}

func TestCodexConversionResultTypedPolicyOverridesStaleLegacyExternal(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	tests := []struct {
		name   string
		policy hcc.ReconciliationPolicy
	}{
		{name: "replace", policy: hcc.ReconciliationPolicy{Mode: hcc.ReconciliationReplace}},
		{name: "marker", policy: hcc.ReconciliationPolicy{Mode: hcc.ReconciliationMarker, MarkerField: "_managed_by", MarkerValue: "agent-marketplace"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := codexConversionResult(&hcc.Result{Artifacts: []hcc.Artifact{{
				Path:                           codexConversionHooksPath,
				Bytes:                          []byte(`{"hooks":{}}`),
				Reconciliation:                 tt.policy,
				Fragment:                       tt.policy.Mode == hcc.ReconciliationMarker,
				ExternalReconciliationRequired: true,
			}}})
			if err != nil {
				t.Fatalf("codexConversionResult: %v", err)
			}
			if result.Artifacts[0].ExternalReconciliationRequired {
				t.Fatal("stale legacy external policy was retained")
			}
			got, err := result.Artifacts[0].Policy()
			if err != nil {
				t.Fatalf("converted policy: %v", err)
			}
			if got != tt.policy {
				t.Fatalf("converted policy = %+v, want %+v", got, tt.policy)
			}
		})
	}
}

func TestConversionRootRevalidationRejectsConcurrentSubstitution(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	root := filepath.Join(t.TempDir(), "source")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	snapshot, err := snapshotConversionRoot(root, true)
	if err != nil {
		t.Fatalf("snapshot conversion root: %v", err)
	}
	replacement := filepath.Join(t.TempDir(), "replacement")
	if err := os.Mkdir(replacement, 0o755); err != nil {
		t.Fatal(err)
	}
	startSubstitution := make(chan struct{})
	substitutionDone := make(chan error, 1)
	go func() {
		<-startSubstitution
		if err := os.Remove(root); err != nil {
			substitutionDone <- err
			return
		}
		substitutionDone <- os.Rename(replacement, root)
	}()
	close(startSubstitution)
	if err := <-substitutionDone; err != nil {
		t.Fatalf("substitute source root: %v", err)
	}
	if err := revalidateConversionRoot(snapshot); err == nil {
		t.Fatal("source root substitution was not rejected")
	}

	output := filepath.Join(t.TempDir(), "output")
	if err := os.Mkdir(output, 0o755); err != nil {
		t.Fatal(err)
	}
	outputSnapshot, err := snapshotConversionRoot(output, false)
	if err != nil {
		t.Fatalf("snapshot conversion output root: %v", err)
	}
	replacementOutput := filepath.Join(t.TempDir(), "replacement-output")
	if err := os.Mkdir(replacementOutput, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(output); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacementOutput, output); err != nil {
		t.Fatal(err)
	}
	if err := revalidateConversionRoot(outputSnapshot); err == nil {
		t.Fatal("output root substitution was not rejected")
	}
}

func TestConvertCommandToCodexPublishesCompleteSharedAggregates(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	stateRoot := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateRoot)

	srcDir := t.TempDir()
	writeClaudePluginSkill(t, srcDir, "demo-skill")
	writeClaudePluginSharedConfig(t, srcDir,
		`{"hooks":{"PreToolUse":[{"type":"command","command":"first-hook"}],"PostToolUse":[{"type":"command","command":"second-hook"}]}}`,
		`{"mcpServers":{"alpha":{"command":"alpha-server"},"beta":{"command":"beta-server","args":["--stdio"]}}}`,
	)

	outDir := filepath.Join(t.TempDir(), "codex-out")
	output, err := executeCommand("convert", "--from", "claude", "--to", "codex", "--out", outDir, srcDir)
	if err != nil {
		t.Fatalf("convert to Codex: %v\n%s", err, output)
	}

	hooks := readCodexHooks(t, filepath.Join(outDir, ".codex", "hooks.json"))
	for _, event := range []string{"PreToolUse", "PostToolUse"} {
		if len(hooks[event]) != 1 {
			t.Errorf("hooks[%q] has %d entries, want 1", event, len(hooks[event]))
		}
	}
	servers := readCodexMCPServers(t, filepath.Join(outDir, ".codex", "config.toml"))
	for _, name := range []string{"alpha", "beta"} {
		if _, ok := servers[name]; !ok {
			t.Errorf("Codex MCP aggregate is missing %q: %#v", name, servers)
		}
	}
	assertFileExists(t, filepath.Join(outDir, ".agents", "skills", "demo-skill", "SKILL.md"))

	if _, err := os.Stat(filepath.Join(stateRoot, "agent-marketplace", "ownership")); !os.IsNotExist(err) {
		t.Fatalf("conversion created durable ownership state: %v", err)
	}
}

func TestConvertCommandToCodexEmitsOnlyPresentSharedKinds(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	tests := []struct {
		name      string
		hooks     string
		mcp       string
		wantHooks bool
		wantMCP   bool
	}{
		{
			name:      "hooks only",
			hooks:     `{"hooks":{"PreToolUse":[{"type":"command","command":"lint"}]}}`,
			wantHooks: true,
		},
		{
			name:    "MCP only",
			mcp:     `{"mcpServers":{"docs":{"command":"docs-server"}}}`,
			wantMCP: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srcDir := t.TempDir()
			writeClaudePluginSharedConfig(t, srcDir, tt.hooks, tt.mcp)
			outDir := filepath.Join(t.TempDir(), "codex-out")
			output, err := executeCommand("convert", "--from", "claude", "--to", "codex", "--out", outDir, srcDir)
			if err != nil {
				t.Fatalf("convert to Codex: %v\n%s", err, output)
			}

			assertPathExistence(t, filepath.Join(outDir, ".codex", "hooks.json"), tt.wantHooks)
			assertPathExistence(t, filepath.Join(outDir, ".codex", "config.toml"), tt.wantMCP)
		})
	}
}

func TestConvertCommandToCodexRefusesExistingSharedFileBeforeWriting(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	for _, sharedPath := range []string{".codex/hooks.json", ".codex/config.toml"} {
		t.Run(sharedPath, func(t *testing.T) {
			srcDir := t.TempDir()
			writeClaudePluginSkill(t, srcDir, "must-not-land")
			writeClaudePluginSharedConfig(t, srcDir,
				`{"hooks":{"PreToolUse":[{"type":"command","command":"new-hook"}]}}`,
				`{"mcpServers":{"new-server":{"command":"new-server"}}}`,
			)
			outDir := filepath.Join(t.TempDir(), "codex-out")
			sharedFile := filepath.Join(outDir, filepath.FromSlash(sharedPath))
			const sentinel = "handwritten-content\n"
			writeFile(t, sharedFile, sentinel)
			policyFiles := map[string]string{
				filepath.Join(outDir, ".gitignore"):                  "user-policy\n",
				filepath.Join(outDir, ".codex", "trust-policy.toml"): "trusted = false\n",
			}
			for path, body := range policyFiles {
				writeFile(t, path, body)
			}

			output, err := executeCommand("convert", "--from", "claude", "--to", "codex", "--out", outDir, srcDir)
			if err == nil {
				t.Fatalf("expected existing %s to refuse conversion, got:\n%s", sharedPath, output)
			}
			message := output + err.Error()
			if !strings.Contains(message, "in-place conversion to Codex is unsupported") || !strings.Contains(message, "empty destination") {
				t.Fatalf("missing empty-destination guidance:\n%s", message)
			}
			got, readErr := os.ReadFile(sharedFile)
			if readErr != nil {
				t.Fatalf("read preserved shared file: %v", readErr)
			}
			if string(got) != sentinel {
				t.Fatalf("shared file changed: got %q, want %q", got, sentinel)
			}
			for path, want := range policyFiles {
				got, readErr := os.ReadFile(path)
				if readErr != nil || string(got) != want {
					t.Errorf("policy file %s changed: got %q, err=%v", path, got, readErr)
				}
			}
			assertPathExistence(t, filepath.Join(outDir, ".agents", "skills", "must-not-land", "SKILL.md"), false)
			other := filepath.Join(outDir, ".codex", "config.toml")
			if sharedPath == ".codex/config.toml" {
				other = filepath.Join(outDir, ".codex", "hooks.json")
			}
			assertPathExistence(t, other, false)
		})
	}
}

func TestConvertCommandToCodexSerializesConcurrentWriters(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	outDir := filepath.Join(t.TempDir(), "codex-out")

	type conversion struct {
		name string
		src  string
	}
	conversions := []conversion{{name: "alpha", src: t.TempDir()}, {name: "beta", src: t.TempDir()}}
	for _, conversion := range conversions {
		writeClaudePluginSkill(t, conversion.src, conversion.name)
		writeClaudePluginSharedConfig(t, conversion.src,
			`{"hooks":{"PreToolUse":[{"type":"command","command":"`+conversion.name+`-hook"}]}}`, "")
	}

	type result struct {
		name   string
		output string
		err    error
	}
	start := make(chan struct{})
	results := make(chan result, len(conversions))
	var wg sync.WaitGroup
	for _, conversion := range conversions {
		conversion := conversion
		wg.Go(func() {
			<-start
			// Concurrent production writers are separate CLI processes. Calling the
			// in-process Cobra helper concurrently instead couples both workers through
			// process-global root/logging initialization before conversion starts.
			var output strings.Builder
			err := runConvert(&output, conversion.src, "claude", "codex", outDir, false, false, false)
			results <- result{name: conversion.name, output: output.String(), err: err}
		})
	}
	close(start)
	wg.Wait()
	close(results)

	var winner string
	for result := range results {
		if result.err == nil {
			if winner != "" {
				t.Fatalf("both concurrent conversions succeeded (%s and %s)", winner, result.name)
			}
			winner = result.name
			continue
		}
		message := result.output + result.err.Error()
		if !strings.Contains(message, "empty destination") {
			t.Errorf("losing conversion %q lacks refusal guidance: %s", result.name, message)
		}
	}
	if winner == "" {
		t.Fatal("both concurrent conversions failed")
	}

	hooksRaw, err := os.ReadFile(filepath.Join(outDir, ".codex", "hooks.json"))
	if err != nil {
		t.Fatalf("read winning hooks: %v", err)
	}
	if !strings.Contains(string(hooksRaw), winner+"-hook") {
		t.Fatalf("hooks do not belong to winning conversion %q:\n%s", winner, hooksRaw)
	}
	for _, conversion := range conversions {
		assertPathExistence(t, filepath.Join(outDir, ".agents", "skills", conversion.name, "SKILL.md"), conversion.name == winner)
	}
}

func TestConvertCommandToCodexDryRunDoesNotWrite(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	stateRoot := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateRoot)
	srcDir := t.TempDir()
	writeClaudePluginSharedConfig(t, srcDir,
		`{"hooks":{"PreToolUse":[{"type":"command","command":"lint"}]}}`,
		`{"mcpServers":{"docs":{"command":"docs-server"}}}`,
	)
	outDir := filepath.Join(t.TempDir(), "codex-out")

	output, err := executeCommand("convert", "--from", "claude", "--to", "codex", "--out", outDir, "--dry-run", srcDir)
	if err != nil {
		t.Fatalf("dry-run conversion to Codex: %v\n%s", err, output)
	}
	assertPathExistence(t, outDir, false)
	assertPathExistence(t, filepath.Join(stateRoot, "agent-marketplace"), false)
}

func TestConvertCommandToCodexSyncPrunesStaleOutput(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	srcDir := t.TempDir()
	writeClaudePluginSkill(t, srcDir, "demo-skill")
	writeClaudePluginSharedConfig(t, srcDir,
		`{"hooks":{"PreToolUse":[{"type":"command","command":"lint"}]}}`, "")
	outDir := filepath.Join(t.TempDir(), "codex-out")
	stale := filepath.Join(outDir, "stale.txt")
	writeFile(t, stale, "stale")

	output, err := executeCommand("convert", "--from", "claude", "--to", "codex", "--out", outDir, "--sync", srcDir)
	if err != nil {
		t.Fatalf("sync conversion to Codex: %v\n%s", err, output)
	}
	assertPathExistence(t, stale, false)
	assertFileExists(t, filepath.Join(outDir, ".codex", "hooks.json"))
	assertFileExists(t, filepath.Join(outDir, ".agents", "skills", "demo-skill", "SKILL.md"))
}

func TestConvertSyncPreservesConcurrentlyModifiedStaleCandidate(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	outDir := t.TempDir()
	stalePath := filepath.Join(outDir, "stale.txt")
	writeFile(t, stalePath, "planned-old\n")
	planned, err := hcc.Materialize(&hcc.Result{Artifacts: []hcc.Artifact{{Path: "kept.txt", Bytes: []byte("kept\n")}}}, outDir, hcc.WithWriteMode(hcc.WriteModeSync), hcc.WithDryRun())
	if err != nil {
		t.Fatalf("plan stale prune: %v", err)
	}
	if len(planned.StaleCandidates) != 1 || planned.StaleCandidates[0].Path != "stale.txt" {
		t.Fatalf("stale candidates = %+v, want stale.txt", planned.StaleCandidates)
	}

	changed := make(chan struct{})
	go func() {
		_ = os.WriteFile(stalePath, []byte("external-writer\n"), 0o644)
		close(changed)
	}()
	<-changed

	removeCalled := false
	err = pruneCodexConversionStaleCandidatesWith(outDir, planned.StaleCandidates, func(path string) error {
		removeCalled = true
		return os.Remove(path)
	})
	if !errors.Is(err, hcc.ErrStalePruneConflict) {
		t.Fatalf("prune error = %v, want ErrStalePruneConflict", err)
	}
	if removeCalled {
		t.Fatal("stale candidate was removed after an external modification")
	}
	assertFileContents(t, stalePath, "external-writer\n")
}

func TestPublishCodexConversionRollsBackOnlyConversionArtifacts(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	stageDir := t.TempDir()
	outDir := t.TempDir()
	const (
		hooksRel    = ".codex/hooks.json"
		existingRel = ".agents/skills/existing/SKILL.md"
		newRel      = ".agents/skills/new/SKILL.md"
		unrelated   = "keep.txt"
	)
	writeFile(t, filepath.Join(stageDir, filepath.FromSlash(hooksRel)), `{"hooks":{}}`)
	writeFile(t, filepath.Join(stageDir, filepath.FromSlash(existingRel)), "replacement\n")
	writeFile(t, filepath.Join(stageDir, filepath.FromSlash(newRel)), "new\n")
	writeFile(t, filepath.Join(outDir, filepath.FromSlash(existingRel)), "original\n")
	writeFile(t, filepath.Join(outDir, unrelated), "unrelated\n")

	injected := errors.New("injected publication failure")
	publicationCount := 0
	err := publishCodexConversionWith(stageDir, outDir, []string{hooksRel, existingRel, newRel}, func(publication codexConversionPublication) error {
		publicationCount++
		if publicationCount == 3 {
			return injected
		}
		return publishCodexConversionFile(publication)
	})
	if !errors.Is(err, injected) {
		t.Fatalf("publish error = %v, want injected failure", err)
	}
	if publicationCount != 3 {
		t.Fatalf("publication calls = %d, want 3", publicationCount)
	}

	assertPathExistence(t, filepath.Join(outDir, filepath.FromSlash(hooksRel)), false)
	assertPathExistence(t, filepath.Join(outDir, filepath.FromSlash(newRel)), false)
	assertFileContents(t, filepath.Join(outDir, filepath.FromSlash(existingRel)), "original\n")
	assertFileContents(t, filepath.Join(outDir, unrelated), "unrelated\n")
	assertPathExistence(t, filepath.Join(outDir, ".codex"), false)
	assertPathExistence(t, filepath.Join(outDir, ".agents", "skills", "new"), false)
}

func TestPublishCodexConversionRollbackPreservesExternallyReplacedArtifact(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, initiallyExists := range []bool{false, true} {
		name := "created"
		if initiallyExists {
			name = "replaced"
		}
		t.Run(name, func(t *testing.T) {
			stageDir := t.TempDir()
			outDir := t.TempDir()
			const (
				firstRel  = ".agents/skills/first/SKILL.md"
				secondRel = ".agents/skills/second/SKILL.md"
			)
			writeFile(t, filepath.Join(stageDir, filepath.FromSlash(firstRel)), "conversion\n")
			writeFile(t, filepath.Join(stageDir, filepath.FromSlash(secondRel)), "must-not-land\n")
			if initiallyExists {
				writeFile(t, filepath.Join(outDir, filepath.FromSlash(firstRel)), "preimage\n")
			}

			injected := errors.New("injected publication failure")
			publicationCount := 0
			err := publishCodexConversionWith(stageDir, outDir, []string{firstRel, secondRel}, func(publication codexConversionPublication) error {
				publicationCount++
				if publicationCount == 2 {
					return injected
				}
				if err := publishCodexConversionFile(publication); err != nil {
					return err
				}
				if err := os.Remove(publication.dst); err != nil {
					return err
				}
				return os.WriteFile(publication.dst, []byte("external\n"), 0o644)
			})
			if !errors.Is(err, injected) {
				t.Fatalf("publish error = %v, want injected failure", err)
			}
			if !strings.Contains(err.Error(), "rollback conflict") {
				t.Fatalf("publish error = %v, want explicit rollback conflict", err)
			}
			assertFileContents(t, filepath.Join(outDir, filepath.FromSlash(firstRel)), "external\n")
			assertPathExistence(t, filepath.Join(outDir, filepath.FromSlash(secondRel)), false)
		})
	}
}

func TestPublishCodexConversionPreservesOrdinaryFileChangedAfterPlanning(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	stageDir, outDir := t.TempDir(), t.TempDir()
	const rel = ".agents/skills/demo/SKILL.md"
	dst := filepath.Join(outDir, filepath.FromSlash(rel))
	writeFile(t, filepath.Join(stageDir, filepath.FromSlash(rel)), "conversion\n")
	writeFile(t, dst, "preimage\n")

	err := publishCodexConversionWith(stageDir, outDir, []string{rel}, func(publication codexConversionPublication) error {
		if err := os.Remove(publication.dst); err != nil {
			return err
		}
		if err := os.WriteFile(publication.dst, []byte("external\n"), 0o644); err != nil {
			return err
		}
		return publishCodexConversionFile(publication)
	})
	if err == nil || !strings.Contains(err.Error(), "publication conflict") {
		t.Fatalf("publish error = %v, want publication conflict", err)
	}
	assertFileContents(t, dst, "external\n")
}

func TestPublishCodexConversionPreservesOrdinaryFileModifiedInPlaceAfterPlanning(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	stageDir, outDir := t.TempDir(), t.TempDir()
	const rel = ".agents/skills/demo/SKILL.md"
	dst := filepath.Join(outDir, filepath.FromSlash(rel))
	writeFile(t, filepath.Join(stageDir, filepath.FromSlash(rel)), "conversion\n")
	writeFile(t, dst, "preimage\n")

	err := publishCodexConversionWith(stageDir, outDir, []string{rel}, func(publication codexConversionPublication) error {
		if err := os.WriteFile(publication.dst, []byte("external\n"), 0o644); err != nil {
			return err
		}
		return publishCodexConversionFile(publication)
	})
	if err == nil || !strings.Contains(err.Error(), "publication conflict") {
		t.Fatalf("publish error = %v, want publication conflict", err)
	}
	assertFileContents(t, dst, "external\n")
}

func TestPublishCodexConversionPreservesWriterThatWinsDuringPublication(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	stageDir, outDir := t.TempDir(), t.TempDir()
	const rel = ".agents/skills/demo/SKILL.md"
	dst := filepath.Join(outDir, filepath.FromSlash(rel))
	writeFile(t, filepath.Join(stageDir, filepath.FromSlash(rel)), "conversion\n")
	writeFile(t, dst, "preimage\n")

	linkCalls := 0
	recoveryPath := ""
	err := publishCodexConversionWith(stageDir, outDir, []string{rel}, func(publication codexConversionPublication) error {
		recoveryPath = publication.backup
		return replaceCodexConversionFile(publication, func(oldPath, newPath string) error {
			linkCalls++
			if linkCalls == 1 {
				if err := os.WriteFile(publication.dst, []byte("external\n"), 0o644); err != nil {
					return err
				}
			}
			return os.Link(oldPath, newPath)
		})
	})
	if err == nil || !strings.Contains(err.Error(), "publication conflict") {
		t.Fatalf("publish error = %v, want publication conflict", err)
	}
	if recoveryPath == "" || !strings.Contains(err.Error(), recoveryPath) {
		t.Fatalf("publish error = %v, want recovery path %q", err, recoveryPath)
	}
	assertFileContents(t, dst, "external\n")
	assertFileContents(t, recoveryPath, "preimage\n")
}

func TestPublishCodexConversionRollbackPreservesInPlaceWriteAfterPublication(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	stageDir, outDir := t.TempDir(), t.TempDir()
	const (
		firstRel  = ".agents/skills/first/SKILL.md"
		secondRel = ".agents/skills/second/SKILL.md"
	)
	firstDst := filepath.Join(outDir, filepath.FromSlash(firstRel))
	writeFile(t, filepath.Join(stageDir, filepath.FromSlash(firstRel)), "conversion\n")
	writeFile(t, filepath.Join(stageDir, filepath.FromSlash(secondRel)), "must-not-land\n")
	writeFile(t, firstDst, "preimage\n")

	injected := errors.New("injected publication failure")
	publicationCount := 0
	recoveryPath := ""
	err := publishCodexConversionWith(stageDir, outDir, []string{firstRel, secondRel}, func(publication codexConversionPublication) error {
		publicationCount++
		if publicationCount == 2 {
			return injected
		}
		recoveryPath = publication.backup
		if err := publishCodexConversionFile(publication); err != nil {
			return err
		}
		return os.WriteFile(publication.dst, []byte("external\n"), 0o644)
	})
	if !errors.Is(err, injected) {
		t.Fatalf("publish error = %v, want injected failure", err)
	}
	if !strings.Contains(err.Error(), "rollback conflict") {
		t.Fatalf("publish error = %v, want explicit rollback conflict", err)
	}
	assertFileContents(t, firstDst, "external\n")
	if recoveryPath == "" || !strings.Contains(err.Error(), recoveryPath) {
		t.Fatalf("publish error = %v, want recovery path %q", err, recoveryPath)
	}
	assertFileContents(t, recoveryPath, "preimage\n")
	assertPathExistence(t, filepath.Join(outDir, filepath.FromSlash(secondRel)), false)
}

func TestReplaceCodexConversionFileRestoresOriginalWhenPublicationFails(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	stageDir, outDir := t.TempDir(), t.TempDir()
	const rel = "destination"
	writeFile(t, filepath.Join(stageDir, rel), "replacement\n")
	writeFile(t, filepath.Join(outDir, rel), "original\n")
	publications, err := planCodexConversionPublication(stageDir, outDir, []string{rel})
	if err != nil {
		t.Fatalf("plan publication: %v", err)
	}
	publication := publications[0]
	t.Cleanup(func() { removeEmptyCodexConversionRecoveryDirs(publications) })

	injected := errors.New("injected link failure")
	linkCalls := 0
	err = replaceCodexConversionFile(publication, func(oldPath, newPath string) error {
		linkCalls++
		if linkCalls == 1 {
			return injected
		}
		return os.Link(oldPath, newPath)
	})
	if !errors.Is(err, injected) {
		t.Fatalf("replace error = %v, want injected failure", err)
	}
	assertFileContents(t, publication.dst, "original\n")
	assertFileContents(t, publication.src, "replacement\n")
}

func TestCodexConversionStageParentUsesExistingOutputDirectory(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	outDir := filepath.Join(t.TempDir(), "existing-output")
	if err := os.Mkdir(outDir, 0o755); err != nil {
		t.Fatalf("create output directory: %v", err)
	}

	got, err := codexConversionStageParent(outDir)
	if err != nil {
		t.Fatalf("resolve staging parent: %v", err)
	}
	if got != outDir {
		t.Fatalf("staging parent = %q, want existing output directory %q", got, outDir)
	}
}

func TestPruneCodexConversionStaleReturnsAllErrors(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	outDir := t.TempDir()
	for _, rel := range []string{"first.txt", "second.txt"} {
		writeFile(t, filepath.Join(outDir, rel), "stale\n")
	}
	firstErr := errors.New("first prune failure")
	secondErr := errors.New("second prune failure")
	err := pruneCodexConversionStaleWith(outDir, []string{"first.txt", "second.txt"}, func(path string) error {
		if filepath.Base(path) == "first.txt" {
			return firstErr
		}
		return secondErr
	})
	if !errors.Is(err, firstErr) || !errors.Is(err, secondErr) {
		t.Fatalf("prune error = %v, want both injected failures", err)
	}
	assertFileContents(t, filepath.Join(outDir, "first.txt"), "stale\n")
	assertFileContents(t, filepath.Join(outDir, "second.txt"), "stale\n")
}

func TestPruneCodexConversionStaleStopsAtParentFixedPoint(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	var removed []string
	err := pruneCodexConversionStaleWith("root", []string{"../stale.txt"}, func(path string) error {
		removed = append(removed, path)
		if len(removed) > 2 {
			t.Fatal("parent cleanup did not stop when filepath.Dir stopped progressing")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("prune stale output: %v", err)
	}
	if got, want := strings.Join(removed, ","), strings.Join([]string{"stale.txt", "."}, ","); got != want {
		t.Fatalf("removed paths = %q, want %q", got, want)
	}
}

func TestFinishCodexConversionReportsFailedPruneAfterCommit(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	report := &hcc.WriteReport{Written: []string{codexConversionHooksPath}, Removed: []string{"stale.txt"}}
	injected := errors.New("injected prune failure")
	gotReport, err := finishCodexConversion("destination", report, nil, func(string, []string) error {
		return injected
	})
	if gotReport != report {
		t.Fatalf("report pointer changed: got %p, want %p", gotReport, report)
	}
	if !errors.Is(err, injected) {
		t.Fatalf("finish error = %v, want injected prune failure", err)
	}
	if !strings.Contains(err.Error(), "output was published") {
		t.Fatalf("finish error does not define the commit boundary: %v", err)
	}
}

func TestConvertCommandToDevinPublishesCompleteSharedAggregates(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	stateRoot := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateRoot)
	srcDir := t.TempDir()
	writeClaudePluginSkill(t, srcDir, "demo-skill")
	writeClaudePluginSharedConfig(t, srcDir,
		`{"hooks":{"PreToolUse":[{"type":"command","command":"lint"}]}}`,
		`{"mcpServers":{"docs":{"command":"docs-server"}}}`,
	)
	outDir := filepath.Join(t.TempDir(), "devin-out")
	output, err := executeCommand("convert", "--from", "claude", "--to", "devin", "--out", outDir, srcDir)
	if err != nil {
		t.Fatalf("convert to Devin: %v\\n%s", err, output)
	}
	assertFileExists(t, filepath.Join(outDir, "skills", "demo-skill", "SKILL.md"))
	assertFileExists(t, filepath.Join(outDir, "hooks.v1.json"))
	assertFileExists(t, filepath.Join(outDir, "mcp_config.json"))
	if _, err := os.Stat(filepath.Join(stateRoot, "agent-marketplace", "ownership")); !os.IsNotExist(err) {
		t.Fatalf("conversion created durable ownership state: %v", err)
	}
}

func TestConvertCommandToDevinSyncPrunesStaleOutput(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	srcDir := t.TempDir()
	writeClaudePluginSkill(t, srcDir, "stale-skill")
	outDir := filepath.Join(t.TempDir(), "devin-out")
	if output, err := executeCommand("convert", "--from", "claude", "--to", "devin", "--out", outDir, srcDir); err != nil {
		t.Fatalf("initial Devin conversion: %v\\n%s", err, output)
	}
	stale := filepath.Join(outDir, "stale.txt")
	writeFile(t, stale, "stale\\n")
	if err := os.RemoveAll(filepath.Join(srcDir, "skills", "stale-skill")); err != nil {
		t.Fatalf("remove source skill: %v", err)
	}
	output, err := executeCommand("convert", "--from", "claude", "--to", "devin", "--out", outDir, "--sync", srcDir)
	if err != nil {
		t.Fatalf("sync Devin conversion: %v\\n%s", err, output)
	}
	assertPathExistence(t, filepath.Join(outDir, "skills", "stale-skill", "SKILL.md"), false)
	assertPathExistence(t, stale, false)
}

func TestPruneCodexConversionStaleRejectsParentSymlinkSubstitution(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	outDir := t.TempDir()
	nested := filepath.Join(outDir, "nested")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatalf("create stale parent: %v", err)
	}
	stale := filepath.Join(nested, "stale.txt")
	writeFile(t, stale, "managed\\n")
	planned, err := hcc.Materialize(&hcc.Result{Artifacts: []hcc.Artifact{{Path: "kept.txt", Bytes: []byte("kept\\n")}}}, outDir, hcc.WithWriteMode(hcc.WriteModeSync), hcc.WithDryRun())
	if err != nil {
		t.Fatalf("plan stale prune: %v", err)
	}
	if len(planned.StaleCandidates) != 1 || planned.StaleCandidates[0].Path != "nested/stale.txt" {
		t.Fatalf("stale candidates = %+v, want nested/stale.txt", planned.StaleCandidates)
	}

	outside := t.TempDir()
	outsideStale := filepath.Join(outside, "stale.txt")
	writeFile(t, outsideStale, "outside\\n")
	if err := os.RemoveAll(nested); err != nil {
		t.Fatalf("replace stale parent: %v", err)
	}
	if err := os.Symlink(outside, nested); err != nil {
		t.Fatalf("create stale parent symlink: %v", err)
	}
	removeCalled := false
	err = pruneCodexConversionStaleCandidatesWith(outDir, planned.StaleCandidates, func(path string) error {
		removeCalled = true
		return os.Remove(path)
	})
	if !errors.Is(err, hcc.ErrStalePruneConflict) {
		t.Fatalf("prune error = %v, want ErrStalePruneConflict", err)
	}
	if removeCalled {
		t.Fatal("stale path was removed through a substituted parent symlink")
	}
	assertFileContents(t, outsideStale, "outside\\n")
}

func TestPruneCodexConversionStaleRejectsOutputRootSubstitution(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, substituteWithSymlink := range []bool{false, true} {
		name := "directory"
		if substituteWithSymlink {
			name = "symlink"
		}
		t.Run(name, func(t *testing.T) {
			outDir := t.TempDir()
			stale := filepath.Join(outDir, "stale.txt")
			writeFile(t, stale, "planned\\n")
			planned, err := hcc.Materialize(&hcc.Result{Artifacts: []hcc.Artifact{{Path: "kept.txt", Bytes: []byte("kept\\n")}}}, outDir, hcc.WithWriteMode(hcc.WriteModeSync), hcc.WithDryRun())
			if err != nil {
				t.Fatalf("plan stale prune: %v", err)
			}
			if len(planned.StaleCandidates) != 1 || planned.StaleCandidates[0].Path != "stale.txt" {
				t.Fatalf("stale candidates = %+v, want root-level stale.txt", planned.StaleCandidates)
			}

			replacementParent := t.TempDir()
			replacement := filepath.Join(replacementParent, "replacement")
			if err := os.Mkdir(replacement, 0o755); err != nil {
				t.Fatalf("create replacement root: %v", err)
			}
			replacementStale := filepath.Join(replacement, "stale.txt")
			writeFile(t, replacementStale, "replacement\\n")
			if err := os.RemoveAll(outDir); err != nil {
				t.Fatalf("remove planned root: %v", err)
			}
			if substituteWithSymlink {
				if err := os.Symlink(replacement, outDir); err != nil {
					t.Fatalf("substitute output root with symlink: %v", err)
				}
			} else if err := os.Rename(replacement, outDir); err != nil {
				t.Fatalf("substitute output root with directory: %v", err)
			}

			removeCalled := false
			err = pruneCodexConversionStaleCandidatesWith(outDir, planned.StaleCandidates, func(path string) error {
				removeCalled = true
				return os.Remove(path)
			})
			if !errors.Is(err, hcc.ErrStalePruneConflict) {
				t.Fatalf("prune error = %v, want ErrStalePruneConflict", err)
			}
			if !strings.Contains(err.Error(), "stale.txt") {
				t.Fatalf("prune error = %v, want the preserved root-level candidate named", err)
			}
			if removeCalled {
				t.Fatal("stale path was removed through a substituted output root")
			}
			assertFileContents(t, filepath.Join(outDir, "stale.txt"), "replacement\\n")
		})
	}
}

func TestConvertCommandToDevinSyncProtectsAllSharedAggregatesWhenSourceOmitsThem(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	srcDir := t.TempDir()
	writeClaudePluginSkill(t, srcDir, "skill-only")
	outDir := filepath.Join(t.TempDir(), "devin-out")
	if output, err := executeCommand("convert", "--from", "claude", "--to", "devin", "--out", outDir, srcDir); err != nil {
		t.Fatalf("initial Devin conversion: %v\\n%s", err, output)
	}
	shared := map[string]string{
		"config.json": `{
  "hooks": {
    "PreToolUse": []
  }
}
`,
		"hooks.v1.json": `{
  "PreToolUse": []
}
`,
		"mcp_config.json": `{
  "mcpServers": {}
}
`,
	}
	for rel, contents := range shared {
		writeFile(t, filepath.Join(outDir, rel), contents)
	}
	stale := filepath.Join(outDir, "stale.txt")
	writeFile(t, stale, "stale\\n")

	output, err := executeCommand("convert", "--from", "claude", "--to", "devin", "--out", outDir, "--sync", srcDir)
	if err == nil || !strings.Contains(output, `shared file "config.json" already exists`) {
		t.Fatalf("sync Devin conversion error = %v, output=%s", err, output)
	}
	for rel, want := range shared {
		assertFileContents(t, filepath.Join(outDir, rel), want)
	}
	assertPathExistence(t, stale, true)
}

func TestProtectConversionSharedCandidatesExcludesCompleteDevinSet(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	report := &hcc.WriteReport{
		Removed: []string{"config.json", "hooks.v1.json", "mcp_config.json", "stale.txt"},
		StaleCandidates: []hcc.StaleFileCandidate{
			{Path: "config.json"},
			{Path: "hooks.v1.json"},
			{Path: "mcp_config.json"},
			{Path: "stale.txt"},
		},
	}
	protectConversionSharedCandidates(report, devinConversionSharedPaths[:])
	if got, want := report.Removed, []string{"stale.txt"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Removed = %v, want %v", got, want)
	}
	if len(report.StaleCandidates) != 1 || report.StaleCandidates[0].Path != "stale.txt" {
		t.Fatalf("StaleCandidates = %+v, want stale.txt", report.StaleCandidates)
	}
}

func TestPublishDevinConversionUsesCollisionSafeRecovery(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	stageDir, outDir := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(stageDir, "a.txt"), "replacement\n")
	writeFile(t, filepath.Join(stageDir, "b.txt"), "second\n")
	writeFile(t, filepath.Join(outDir, "a.txt"), "original\n")
	const userBackup = "user backup must survive\n"
	writeFile(t, filepath.Join(outDir, "a.txt.backup"), userBackup)

	injected := errors.New("injected Devin publication failure")
	calls := 0
	err := publishConversionWith(stageDir, outDir, []string{"a.txt", "b.txt"}, nil, "Devin", func(publication codexConversionPublication) error {
		calls++
		if calls == 2 {
			return injected
		}
		return publishCodexConversionFile(publication)
	})
	if !errors.Is(err, injected) {
		t.Fatalf("publish error = %v, want injected failure", err)
	}
	assertFileContents(t, filepath.Join(outDir, "a.txt"), "original\n")
	assertFileContents(t, filepath.Join(outDir, "a.txt.backup"), userBackup)
	assertPathExistence(t, filepath.Join(outDir, "b.txt"), false)
}

func writeClaudePluginSkill(t *testing.T, root, name string) {
	t.Helper()
	writeFile(t, filepath.Join(root, ".claude-plugin", "plugin.json"), `{"name":"test-plugin","version":"1.0.0"}`)
	writeFile(t, filepath.Join(root, "skills", name, "SKILL.md"), `---
name: `+name+`
description: Demo skill
metadata:
  tags:
    - demo
---

# Demo skill
`)
	if _, err := os.Stat(filepath.Join(root, ".claude-plugin", "plugin.json")); err != nil {
		t.Fatalf("expected Claude plugin marker: %v", err)
	}
}

func writeClaudePluginSharedConfig(t *testing.T, root, hooks, mcp string) {
	t.Helper()
	writeFile(t, filepath.Join(root, ".claude-plugin", "plugin.json"), `{"name":"test-plugin","version":"1.0.0"}`)
	if hooks != "" {
		writeFile(t, filepath.Join(root, "hooks", "hooks.json"), hooks)
	}
	if mcp != "" {
		writeFile(t, filepath.Join(root, ".mcp.json"), mcp)
	}
}

func readCodexHooks(t *testing.T, path string) map[string][]json.RawMessage {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read Codex hooks: %v", err)
	}
	var document struct {
		Hooks map[string][]json.RawMessage `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("decode Codex hooks: %v\n%s", err, raw)
	}
	return document.Hooks
}

func readCodexMCPServers(t *testing.T, path string) map[string]map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read Codex MCP config: %v", err)
	}
	var document struct {
		Servers map[string]map[string]any `toml:"mcp_servers"`
	}
	if err := toml.Unmarshal(raw, &document); err != nil {
		t.Fatalf("decode Codex MCP config: %v\n%s", err, raw)
	}
	return document.Servers
}

func assertPathExistence(t *testing.T, path string, want bool) {
	t.Helper()
	_, err := os.Lstat(path)
	if want && err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
	if !want && !os.IsNotExist(err) {
		t.Fatalf("expected %s to be absent, got: %v", path, err)
	}
}

func assertFileContents(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != want {
		t.Fatalf("%s = %q, want %q", path, got, want)
	}
}
