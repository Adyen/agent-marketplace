package materialize

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"

	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
)

// writeDevinMcp writes a project-scope mcp_config.json verbatim.
func writeDevinMcp(t *testing.T, repoRoot, body string) string {
	t.Helper()
	root := devinProjectRoot(repoRoot)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, harness.DevinMcpConfigFile)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// conflictPreviewFor previews one root and returns the mcp_config.json file, or
// nil when the aggregate produced no repair file at all.
func conflictPreviewFor(t *testing.T, repoRoot string, selection []resolver.ResolvedArtifact) (*RepairPreview, *RepairFile) {
	t.Helper()
	preview, err := DevinRepairPreview(context.Background(), repoRoot, selection)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	for _, file := range preview.Files {
		if file.Rel == harness.DevinMcpConfigFile {
			return preview, file
		}
	}
	return preview, nil
}

// TestAdversarialConflictReplaceRefusesNonMcpAggregatesAndHooks attacks the
// MCP-only replace fence directly rather than through the preview that normally
// applies it: a ConflictCandidate for a HOOK, and one for an MCP-shaped entry
// in a file that is not mcp_config.json. Both must be refused before any store,
// document, or byte is touched.
//
// With a total hook identity, ValueConflictCandidates cannot nominate a real
// hook conflict because different values produce different identities. The
// candidate is constructed artificially here by re-using the same identity with
// different value digests, and the test verifies that devinConflictReplaceable
// still rejects it.
func TestAdversarialConflictReplaceRefusesNonMcpAggregatesAndHooks(t *testing.T) {
	hookID := ownership.EntryID{
		Kind:           ownership.KindHook,
		Container:      "PostAgentResponse",
		IdentityDigest: ownership.Digest([]byte("hook-identity")),
	}
	hookCandidates := ownership.ValueConflictCandidates(ownership.Proof{},
		[]ownership.EntryValue{{ID: hookID, ValueDigest: ownership.Digest([]byte("existing"))}},
		[]ownership.EntryValue{{ID: hookID, ValueDigest: ownership.Digest([]byte("desired"))}})
	if len(hookCandidates) != 1 {
		t.Fatalf("ownership nominated %d hook candidates, want 1 (the fence is what must reject it, not the nominator)", len(hookCandidates))
	}
	if devinConflictReplaceable(harness.DevinProjectHooksFile, hookID) {
		t.Fatal("a hook was declared replaceable; the MCP-only replace fence must reject hooks")
	}
	if devinConflictReplaceable(harness.DevinUserConfigFile, hookID) {
		t.Fatal("a hook in config.json was declared replaceable")
	}

	mcpID := ownership.EntryID{
		Kind:           ownership.KindMcpServer,
		Container:      "mcpServers",
		Key:            "server",
		IdentityDigest: ownership.McpIdentityDigest("server"),
	}
	for _, rel := range []string{harness.DevinProjectHooksFile, harness.DevinUserConfigFile} {
		if devinConflictReplaceable(rel, mcpID) {
			t.Fatalf("an mcp-server identity was declared replaceable in %q", rel)
		}
	}
	if devinConflictReplaceable(harness.DevinMcpConfigFile, ownership.EntryID{
		Kind: ownership.KindMcpServer, Container: "mcpServers", Key: "",
		IdentityDigest: ownership.McpIdentityDigest("")}) {
		t.Fatal("an empty MCP key was declared replaceable")
	}

	// The method itself re-applies the fence, so a caller holding a candidate
	// the preview would have filtered still cannot act on it.
	outDir := t.TempDir()
	store, err := ownership.Open(outDir)
	if err != nil {
		t.Fatal(err)
	}
	file := &RepairFile{
		Rel:       harness.DevinProjectHooksFile,
		OutDir:    outDir,
		Conflicts: []ConflictCandidate{{Candidate: hookCandidates[0], Resource: "some-hook"}},
		store:     store,
	}
	if _, err := file.ReplaceConflict(0, true); err == nil ||
		!strings.Contains(err.Error(), "is not a replaceable conflict") {
		t.Fatalf("ReplaceConflict on a hook error = %v, want a scope refusal", err)
	}
}

// TestAdversarialDuplicateMcpKeyIsNeverReplaceable attacks the "same key twice"
// shape: encoding/json would silently keep the last value, and a repair that
// replaced "the" member of a duplicated key would delete one value and record
// ownership of another. The aggregate must be refused as malformed instead.
func TestAdversarialDuplicateMcpKeyIsNeverReplaceable(t *testing.T) {
	repoRoot, _ := devinEnv(t)
	body := "{\n  \"mcpServers\": {\n" +
		"    \"dup\": { \"command\": \"/first\", \"env\": { \"TOKEN\": \"" + conflictSecret + "-1\" } },\n" +
		"    \"dup\": { \"command\": \"/second\", \"env\": { \"TOKEN\": \"" + conflictSecret + "-2\" } }\n" +
		"  }\n}\n"
	path := writeDevinMcp(t, repoRoot, body)
	before := readFileString(t, path)
	selection := []resolver.ResolvedArtifact{
		devinResolved("dup", ir.NewMcpServer("dup", "generated-binary", nil, nil, nil, ir.SourceRef{}), false),
	}

	preview, file := conflictPreviewFor(t, repoRoot, selection)
	if file != nil && len(file.Conflicts) > 0 {
		t.Fatalf("a duplicated MCP key produced %d replaceable conflicts", len(file.Conflicts))
	}
	if !preview.HasErrors() {
		t.Fatalf("a duplicate-key aggregate must be a corrupt-file error, got %v", preview.Diags)
	}
	for _, d := range preview.Diags {
		if strings.Contains(d.Message+d.Hint, conflictSecret) {
			t.Fatalf("diagnostic %s echoed a value from the offending file: %s", d.Code, d.Message)
		}
	}
	if got := readFileString(t, path); got != before {
		t.Fatalf("the preview mutated a malformed aggregate:\n%s", got)
	}
}

// TestAdversarialNonObjectMcpServersMemberIsNeverReplaceable attacks the
// container shape: mcpServers as an array, a string, and a number. None of them
// may produce a candidate, and none may be rewritten.
func TestAdversarialNonObjectMcpServersMemberIsNeverReplaceable(t *testing.T) {
	for _, shape := range []string{`[ {"name": "x"} ]`, `"not-an-object"`, `42`, `null`} {
		t.Run(shape, func(t *testing.T) {
			repoRoot, _ := devinEnv(t)
			path := writeDevinMcp(t, repoRoot, "{ \"mcpServers\": "+shape+" }\n")
			before := readFileString(t, path)
			selection := []resolver.ResolvedArtifact{
				devinResolved("x", ir.NewMcpServer("x", "generated-binary", nil, nil, nil, ir.SourceRef{}), false),
			}
			_, file := conflictPreviewFor(t, repoRoot, selection)
			if file != nil && len(file.Conflicts) > 0 {
				t.Fatalf("mcpServers=%s produced %d replaceable conflicts", shape, len(file.Conflicts))
			}
			if got := readFileString(t, path); got != before {
				t.Fatalf("mcpServers=%s was rewritten:\n%s", shape, got)
			}
		})
	}
}

// TestAdversarialConflictReplaceLeavesTheFileIntactWhenTheStagedWriteFails
// attacks the window between the mandatory backup and the rename: the staging
// file is written next to the aggregate, so a read-only install root fails
// there — after the backup exists and after the journal is durable.
//
// The aggregate must be the OLD bytes exactly (never a mix), and the backup
// must exist and hold them.
func TestAdversarialConflictReplaceLeavesTheFileIntactWhenTheStagedWriteFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	repoRoot, _ := devinEnv(t)
	path, selection := writeHandwrittenConflict(t, repoRoot, "genai-atlassian")
	if _, err := Run(context.Background(), repoRoot, selection); err != nil {
		t.Fatalf("seed sync: %v", err)
	}
	before := readFileString(t, path)
	_, file := conflictPreviewFor(t, repoRoot, selection)
	if file == nil || len(file.Conflicts) != 1 {
		t.Fatalf("fixture did not produce exactly one conflict candidate")
	}

	outDir := devinProjectRoot(repoRoot)
	if err := os.Chmod(outDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(outDir, 0o755) })

	if _, err := file.ReplaceConflict(0, true); err == nil {
		t.Fatal("a failed staged write must fail the replacement")
	}
	if got := readFileString(t, path); got != before {
		t.Fatalf("a failed replacement left a partially written aggregate:\n%s", got)
	}
	// No half-written staging artifact may be left behind in the install root.
	names, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if strings.Contains(n.Name(), ".am-staged-") {
			if data, readErr := os.ReadFile(filepath.Join(outDir, n.Name())); readErr == nil && len(data) > 0 {
				t.Fatalf("a non-empty staged file %q survived a failed replacement", n.Name())
			}
		}
	}

	// The user's bytes are recoverable: the mandatory backup was taken first.
	store, err := ownership.Open(outDir)
	if err != nil {
		t.Fatal(err)
	}
	backups, err := os.ReadDir(store.SafetyBackupDir(harness.DevinMcpConfigFile))
	if err != nil {
		t.Fatalf("no safety backup directory after a failed destructive repair: %v", err)
	}
	found := false
	for _, b := range backups {
		data, readErr := os.ReadFile(filepath.Join(store.SafetyBackupDir(harness.DevinMcpConfigFile), b.Name()))
		if readErr == nil && string(data) == before {
			found = true
		}
	}
	if !found {
		t.Fatal("no safety backup holds the pre-replacement bytes")
	}
}

// TestAdversarialConflictReplaceInBothScopesAtOnce attacks the two-root shape:
// mcp_config.json exists in BOTH the repo root and the machine root, and the
// SAME key conflicts in both. Each replacement must act on its own root and
// record ownership under its own root only.
func TestAdversarialConflictReplaceInBothScopesAtOnce(t *testing.T) {
	repoRoot, home := devinEnv(t)
	body := "{\n  \"mcpServers\": {\n    \"shared\": { \"command\": \"/hand/%s\", \"env\": { \"TOKEN\": \"" +
		conflictSecret + "-%s\" } }\n  }\n}\n"

	repoPath := writeDevinMcp(t, repoRoot, strings.NewReplacer("%s", "repo").Replace(body))
	userRoot := devinUserRoot(home)
	if err := os.MkdirAll(userRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	userPath := filepath.Join(userRoot, harness.DevinMcpConfigFile)
	if err := os.WriteFile(userPath, []byte(strings.NewReplacer("%s", "user").Replace(body)), 0o644); err != nil {
		t.Fatal(err)
	}

	selection := []resolver.ResolvedArtifact{
		devinResolved("shared", ir.NewMcpServer("shared", "generated-binary", nil, nil, nil, ir.SourceRef{}), false),
		devinResolved("shared", ir.NewMcpServer("shared", "generated-binary", nil, nil, nil, ir.SourceRef{}), true),
	}
	preview, err := DevinRepairPreview(context.Background(), repoRoot, selection)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	var files []*RepairFile
	for _, f := range preview.Files {
		if f.Rel == harness.DevinMcpConfigFile && len(f.Conflicts) == 1 {
			files = append(files, f)
		}
	}
	if len(files) != 2 {
		t.Fatalf("got %d conflicting mcp_config.json files, want one per scope", len(files))
	}

	// Replace the REPO one only. The machine-scope file must be byte-identical
	// afterwards, and its entry must still be unmanaged.
	userBefore := readFileString(t, userPath)
	var repoFile, userFile *RepairFile
	for _, f := range files {
		if f.MachineScope {
			userFile = f
		} else {
			repoFile = f
		}
	}
	if repoFile == nil || userFile == nil {
		t.Fatal("the preview did not produce one repair file per scope")
	}
	if _, err := repoFile.ReplaceConflict(0, true); err != nil {
		t.Fatalf("repo-scope replace: %v", err)
	}
	if got := readFileString(t, userPath); got != userBefore {
		t.Fatalf("replacing in the repo scope changed the machine-scope aggregate:\n%s", got)
	}
	if got := readFileString(t, repoPath); strings.Contains(got, conflictSecret) {
		t.Fatalf("the repo-scope value survived:\n%s", got)
	}

	userStore, err := ownership.Open(userRoot)
	if err != nil {
		t.Fatal(err)
	}
	proof, _ := userStore.Proof(harness.DevinMcpConfigFile)
	if proof.OwnedCount() != 0 {
		t.Fatalf("the machine root recorded %d owned entries from a repo-scope replacement", proof.OwnedCount())
	}

	// Now the machine one, from the SAME preview.
	if _, err := userFile.ReplaceConflict(0, true); err != nil {
		t.Fatalf("machine-scope replace: %v", err)
	}
	if got := readFileString(t, userPath); strings.Contains(got, conflictSecret) {
		t.Fatalf("the machine-scope value survived:\n%s", got)
	}
	repoStore, err := ownership.Open(devinProjectRoot(repoRoot))
	if err != nil {
		t.Fatal(err)
	}
	if repoStore.RootKey() == userStore.RootKey() {
		t.Fatal("the two scopes share an ownership root key")
	}
}

// TestAdversarialReplacedEntryIsOwnedEnoughToPrune closes the ownership
// invariant from the destructive side: a replaced entry must be PROVEN owned,
// which means a later sync that no longer selects it must PRUNE it. If the
// record were somehow weaker than a normal sync's, the entry would survive as
// unmanaged residue forever.
func TestAdversarialReplacedEntryIsOwnedEnoughToPrune(t *testing.T) {
	repoRoot, _ := devinEnv(t)
	path, selection := writeHandwrittenConflict(t, repoRoot, "genai-atlassian")
	if _, err := Run(context.Background(), repoRoot, selection); err != nil {
		t.Fatalf("seed sync: %v", err)
	}
	_, file := conflictPreviewFor(t, repoRoot, selection)
	if file == nil || len(file.Conflicts) != 1 {
		t.Fatal("fixture did not produce exactly one conflict candidate")
	}
	if _, err := file.ReplaceConflict(0, true); err != nil {
		t.Fatalf("replace: %v", err)
	}

	// Deselect the resource: the replaced entry is now a stale PROVEN owned
	// entry, which is the only class sync may prune.
	if _, err := Run(context.Background(), repoRoot, nil); err != nil {
		t.Fatalf("prune sync: %v", err)
	}
	after := readFileString(t, path)
	if strings.Contains(after, "generated-binary") {
		t.Fatalf("the replaced entry was not pruned once deselected, so it was not proven owned:\n%s", after)
	}
	if !strings.Contains(after, "leave-me-alone") {
		t.Fatalf("the prune removed an unmanaged hand-written entry:\n%s", after)
	}
}

// TestAdversarialConflictReplaceDoesNotDiscardAnIncompleteGeneration attacks
// the sequencing rule internal/ownership states outright: "Recover ... once,
// before any mutation". Sync calls Store.Recover; the repair path never does,
// because DevinRepairPreview is documented as never calling it and the mutating
// methods do not call it either.
//
// So a `doctor --repair` run performed while a generation is in flight writes a
// NEW generation straight over the journal. The interrupted generation's
// NextRecord — the only remaining description of entries that are already live
// in the aggregate — is destroyed, and those entries silently become unmanaged
// forever: exactly the closed loop this repair class exists to open.
//
// Either outcome is acceptable: recover first (the entry ends up owned), or
// refuse to mutate while a generation is pending. Quietly dropping it is not.
func TestAdversarialConflictReplaceDoesNotDiscardAnIncompleteGeneration(t *testing.T) {
	repoRoot, _ := devinEnv(t)
	// "gamma" is the hand-written conflict; "alpha" is the entry an interrupted
	// generation already published into the file but never committed ownership
	// for.
	handwritten := "{\n  \"mcpServers\": {\n    \"gamma\": { \"command\": \"/usr/local/bin/gamma\", \"env\": { \"TOKEN\": \"" +
		conflictSecret + "\" } }\n  }\n}\n"
	path := writeDevinMcp(t, repoRoot, handwritten)
	published := "{\n  \"mcpServers\": {\n    \"gamma\": { \"command\": \"/usr/local/bin/gamma\", \"env\": { \"TOKEN\": \"" +
		conflictSecret + "\" } },\n    \"alpha\": { \"command\": \"alpha-binary\" }\n  }\n}\n"

	outDir := devinProjectRoot(repoRoot)
	alpha := mcpEntryFromBytes(t, []byte(published), "alpha", "alpha")

	crashed, err := ownership.Open(outDir, ownership.WithCrasher(ownership.CrashAt(ownership.CrashAfterAggregateRename)))
	if err != nil {
		t.Fatal(err)
	}
	_, applyErr := crashed.Apply(ownership.Mutation{
		Rel:                   harness.DevinMcpConfigFile,
		AbsFile:               path,
		NextBytes:             []byte(published),
		Entries:               []ownership.Entry{alpha},
		Reason:                ownership.ReasonSync,
		ExpectPriorDigest:     ownership.Digest([]byte(handwritten)),
		ExpectPriorGeneration: 0,
	})
	if !errors.Is(applyErr, ownership.ErrInjectedCrash) {
		t.Fatalf("injected crash error = %v, want ErrInjectedCrash", applyErr)
	}
	journal, err := crashed.ReadJournal()
	if err != nil || journal == nil {
		t.Fatalf("fixture did not leave an in-flight generation: journal=%v err=%v", journal, err)
	}
	if got := readFileString(t, path); got != published {
		t.Fatalf("fixture did not publish the aggregate:\n%s", got)
	}

	selection := []resolver.ResolvedArtifact{
		devinResolved("gamma", ir.NewMcpServer("gamma", "generated-binary", nil, nil, nil, ir.SourceRef{}), false),
	}
	_, file := conflictPreviewFor(t, repoRoot, selection)
	if file == nil || len(file.Conflicts) != 1 {
		t.Fatalf("fixture did not produce exactly one conflict candidate")
	}

	_, replaceErr := file.ReplaceConflict(0, true)
	if replaceErr != nil {
		// Refusing while a generation is pending is a legitimate answer, as
		// long as nothing was destroyed and the interrupted generation is still
		// recoverable by the command that does recover.
		if !errors.Is(replaceErr, ErrRepairGenerationInFlight) {
			t.Fatalf("replace error = %v, want ErrRepairGenerationInFlight", replaceErr)
		}
		if got := readFileString(t, path); got != published {
			t.Fatalf("a refused replacement still changed the aggregate:\n%s", got)
		}
		still, err := crashed.ReadJournal()
		if err != nil || still == nil {
			t.Fatalf("the refused replacement destroyed the in-flight journal anyway: %v %v", still, err)
		}
		// sync recovers it, and the entry that was already live becomes owned.
		if _, err := Run(context.Background(), repoRoot, selection); err != nil {
			t.Fatalf("recovering sync: %v", err)
		}
		store, err := ownership.Open(outDir)
		if err != nil {
			t.Fatal(err)
		}
		if journal, err := store.ReadJournal(); err != nil || journal != nil {
			t.Fatalf("sync did not clear the in-flight journal: %v %v", journal, err)
		}
		return
	}

	// It replaced. Then the interrupted generation must have been recovered
	// first, so "alpha" — which is live in the file — is still proven owned.
	store, err := ownership.Open(outDir)
	if err != nil {
		t.Fatal(err)
	}
	proof, _ := store.Proof(harness.DevinMcpConfigFile)
	if proof.Classify(alpha.ID(), alpha.ValueDigest) != ownership.StatusOwnedClean {
		t.Fatalf("the repair discarded an in-flight generation's ownership record: "+
			"%q is live in the aggregate but is no longer proven owned (%d owned entries recorded)",
			alpha.Key, proof.OwnedCount())
	}
}

// mcpEntryFromBytes derives the ownership entry for one mcpServers member of an
// aggregate, using exactly the derivation the product uses, so a test fixture
// cannot record a digest the reader would disagree with.
func mcpEntryFromBytes(t *testing.T, raw []byte, key, resource string) ownership.Entry {
	t.Helper()
	doc, err := jsonc.Parse(harness.DevinMcpConfigFile, raw)
	if err != nil {
		t.Fatal(err)
	}
	container, _, err := ownedJSONContainerNode(doc, harness.DevinMcpConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := devinExistingEntries(harness.DevinMcpConfigFile, container)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.value.ID.Key != key {
			continue
		}
		return ownership.Entry{
			Kind:           e.value.ID.Kind,
			Container:      e.value.ID.Container,
			Key:            e.value.ID.Key,
			IdentityDigest: e.value.ID.IdentityDigest,
			ValueDigest:    e.value.ValueDigest,
			Resource:       resource,
		}
	}
	t.Fatalf("fixture has no mcpServers member %q", key)
	return ownership.Entry{}
}

// TestAdversarialUnsafeMcpKeyFailsClosedWithoutTouchingTheFile attacks the
// fence's own predicate: devinConflictReplaceable admits ANY non-empty key,
// while internal/ownership only accepts a key matching its non-secret token
// shape. A key with a space therefore reaches ReplaceConflict and is rejected
// by ownership.Apply — which must happen BEFORE the mandatory backup, so a
// refusal writes nothing at all.
func TestAdversarialUnsafeMcpKeyFailsClosedWithoutTouchingTheFile(t *testing.T) {
	for _, key := range []string{"has space", " ", "-leading-dash", "../escape"} {
		t.Run(key, func(t *testing.T) {
			repoRoot, _ := devinEnv(t)
			body := "{\n  \"mcpServers\": {\n    " + quoteJSON(t, key) +
				": { \"command\": \"/usr/local/bin/hand\", \"env\": { \"TOKEN\": \"" + conflictSecret + "\" } }\n  }\n}\n"
			path := writeDevinMcp(t, repoRoot, body)
			before := readFileString(t, path)
			selection := []resolver.ResolvedArtifact{
				devinResolved(key, ir.NewMcpServer(key, "generated-binary", nil, nil, nil, ir.SourceRef{}), false),
			}
			_, file := conflictPreviewFor(t, repoRoot, selection)
			if file == nil || len(file.Conflicts) == 0 {
				t.Logf("key %q was never nominated", key)
				return // never offered: also a correct answer
			}
			t.Logf("key %q WAS nominated; the refusal has to come from ownership.Apply", key)
			if _, err := file.ReplaceConflict(0, true); err == nil {
				t.Fatalf("key %q was replaced despite not being a recordable identity", key)
			}
			if got := readFileString(t, path); got != before {
				t.Fatalf("a refused replacement changed the file:\n%s", got)
			}
			store, err := ownership.Open(devinProjectRoot(repoRoot))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(store.SafetyBackupDir(harness.DevinMcpConfigFile)); err == nil {
				t.Fatal("a refused replacement took a safety backup, so it wrote before validating")
			}
		})
	}
}

func quoteJSON(t *testing.T, s string) string {
	t.Helper()
	out, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// TestAdversarialRepeatedConflictReplaceIsIdempotent attacks repetition: the
// second confirmed replacement of the same entry must be a no-op refusal, never
// a second destructive generation, and the file must not change again.
func TestAdversarialRepeatedConflictReplaceIsIdempotent(t *testing.T) {
	repoRoot, _ := devinEnv(t)
	path, selection := writeHandwrittenConflict(t, repoRoot, "genai-atlassian")
	if _, err := Run(context.Background(), repoRoot, selection); err != nil {
		t.Fatalf("seed sync: %v", err)
	}
	_, file := conflictPreviewFor(t, repoRoot, selection)
	if file == nil || len(file.Conflicts) != 1 {
		t.Fatal("fixture did not produce exactly one conflict candidate")
	}
	if _, err := file.ReplaceConflict(0, true); err != nil {
		t.Fatalf("first replace: %v", err)
	}
	replaced := readFileString(t, path)

	// The SAME stale RepairFile, replayed. The entry is now proven owned, so it
	// is no longer a conflict candidate at all.
	if _, err := file.ReplaceConflict(0, true); !errors.Is(err, ErrRepairCandidateStale) {
		t.Fatalf("second replace error = %v, want ErrRepairCandidateStale", err)
	}
	if got := readFileString(t, path); got != replaced {
		t.Fatalf("a replayed replacement changed the file again:\n%s", got)
	}

	// And a fresh preview offers nothing.
	_, again := conflictPreviewFor(t, repoRoot, selection)
	if again != nil && len(again.Conflicts) != 0 {
		t.Fatalf("a second preview re-nominated %d conflicts", len(again.Conflicts))
	}
}

// concurrentConflictReplaceFixture runs two `doctor --repair` replacements
// against ONE root at the same time and returns the resulting store, the parsed
// aggregate, and the user's original bytes.
//
// The two previews produce two independent *ownership.Store values, so the two
// racers hold two open file descriptions on the root's lock and genuinely
// exclude each other in the kernel rather than sharing a reentrant Store (one
// Store shared by two goroutines is explicitly NOT mutual exclusion between
// them; see ownership.Store.Lock).
func concurrentConflictReplaceFixture(t *testing.T) (*ownership.Store, *devinAggregateState, string) {
	t.Helper()
	repoRoot, _ := devinEnv(t)
	body := "{\n  \"mcpServers\": {\n" +
		"    \"aaa\": { \"command\": \"/hand/aaa\", \"env\": { \"TOKEN\": \"" + conflictSecret + "-A\" } },\n" +
		"    \"zzz\": { \"command\": \"/hand/zzz\", \"env\": { \"TOKEN\": \"" + conflictSecret + "-B\" } }\n" +
		"  }\n}\n"
	path := writeDevinMcp(t, repoRoot, body)
	original := readFileString(t, path)
	selection := []resolver.ResolvedArtifact{
		devinResolved("aaa", ir.NewMcpServer("aaa", "aaa-binary", nil, nil, nil, ir.SourceRef{}), false),
		devinResolved("zzz", ir.NewMcpServer("zzz", "zzz-binary", nil, nil, nil, ir.SourceRef{}), false),
	}
	if _, err := Run(context.Background(), repoRoot, selection); err != nil {
		t.Fatalf("seed sync: %v", err)
	}

	// Two independent previews, exactly as two processes would compute them.
	_, first := conflictPreviewFor(t, repoRoot, selection)
	_, second := conflictPreviewFor(t, repoRoot, selection)
	if first == nil || second == nil || len(first.Conflicts) != 2 || len(second.Conflicts) != 2 {
		t.Fatal("fixture did not produce two conflict candidates in each preview")
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _, _ = first.ReplaceConflict(0, true) }()
	go func() { defer wg.Done(); _, _ = second.ReplaceConflict(1, true) }()
	wg.Wait()

	outDir := devinProjectRoot(repoRoot)
	store, err := ownership.Open(outDir)
	if err != nil {
		t.Fatal(err)
	}
	state, diags := readDevinAggregateState(store, outputKey{target: hcc.TargetDevin, machineScope: false}, outDir, harness.DevinMcpConfigFile)
	if state == nil {
		t.Fatalf("the aggregate no longer parses after two concurrent repairs: %v", diags)
	}
	return store, state, original
}

// TestAdversarialConcurrentConflictReplacesKeepUserBytesRecoverable pins what
// DOES hold when two repairs race on one root: the aggregate is still whole and
// parseable (the rename is atomic, so a reader sees one version or the other,
// never a mix), ownership never claims an entry that is absent from the file,
// and the user's original bytes are still in a retained safety backup.
func TestAdversarialConcurrentConflictReplacesKeepUserBytesRecoverable(t *testing.T) {
	store, state, original := concurrentConflictReplaceFixture(t)

	byID := make(map[string]ownership.EntryValue, len(state.existingValues))
	for _, e := range state.existingValues {
		byID[devinIDKey(e.ID)] = e
	}
	for _, id := range state.proof.OwnedIDs() {
		if _, present := byID[devinIDKey(id)]; !present {
			t.Fatalf("ownership claims %q, which is not in the aggregate at all", id.Key)
		}
	}

	dir := store.SafetyBackupDir(harness.DevinMcpConfigFile)
	backups, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("no safety backups after two destructive repairs: %v", err)
	}
	for _, b := range backups {
		if data, readErr := os.ReadFile(filepath.Join(dir, b.Name())); readErr == nil && string(data) == original {
			return
		}
	}
	t.Fatal("no retained safety backup holds the user's original bytes")
}

// TestAdversarialConcurrentConflictReplacesForgeOwnership was the KNOWN-DEFECT
// repro of the agent-marketplace-0w2g review, kept skipped in the tree so the fix
// would have an executable acceptance test. Bead agent-marketplace-npwr (B4) is
// that fix and this is un-skipped.
//
// The defect: internal/ownership took no lock of any kind, and Store.Apply is not
// a compare-and-swap, so the record is read (recordedEntries), the aggregate is
// staged, renamed, and only then is the record committed. Two repairs racing on
// one root therefore published the file and the record at different instants, and
// the surviving record could claim an entry at a value digest the surviving file
// did not have.
//
// That is not merely a lost replacement. A recorded-but-mismatched entry is
// classified OwnedDrifted, and drift is "replaced from marketplace
// configuration" by the very next `sync` — silently, with no backup prompt and
// no per-entry confirmation. So the race converted a hand-written value that the
// user explicitly DECLINED to replace into one that sync overwrites on its own.
//
// # What makes it pass, and what this test does NOT prove
//
// The two racers hold two distinct *ownership.Store values (two independent
// previews), therefore two open file descriptions on locks/<rootKey>.lock,
// therefore they genuinely contend in the kernel — a flock belongs to the
// description, not to the process. Each ReplaceConflict now runs its
// requireCompleteGeneration, its re-derivation from the current file, its
// recordedEntries and its Apply inside ONE held session, so the loser runs
// entirely after the winner committed and stages from the file the winner
// published.
//
// It is NECESSARY BUT NOT SUFFICIENT in two separate ways, and both were
// measured rather than assumed (agent-marketplace-npwr).
//
// First, with the repair session replaced by a pass-through — B4 deleted, B1's
// lock and B2's lock-inside-Apply left in place — THIS TEST STILL PASSES at
// -count=50 -race. Apply's own lock keeps the surviving record internally
// consistent: the loser's recordedEntries() predates the winner's commit, so it
// commits a record that OMITS the winner's entry rather than one that claims a
// digest the file lacks, and this test only looks for the second thing. The
// behavioural consequence — the winner's confirmed replacement being silently
// overwritten by bytes the loser staged before it — is asserted by
// TestAdversarialConcurrentRepairsMustNotLoseAReplacement
// (devin_repair_session_test.go), which fails 20/20 with the session removed.
//
// Second, and as the design says explicitly
// (docs/plans/2026-08-06-ownership-locking-design.md, "The skipped repro",
// caveat 1): both racers are goroutines in ONE process, so a lazy fix — a
// sync.Mutex keyed by root in a package map — would turn this green while leaving
// the real two-terminal defect completely intact. The multi-PROCESS proof is
// bead B6's, with TestDevinSyncAndRepairSerializeAcrossProcesses
// (devin_repair_session_test.go) as B4's own cross-process contribution. Do not
// treat this test as the acceptance bar on its own.
func TestAdversarialConcurrentConflictReplacesForgeOwnership(t *testing.T) {
	_, state, _ := concurrentConflictReplaceFixture(t)
	byID := make(map[string]ownership.EntryValue, len(state.existingValues))
	for _, e := range state.existingValues {
		byID[devinIDKey(e.ID)] = e
	}
	for _, id := range state.proof.OwnedIDs() {
		existing, present := byID[devinIDKey(id)]
		if !present {
			t.Fatalf("ownership claims %q, which is absent from the aggregate entirely", id.Key)
		}
		if state.proof.Classify(id, existing.ValueDigest) == ownership.StatusOwnedDrifted {
			t.Fatalf("ownership claims %q at a digest the file does not have: the next sync "+
				"would overwrite a hand-written value as drift, with no confirmation", id.Key)
		}
	}
}

// TestAdversarialMalformedAggregateDiagnosticNeverEchoesAValue attacks the
// likeliest leak vector: a parse error. JSON parsers routinely quote the
// offending input, and a malformed mcp_config.json is full of secrets.
func TestAdversarialMalformedAggregateDiagnosticNeverEchoesAValue(t *testing.T) {
	repoRoot, _ := devinEnv(t)
	writeDevinMcp(t, repoRoot, "{ \"mcpServers\": { \"x\": { \"env\": { \"TOKEN\": \""+conflictSecret+"\" } }, } \n")
	selection := []resolver.ResolvedArtifact{
		devinResolved("x", ir.NewMcpServer("x", "generated-binary", nil, nil, nil, ir.SourceRef{}), false),
	}
	preview, err := DevinRepairPreview(context.Background(), repoRoot, selection)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if !preview.HasErrors() {
		t.Fatalf("a malformed aggregate must be an error, got %v", preview.Diags)
	}
	for _, d := range preview.Diags {
		if strings.Contains(d.Message+d.Hint, conflictSecret) {
			t.Fatalf("diagnostic %s leaked a value from the malformed file: %s", d.Code, d.Message)
		}
		if d.Code == diag.CodeMaterializeDevinAggregateMalformed && strings.Contains(d.Message, "TOKEN") {
			t.Fatalf("diagnostic %s echoed a key from inside an MCP value: %s", d.Code, d.Message)
		}
	}
}
