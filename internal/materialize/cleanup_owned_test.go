package materialize

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
)

func ownedCleanupFixture(t *testing.T, desired []string) (*RepairFile, ownership.EntryValue, []byte, *ownership.Record) {
	t.Helper()
	repoRoot, home := devinEnv(t)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	report, err := Run(context.Background(), repoRoot, devinMcpSelection("remove", "keep"))
	if err != nil || report == nil || len(report.Diags) != 0 {
		t.Fatalf("seed Run = %#v err=%v", report, err)
	}
	preview, err := DevinRepairPreview(context.Background(), repoRoot, devinMcpSelection(desired...))
	if err != nil {
		t.Fatal(err)
	}
	file := repairFileFor(t, preview, harness.DevinMcpConfigFile)
	state, diags := file.adapter.read(file.store, outputKey{target: file.Target, machineScope: file.MachineScope}, file.OutDir, file.Rel)
	if state == nil || len(diags) != 0 {
		t.Fatalf("cleanup state = %#v diagnostics=%v", state, diags)
	}
	var want ownership.EntryValue
	for _, existing := range state.existing {
		if existing.ID.Key == "remove" {
			want = existing
			break
		}
	}
	if want.ID.Key != "remove" || state.proof.Classify(want.ID, want.ValueDigest) != ownership.StatusOwnedClean {
		t.Fatalf("target cleanup value = %#v", want)
	}
	raw, err := os.ReadFile(file.AbsFile())
	if err != nil {
		t.Fatal(err)
	}
	record, err := file.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	return file, want, raw, record
}

func ownedCleanupDiskState(t *testing.T, file *RepairFile) map[string][]byte {
	t.Helper()
	state := map[string][]byte{}
	for _, root := range []string{file.AbsFile(), filepath.Dir(file.store.StateFile()), file.store.SafetyBackupDir(file.Rel)} {
		info, err := os.Stat(root)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if !info.IsDir() {
			data, err := os.ReadFile(root)
			if err != nil {
				t.Fatal(err)
			}
			state[root] = data
			continue
		}
		err = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			state[path] = data
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	parent := filepath.Dir(file.AbsFile())
	entries, err := os.ReadDir(parent)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	prefix := filepath.Base(file.AbsFile()) + ".am-staged-"
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			t.Fatal(err)
		}
		if !info.Mode().IsRegular() {
			t.Fatalf("unexpected staging entry %q with mode %v", entry.Name(), info.Mode())
		}
		path := filepath.Join(parent, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		state[path] = data
	}
	return state
}

func requireOwnedCleanupDiskState(t *testing.T, file *RepairFile, before map[string][]byte) {
	t.Helper()
	if after := ownedCleanupDiskState(t, file); !reflect.DeepEqual(before, after) {
		t.Fatalf("refused cleanup changed disk state: before=%#v after=%#v", before, after)
	}
}

func TestOwnedCleanupSnapshotDetectsStagingResidue(t *testing.T) {
	file, _, _, _ := ownedCleanupFixture(t, []string{"keep"})
	before := ownedCleanupDiskState(t, file)
	path := file.AbsFile() + ".am-staged-test"
	want := []byte("staged residue")
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatal(err)
	}
	after := ownedCleanupDiskState(t, file)
	if reflect.DeepEqual(before, after) || !bytes.Equal(after[path], want) {
		t.Fatalf("staging residue was not captured: before=%#v after=%#v", before, after)
	}
}

func cleanupMCPSelection(target hcc.Target, names ...string) []resolver.ResolvedArtifact {
	resolved := make([]resolver.ResolvedArtifact, 0, len(names))
	for _, name := range names {
		source := config.RepoCommitted
		if target == hcc.TargetCodex {
			source = config.UserPersonal
		}
		resolved = append(resolved, resolver.ResolvedArtifact{Entry: catalog.Entry{Name: name, Kind: ir.KindMcpServer, Resource: ir.NewMcpServer(name, name+"-binary", nil, nil, nil, ir.SourceRef{})}, Source: source, Harnesses: []hcc.Target{target}})
	}
	return resolved
}

func TestRemoveOwnedPreservesDriftedAndHandwrittenSiblings(t *testing.T) {
	for _, target := range []hcc.Target{hcc.TargetDevin, hcc.TargetCodex} {
		t.Run(string(target), func(t *testing.T) {
			repoRoot, home := devinEnv(t)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
			t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
			report, err := Run(context.Background(), repoRoot, cleanupMCPSelection(target, "remove", "keep"))
			if err != nil || report == nil || len(report.Diags) != 0 {
				t.Fatalf("seed Run = %#v err=%v", report, err)
			}
			var file *RepairFile
			var preview *RepairPreview
			if target == hcc.TargetDevin {
				preview, err = DevinRepairPreview(context.Background(), repoRoot, cleanupMCPSelection(target, "keep"))
				if err == nil {
					file = repairFileFor(t, preview, harness.DevinMcpConfigFile)
				}
			} else {
				preview, err = CodexRepairPreview(context.Background(), repoRoot, cleanupMCPSelection(target, "keep"))
				if err == nil {
					root, rootErr := filepath.EvalSymlinks(filepath.Join(home, ".codex"))
					if rootErr != nil {
						t.Fatal(rootErr)
					}
					file = codexRepairFileFor(t, preview, root, codexMCPFile)
				}
			}
			if err != nil || file == nil {
				t.Fatalf("preview = %#v err=%v", preview, err)
			}
			raw, err := os.ReadFile(file.AbsFile())
			if err != nil {
				t.Fatal(err)
			}
			var edited []byte
			if target == hcc.TargetDevin {
				edited = bytes.Replace(raw, []byte("keep-binary"), []byte("keep-edited"), 1)
				edited = bytes.Replace(edited, []byte("{\n  \"mcpServers\": {"), []byte("{\n  // retained comment\n  \"custom\": \"setting\",\n  \"mcpServers\": {\"handwritten\":{\"command\":\"manual\"},"), 1)
			} else {
				edited = bytes.Replace(raw, []byte("keep-binary"), []byte("keep-edited"), 1)
				edited = append([]byte("# retained comment\ncustom = \"setting\"\n"), edited...)
				edited = append(edited, []byte("\n[mcp_servers.handwritten]\ncommand = \"manual\"\n")...)
			}
			if bytes.Equal(raw, edited) || !bytes.Contains(edited, []byte("keep-edited")) || !bytes.Contains(edited, []byte("handwritten")) {
				t.Fatal("fixture did not create drifted and handwritten siblings")
			}
			if err := os.WriteFile(file.AbsFile(), edited, 0o600); err != nil {
				t.Fatal(err)
			}
			wantMode := os.FileMode(0o600)
			if target == hcc.TargetCodex {
				wantMode = 0o640
				if err := os.Chmod(file.AbsFile(), wantMode); err != nil {
					t.Fatal(err)
				}
			}
			state, diags := file.adapter.read(file.store, outputKey{target: file.Target, machineScope: file.MachineScope}, file.OutDir, file.Rel)
			if state == nil || len(diags) != 0 {
				t.Fatalf("state = %#v diagnostics=%v", state, diags)
			}
			var targetValue, keepValue ownership.EntryValue
			for _, existing := range state.existing {
				switch existing.ID.Key {
				case "remove":
					targetValue = existing
				case "keep":
					keepValue = existing
				}
			}
			if targetValue.ID.Key != "remove" || state.proof.Classify(targetValue.ID, targetValue.ValueDigest) != ownership.StatusOwnedClean || state.proof.Classify(keepValue.ID, keepValue.ValueDigest) != ownership.StatusOwnedDrifted {
				t.Fatalf("pre-cleanup classifications target=%#v keep=%#v", targetValue, keepValue)
			}
			var retainedSpan []byte
			if target == hcc.TargetDevin {
				retainedSpan = []byte(`"keep":{"command":"keep-edited"}`)
			} else {
				sections, _, err := codexMCPSections(edited)
				if err != nil {
					t.Fatal(err)
				}
				section, ok := sections["keep"]
				if !ok {
					t.Fatal("edited keep table missing")
				}
				retainedSpan = append([]byte(nil), edited[section.start:section.end]...)
			}
			if len(retainedSpan) == 0 || !bytes.Contains(edited, retainedSpan) {
				t.Fatal("retained raw span missing before cleanup")
			}
			recordBefore, err := file.store.Load()
			if err != nil {
				t.Fatal(err)
			}
			var keepRecord ownership.Entry
			for _, entry := range recordBefore.Files[file.Rel].Entries {
				if entry.Key == "keep" {
					keepRecord = entry
				}
			}
			if _, err := file.RemoveOwned(context.Background(), targetValue, true); err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(file.AbsFile())
			if err != nil || bytes.Contains(after, []byte("remove-binary")) || !bytes.Contains(after, []byte("keep-edited")) || !bytes.Contains(after, []byte("handwritten")) || !bytes.Contains(after, []byte("retained comment")) || !bytes.Contains(after, []byte("custom")) {
				t.Fatalf("preserved aggregate = %q err=%v", after, err)
			}
			if !bytes.Contains(after, retainedSpan) {
				t.Fatalf("retained raw span changed: %q", after)
			}
			commentAt := bytes.Index(after, []byte("retained comment"))
			customAt := bytes.Index(after, []byte("custom"))
			handwrittenAt := bytes.Index(after, []byte("handwritten"))
			keepAt := bytes.Index(after, []byte("keep-edited"))
			if commentAt < 0 || customAt <= commentAt || (target == hcc.TargetDevin && (customAt >= handwrittenAt || handwrittenAt >= keepAt)) || (target == hcc.TargetCodex && (customAt >= keepAt || keepAt >= handwrittenAt)) {
				t.Fatalf("retained ordering changed: %q", after)
			}
			info, err := os.Stat(file.AbsFile())
			if err != nil || info.Mode().Perm() != wantMode {
				t.Fatalf("mode = %v err=%v want=%v", info, err, wantMode)
			}
			recordAfter, err := file.store.Load()
			if err != nil {
				t.Fatal(err)
			}
			var keepAfter ownership.Entry
			for _, entry := range recordAfter.Files[file.Rel].Entries {
				if entry.Key == "keep" {
					keepAfter = entry
				}
			}
			if !reflect.DeepEqual(keepAfter, keepRecord) {
				t.Fatalf("keep metadata changed: before=%#v after=%#v", keepRecord, keepAfter)
			}
			fresh, freshDiags := file.adapter.read(file.store, outputKey{target: file.Target, machineScope: file.MachineScope}, file.OutDir, file.Rel)
			if fresh == nil || len(freshDiags) != 0 {
				t.Fatalf("fresh state = %#v diagnostics=%v", fresh, freshDiags)
			}
			keepFound := false
			for _, existing := range fresh.existing {
				if existing.ID.Key != "keep" {
					continue
				}
				keepFound = true
				if fresh.proof.Classify(existing.ID, existing.ValueDigest) != ownership.StatusOwnedDrifted {
					t.Fatalf("keep classification = %v", fresh.proof.Classify(existing.ID, existing.ValueDigest))
				}
			}
			if !keepFound {
				t.Fatal("drifted keep entry missing after cleanup")
			}
		})
	}
}

func TestRemoveOwnedRejectsRetargetedDestination(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*testing.T, *RepairFile, []byte) string
	}{
		{name: "nested copy", mutate: func(t *testing.T, file *RepairFile, raw []byte) string {
			dir := filepath.Join(file.OutDir, "nested")
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, file.Rel)
			if err := os.WriteFile(path, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			file.OutDir = dir
			return path
		}},
		{name: "external copy", mutate: func(t *testing.T, file *RepairFile, raw []byte) string {
			dir := t.TempDir()
			path := filepath.Join(dir, file.Rel)
			if err := os.WriteFile(path, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			file.OutDir = dir
			return path
		}},
		{name: "changed rel", mutate: func(_ *testing.T, file *RepairFile, _ []byte) string {
			file.Rel = "other.json"
			return ""
		}},
		{name: "parent rel", mutate: func(_ *testing.T, file *RepairFile, _ []byte) string {
			file.Rel = "../other.json"
			return ""
		}},
		{name: "absolute rel", mutate: func(t *testing.T, file *RepairFile, _ []byte) string {
			file.Rel = filepath.Join(t.TempDir(), "other.json")
			return ""
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file, want, original, _ := ownedCleanupFixture(t, []string{"keep"})
			originalPath := file.AbsFile()
			diskBefore := ownedCleanupDiskState(t, file)
			copyPath := tc.mutate(t, file, original)
			copyBefore := []byte(nil)
			if copyPath != "" {
				var err error
				copyBefore, err = os.ReadFile(copyPath)
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, err := file.RemoveOwned(context.Background(), want, true); !errors.Is(err, ErrRepairCandidateStale) {
				t.Fatalf("error = %v, want ErrRepairCandidateStale", err)
			}
			originalAfter, err := os.ReadFile(originalPath)
			if err != nil || !bytes.Equal(original, originalAfter) {
				t.Fatalf("original changed: %q err=%v", originalAfter, err)
			}
			if copyPath != "" {
				copyAfter, err := os.ReadFile(copyPath)
				if err != nil || !bytes.Equal(copyBefore, copyAfter) {
					t.Fatalf("copy changed: %q err=%v", copyAfter, err)
				}
			}
			file.OutDir = filepath.Dir(originalPath)
			file.Rel = filepath.Base(originalPath)
			requireOwnedCleanupDiskState(t, file, diskBefore)
		})
	}
	t.Run("symlink root alias", func(t *testing.T) {
		file, want, _, _ := ownedCleanupFixture(t, []string{"keep"})
		alias := filepath.Join(t.TempDir(), "alias")
		if err := os.Symlink(file.OutDir, alias); err != nil {
			t.Fatal(err)
		}
		file.OutDir = alias
		if _, err := file.RemoveOwned(context.Background(), want, true); err != nil {
			t.Fatalf("canonical root alias rejected: %v", err)
		}
	})
}

func TestRemoveOwnedRemovesOnlyProvenTarget(t *testing.T) {
	file, want, before, recordBefore := ownedCleanupFixture(t, []string{"keep"})
	result, err := file.RemoveOwned(context.Background(), want, true)
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(file.AbsFile())
	if err != nil {
		t.Fatal(err)
	}
	servers := nativeMCPDocument(t, after)["mcpServers"].(map[string]any)
	if _, exists := servers["remove"]; exists || servers["keep"] == nil {
		t.Fatalf("servers after cleanup = %#v", servers)
	}
	recordAfter, err := file.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	var keepBefore ownership.Entry
	for _, entry := range recordBefore.Files[file.Rel].Entries {
		if entry.Key == "keep" {
			keepBefore = entry
		}
	}
	if len(recordBefore.Files[file.Rel].Entries) != 2 || len(recordAfter.Files[file.Rel].Entries) != 1 || !reflect.DeepEqual(recordAfter.Files[file.Rel].Entries[0], keepBefore) {
		t.Fatalf("ownership entries changed unexpectedly: before=%#v after=%#v", recordBefore.Files[file.Rel].Entries, recordAfter.Files[file.Rel].Entries)
	}
	backup, err := os.ReadFile(result.BackupPath)
	if err != nil || !bytes.Equal(backup, before) {
		t.Fatalf("backup = %q err=%v", backup, err)
	}
	beforeInfo, err := os.Stat(file.AbsFile())
	if err != nil || beforeInfo.Mode().Perm()&0o077 != 0 {
		t.Fatalf("cleanup mode = %v err=%v", beforeInfo, err)
	}
}

func TestRemoveOwnedRejectsSelectedUnconfirmedCancelledAndNil(t *testing.T) {
	t.Run("selected", func(t *testing.T) {
		file, want, _, _ := ownedCleanupFixture(t, []string{"remove", "keep"})
		before := ownedCleanupDiskState(t, file)
		if _, err := file.RemoveOwned(context.Background(), want, true); !errors.Is(err, ErrRepairCandidateStale) {
			t.Fatalf("error = %v, want ErrRepairCandidateStale", err)
		}
		requireOwnedCleanupDiskState(t, file, before)
	})
	t.Run("unconfirmed", func(t *testing.T) {
		file, want, _, _ := ownedCleanupFixture(t, []string{"keep"})
		before := ownedCleanupDiskState(t, file)
		lockBefore, err := os.ReadFile(file.store.LockPath())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.RemoveOwned(context.Background(), want, false); !errors.Is(err, ownership.ErrConfirmationRequired) {
			t.Fatalf("error = %v, want ErrConfirmationRequired", err)
		}
		requireOwnedCleanupDiskState(t, file, before)
		lockAfter, err := os.ReadFile(file.store.LockPath())
		if err != nil || !bytes.Equal(lockBefore, lockAfter) {
			t.Fatalf("unconfirmed cleanup changed lock: %q err=%v", lockAfter, err)
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		file, want, _, _ := ownedCleanupFixture(t, []string{"keep"})
		before := ownedCleanupDiskState(t, file)
		lockBefore, err := os.ReadFile(file.store.LockPath())
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := file.RemoveOwned(ctx, want, true); !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
		requireOwnedCleanupDiskState(t, file, before)
		lockAfter, err := os.ReadFile(file.store.LockPath())
		if err != nil || !bytes.Equal(lockBefore, lockAfter) {
			t.Fatalf("cancelled cleanup changed lock: %q err=%v", lockAfter, err)
		}
	})
	t.Run("nil", func(t *testing.T) {
		var file *RepairFile
		if _, err := file.RemoveOwned(context.Background(), ownership.EntryValue{}, true); !errors.Is(err, ErrRepairCandidateStale) {
			t.Fatalf("error = %v, want ErrRepairCandidateStale", err)
		}
	})
	t.Run("zero", func(t *testing.T) {
		if _, err := new(RepairFile).RemoveOwned(context.Background(), ownership.EntryValue{}, true); !errors.Is(err, ErrRepairCandidateStale) {
			t.Fatalf("error = %v, want ErrRepairCandidateStale", err)
		}
	})
}

func TestRemoveOwnedRejectsPresentUnmanagedTarget(t *testing.T) {
	file, _, raw, _ := ownedCleanupFixture(t, []string{"keep"})
	edited := bytes.Replace(raw, []byte("{\"keep\""), []byte("{\"handwritten\":{\"command\":\"manual\"},\"keep\""), 1)
	if bytes.Equal(raw, edited) {
		t.Fatal("fixture did not add handwritten target")
	}
	if err := os.WriteFile(file.AbsFile(), edited, 0o600); err != nil {
		t.Fatal(err)
	}
	state, diags := file.adapter.read(file.store, outputKey{target: file.Target, machineScope: file.MachineScope}, file.OutDir, file.Rel)
	if state == nil || len(diags) != 0 {
		t.Fatalf("state = %#v diagnostics=%v", state, diags)
	}
	var handwritten ownership.EntryValue
	for _, existing := range state.existing {
		if existing.ID.Key == "handwritten" {
			handwritten = existing
		}
	}
	if handwritten.ID.Key != "handwritten" || state.proof.Classify(handwritten.ID, handwritten.ValueDigest) != ownership.StatusUnmanaged {
		t.Fatalf("handwritten target = %#v", handwritten)
	}
	before := ownedCleanupDiskState(t, file)
	if _, err := file.RemoveOwned(context.Background(), handwritten, true); !errors.Is(err, ErrRepairCandidateStale) {
		t.Fatalf("error = %v, want ErrRepairCandidateStale", err)
	}
	requireOwnedCleanupDiskState(t, file, before)
}

func TestRemoveOwnedRejectsWrongIdentityDigestAndMissingState(t *testing.T) {
	for _, tc := range []struct {
		name    string
		wantErr error
		mutate  func(*RepairFile, *ownership.EntryValue) error
	}{
		{name: "wrong digest", wantErr: ErrRepairCandidateStale, mutate: func(_ *RepairFile, want *ownership.EntryValue) error {
			want.ValueDigest = ownership.Digest([]byte("wrong"))
			return nil
		}},
		{name: "unknown identity", wantErr: ErrRepairCandidateStale, mutate: func(_ *RepairFile, want *ownership.EntryValue) error {
			want.ID.Key = "unknown"
			want.ID.IdentityDigest = ownership.Digest([]byte("unknown"))
			return nil
		}},
		{name: "missing state", wantErr: ErrRepairCandidateStale, mutate: func(file *RepairFile, _ *ownership.EntryValue) error {
			return os.Remove(file.store.StateFile())
		}},
		{name: "corrupt state", wantErr: ErrRepairCandidateStale, mutate: func(file *RepairFile, _ *ownership.EntryValue) error {
			return os.WriteFile(file.store.StateFile(), []byte("{"), 0o600)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file, want, before, _ := ownedCleanupFixture(t, []string{"keep"})
			if err := tc.mutate(file, &want); err != nil {
				t.Fatal(err)
			}
			diskBefore := ownedCleanupDiskState(t, file)
			if _, err := file.RemoveOwned(context.Background(), want, true); !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}
			requireOwnedCleanupDiskState(t, file, diskBefore)
			after, err := os.ReadFile(file.AbsFile())
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("refused cleanup changed aggregate: %q err=%v", after, err)
			}
		})
	}
}

func TestRemoveOwnedUsesOneWriteSession(t *testing.T) {
	file, want, _, _ := ownedCleanupFixture(t, []string{"keep"})
	counts := installRepairLockCounter(t)
	if _, err := file.RemoveOwned(context.Background(), want, true); err != nil {
		t.Fatal(err)
	}
	requireOneRepairSection(t, "RemoveOwned", counts)
}

func TestRemoveOwnedRejectsPendingGeneration(t *testing.T) {
	file, want, before, record := ownedCleanupFixture(t, []string{"keep"})
	crashed, err := ownership.Open(file.OutDir, ownership.WithCrasher(ownership.CrashAt(ownership.CrashAfterJournalPrepared)))
	if err != nil {
		t.Fatal(err)
	}
	_, err = crashed.Apply(ownership.Mutation{Rel: file.Rel, AbsFile: file.AbsFile(), NextBytes: append(append([]byte(nil), before...), '\n'), Entries: record.Files[file.Rel].Entries, Reason: ownership.ReasonSync, ExpectPriorGeneration: record.Generation, ExpectPriorDigest: ownership.Digest(before)})
	if !errors.Is(err, ownership.ErrInjectedCrash) {
		t.Fatalf("crash error = %v, want ErrInjectedCrash", err)
	}
	journalBefore, err := os.ReadFile(crashed.JournalFile())
	if err != nil {
		t.Fatal(err)
	}
	recordBefore, err := os.ReadFile(crashed.StateFile())
	if err != nil {
		t.Fatal(err)
	}
	diskBefore := ownedCleanupDiskState(t, file)
	if _, err := file.RemoveOwned(context.Background(), want, true); !errors.Is(err, ErrRepairGenerationInFlight) {
		t.Fatalf("error = %v, want ErrRepairGenerationInFlight", err)
	}
	after, err := os.ReadFile(file.AbsFile())
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("pending cleanup changed aggregate: %q err=%v", after, err)
	}
	journalAfter, journalErr := os.ReadFile(crashed.JournalFile())
	recordAfter, recordErr := os.ReadFile(crashed.StateFile())
	if journalErr != nil || recordErr != nil || !bytes.Equal(journalBefore, journalAfter) || !bytes.Equal(recordBefore, recordAfter) {
		t.Fatalf("pending state changed: journalErr=%v recordErr=%v", journalErr, recordErr)
	}
	requireOwnedCleanupDiskState(t, file, diskBefore)
}

func TestRemoveOwnedSequentialHooksAndDuplicate(t *testing.T) {
	t.Run("sequential distinct hooks", func(t *testing.T) {
		repoRoot, home := devinEnv(t)
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
		t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
		selection := []resolver.ResolvedArtifact{
			devinResolved(t, "first", ir.NewHook("first", "PostToolUse", "echo first", "command", nil, ir.SourceRef{}), false),
			devinResolved(t, "second", ir.NewHook("second", "PostToolUse", "echo second", "command", nil, ir.SourceRef{}), false),
		}
		report, err := Run(context.Background(), repoRoot, selection)
		if err != nil || report == nil || len(nonReviewDiags(report)) != 0 {
			t.Fatalf("seed Run = %#v err=%v", report, err)
		}
		preview, err := DevinRepairPreview(context.Background(), repoRoot, nil)
		if err != nil {
			t.Fatal(err)
		}
		file := repairFileFor(t, preview, harness.DevinProjectHooksFile)
		state, diags := file.adapter.read(file.store, outputKey{target: file.Target, machineScope: file.MachineScope}, file.OutDir, file.Rel)
		if state == nil || len(diags) != 0 || len(state.existing) != 2 {
			t.Fatalf("hook state = %#v diagnostics=%v", state, diags)
		}
		if _, err := file.RemoveOwned(context.Background(), state.existing[0], true); err != nil {
			t.Fatal(err)
		}
		firstAfter, err := os.ReadFile(file.AbsFile())
		if err != nil || bytes.Contains(firstAfter, []byte("echo first")) == bytes.Contains(firstAfter, []byte("echo second")) {
			t.Fatalf("first hook cleanup = %q err=%v", firstAfter, err)
		}
		state, diags = file.adapter.read(file.store, outputKey{target: file.Target, machineScope: file.MachineScope}, file.OutDir, file.Rel)
		if state == nil || len(diags) != 0 || len(state.existing) != 1 {
			t.Fatalf("fresh hook state = %#v diagnostics=%v", state, diags)
		}
		if _, err := file.RemoveOwned(context.Background(), state.existing[0], true); err != nil {
			t.Fatal(err)
		}
		secondAfter, err := os.ReadFile(file.AbsFile())
		if err != nil || bytes.Contains(secondAfter, []byte("echo first")) || bytes.Contains(secondAfter, []byte("echo second")) {
			t.Fatalf("second hook cleanup = %q err=%v", secondAfter, err)
		}
	})
	t.Run("identical handwritten copy remains", func(t *testing.T) {
		repoRoot, home := devinEnv(t)
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
		t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
		selection := []resolver.ResolvedArtifact{devinResolved(t, "hook", ir.NewHook("hook", "PostToolUse", "echo duplicate", "command", nil, ir.SourceRef{}), false)}
		report, err := Run(context.Background(), repoRoot, selection)
		if err != nil || report == nil || len(nonReviewDiags(report)) != 0 {
			t.Fatalf("seed Run = %#v err=%v", report, err)
		}
		path := filepath.Join(devinProjectRoot(repoRoot), harness.DevinProjectHooksFile)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var document map[string]any
		if err := json.Unmarshal(raw, &document); err != nil {
			t.Fatal(err)
		}
		groups := document["PostToolUse"].([]any)
		document["PostToolUse"] = append(groups, groups[0])
		duplicated, err := json.MarshalIndent(document, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		duplicated = append(duplicated, '\n')
		if err := os.WriteFile(path, duplicated, 0o600); err != nil {
			t.Fatal(err)
		}
		preview, err := DevinRepairPreview(context.Background(), repoRoot, nil)
		if err != nil {
			t.Fatal(err)
		}
		file := repairFileFor(t, preview, harness.DevinProjectHooksFile)
		state, diags := file.adapter.read(file.store, outputKey{target: file.Target, machineScope: file.MachineScope}, file.OutDir, file.Rel)
		if state == nil || len(diags) != 0 || len(state.existing) != 2 || state.existing[0] != state.existing[1] {
			t.Fatalf("duplicate hook state = %#v diagnostics=%v", state, diags)
		}
		if _, err := file.RemoveOwned(context.Background(), state.existing[0], true); err != nil {
			t.Fatal(err)
		}
		after, err := os.ReadFile(path)
		if err != nil || bytes.Count(after, []byte("echo duplicate")) != 1 {
			t.Fatalf("duplicate cleanup = %q err=%v", after, err)
		}
	})
}

type cleanupForeignMutation struct {
	path string
	raw  []byte
}

func (m cleanupForeignMutation) Crash(point ownership.CrashPoint) error {
	if point == ownership.CrashAfterStagedWrite {
		return os.WriteFile(m.path, m.raw, 0o600)
	}
	return nil
}

func TestRemoveOwnedRefusesForeignCASMutation(t *testing.T) {
	file, want, _, _ := ownedCleanupFixture(t, []string{"keep"})
	foreign := []byte("{\n  \"mcpServers\": {\"foreign\": {\"command\": \"manual\"}}\n}\n")
	store, err := ownership.Open(file.OutDir, ownership.WithCrasher(cleanupForeignMutation{path: file.AbsFile(), raw: foreign}))
	if err != nil {
		t.Fatal(err)
	}
	file.store = store
	before := ownedCleanupDiskState(t, file)
	if _, err := file.RemoveOwned(context.Background(), want, true); !errors.Is(err, ownership.ErrGenerationConflict) {
		t.Fatalf("error = %v, want ErrGenerationConflict", err)
	}
	expected := make(map[string][]byte, len(before))
	for path, data := range before {
		expected[path] = bytes.Clone(data)
	}
	expected[file.AbsFile()] = foreign
	after := ownedCleanupDiskState(t, file)
	if !reflect.DeepEqual(expected, after) {
		for path, want := range expected {
			if got, ok := after[path]; !ok || !bytes.Equal(want, got) {
				t.Errorf("CAS path %q = %q present=%v, want %q", path, got, ok, want)
			}
		}
		for path := range after {
			if _, ok := expected[path]; !ok {
				t.Errorf("CAS left unexpected path %q", path)
			}
		}
		t.FailNow()
	}
}

func cleanupCodexHookSelection(t testing.TB, commands ...string) []resolver.ResolvedArtifact {
	t.Helper()
	selection := make([]resolver.ResolvedArtifact, 0, len(commands))
	for i, command := range commands {
		name := fmt.Sprintf("hook-%d", i)
		sourceRoot := t.TempDir()
		definitionPath := filepath.Join(sourceRoot, "hooks", "hooks.json")
		definition := fmt.Sprintf(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":%q}]}]}}`, command)
		if err := os.MkdirAll(filepath.Dir(definitionPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(definitionPath, []byte(definition), 0o644); err != nil {
			t.Fatal(err)
		}
		loaded, err := catalog.Load(sourceRoot, "claude")
		if err != nil {
			t.Fatalf("Claude hook catalog: %v", err)
		}
		if len(loaded.Entries) != 1 || loaded.Entries[0].Kind != ir.KindHook {
			t.Fatalf("Claude source hooks = %+v, want one hook", loaded.Entries)
		}
		entry := loaded.Entries[0]
		hook, ok := entry.Resource.(*ir.Hook)
		if !ok || hook.Source.File != "hooks/hooks.json" || hook.SourceDefinitionDigest() == "" {
			t.Fatalf("Claude source hook is not bound to its written definition: %#v", entry.Resource)
		}
		entry.Name = name
		entry.Path = definitionPath
		selection = append(selection, resolver.ResolvedArtifact{
			Entry:       entry,
			Source:      config.UserPersonal,
			RegistryURL: sourceRoot,
			CatalogKey:  sourceRoot,
			V4Kind:      config.V4SourceDirectory,
			Harnesses:   []hcc.Target{hcc.TargetCodex},
		})
	}
	return selection
}

func TestRemoveOwnedCodexRawHooks(t *testing.T) {
	t.Run("sequential and wrong digest", func(t *testing.T) {
		repoRoot, home := devinEnv(t)
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
		t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
		report, err := Run(context.Background(), repoRoot, cleanupCodexHookSelection(t, "./first", "./second"))
		if err != nil || report == nil || len(nonReviewDiags(report)) != 0 {
			t.Fatalf("seed Run = %#v err=%v", report, err)
		}
		preview, err := CodexRepairPreview(context.Background(), repoRoot, nil)
		if err != nil {
			t.Fatal(err)
		}
		root, err := filepath.EvalSymlinks(filepath.Join(home, ".codex"))
		if err != nil {
			t.Fatal(err)
		}
		file := codexRepairFileFor(t, preview, root, codexHooksFile)
		state, diags := file.adapter.read(file.store, outputKey{target: file.Target, machineScope: file.MachineScope}, file.OutDir, file.Rel)
		if state == nil || len(diags) != 0 || len(state.existing) != 2 {
			t.Fatalf("state = %#v diagnostics=%v", state, diags)
		}
		if _, err := file.RemoveOwned(context.Background(), state.existing[0], true); err != nil {
			t.Fatal(err)
		}
		fresh, freshDiags := file.adapter.read(file.store, outputKey{target: file.Target, machineScope: file.MachineScope}, file.OutDir, file.Rel)
		if fresh == nil || len(freshDiags) != 0 || len(fresh.existing) != 1 {
			t.Fatalf("fresh = %#v diagnostics=%v", fresh, freshDiags)
		}
		wrong := fresh.existing[0]
		wrong.ValueDigest = ownership.Digest([]byte("wrong raw hook"))
		before := ownedCleanupDiskState(t, file)
		if _, err := file.RemoveOwned(context.Background(), wrong, true); !errors.Is(err, ErrRepairCandidateStale) {
			t.Fatalf("wrong raw digest error = %v", err)
		}
		requireOwnedCleanupDiskState(t, file, before)
		if _, err := file.RemoveOwned(context.Background(), fresh.existing[0], true); err != nil {
			t.Fatal(err)
		}
		after, err := os.ReadFile(file.AbsFile())
		if err != nil || bytes.Contains(after, []byte("./first")) || bytes.Contains(after, []byte("./second")) {
			t.Fatalf("sequential Codex cleanup = %q err=%v", after, err)
		}
	})
	t.Run("identical handwritten copy remains", func(t *testing.T) {
		repoRoot, home := devinEnv(t)
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
		t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
		report, err := Run(context.Background(), repoRoot, cleanupCodexHookSelection(t, "./duplicate"))
		if err != nil || report == nil || len(nonReviewDiags(report)) != 0 {
			t.Fatalf("seed Run = %#v err=%v", report, err)
		}
		root, err := filepath.EvalSymlinks(filepath.Join(home, ".codex"))
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, codexHooksFile)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		document, err := jsonc.Parse(codexHooksFile, raw)
		if err != nil {
			t.Fatal(err)
		}
		hooks, ok := document.Root().Member(codexHooksContainer)
		if !ok {
			t.Fatal("Codex hooks container missing")
		}
		event, ok := hooks.Member("PreToolUse")
		if !ok || event.Len() != 1 {
			t.Fatalf("PreToolUse event = %#v", event)
		}
		element, ok := event.Element(0)
		if !ok {
			t.Fatal("PreToolUse element missing")
		}
		elementRaw := append([]byte(nil), element.Raw()...)
		if bytes.Count(raw, elementRaw) != 1 {
			t.Fatalf("owned element count = %d", bytes.Count(raw, elementRaw))
		}
		replacement := make([]byte, 0, len(elementRaw)*2+2)
		replacement = append(replacement, elementRaw...)
		replacement = append(replacement, ',', '\n')
		replacement = append(replacement, elementRaw...)
		duplicated := bytes.Replace(raw, elementRaw, replacement, 1)
		if err := os.WriteFile(path, duplicated, 0o600); err != nil {
			t.Fatal(err)
		}
		preview, err := CodexRepairPreview(context.Background(), repoRoot, nil)
		if err != nil {
			t.Fatal(err)
		}
		file := codexRepairFileFor(t, preview, root, codexHooksFile)
		state, diags := file.adapter.read(file.store, outputKey{target: file.Target, machineScope: file.MachineScope}, file.OutDir, file.Rel)
		if state == nil || len(diags) != 0 || len(state.existing) != 2 || state.existing[0] != state.existing[1] {
			t.Fatalf("duplicate state = %#v diagnostics=%v", state, diags)
		}
		if state.proof.Classify(state.existing[0].ID, state.existing[0].ValueDigest) != ownership.StatusOwnedClean {
			t.Fatalf("duplicate ownership = %v", state.proof.Classify(state.existing[0].ID, state.existing[0].ValueDigest))
		}
		if _, err := file.RemoveOwned(context.Background(), state.existing[0], true); err != nil {
			t.Fatal(err)
		}
		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		afterDocument, err := jsonc.Parse(codexHooksFile, after)
		if err != nil {
			t.Fatal(err)
		}
		afterHooks, ok := afterDocument.Root().Member(codexHooksContainer)
		if !ok {
			t.Fatal("Codex hooks container missing after cleanup")
		}
		afterEvent, ok := afterHooks.Member("PreToolUse")
		if !ok || afterEvent.Len() != 1 {
			t.Fatalf("PreToolUse after cleanup = %#v", afterEvent)
		}
		afterElement, ok := afterEvent.Element(0)
		if !ok {
			t.Fatal("retained duplicate missing")
		}
		if !bytes.Equal(afterElement.Raw(), elementRaw) {
			t.Fatalf("retained duplicate raw = %q, want %q", afterElement.Raw(), elementRaw)
		}
	})
	t.Run("reformatted duplicate refused", func(t *testing.T) {
		repoRoot, home := devinEnv(t)
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
		t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
		report, err := Run(context.Background(), repoRoot, cleanupCodexHookSelection(t, "./duplicate"))
		if err != nil || report == nil || len(nonReviewDiags(report)) != 0 {
			t.Fatalf("seed Run = %#v err=%v", report, err)
		}
		root, err := filepath.EvalSymlinks(filepath.Join(home, ".codex"))
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, codexHooksFile)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var document map[string]any
		if err := json.Unmarshal(raw, &document); err != nil {
			t.Fatal(err)
		}
		hooks := document["hooks"].(map[string]any)
		groups := hooks["PreToolUse"].([]any)
		hooks["PreToolUse"] = append(groups, groups[0])
		reformatted, err := json.MarshalIndent(document, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		reformatted = append(reformatted, '\n')
		if bytes.Equal(raw, reformatted) {
			t.Fatal("reformatted fixture did not change raw hook bytes")
		}
		if err := os.WriteFile(path, reformatted, 0o600); err != nil {
			t.Fatal(err)
		}
		preview, err := CodexRepairPreview(context.Background(), repoRoot, nil)
		if err != nil {
			t.Fatal(err)
		}
		file := codexRepairFileFor(t, preview, root, codexHooksFile)
		state, diags := file.adapter.read(file.store, outputKey{target: file.Target, machineScope: file.MachineScope}, file.OutDir, file.Rel)
		if state == nil || len(diags) != 0 || len(state.existing) != 2 {
			t.Fatalf("reformatted state = %#v diagnostics=%v", state, diags)
		}
		before := ownedCleanupDiskState(t, file)
		if _, err := file.RemoveOwned(context.Background(), state.existing[0], true); !errors.Is(err, ErrRepairCandidateStale) {
			t.Fatalf("error = %v, want ErrRepairCandidateStale", err)
		}
		requireOwnedCleanupDiskState(t, file, before)
	})
}

func TestOwnedCleanupRejectsModifiedTarget(t *testing.T) {
	file, want, before, _ := ownedCleanupFixture(t, []string{"keep"})
	changed := bytes.Replace(before, []byte("remove-binary"), []byte("changed-binary"), 1)
	if bytes.Equal(before, changed) {
		t.Fatal("fixture did not modify target")
	}
	if err := os.WriteFile(file.AbsFile(), changed, 0o600); err != nil {
		t.Fatal(err)
	}
	diskBefore := ownedCleanupDiskState(t, file)
	if _, err := file.RemoveOwned(context.Background(), want, true); !errors.Is(err, ErrRepairCandidateStale) {
		t.Fatalf("error = %v, want ErrRepairCandidateStale", err)
	}
	requireOwnedCleanupDiskState(t, file, diskBefore)
	after, err := os.ReadFile(file.AbsFile())
	if err != nil || !bytes.Equal(changed, after) {
		t.Fatalf("stale cleanup changed aggregate: %q err=%v", after, err)
	}
}
