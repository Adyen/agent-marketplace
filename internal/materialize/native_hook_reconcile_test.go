package materialize

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/ownership"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

const (
	nativeHookJSONSelectedResource = "d9-selected"
	nativeHookJSONSelectedCommand  = "echo d9-selected-command"
	nativeHookJSONManualCommand    = "echo d9-handwritten-command"
	nativeHookJSONOtherCommand     = "echo d9-unrelated-command"
	nativeHookJSONPrivateCommand   = "echo d9-private-command"
	nativeHookJSONFIFOChildEnv     = "AGENT_MARKETPLACE_NATIVE_HOOK_JSON_FIFO_CHILD"
)

type nativeHookJSONLayoutCase struct {
	name         string
	target       hcc.Target
	machineScope bool
	root         func(repoRoot, home, codexHome string) string
	rel          string
	container    string
	shape        targetmeta.HookShape
	event        string
	otherEvent   string
	matcher      string
	envelope     map[string]any
}

func nativeHookJSONLayoutCases() []nativeHookJSONLayoutCase {
	return []nativeHookJSONLayoutCase{
		{name: "claude_repo", target: hcc.TargetClaude, root: func(repo, _, _ string) string { return filepath.Join(repo, ".claude") }, rel: "settings.json", container: "hooks", shape: targetmeta.HookShapeMatcherGroups, event: "PreToolUse", otherEvent: "PostToolUse", matcher: "Bash"},
		{name: "claude_machine", target: hcc.TargetClaude, machineScope: true, root: func(_, home, _ string) string { return filepath.Join(home, ".claude") }, rel: "settings.json", container: "hooks", shape: targetmeta.HookShapeMatcherGroups, event: "PreToolUse", otherEvent: "PostToolUse", matcher: "Bash"},
		{name: "factory_repo", target: hcc.TargetFactory, root: func(repo, _, _ string) string { return filepath.Join(repo, ".factory") }, rel: "hooks.json", shape: targetmeta.HookShapeMatcherGroups, event: "PreToolUse", otherEvent: "PostToolUse", matcher: "Execute"},
		{name: "factory_machine", target: hcc.TargetFactory, machineScope: true, root: func(_, home, _ string) string { return filepath.Join(home, ".factory") }, rel: "hooks.json", shape: targetmeta.HookShapeMatcherGroups, event: "PreToolUse", otherEvent: "PostToolUse", matcher: "Execute"},
		{name: "cursor_repo", target: hcc.TargetCursor, root: func(repo, _, _ string) string { return filepath.Join(repo, ".cursor") }, rel: "hooks.json", container: "hooks", shape: targetmeta.HookShapeEventHandlers, event: "preToolUse", otherEvent: "postToolUse", matcher: "^Shell$", envelope: map[string]any{"version": 1}},
		{name: "cursor_machine", target: hcc.TargetCursor, machineScope: true, root: func(_, home, _ string) string { return filepath.Join(home, ".cursor") }, rel: "hooks.json", container: "hooks", shape: targetmeta.HookShapeEventHandlers, event: "preToolUse", otherEvent: "postToolUse", matcher: "^Shell$", envelope: map[string]any{"version": 1}},
		{name: "gemini_repo", target: hcc.TargetGemini, root: func(repo, _, _ string) string { return filepath.Join(repo, ".gemini") }, rel: "settings.json", container: "hooks", shape: targetmeta.HookShapeMatcherGroups, event: "BeforeTool", otherEvent: "AfterTool", matcher: "^run_shell_command$"},
		{name: "gemini_machine", target: hcc.TargetGemini, machineScope: true, root: func(_, home, _ string) string { return filepath.Join(home, ".gemini") }, rel: "settings.json", container: "hooks", shape: targetmeta.HookShapeMatcherGroups, event: "BeforeTool", otherEvent: "AfterTool", matcher: "^run_shell_command$"},
		{name: "windsurf_repo", target: hcc.TargetWindsurf, root: func(repo, _, _ string) string { return repo }, rel: ".devin/hooks.json", container: "hooks", shape: targetmeta.HookShapeEventHandlers, event: "pre_run_command", otherEvent: "post_run_command"},
		{name: "windsurf_machine", target: hcc.TargetWindsurf, machineScope: true, root: func(_, home, _ string) string { return filepath.Join(home, ".codeium", "windsurf") }, rel: "hooks.json", container: "hooks", shape: targetmeta.HookShapeEventHandlers, event: "pre_run_command", otherEvent: "post_run_command"},
		{name: "devin_repo", target: hcc.TargetDevin, root: func(repo, _, _ string) string { return filepath.Join(repo, ".devin") }, rel: "hooks.v1.json", shape: targetmeta.HookShapeMatcherGroups, event: "PreToolUse", otherEvent: "PostToolUse", matcher: "^exec$"},
		{name: "devin_machine", target: hcc.TargetDevin, machineScope: true, root: func(_, home, _ string) string { return filepath.Join(home, ".config", "devin") }, rel: "config.json", container: "hooks", shape: targetmeta.HookShapeMatcherGroups, event: "PreToolUse", otherEvent: "PostToolUse", matcher: "^exec$"},
		{name: "codex_repo", target: hcc.TargetCodex, root: func(repo, _, _ string) string { return filepath.Join(repo, ".codex") }, rel: "hooks.json", container: "hooks", shape: targetmeta.HookShapeMatcherGroups, event: "PreToolUse", otherEvent: "PostToolUse", matcher: "^Bash$"},
		{name: "codex_machine", target: hcc.TargetCodex, machineScope: true, root: func(_, _, codexHome string) string { return codexHome }, rel: "hooks.json", container: "hooks", shape: targetmeta.HookShapeMatcherGroups, event: "PreToolUse", otherEvent: "PostToolUse", matcher: "^Bash$"},
	}
}

func nativeHookJSONCaseByName(t *testing.T, name string) nativeHookJSONLayoutCase {
	t.Helper()
	for _, tc := range nativeHookJSONLayoutCases() {
		if tc.name == name {
			return tc
		}
	}
	t.Fatalf("unknown native hook layout test case %q", name)
	return nativeHookJSONLayoutCase{}
}

func nativeHookJSONCaseFor(t *testing.T, target hcc.Target, machineScope bool) nativeHookJSONLayoutCase {
	t.Helper()
	for _, tc := range nativeHookJSONLayoutCases() {
		if tc.target == target && tc.machineScope == machineScope {
			return tc
		}
	}
	t.Fatalf("unknown native hook layout target %q", target)
	return nativeHookJSONLayoutCase{}
}

func nativeHookJSONTestEnvironment(t *testing.T) (repoRoot, home, codexHome string) {
	t.Helper()
	repoRoot, home = additiveSafetyEnv(t)
	codexHome = filepath.Join(t.TempDir(), "custom-codex-home")
	t.Setenv("CODEX_HOME", codexHome)
	return repoRoot, home, codexHome
}

func nativeHookJSONOptions() ownedJSONReconcileOptions {
	return ownedJSONReconcileOptions{
		rootID:             "must-be-replaced-by-destination",
		sessionName:        "reconcile-native-hook-json-test",
		writeCode:          "materialize.write.error",
		malformedCode:      "materialize.write.error",
		shapeCode:          "materialize.write.error",
		satisfiedCode:      diag.CodeMaterializeHookSatisfiedByUnmanaged,
		prunePostponedCode: "materialize.write.error",
	}
}

func nativeHookJSONJSONString(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func nativeHookJSONMatcherProperty(matcher string) string {
	if matcher == "" {
		return ""
	}
	return `"matcher":` + nativeHookJSONJSONString(matcher)
}

func nativeHookJSONFlatHandler(command, matcher string) string {
	fields := `"command":` + nativeHookJSONJSONString(command)
	if matcher != "" {
		fields += `,"matcher":` + nativeHookJSONJSONString(matcher)
	}
	return "{" + fields + "}"
}

func nativeHookJSONGroup(command, matcher, custom string) string {
	fields := ""
	if matcher != "" {
		fields = nativeHookJSONMatcherProperty(matcher) + ","
	}
	fields += `"hooks":[{"type":"command","command":` + nativeHookJSONJSONString(command) + `}]`
	if custom != "" {
		fields += `,"custom":` + nativeHookJSONJSONString(custom)
	}
	return "{" + fields + "}"
}

func nativeHookJSONWork(t *testing.T, tc nativeHookJSONLayoutCase, resource, command string) *ownedJSONAggregateWork {
	t.Helper()
	var rule string
	switch tc.shape {
	case targetmeta.HookShapeMatcherGroups:
		rule = nativeHookJSONGroup(command, tc.matcher, "")
	case targetmeta.HookShapeEventHandlers:
		rule = nativeHookJSONFlatHandler(command, tc.matcher)
	default:
		t.Fatal("unsupported native hook test shape")
	}
	entries, err := ownedJSONHookEntries(tc.rel, map[string][]json.RawMessage{
		tc.event: {json.RawMessage(rule)},
	}, resource)
	if err != nil {
		t.Fatal("construct desired native hook test entry")
	}
	work := &ownedJSONAggregateWork{}
	for _, entry := range entries {
		work.add(entry)
	}
	return work
}

func nativeHookJSONDocument(tc nativeHookJSONLayoutCase, handwritten, other string) ([]byte, string) {
	if tc.container == "" {
		data := "{\n  // retain an unrelated event\n  " + nativeHookJSONJSONString(tc.otherEvent) + " : [\n    " + other +
			"\n  ],\n  " + nativeHookJSONJSONString(tc.event) + " : [\n    " + handwritten + "\n  ]\n}\n"
		return []byte(data), ""
	}
	version := ""
	if tc.envelope != nil {
		version = "  \"version\" : 1,\n"
	}
	unrelated := `"otherSettings" : { "keep" : "d9-unrelated-root-setting" }`
	data := "{\n  /* preserve unrelated root settings */\n  " + unrelated + ",\n" + version +
		"  " + nativeHookJSONJSONString(tc.container) + " : {\n    // retain an unrelated event\n    " + nativeHookJSONJSONString(tc.otherEvent) + " : [\n      " + other +
		"\n    ],\n    " + nativeHookJSONJSONString(tc.event) + " : [\n      " + handwritten + "\n    ]\n  }\n}\n"
	return []byte(data), unrelated
}

func nativeHookJSONSeed(tc nativeHookJSONLayoutCase) ([]byte, string, string, string) {
	var handwritten, other string
	if tc.shape == targetmeta.HookShapeMatcherGroups {
		matcher := nativeHookJSONJSONString(tc.matcher)
		handwritten = "{\n      /* handwritten matcher group */\n      \"hooks\" : [ { \"type\" : \"command\", \"command\" : " + nativeHookJSONJSONString(nativeHookJSONManualCommand) + " } ],\n      \"matcher\" : " + matcher + "\n    }"
		other = "{ \"hooks\" : [ { \"type\" : \"command\", \"command\" : " + nativeHookJSONJSONString(nativeHookJSONOtherCommand) + " } ], \"matcher\" : " + matcher + " }"
	} else {
		handwritten = "{\n      /* handwritten flat handler */\n      \"command\" : " + nativeHookJSONJSONString(nativeHookJSONManualCommand)
		if tc.matcher != "" {
			handwritten += ",\n      \"matcher\" : " + nativeHookJSONJSONString(tc.matcher)
		}
		handwritten += "\n    }"
		other = nativeHookJSONFlatHandler(nativeHookJSONOtherCommand, tc.matcher)
	}
	data, unrelated := nativeHookJSONDocument(tc, handwritten, other)
	return data, handwritten, other, unrelated
}

func nativeHookJSONIdenticalSeed(tc nativeHookJSONLayoutCase) ([]byte, string, string, string) {
	command := nativeHookJSONJSONString(nativeHookJSONSelectedCommand)
	var handwritten, other string
	if tc.shape == targetmeta.HookShapeMatcherGroups {
		handwritten = "{\n      /* identical handwritten matcher group */\n      \"hooks\" : [ { \"command\" : " + command + ", \"type\" : \"command\" } ],\n      \"matcher\" : " + nativeHookJSONJSONString(tc.matcher) + "\n    }"
		other = nativeHookJSONGroup(nativeHookJSONOtherCommand, tc.matcher, "")
	} else {
		handwritten = "{\n      /* identical handwritten flat handler */"
		if tc.matcher != "" {
			handwritten += "\n      \"matcher\" : " + nativeHookJSONJSONString(tc.matcher) + ","
		}
		handwritten += "\n      \"command\" : " + command + "\n    }"
		other = nativeHookJSONFlatHandler(nativeHookJSONOtherCommand, tc.matcher)
	}
	data, unrelated := nativeHookJSONDocument(tc, handwritten, other)
	return data, handwritten, other, unrelated
}

func nativeHookJSONCommands(t *testing.T, data []byte, rel, container, eventName string, shape targetmeta.HookShape) []string {
	t.Helper()
	doc, err := jsonc.Parse(rel, data)
	if err != nil {
		t.Fatal("native hook JSON did not parse")
	}
	entries := doc.Root()
	if container != "" {
		var ok bool
		entries, ok = entries.Member(container)
		if !ok || entries.Kind() != jsonc.KindObject {
			t.Fatal("native hook container is missing")
		}
	}
	event, ok := entries.Member(eventName)
	if !ok || event.Kind() != jsonc.KindArray {
		t.Fatal("declared native hook event is missing or not an array")
	}
	var commands []string
	for i := 0; i < event.Len(); i++ {
		item, _ := event.Element(i)
		var handlers []*jsonc.Node
		switch shape {
		case targetmeta.HookShapeMatcherGroups:
			groupHandlers, present := item.Member("hooks")
			if !present || groupHandlers.Kind() != jsonc.KindArray {
				t.Fatal("matcher group has no hooks array")
			}
			for j := 0; j < groupHandlers.Len(); j++ {
				handler, _ := groupHandlers.Element(j)
				handlers = append(handlers, handler)
			}
		case targetmeta.HookShapeEventHandlers:
			handlers = append(handlers, item)
		default:
			t.Fatal("unexpected native hook test shape")
		}
		for _, handler := range handlers {
			commandNode, present := handler.Member("command")
			if !present {
				t.Fatal("native hook handler has no command")
			}
			var command string
			if err := json.Unmarshal(commandNode.Raw(), &command); err != nil {
				t.Fatal("native hook command is not a JSON string")
			}
			commands = append(commands, command)
		}
	}
	return commands
}

func assertNativeHookJSONEnvelope(t *testing.T, data []byte, envelope map[string]any) {
	t.Helper()
	if len(envelope) == 0 {
		return
	}
	doc, err := jsonc.Parse("native-hook.json", data)
	if err != nil {
		t.Fatal("native hook JSON did not parse")
	}
	for key, want := range envelope {
		node, ok := doc.Root().Member(key)
		if !ok {
			t.Fatal("declared envelope member is missing")
		}
		wantRaw, err := json.Marshal(want)
		if err != nil {
			t.Fatal(err)
		}
		gotCanonical, err := canonicalOwnedJSON(node.Raw())
		if err != nil {
			t.Fatal("declared envelope member is not valid JSON")
		}
		wantCanonical, err := canonicalOwnedJSON(wantRaw)
		if err != nil || !bytes.Equal(gotCanonical, wantCanonical) {
			t.Fatal("native hook envelope does not match its declared scalar")
		}
	}
}

func captureNativeHookJSONLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	previous := slog.Default()
	var output bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &output
}

func assertNativeHookJSONNoPayload(t *testing.T, report *Report, logs *bytes.Buffer, payloads ...string) {
	t.Helper()
	text := fmt.Sprint(report.Diags) + logs.String()
	for _, payload := range payloads {
		if payload != "" && strings.Contains(text, payload) {
			t.Fatal("native hook refusal exposed command or matcher input")
		}
	}
}

func nativeHookJSONReport() *Report {
	return &Report{PerTarget: make(map[hcc.Target]TargetReport)}
}

func countNativeHookCommand(commands []string, want string) int {
	count := 0
	for _, command := range commands {
		if command == want {
			count++
		}
	}
	return count
}

func seedNativeHookJSONLock(t *testing.T, root string) {
	t.Helper()
	store, err := ownership.Open(root)
	if err != nil {
		t.Fatal("open ownership store for lock baseline")
	}
	release, err := store.Lock()
	if err != nil {
		t.Fatal("create ownership lock baseline")
	}
	release()
}

func TestNativeHookJSONLayoutMatchesDeclaredSevenTargetMatrix(t *testing.T) {
	for _, tc := range nativeHookJSONLayoutCases() {
		t.Run(tc.name, func(t *testing.T) {
			repoRoot, home, codexHome := nativeHookJSONTestEnvironment(t)
			destination, spec, err := nativeHookJSONLayout(outputKey{target: tc.target, machineScope: tc.machineScope}, repoRoot)
			if err != nil {
				t.Fatal("native hook layout did not resolve")
			}
			wantRoot, err := ownership.CanonicalRoot(tc.root(repoRoot, home, codexHome))
			if err != nil {
				t.Fatal("expected root could not be canonicalized")
			}
			gotRoot, err := ownership.CanonicalRoot(destination.Root)
			if err != nil || gotRoot != wantRoot {
				t.Fatal("native hook destination root differs from the declared root")
			}
			if len(destination.AggregateRoutes) != 0 || spec.rel != tc.rel || spec.container != tc.container ||
				spec.hookShape != tc.shape || spec.keyed || spec.rawValueDigest || !reflect.DeepEqual(spec.envelope, tc.envelope) {
				t.Fatal("native hook file spec differs from the independently declared layout")
			}
		})
	}
}

// The non-identical positive control is TestNativeHookJSONReconcilePreservesAndDeselectsAcrossLayouts.
func TestNativeHookJSONIdenticalHandwrittenEntryIsNotAdopted(t *testing.T) {
	for _, tc := range nativeHookJSONLayoutCases() {
		t.Run(tc.name, func(t *testing.T) {
			repoRoot, home, codexHome := nativeHookJSONTestEnvironment(t)
			key := outputKey{target: tc.target, machineScope: tc.machineScope}
			destination, spec, err := nativeHookJSONLayout(key, repoRoot)
			if err != nil {
				t.Fatal("native hook layout did not resolve")
			}
			wantRoot, err := ownership.CanonicalRoot(tc.root(repoRoot, home, codexHome))
			if err != nil {
				t.Fatal("expected root could not be canonicalized")
			}
			gotRoot, err := ownership.CanonicalRoot(destination.Root)
			if err != nil || gotRoot != wantRoot || spec.rel != tc.rel || spec.container != tc.container || spec.hookShape != tc.shape || !reflect.DeepEqual(spec.envelope, tc.envelope) {
				t.Fatal("native hook layout differs from the independent expectation")
			}
			path := filepath.Join(wantRoot, filepath.FromSlash(tc.rel))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal("create identical native aggregate parent")
			}
			seed, handwritten, other, unrelated := nativeHookJSONIdenticalSeed(tc)
			if err := os.WriteFile(path, seed, 0o640); err != nil {
				t.Fatal("seed identical handwritten native aggregate")
			}
			if err := os.Chmod(path, 0o640); err != nil {
				t.Fatal("set existing aggregate mode")
			}
			seedNativeHookJSONLock(t, destination.Root)
			fileBefore, fileMode, fileMtime := fileSnapshot(t, path)
			stateBefore := nativeSupportPersistentSnapshot(t, repoRoot, home)
			logs := captureNativeHookJSONLogs(t)
			work := nativeHookJSONWork(t, tc, nativeHookJSONSelectedResource, nativeHookJSONSelectedCommand)
			report := reconcileNativeHookJSON(context.Background(), nativeHookJSONReport(), key, repoRoot, work, nativeHookJSONOptions(), false)
			if report == nil || diag.HasErrors(report.Diags) || !diag.HasCode(report.Diags, diag.CodeMaterializeHookSatisfiedByUnmanaged) {
				t.Fatal("identical handwritten hook was not satisfied by the generic unmanaged-entry diagnostic")
			}
			assertNativeHookJSONNoPayload(t, report, logs, nativeHookJSONSelectedCommand, tc.matcher)
			assertFileUnchanged(t, path, fileBefore, fileMode, fileMtime)
			assertNativeSupportSnapshot(t, stateBefore, nativeSupportPersistentSnapshot(t, repoRoot, home))
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal("read identical native aggregate")
			}
			commands := nativeHookJSONCommands(t, data, tc.rel, tc.container, tc.event, tc.shape)
			if countNativeHookCommand(commands, nativeHookJSONSelectedCommand) != 1 ||
				!bytes.Contains(data, []byte(handwritten)) || !bytes.Contains(data, []byte(other)) ||
				(unrelated != "" && !bytes.Contains(data, []byte(unrelated))) || bytes.Contains(data, []byte("_managed_by")) {
				t.Fatal("identical handwritten aggregate changed or gained a duplicate")
			}
			store, err := ownership.Open(destination.Root)
			if err != nil {
				t.Fatal("open native ownership store")
			}
			proof, proofErr := store.Proof(spec.rel)
			if proofErr != nil && !errors.Is(proofErr, ownership.ErrStateUninitialized) || proof.OwnedCount() != 0 || store.EverRecorded() {
				t.Fatal("identical handwritten hook was adopted into ownership state")
			}
		})
	}
}

func TestNativeHookJSONReconcilePreservesAndDeselectsAcrossLayouts(t *testing.T) {
	for _, tc := range nativeHookJSONLayoutCases() {
		t.Run(tc.name, func(t *testing.T) {
			repoRoot, home, codexHome := nativeHookJSONTestEnvironment(t)
			key := outputKey{target: tc.target, machineScope: tc.machineScope}
			destination, spec, err := nativeHookJSONLayout(key, repoRoot)
			if err != nil {
				t.Fatal("native hook layout did not resolve")
			}
			wantRoot, err := ownership.CanonicalRoot(tc.root(repoRoot, home, codexHome))
			if err != nil {
				t.Fatal("expected root could not be canonicalized")
			}
			gotRoot, err := ownership.CanonicalRoot(destination.Root)
			if err != nil || gotRoot != wantRoot || spec.rel != tc.rel || spec.container != tc.container || spec.hookShape != tc.shape || !reflect.DeepEqual(spec.envelope, tc.envelope) {
				t.Fatal("native hook layout differs from the independent expectation")
			}
			path := filepath.Join(wantRoot, filepath.FromSlash(tc.rel))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal("create test aggregate parent")
			}
			seed, handwritten, other, unrelated := nativeHookJSONSeed(tc)
			if err := os.WriteFile(path, seed, 0o640); err != nil {
				t.Fatal("seed handwritten native aggregate")
			}
			if err := os.Chmod(path, 0o640); err != nil {
				t.Fatal("set existing aggregate mode")
			}
			options := nativeHookJSONOptions()
			work := nativeHookJSONWork(t, tc, nativeHookJSONSelectedResource, nativeHookJSONSelectedCommand)
			report := reconcileNativeHookJSON(context.Background(), nativeHookJSONReport(), key, repoRoot, work, options, false)
			if report == nil || diag.HasErrors(report.Diags) {
				t.Fatal("native hook reconciliation failed")
			}
			wroteWithDestinationID := false
			for _, rootReport := range report.PerRoot {
				if rootReport.RootID == destination.ID && rootReport.OutDir == wantRoot && len(rootReport.Written) == 1 && rootReport.Written[0] == tc.rel {
					wroteWithDestinationID = true
				}
			}
			if !wroteWithDestinationID {
				t.Fatal("native hook write did not use the resolved destination authority")
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal("read reconciled native hook aggregate")
			}
			commands := nativeHookJSONCommands(t, data, tc.rel, tc.container, tc.event, tc.shape)
			selectedCount := countNativeHookCommand(commands, nativeHookJSONSelectedCommand)
			manualCount := countNativeHookCommand(commands, nativeHookJSONManualCommand)
			if selectedCount != 1 || manualCount != 1 {
				t.Fatalf("native hook command counts are invalid: selected=%d handwritten=%d", selectedCount, manualCount)
			}
			if !bytes.Contains(data, []byte(handwritten)) {
				t.Fatal("native reconcile changed handwritten entry bytes")
			}
			if !bytes.Contains(data, []byte(other)) || unrelated != "" && !bytes.Contains(data, []byte(unrelated)) {
				t.Fatal("native reconcile changed unrelated native entries")
			}
			if bytes.Index(data, []byte(other)) >= bytes.Index(data, []byte(handwritten)) ||
				bytes.Index(data, []byte(handwritten)) >= bytes.Index(data, []byte(nativeHookJSONSelectedCommand)) {
				t.Fatal("native reconcile changed handwritten or generated entry order")
			}
			if bytes.Contains(data, []byte("_managed_by")) {
				t.Fatal("native reconcile added an ownership marker")
			}
			assertNativeHookJSONEnvelope(t, data, tc.envelope)
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != 0o640 {
				t.Fatal("existing aggregate mode was not preserved")
			}
			store, err := ownership.Open(destination.Root)
			if err != nil {
				t.Fatal("open native ownership store")
			}
			proof, err := store.Proof(spec.rel)
			if err != nil || proof.OwnedCount() != 1 {
				t.Fatal("native ownership proof did not contain exactly the generated entry")
			}
			ids := proof.OwnedIDs()
			if len(ids) != 1 || len(work.entries) != 1 || ids[0] != work.entries[0].id() {
				t.Fatal("native proof does not contain only the selected identity")
			}
			resource, ok := proof.Resource(ids[0])
			if !ok || resource != nativeHookJSONSelectedResource {
				t.Fatal("native proof adopted handwritten content or omitted the selected resource")
			}
			fileBefore, fileMode, fileMtime := fileSnapshot(t, path)
			statePath := store.StateFile()
			stateBefore, stateMode, stateMtime := fileSnapshot(t, statePath)
			repeated := reconcileNativeHookJSON(context.Background(), nativeHookJSONReport(), key, repoRoot, work, options, false)
			if repeated == nil || diag.HasErrors(repeated.Diags) {
				t.Fatal("repeated native hook reconciliation failed")
			}
			assertFileUnchanged(t, path, fileBefore, fileMode, fileMtime)
			assertFileUnchanged(t, statePath, stateBefore, stateMode, stateMtime)

			deselected := reconcileNativeHookJSON(context.Background(), nativeHookJSONReport(), key, repoRoot, nil, options, false)
			if deselected == nil || diag.HasErrors(deselected.Diags) {
				t.Fatal("native hook deselection failed")
			}
			afterDeselect, err := os.ReadFile(path)
			if err != nil {
				t.Fatal("read aggregate after deselection")
			}
			commands = nativeHookJSONCommands(t, afterDeselect, tc.rel, tc.container, tc.event, tc.shape)
			if countNativeHookCommand(commands, nativeHookJSONSelectedCommand) != 0 || countNativeHookCommand(commands, nativeHookJSONManualCommand) != 1 ||
				!bytes.Contains(afterDeselect, []byte(handwritten)) || !bytes.Contains(afterDeselect, []byte(other)) ||
				(unrelated != "" && !bytes.Contains(afterDeselect, []byte(unrelated))) || bytes.Contains(afterDeselect, []byte("_managed_by")) {
				t.Fatal("deselection did not remove only the owned native hook")
			}
			if bytes.Index(afterDeselect, []byte(other)) >= bytes.Index(afterDeselect, []byte(handwritten)) {
				t.Fatal("deselection changed handwritten event order")
			}
			info, err = os.Stat(path)
			if err != nil || info.Mode().Perm() != 0o640 {
				t.Fatal("deselection did not preserve existing aggregate mode")
			}
			proofAfterDeselect, err := store.Proof(spec.rel)
			if err != nil || proofAfterDeselect.OwnedCount() != 0 {
				t.Fatal("deselection left proven ownership entries")
			}
		})
	}
}

func TestNativeHookJSONFreshAggregatesArePrivateAndUnmarked(t *testing.T) {
	for _, tc := range nativeHookJSONLayoutCases() {
		t.Run(tc.name, func(t *testing.T) {
			repoRoot, home, codexHome := nativeHookJSONTestEnvironment(t)
			key := outputKey{target: tc.target, machineScope: tc.machineScope}
			_, _, err := nativeHookJSONLayout(key, repoRoot)
			if err != nil {
				t.Fatal("native hook layout did not resolve")
			}
			wantRoot, err := ownership.CanonicalRoot(tc.root(repoRoot, home, codexHome))
			if err != nil {
				t.Fatal("expected root could not be canonicalized")
			}
			path := filepath.Join(wantRoot, filepath.FromSlash(tc.rel))
			if _, err := os.Lstat(path); !os.IsNotExist(err) {
				t.Fatal("fresh aggregate fixture already exists")
			}
			work := nativeHookJSONWork(t, tc, nativeHookJSONSelectedResource, nativeHookJSONSelectedCommand)
			report := reconcileNativeHookJSON(context.Background(), nativeHookJSONReport(), key, repoRoot, work, nativeHookJSONOptions(), false)
			if report == nil || diag.HasErrors(report.Diags) {
				t.Fatal("fresh native hook aggregate did not materialize")
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal("fresh native hook aggregate is missing")
			}
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != 0o600 {
				t.Fatal("fresh native hook aggregate is not private")
			}
			commands := nativeHookJSONCommands(t, data, tc.rel, tc.container, tc.event, tc.shape)
			selectedCount := countNativeHookCommand(commands, nativeHookJSONSelectedCommand)
			if selectedCount != 1 {
				t.Fatalf("fresh native hook command count is invalid: selected=%d", selectedCount)
			}
			if bytes.Contains(data, []byte("_managed_by")) {
				t.Fatal("fresh native hook aggregate contains an ownership marker")
			}
			assertNativeHookJSONEnvelope(t, data, tc.envelope)
		})
	}
}

func TestNativeHookJSONDryRunMissingRootHasNoSideEffects(t *testing.T) {
	repoRoot, home, codexHome := nativeHookJSONTestEnvironment(t)
	tc := nativeHookJSONCaseByName(t, "factory_repo")
	key := outputKey{target: tc.target, machineScope: tc.machineScope}
	destination, _, err := nativeHookJSONLayout(key, repoRoot)
	if err != nil {
		t.Fatal("native hook layout did not resolve")
	}
	wantRoot, err := ownership.CanonicalRoot(tc.root(repoRoot, home, codexHome))
	if err != nil {
		t.Fatal("expected root could not be canonicalized")
	}
	path := filepath.Join(wantRoot, filepath.FromSlash(tc.rel))
	if _, err := os.Lstat(destination.Root); !os.IsNotExist(err) {
		t.Fatal("missing-root dry-run fixture has an existing destination")
	}
	before := nativeSupportSnapshot(t, repoRoot, home, codexHome)
	work := nativeHookJSONWork(t, tc, nativeHookJSONSelectedResource, nativeHookJSONSelectedCommand)
	report := reconcileNativeHookJSON(context.Background(), nativeHookJSONReport(), key, repoRoot, work, nativeHookJSONOptions(), true)
	if report == nil || diag.HasErrors(report.Diags) || len(report.PerRoot) != 1 || len(report.PerRoot[0].Written) != 1 {
		t.Fatal("missing-root dry run did not report its pending write")
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatal("missing-root dry run created native file")
	}
	if _, err := os.Stat(destination.Root); !os.IsNotExist(err) {
		t.Fatal("missing-root dry run created native destination")
	}
	assertNativeSupportSnapshot(t, before, nativeSupportSnapshot(t, repoRoot, home, codexHome))

	synced := reconcileNativeHookJSON(context.Background(), nativeHookJSONReport(), key, repoRoot, work, nativeHookJSONOptions(), false)
	if synced == nil || diag.HasErrors(synced.Diags) {
		t.Fatal("real sync control failed after missing-root dry run")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("real sync control did not create a private aggregate")
	}
}

func TestNativeHookJSONCursorRequiresDeclaredVersion(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version string
	}{
		{name: "missing"},
		{name: "wrong_scalar", version: `"version" : "1",`},
		{name: "wrong_object", version: `"version" : {"number":1},`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repoRoot, home, _ := nativeHookJSONTestEnvironment(t)
			layoutCase := nativeHookJSONCaseByName(t, "cursor_repo")
			key := outputKey{target: layoutCase.target, machineScope: layoutCase.machineScope}
			destination, _, err := nativeHookJSONLayout(key, repoRoot)
			if err != nil {
				t.Fatal("Cursor native hook layout did not resolve")
			}
			root, err := ownership.CanonicalRoot(layoutCase.root(repoRoot, home, ""))
			if err != nil {
				t.Fatal("Cursor root could not be canonicalized")
			}
			if err := os.MkdirAll(root, 0o755); err != nil {
				t.Fatal("create Cursor destination")
			}
			path := filepath.Join(root, filepath.FromSlash(layoutCase.rel))
			handler := nativeHookJSONFlatHandler(nativeHookJSONPrivateCommand, layoutCase.matcher)
			event := nativeHookJSONJSONString(layoutCase.event) + " : [" + handler + "]"
			data := []byte("{\n  \"otherSettings\" : { \"keep\" : \"d9-private-root\" },\n  " + tc.version +
				"  " + nativeHookJSONJSONString(layoutCase.container) + " : {" + event + "}\n}\n")
			if err := os.WriteFile(path, data, 0o640); err != nil {
				t.Fatal("seed invalid Cursor envelope")
			}
			if err := os.Chmod(path, 0o640); err != nil {
				t.Fatal("set Cursor fixture mode")
			}
			seedNativeHookJSONLock(t, destination.Root)
			before := nativeSupportPersistentSnapshot(t, repoRoot, home)
			logs := captureNativeHookJSONLogs(t)
			work := nativeHookJSONWork(t, layoutCase, nativeHookJSONSelectedResource, nativeHookJSONSelectedCommand)
			report := reconcileNativeHookJSON(context.Background(), nativeHookJSONReport(), key, repoRoot, work, nativeHookJSONOptions(), false)
			if report == nil || !diag.HasCode(report.Diags, "materialize.write.error") {
				t.Fatal("Cursor envelope mismatch was not refused with the shape diagnostic")
			}
			assertNativeSupportSnapshot(t, before, nativeSupportPersistentSnapshot(t, repoRoot, home))
			assertNativeHookJSONNoPayload(t, report, logs, nativeHookJSONPrivateCommand, layoutCase.matcher)
		})
	}
}

func TestNativeHookJSONRejectsMalformedHookShapes(t *testing.T) {
	cases := []struct {
		name   string
		target hcc.Target
		body   string
	}{
		{name: "matcher_groups_event_not_array", target: hcc.TargetFactory, body: `{ {{event}}:{"matcher":{{matcher}},"command":{{command}}}}`},
		{name: "matcher_groups_rule_not_object", target: hcc.TargetFactory, body: `{ {{event}}:[{{command}}]}`},
		{name: "matcher_group_missing_hooks_array", target: hcc.TargetFactory, body: `{ {{event}}:[{"matcher":{{matcher}},"custom":{{command}}}]}`},
		{name: "matcher_group_hooks_not_array", target: hcc.TargetFactory, body: `{ {{event}}:[{"matcher":{{matcher}},"hooks":{{command}}}]}`},
		{name: "matcher_group_handler_not_object", target: hcc.TargetFactory, body: `{ {{event}}:[{"matcher":{{matcher}},"hooks":[{{command}}]}]}`},
		{name: "event_handlers_event_not_array", target: hcc.TargetWindsurf, body: `{ {{event}}:{{command}}}`},
		{name: "event_handler_not_object", target: hcc.TargetWindsurf, body: `{ {{event}}:[{{command}}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repoRoot, home, codexHome := nativeHookJSONTestEnvironment(t)
			layoutCase := nativeHookJSONCaseFor(t, tc.target, false)
			key := outputKey{target: tc.target}
			destination, _, err := nativeHookJSONLayout(key, repoRoot)
			if err != nil {
				t.Fatal("native hook layout did not resolve")
			}
			root, err := ownership.CanonicalRoot(layoutCase.root(repoRoot, home, codexHome))
			if err != nil {
				t.Fatal("expected root could not be canonicalized")
			}
			path := filepath.Join(root, filepath.FromSlash(layoutCase.rel))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal("create malformed aggregate parent")
			}
			body := strings.NewReplacer(
				"{{event}}", nativeHookJSONJSONString(layoutCase.event),
				"{{matcher}}", nativeHookJSONJSONString(layoutCase.matcher),
				"{{command}}", nativeHookJSONJSONString(nativeHookJSONPrivateCommand),
			).Replace(tc.body)
			if layoutCase.container != "" {
				body = "{" + nativeHookJSONJSONString(layoutCase.container) + ":" + body + "}"
			}
			if layoutCase.envelope != nil {
				body = `{"version":1,` + strings.TrimPrefix(body, "{")
			}
			if err := os.WriteFile(path, []byte(body), 0o640); err != nil {
				t.Fatal("seed malformed native hook shape")
			}
			seedNativeHookJSONLock(t, destination.Root)
			before := nativeSupportPersistentSnapshot(t, repoRoot, home)
			logs := captureNativeHookJSONLogs(t)
			work := nativeHookJSONWork(t, layoutCase, nativeHookJSONSelectedResource, nativeHookJSONSelectedCommand)
			report := reconcileNativeHookJSON(context.Background(), nativeHookJSONReport(), key, repoRoot, work, nativeHookJSONOptions(), false)
			if report == nil {
				t.Fatal("malformed native hook shape returned no report")
			}
			assertNativeHookJSONNoPayload(t, report, logs, nativeHookJSONPrivateCommand, layoutCase.matcher)
			if !diag.HasCode(report.Diags, "materialize.write.error") {
				codes := make([]string, len(report.Diags))
				for i, diagnostic := range report.Diags {
					codes[i] = diagnostic.Code
				}
				t.Fatalf("malformed native hook shape diagnostic codes = %v", codes)
			}
			assertNativeSupportSnapshot(t, before, nativeSupportPersistentSnapshot(t, repoRoot, home))
		})
	}
}

func TestNativeHookJSONDoesNotCrossDevinWindsurfFiles(t *testing.T) {
	repoRoot, home, codexHome := nativeHookJSONTestEnvironment(t)
	devinCase := nativeHookJSONCaseByName(t, "devin_repo")
	windsurfCase := nativeHookJSONCaseByName(t, "windsurf_repo")
	devinKey := outputKey{target: hcc.TargetDevin}
	windsurfKey := outputKey{target: hcc.TargetWindsurf}
	devinDestination, _, err := nativeHookJSONLayout(devinKey, repoRoot)
	if err != nil {
		t.Fatal("Devin native hook layout did not resolve")
	}
	windsurfDestination, _, err := nativeHookJSONLayout(windsurfKey, repoRoot)
	if err != nil {
		t.Fatal("Windsurf native hook layout did not resolve")
	}
	devinRoot, err := ownership.CanonicalRoot(devinCase.root(repoRoot, home, codexHome))
	if err != nil {
		t.Fatal("Devin root could not be canonicalized")
	}
	windsurfRoot, err := ownership.CanonicalRoot(windsurfCase.root(repoRoot, home, codexHome))
	if err != nil {
		t.Fatal("Windsurf root could not be canonicalized")
	}
	devinPath := filepath.Join(devinRoot, filepath.FromSlash(devinCase.rel))
	windsurfPath := filepath.Join(windsurfRoot, filepath.FromSlash(windsurfCase.rel))
	if err := os.MkdirAll(filepath.Dir(devinPath), 0o755); err != nil {
		t.Fatal("create Devin aggregate parent")
	}
	if err := os.MkdirAll(filepath.Dir(windsurfPath), 0o755); err != nil {
		t.Fatal("create Windsurf aggregate parent")
	}
	devinSeed, _, _, _ := nativeHookJSONSeed(devinCase)
	windsurfSeed, _, _, _ := nativeHookJSONSeed(windsurfCase)
	if err := os.WriteFile(devinPath, devinSeed, 0o640); err != nil {
		t.Fatal("seed Devin sibling aggregate")
	}
	if err := os.WriteFile(windsurfPath, windsurfSeed, 0o640); err != nil {
		t.Fatal("seed Windsurf sibling aggregate")
	}
	windsurfBefore, windsurfMode, windsurfMtime := fileSnapshot(t, windsurfPath)
	devinReport := reconcileNativeHookJSON(context.Background(), nativeHookJSONReport(), devinKey, repoRoot,
		nativeHookJSONWork(t, devinCase, nativeHookJSONSelectedResource, nativeHookJSONSelectedCommand), nativeHookJSONOptions(), false)
	if devinReport == nil || diag.HasErrors(devinReport.Diags) {
		t.Fatal("Devin native hook reconciliation failed")
	}
	assertFileUnchanged(t, windsurfPath, windsurfBefore, windsurfMode, windsurfMtime)
	devinAfter, devinMode, devinMtime := fileSnapshot(t, devinPath)
	windsurfReport := reconcileNativeHookJSON(context.Background(), nativeHookJSONReport(), windsurfKey, repoRoot,
		nativeHookJSONWork(t, windsurfCase, nativeHookJSONSelectedResource, nativeHookJSONSelectedCommand), nativeHookJSONOptions(), false)
	if windsurfReport == nil || diag.HasErrors(windsurfReport.Diags) {
		t.Fatal("Windsurf native hook reconciliation failed")
	}
	assertFileUnchanged(t, devinPath, devinAfter, devinMode, devinMtime)
	if devinDestination.Root == windsurfDestination.Root {
		t.Fatal("Devin and Windsurf native hook roots unexpectedly coincide")
	}
}

func TestNativeHookJSONRejectsUnknownLayoutWithoutWrites(t *testing.T) {
	repoRoot, home, codexHome := nativeHookJSONTestEnvironment(t)
	before := nativeSupportSnapshot(t, repoRoot, home, codexHome)
	key := outputKey{target: hcc.Target("unknown-native-hook-target")}
	report := reconcileNativeHookJSON(context.Background(), nativeHookJSONReport(), key, repoRoot, nil, nativeHookJSONOptions(), false)
	if report == nil || len(report.Diags) != 1 || report.Diags[0].Code != "materialize.destination.error" ||
		report.Diags[0].Message != "native hook destination could not be resolved safely; aggregate was left unchanged" {
		t.Fatal("invalid native hook layout did not return the static destination refusal")
	}
	assertNativeSupportSnapshot(t, before, nativeSupportSnapshot(t, repoRoot, home, codexHome))
}

func TestNativeHookJSONRefusesUnsafeFiles(t *testing.T) {
	for _, tc := range []struct {
		name string
		kind string
	}{
		{name: "symlink_leaf", kind: "leaf"},
		{name: "symlink_parent", kind: "parent"},
		{name: "non_directory_parent", kind: "parent_file"},
		{name: "hard_link_leaf", kind: "hardlink"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repoRoot, home, codexHome := nativeHookJSONTestEnvironment(t)
			layoutCase := nativeHookJSONCaseByName(t, "windsurf_repo")
			key := outputKey{target: hcc.TargetWindsurf}
			destination, _, err := nativeHookJSONLayout(key, repoRoot)
			if err != nil {
				t.Fatal("Windsurf native hook layout did not resolve")
			}
			root, err := ownership.CanonicalRoot(layoutCase.root(repoRoot, home, codexHome))
			if err != nil {
				t.Fatal("Windsurf root could not be canonicalized")
			}
			parent := filepath.Join(root, ".devin")
			path := filepath.Join(root, filepath.FromSlash(layoutCase.rel))
			payload := []byte(`{"hooks":{` + nativeHookJSONJSONString(layoutCase.event) + `:[` + nativeHookJSONFlatHandler(nativeHookJSONPrivateCommand, layoutCase.matcher) + `]}}`)
			outsideDir := t.TempDir()
			outside := filepath.Join(outsideDir, "hooks.json")
			if err := os.WriteFile(outside, payload, 0o640); err != nil {
				t.Fatal("seed outside sentinel")
			}
			switch tc.kind {
			case "leaf":
				if err := os.MkdirAll(parent, 0o755); err != nil {
					t.Fatal("create native hook parent")
				}
				if err := os.Symlink(outside, path); err != nil {
					t.Fatal("create symlinked native hook leaf")
				}
			case "parent":
				if err := os.Symlink(outsideDir, parent); err != nil {
					t.Fatal("create symlinked native hook parent")
				}
			case "parent_file":
				if err := os.WriteFile(parent, payload, 0o640); err != nil {
					t.Fatal("create non-directory native hook parent")
				}
			case "hardlink":
				if err := os.MkdirAll(parent, 0o755); err != nil {
					t.Fatal("create native hook parent")
				}
				if err := os.Link(outside, path); err != nil {
					t.Fatal("create hard-linked native hook leaf")
				}
			}
			seedNativeHookJSONLock(t, destination.Root)
			before := nativeSupportPersistentSnapshot(t, repoRoot, home)
			outsideBefore := nativeSupportSnapshot(t, outsideDir)
			logs := captureNativeHookJSONLogs(t)
			work := nativeHookJSONWork(t, layoutCase, nativeHookJSONSelectedResource, nativeHookJSONSelectedCommand)
			report := reconcileNativeHookJSON(context.Background(), nativeHookJSONReport(), key, repoRoot, work, nativeHookJSONOptions(), false)
			if report == nil || !diag.HasCode(report.Diags, "materialize.write.error") {
				t.Fatal("unsafe native hook file was not refused")
			}
			assertNativeSupportSnapshot(t, before, nativeSupportPersistentSnapshot(t, repoRoot, home))
			assertNativeSupportSnapshot(t, outsideBefore, nativeSupportSnapshot(t, outsideDir))
			assertNativeHookJSONNoPayload(t, report, logs, nativeHookJSONPrivateCommand, layoutCase.matcher)
			switch tc.kind {
			case "leaf":
				if target, err := os.Readlink(path); err != nil || target != outside {
					t.Fatal("symlinked native hook leaf changed")
				}
			case "parent":
				if target, err := os.Readlink(parent); err != nil || target != outsideDir {
					t.Fatal("symlinked native hook parent changed")
				}
			case "parent_file":
				if info, err := os.Stat(parent); err != nil || !info.Mode().IsRegular() {
					t.Fatal("non-directory native hook parent changed")
				}
			case "hardlink":
				insideInfo, insideErr := os.Stat(path)
				outsideInfo, outsideErr := os.Stat(outside)
				if insideErr != nil || outsideErr != nil || !os.SameFile(insideInfo, outsideInfo) {
					t.Fatal("hard-linked native hook leaf changed")
				}
			}
		})
	}
}

func TestNativeHookJSONFIFOChild(t *testing.T) {
	if os.Getenv(nativeHookJSONFIFOChildEnv) != "1" {
		return
	}
	repoRoot, _, _ := nativeHookJSONTestEnvironment(t)
	layoutCase := nativeHookJSONCaseByName(t, "windsurf_repo")
	key := outputKey{target: hcc.TargetWindsurf}
	_, _, err := nativeHookJSONLayout(key, repoRoot)
	if err != nil {
		t.Fatal("Windsurf native hook layout did not resolve")
	}
	path := filepath.Join(repoRoot, filepath.FromSlash(layoutCase.rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal("create FIFO parent")
	}
	if err := syscall.Mkfifo(path, 0o640); err != nil {
		t.Fatal("create FIFO native hook aggregate")
	}
	work := nativeHookJSONWork(t, layoutCase, nativeHookJSONSelectedResource, nativeHookJSONSelectedCommand)
	report := reconcileNativeHookJSON(context.Background(), nativeHookJSONReport(), key, repoRoot, work, nativeHookJSONOptions(), false)
	if report == nil || !diag.HasCode(report.Diags, "materialize.write.error") {
		t.Fatal("FIFO native hook aggregate was not refused")
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatal("FIFO native hook aggregate was replaced")
	}
}

func TestNativeHookJSONFIFOIsBounded(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNativeHookJSONFIFOChild$")
	cmd.Env = []string{nativeHookJSONFIFOChildEnv + "=1", "TMPDIR=" + os.TempDir()}
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("FIFO reconciliation child exceeded deadline: %s", output)
	}
	if err != nil {
		t.Fatalf("FIFO reconciliation child: %v\n%s", err, output)
	}
}
