package materialize

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

type projectionSourceFixture struct {
	root       string
	definition string
	support    string
	resolved   resolver.ResolvedArtifact
}

func writeProjectionSource(t *testing.T, command string) projectionSourceFixture {
	t.Helper()
	root := t.TempDir()
	definition := filepath.Join(root, "hooks", "hooks.json")
	support := filepath.Join(root, "hooks", "guard.sh")
	if err := os.MkdirAll(filepath.Dir(definition), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd, _ := json.Marshal(command)
	data := []byte(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":` + string(cmd) + `}]}]}}`)
	if err := os.WriteFile(definition, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(support, []byte("#!/bin/sh\necho witness\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cat, err := catalog.Load(root, "claude")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
	var entry *catalog.Entry
	for i := range cat.Entries {
		if cat.Entries[i].Kind == ir.KindHook {
			entry = &cat.Entries[i]
		}
	}
	if entry == nil {
		t.Fatalf("catalog entries = %#v, want one hook", cat.Entries)
	}
	return projectionSourceFixture{
		root:       root,
		definition: definition,
		support:    support,
		resolved: resolver.ResolvedArtifact{
			Entry:       *entry,
			RegistryURL: root,
			CatalogKey:  root,
			Source:      config.RepoCommitted,
			V4Kind:      config.V4SourceDirectory,
			Harnesses:   []hcc.Target{hcc.TargetFactory},
		},
	}
}

func writeFactoryProjectionSource(t *testing.T, matcher, command string) projectionSourceFixture {
	t.Helper()
	cmd, _ := json.Marshal(command)
	data := []byte(`{"PreToolUse":[{"matcher":` + strconv.QuoteToASCII(matcher) + `,"hooks":[{"type":"command","command":` + string(cmd) + `}]}]}`)
	data = bytes.ReplaceAll(data, []byte(`\"`), []byte(`"`))
	return writeFactoryProjectionDefinition(t, data)
}

func writeFactoryProjectionDefinition(t *testing.T, data []byte) projectionSourceFixture {
	t.Helper()
	root := t.TempDir()
	definition := filepath.Join(root, ".factory", "hooks", "hooks.json")
	support := filepath.Join(root, ".factory", "hooks", "scripts", "run.sh")
	if err := os.MkdirAll(filepath.Dir(definition), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(support), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(definition, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(support, []byte("#!/bin/sh\necho witness\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cat, err := catalog.Load(root, "factory")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
	var entry *catalog.Entry
	for i := range cat.Entries {
		if cat.Entries[i].Kind == ir.KindHook {
			entry = &cat.Entries[i]
		}
	}
	if entry == nil {
		t.Fatalf("catalog entries = %#v, want one hook", cat.Entries)
	}
	return projectionSourceFixture{
		root:       root,
		definition: definition,
		support:    support,
		resolved: resolver.ResolvedArtifact{
			Entry:       *entry,
			RegistryURL: root,
			CatalogKey:  root,
			Source:      config.RepoCommitted,
			V4Kind:      config.V4SourceDirectory,
			Harnesses:   []hcc.Target{hcc.TargetFactory},
		},
	}
}

func serveMirrorProjectionHook(t *testing.T, canonical artifact.CanonicalArtifact, hookName string) (*httptest.Server, string, string) {
	t.Helper()
	raw, archiveDigest, err := artifact.Encode(canonical)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	decoded, _, err := artifact.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("Decode encoded artifact: %v", err)
	}
	coordinate := store.ResourceCoordinate{
		Marketplace: store.MarketplaceID("mp"),
		Registry:    store.RegistryID("src"),
		Kind:        "hook",
		Name:        hookName,
	}
	revision := store.ResourceRevision{
		Coordinate:    coordinate,
		ContentDigest: store.RevisionDigest(decoded.Manifest.ContentDigest),
		ArchiveDigest: store.ArchiveDigest(archiveDigest),
	}
	assessment, err := store.FinalizeResourceAssessment(store.ResourceAssessment{
		Coordinate: coordinate, ContentDigest: revision.ContentDigest,
		AnalyzerVersion: "test-analyzer", PolicyDigest: store.PolicyDigest("sha256:" + strings.Repeat("c", 64)),
		Decision: store.AssessmentDecisionAllow, InspectionManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("d", 64)),
		AssessedAt: time.Unix(1, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("FinalizeResourceAssessment: %v", err)
	}
	resolvedRevision := store.AssessedResourceRevision{
		ResourceRevision: revision,
		Assessment:       assessment,
		Occurrence: store.ResourceOccurrence{
			Coordinate: coordinate, ContentDigest: revision.ContentDigest,
			Commit: strings.Repeat("a", 40), ManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("b", 64)),
		},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/resources/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resolvedRevision)
	})
	mux.HandleFunc("/api/v1/artifacts/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.agent-marketplace.artifact.v1.tar+gzip")
		_, _ = w.Write(raw)
	})
	mux.HandleFunc("/.well-known/agent-marketplace.json", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("mp", "https://example.com", "https://example.com"))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server, decoded.Manifest.ContentDigest, archiveDigest
}

func resolveMirrorProjectionHook(t *testing.T, support map[string]artifact.SupportFile) (resolver.ResolvedArtifact, string, string) {
	t.Helper()
	hook := ir.NewHookWithMatcher("deploy-hook", "PreToolUse", "Bash", "./run.sh", "command", nil, ir.SourceRef{File: "hooks/hooks.json"})
	hook.SupportFiles = make(map[string]string, len(support))
	for name, file := range support {
		hook.SupportFiles[name] = artifact.SupportFileDigest(file)
	}
	canonical, err := artifact.FromIR(hook)
	if err != nil {
		t.Fatalf("FromIR: %v", err)
	}
	canonical.Support = support
	server, digest, archiveDigest := serveMirrorProjectionHook(t, canonical, hook.Name())
	cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{{
		URL: server.URL, InstanceID: "mp",
		Resources: []config.MarketplaceResourcePin{{
			RegistryID: "src", Kind: "hook", Name: hook.Name(), Revision: "v1",
			Digest: digest, Acquisition: config.MarketplaceAcquisitionMirror,
		}},
	}}}
	marketplaceResolver := &marketplace.Resolver{Cache: marketplace.NewCache(t.TempDir())}
	resolved, errs := marketplaceResolver.Resolve(context.Background(), cfg, []hcc.Target{hcc.TargetFactory}, resolver.MarketplaceResolveModeRefresh)
	if len(errs) != 0 {
		t.Fatalf("Marketplace resolve errors: %v", errs)
	}
	if len(resolved) != 1 {
		t.Fatalf("Marketplace resolved %d artifacts, want one", len(resolved))
	}
	return resolved[0], digest, archiveDigest
}

func oracleFingerprint(identity, name, target, scope, definitionRel string, definition []byte, support ...struct {
	rel  string
	mode fs.FileMode
	data []byte
}) string {
	buf := []byte("hook-source/v1\x00")
	field := func(b []byte) {
		buf = append(buf, strconv.Itoa(len(b))...)
		buf = append(buf, ':')
		buf = append(buf, b...)
	}
	for _, s := range []string{identity, name, target, scope, definitionRel} {
		field([]byte(s))
	}
	field(definition)
	for _, f := range support {
		field([]byte(f.rel))
		field([]byte(fmt.Sprintf("%04o", f.mode.Perm())))
		field(f.data)
	}
	sum := sha256.Sum256(buf)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func oracleSourceKey(t *testing.T, root string) string {
	t.Helper()
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	identity := "local:" + filepath.Clean(abs)
	sum := sha256.Sum256([]byte(identity))
	raw := filepath.Base(root)
	var b strings.Builder
	lastDash := false
	for _, r := range raw {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-'
		if !ok {
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
			continue
		}
		b.WriteRune(r)
		lastDash = r == '-'
	}
	slug := strings.Trim(b.String(), "-.")
	if slug == "" {
		slug = "source"
	}
	return slug + "-" + hex.EncodeToString(sum[:])
}

func decodeProjectionDocument(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("decode projection artifact: %v", err)
	}
	return document
}

func assertHookProjectionRefusal(t *testing.T, err error, root, rel string) {
	t.Helper()
	if err == nil {
		t.Fatal("hook projection did not refuse")
	}
	coded, ok := diag.AsCodedError(nativeHookRefused(root, rel, err))
	if !ok || coded.Code != diag.CodeHookNativeToleranceRefused {
		t.Fatalf("hook projection refusal = %v, want %s", err, diag.CodeHookNativeToleranceRefused)
	}
}

func TestPrepareIndependentNativeHookProjection(t *testing.T) {
	fixture := writeProjectionSource(t, "echo witness")
	repoRoot := t.TempDir()
	work := skillTarget{skill: fixture.resolved, target: hcc.TargetFactory}

	projection, err := prepareNativeHookProjection(work, repoRoot)
	if err != nil {
		t.Fatalf("prepareNativeHookProjection: %v", err)
	}

	if len(projection.artifacts) != 1 {
		t.Fatalf("artifacts = %d, want 1", len(projection.artifacts))
	}
	art := projection.artifacts[0]
	if art.Path != "hooks.json" {
		t.Fatalf("artifact path = %q, want hooks.json", art.Path)
	}
	if art.Mode.Perm() != 0o600 {
		t.Fatalf("artifact mode = %04o, want 0600", art.Mode.Perm())
	}
	if !art.Fragment || art.Reconciliation.Mode != artifact.ReconciliationExternal || art.Reconciliation.MarkerField != "" {
		t.Fatalf("artifact policy = %+v, want external fragment with no marker", art.Reconciliation)
	}
	document := decodeProjectionDocument(t, art.Bytes)
	if len(document) != 1 {
		t.Fatalf("document keys = %v, want only the native event", document)
	}
	groups, ok := document["PreToolUse"].([]any)
	if !ok || len(groups) != 1 {
		t.Fatalf("PreToolUse = %v, want one rule group", document)
	}
	group := groups[0].(map[string]any)
	if group["matcher"] != "Execute" {
		t.Fatalf("group matcher = %v, want native Execute", group["matcher"])
	}
	handler := group["hooks"].([]any)[0].(map[string]any)
	want := map[string]any{"type": "command", "command": "echo witness"}
	if len(handler) != len(want) {
		t.Fatalf("handler = %v, want %v", handler, want)
	}
	for key, value := range want {
		if handler[key] != value {
			t.Fatalf("handler[%q] = %v, want %v", key, handler[key], value)
		}
	}

	abs, err := filepath.Abs(fixture.root)
	if err != nil {
		t.Fatal(err)
	}
	supportBytes, err := os.ReadFile(fixture.support)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := oracleFingerprint(
		"local:"+filepath.Clean(abs), fixture.resolved.Name, "factory", "repository",
		"hooks/hooks.json", mustRead(t, fixture.definition),
		struct {
			rel  string
			mode fs.FileMode
			data []byte
		}{rel: "guard.sh", mode: 0o755, data: supportBytes},
	)
	if projection.sourceDigest != wantDigest {
		t.Fatalf("sourceDigest = %q, want independent oracle %q", projection.sourceDigest, wantDigest)
	}

	wantBase := "hook-support/" + oracleSourceKey(t, fixture.root) + "/" + fixture.resolved.Name
	if projection.supportRelBase != wantBase {
		t.Fatalf("supportRelBase = %q, want independent %q", projection.supportRelBase, wantBase)
	}
	if projection.outputRoot != filepath.Join(repoRoot, ".factory") {
		t.Fatalf("outputRoot = %q, want %q", projection.outputRoot, filepath.Join(repoRoot, ".factory"))
	}
	if projection.layout.rel != "hooks.json" || projection.layout.container != "" {
		t.Fatalf("layout = %+v, want factory hooks.json top-level map", projection.layout)
	}

	if len(projection.support) != 1 {
		t.Fatalf("support = %d, want 1", len(projection.support))
	}
	file := projection.support[0]
	if file.Path != "hooks/guard.sh" || string(file.Bytes) != string(supportBytes) || file.Mode.Perm() != 0o755 {
		t.Fatalf("support artifact = %#v", file)
	}

	entries, err := os.ReadDir(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("repoRoot mutated: %v", entries)
	}
	sourceAfter, err := os.ReadFile(fixture.support)
	if err != nil || !bytes.Equal(sourceAfter, supportBytes) {
		t.Fatal("source support mutated")
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestPrepareIndependentNativeHookProjectionLayouts(t *testing.T) {
	fixture := writeProjectionSource(t, "echo witness")
	repoRoot := t.TempDir()
	for _, tc := range []struct {
		target    hcc.Target
		path      string
		root      string
		container string
		shape     string
		version   int
	}{
		{hcc.TargetClaude, "settings.json", ".claude", "hooks", "matcher-groups", 0},
		{hcc.TargetFactory, "hooks.json", ".factory", "", "matcher-groups", 0},
		{hcc.TargetCursor, "hooks.json", ".cursor", "hooks", "event-handlers", 1},
		{hcc.TargetWindsurf, ".devin/hooks.json", "", "hooks", "event-handlers", 0},
		{hcc.TargetGemini, "settings.json", ".gemini", "hooks", "matcher-groups", 0},
		{hcc.TargetDevin, "hooks.v1.json", ".devin", "", "matcher-groups", 0},
	} {
		t.Run(string(tc.target), func(t *testing.T) {
			projection, err := prepareNativeHookProjection(skillTarget{skill: fixture.resolved, target: tc.target}, repoRoot)
			if err != nil {
				t.Fatalf("prepareNativeHookProjection(%s): %v", tc.target, err)
			}
			if len(projection.artifacts) != 1 || projection.artifacts[0].Path != tc.path {
				t.Fatalf("artifacts = %#v, want one %q", projection.artifacts, tc.path)
			}
			wantRoot := repoRoot
			if tc.root != "" {
				wantRoot = filepath.Join(repoRoot, tc.root)
			}
			if projection.outputRoot != wantRoot {
				t.Fatalf("outputRoot = %q, want %q", projection.outputRoot, wantRoot)
			}
			if projection.layout.rel != tc.path || projection.layout.container != tc.container || string(projection.layout.shape) != tc.shape {
				t.Fatalf("layout = %+v, want rel=%q container=%q shape=%q", projection.layout, tc.path, tc.container, tc.shape)
			}
			if tc.version != 0 && projection.layout.envelope["version"] != tc.version {
				t.Fatalf("layout envelope = %#v, want version %d", projection.layout.envelope, tc.version)
			}
		})
	}
}

func TestPrepareIndependentNativeHookProjectionCodexDestinationRoute(t *testing.T) {
	fixture := writeProjectionSource(t, "echo witness")
	repoRoot := t.TempDir()
	projection, err := prepareNativeHookProjection(skillTarget{skill: fixture.resolved, target: hcc.TargetCodex}, repoRoot)
	if err != nil {
		t.Fatalf("prepareNativeHookProjection: %v", err)
	}
	if len(projection.artifacts) != 1 || projection.artifacts[0].Path != ".codex/hooks.json" {
		t.Fatalf("artifacts = %#v, want .codex/hooks.json", projection.artifacts)
	}
	canonicalRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	wantRoot := filepath.Join(canonicalRepoRoot, ".codex")
	if projection.outputRoot != wantRoot {
		t.Fatalf("outputRoot = %q, want %q", projection.outputRoot, wantRoot)
	}
	if projection.layout.rel != "hooks.json" || projection.layout.container != "hooks" {
		t.Errorf("layout = %+v, want route-relative hooks.json with hooks member", projection.layout)
	}
	wantSupport := "hook-support/" + oracleSourceKey(t, fixture.root) + "/" + fixture.resolved.Name
	if projection.supportRelBase != wantSupport {
		t.Fatalf("supportRelBase = %q, want route-relative %q", projection.supportRelBase, wantSupport)
	}
	if entries, err := os.ReadDir(repoRoot); err != nil || len(entries) != 0 {
		t.Fatalf("repoRoot mutated: %v %v", entries, err)
	}
}

func TestPrepareIndependentNativeHookProjectionLayoutMetadata(t *testing.T) {
	fixture := writeProjectionSource(t, "echo witness")
	projection, err := prepareNativeHookProjection(skillTarget{skill: fixture.resolved, target: hcc.TargetCursor}, t.TempDir())
	if err != nil {
		t.Fatalf("prepareNativeHookProjection: %v", err)
	}
	if string(projection.layout.shape) != "event-handlers" {
		t.Fatalf("layout shape = %q, want declared event-handlers", projection.layout.shape)
	}
	if len(projection.layout.envelope) != 1 || projection.layout.envelope["version"] != 1 {
		t.Fatalf("layout envelope = %#v, want declared version 1", projection.layout.envelope)
	}
}

func TestPrepareIndependentNativeHookProjectionFactoryCanonicalControl(t *testing.T) {
	fixture := writeFactoryProjectionSource(t, "Bash", "${CLAUDE_PLUGIN_ROOT}/scripts/run.sh")
	hook, ok := fixture.resolved.Resource.(*ir.Hook)
	if !ok {
		t.Fatalf("fixture resource = %#v, want hook", fixture.resolved.Resource)
	}
	if hook.HookSource != "factory" || hook.Matcher != "Bash" || hook.Command != "${CLAUDE_PLUGIN_ROOT}/scripts/run.sh" {
		t.Fatalf("canonical pseudo-Factory hook = %#v", hook)
	}
}

func TestPrepareIndependentNativeHookProjectionFactoryNativeSourceProjects(t *testing.T) {
	definition := []byte(`{"PreToolUse":[{"matcher":"Execute","commandRegex":"^make.*$","hooks":[{"type":"command","command":"${DROID_PLUGIN_ROOT}/scripts/run.sh"}]}]}`)
	fixture := writeFactoryProjectionDefinition(t, definition)
	legacy, ok := fixture.resolved.Resource.(*ir.Hook)
	if !ok {
		t.Fatal("fixture resource is not a hook")
	}
	if legacy.HookSource != "factory" || legacy.Matcher != "Execute" || legacy.Command != "${DROID_PLUGIN_ROOT}/scripts/run.sh" ||
		legacy.Extra["commandRegex"] != "^make.*$" || !legacy.GroupExtraKeys["commandRegex"] {
		t.Fatalf("unexpected default Factory-loader control: %#v", legacy)
	}
	repoRoot := t.TempDir()
	definitionBefore := mustRead(t, fixture.definition)
	supportBefore := mustRead(t, fixture.support)
	projection, err := prepareNativeHookProjection(skillTarget{skill: fixture.resolved, target: hcc.TargetFactory}, repoRoot)
	if err != nil {
		t.Fatalf("native Factory snapshot projection: %v", err)
	}
	if len(projection.artifacts) != 1 || projection.artifacts[0].Path != "hooks.json" {
		t.Fatalf("artifacts = %#v, want hooks.json", projection.artifacts)
	}
	wantSupportRelBase := "hook-support/" + oracleSourceKey(t, fixture.root) + "/" + fixture.resolved.Name
	if projection.supportRelBase != wantSupportRelBase {
		t.Fatalf("supportRelBase = %q, want %q", projection.supportRelBase, wantSupportRelBase)
	}
	document := decodeProjectionDocument(t, projection.artifacts[0].Bytes)
	groups, ok := document["PreToolUse"].([]any)
	if !ok || len(groups) != 1 {
		t.Fatalf("PreToolUse = %v, want one native group", document["PreToolUse"])
	}
	group, ok := groups[0].(map[string]any)
	if !ok || group["matcher"] != "Execute" || group["commandRegex"] != "^make.*$" {
		t.Fatalf("native Factory group = %v, want Execute and exact group commandRegex", groups[0])
	}
	handlers, ok := group["hooks"].([]any)
	if !ok || len(handlers) != 1 {
		t.Fatalf("native Factory handlers = %v, want one handler", group["hooks"])
	}
	handler, ok := handlers[0].(map[string]any)
	if !ok || handler["type"] != "command" {
		t.Fatalf("native Factory handler = %v, want command", handlers[0])
	}
	wantCommand := `"$FACTORY_PROJECT_DIR"/` + filepath.ToSlash(filepath.Join(".factory", wantSupportRelBase, "hooks", "scripts", "run.sh"))
	if handler["command"] != wantCommand {
		t.Fatalf("command = %v, want exact native support base %q", handler["command"], wantCommand)
	}
	if _, leaked := handler["commandRegex"]; leaked {
		t.Fatalf("commandRegex leaked into handler: %v", handler)
	}
	if len(projection.support) != 1 || projection.support[0].Path != "hooks/scripts/run.sh" || projection.support[0].Mode.Perm() != 0o755 {
		t.Fatalf("support = %#v, want staged hooks/scripts/run.sh", projection.support)
	}
	if entries, err := os.ReadDir(repoRoot); err != nil || len(entries) != 0 {
		t.Fatalf("repoRoot mutated during pure preparation: %v %v", entries, err)
	}
	if !bytes.Equal(mustRead(t, fixture.definition), definitionBefore) || !bytes.Equal(mustRead(t, fixture.support), supportBefore) {
		t.Fatal("Factory source changed during preparation")
	}

	staleDefinition := bytes.Replace(definitionBefore, []byte("scripts/run.sh"), []byte("scripts/other.sh"), 1)
	if bytes.Equal(staleDefinition, definitionBefore) {
		t.Fatal("test fixture did not change the captured Factory command")
	}
	if err := os.WriteFile(fixture.definition, staleDefinition, 0o644); err != nil {
		t.Fatal(err)
	}
	staleBefore := mustRead(t, fixture.definition)
	staleRepoRoot := t.TempDir()
	staleProjection, staleErr := prepareNativeHookProjection(skillTarget{skill: fixture.resolved, target: hcc.TargetFactory}, staleRepoRoot)
	if staleErr == nil {
		t.Fatalf("stale Factory selection projected: %#v", staleProjection)
	}
	if len(staleProjection.artifacts) != 0 || len(staleProjection.support) != 0 {
		t.Fatalf("stale Factory refusal returned artifacts/support: %#v / %#v", staleProjection.artifacts, staleProjection.support)
	}
	if entries, err := os.ReadDir(staleRepoRoot); err != nil || len(entries) != 0 {
		t.Fatalf("repoRoot mutated for stale Factory source: %v %v", entries, err)
	}
	if !bytes.Equal(mustRead(t, fixture.definition), staleBefore) || !bytes.Equal(mustRead(t, fixture.support), supportBefore) {
		t.Fatal("Factory source changed during stale-source refusal")
	}
}

func TestPrepareIndependentNativeHookProjectionFactoryNativeGroupRefusals(t *testing.T) {
	handler := `{"type":"command","command":"${DROID_PLUGIN_ROOT}/scripts/run.sh"}`
	handlerWithRegex := `{"type":"command","command":"${DROID_PLUGIN_ROOT}/scripts/run.sh","commandRegex":"^handler$"}`
	handlerWithMatcher := `{"type":"command","command":"${DROID_PLUGIN_ROOT}/scripts/run.sh","matcher":"Read"}`
	handlerWithEmptyMatcher := `{"type":"command","command":"${DROID_PLUGIN_ROOT}/scripts/run.sh","matcher":""}`
	handlerWithRegexAlias := `{"type":"command","command":"${DROID_PLUGIN_ROOT}/scripts/run.sh","CommandRegex":"^handler$"}`
	validGroup := `"commandRegex":"^build$",`
	nested := func(groupFields, handlerJSON string) []byte {
		return []byte(`{"PreToolUse":[{"matcher":"Execute",` + groupFields + `"hooks":[` + handlerJSON + `]}]}`)
	}
	flat := func(commandRegex string) []byte {
		return []byte(`{"PreToolUse":[{"matcher":"Execute","type":"command","command":"${DROID_PLUGIN_ROOT}/flat.sh","commandRegex":` + commandRegex + `}]}`)
	}
	cases := []struct {
		name   string
		seed   []byte
		source []byte
	}{
		{name: "group type present empty", seed: nested(validGroup, handler), source: nested(validGroup+`"type":"",`, handler)},
		{name: "group command present empty", seed: nested(validGroup, handler), source: nested(validGroup+`"command":"",`, handler)},
		{name: "group prompt present empty", seed: nested(validGroup, handler), source: nested(validGroup+`"prompt":"",`, handler)},
		{name: "group timeout present zero", seed: nested(validGroup, handler), source: nested(validGroup+`"timeout":0,`, handler)},
		{name: "empty group string extra", seed: nested(validGroup, handler), source: nested(validGroup+`"emptyOption":"",`, handler)},
		{name: "empty group null extra", seed: nested(validGroup, handler), source: nested(validGroup+`"emptyOption":null,`, handler)},
		{name: "empty group object extra", seed: nested(validGroup, handler), source: nested(validGroup+`"emptyOption":{},`, handler)},
		{name: "empty group array extra", seed: nested(validGroup, handler), source: nested(validGroup+`"emptyOption":[],`, handler)},
		{name: "unsupported group extra", seed: nested(validGroup, handler), source: nested(validGroup+`"providerOption":{"mode":"strict"},`, handler)},
		{name: "non-string group commandRegex", seed: nested(validGroup, handler), source: nested(`"commandRegex":7,`, handler)},
		{name: "flat non-string commandRegex", seed: flat(`7`), source: flat(`7`)},
		{name: "commandRegex alias lower first", seed: nested(validGroup, handler), source: nested(`"commandRegex":"^build$","CommandRegex":"^alias$",`, handler)},
		{name: "commandRegex alias upper first", seed: nested(validGroup, handler), source: nested(`"CommandRegex":"^alias$","commandRegex":"^build$",`, handler)},
		{name: "group handler commandRegex conflict", seed: nested(validGroup, handlerWithRegex), source: nested(validGroup, handlerWithRegex)},
		{name: "handler commandRegex without group", seed: nested("", handlerWithRegex), source: nested("", handlerWithRegex)},
		{name: "handler commandRegex alias without group", seed: nested("", handlerWithRegexAlias), source: nested("", handlerWithRegexAlias)},
		{name: "nested handler matcher present", seed: nested(validGroup, handlerWithMatcher), source: nested(validGroup, handlerWithMatcher)},
		{name: "nested handler matcher empty present", seed: nested(validGroup, handlerWithEmptyMatcher), source: nested(validGroup, handlerWithEmptyMatcher)},
		{name: "typed handler alias lower first", seed: nested(validGroup, handler), source: nested(validGroup, `{"type":"command","Type":"prompt","command":"${DROID_PLUGIN_ROOT}/scripts/run.sh"}`)},
		{name: "typed handler alias upper first", seed: nested(validGroup, handler), source: nested(validGroup, `{"Type":"prompt","type":"command","command":"${DROID_PLUGIN_ROOT}/scripts/run.sh"}`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := writeFactoryProjectionDefinition(t, tc.seed)
			if err := os.WriteFile(fixture.definition, tc.source, 0o644); err != nil {
				t.Fatal(err)
			}
			sourceBefore := mustRead(t, fixture.definition)
			supportBefore := mustRead(t, fixture.support)
			repoRoot := t.TempDir()
			projection, err := prepareNativeHookProjection(skillTarget{skill: fixture.resolved, target: hcc.TargetFactory}, repoRoot)
			if err == nil {
				t.Fatalf("unsupported Factory group was projected: %#v", projection)
			}
			if len(projection.artifacts) != 0 || len(projection.support) != 0 {
				t.Fatalf("refusal returned artifacts/support: %#v / %#v", projection.artifacts, projection.support)
			}
			if entries, err := os.ReadDir(repoRoot); err != nil || len(entries) != 0 {
				t.Fatalf("repoRoot mutated on refusal: %v %v", entries, err)
			}
			if !bytes.Equal(mustRead(t, fixture.definition), sourceBefore) || !bytes.Equal(mustRead(t, fixture.support), supportBefore) {
				t.Fatal("Factory source changed during refusal")
			}
		})
	}
}

func TestPrepareIndependentNativeHookProjectionRejectsStaleSource(t *testing.T) {
	fixture := writeProjectionSource(t, "echo witness")
	repoRoot := t.TempDir()
	work := skillTarget{skill: fixture.resolved, target: hcc.TargetFactory}

	if _, err := prepareNativeHookProjection(work, repoRoot); err != nil {
		t.Fatalf("unmodified control: %v", err)
	}

	stale := []byte(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo changed"}]}]}}`)
	if err := os.WriteFile(fixture.definition, stale, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := prepareNativeHookProjection(work, repoRoot)
	assertHookProjectionRefusal(t, err, repoRoot, "hooks.json")

	if err := os.WriteFile(fixture.definition, []byte(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo witness"}]}]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareNativeHookProjection(work, repoRoot); err != nil {
		t.Fatalf("restored definition: %v", err)
	}

	if err := os.WriteFile(fixture.definition, []byte(`{"hooks":{"Other":[{"type":"command","command":"echo x"}]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = prepareNativeHookProjection(work, repoRoot)
	assertHookProjectionRefusal(t, err, repoRoot, "hooks.json")

	if err := os.WriteFile(fixture.definition, []byte(`{malformed`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = prepareNativeHookProjection(work, repoRoot)
	assertHookProjectionRefusal(t, err, repoRoot, "hooks.json")
	if err := os.Remove(fixture.definition); err != nil {
		t.Fatal(err)
	}
	_, err = prepareNativeHookProjection(work, repoRoot)
	assertHookProjectionRefusal(t, err, repoRoot, "hooks.json")
	if entries, err := os.ReadDir(repoRoot); err != nil || len(entries) != 0 {
		t.Fatalf("repoRoot mutated during preparation: %v %v", entries, err)
	}
}

func TestPrepareIndependentNativeHookProjectionSelectsExactlyOne(t *testing.T) {
	name := "PreToolUse-command"
	one := ir.NewHookWithMatcher(name, "PreToolUse", "Bash", "echo one", "command", nil, ir.SourceRef{File: "hooks/hooks.json"})
	two := ir.NewHookWithMatcher(name, "PreToolUse", "Bash", "echo two", "command", nil, ir.SourceRef{File: "hooks/hooks.json"})
	if _, err := selectCapturedSourceHook(&ingest.Bundle{Resources: []ir.Resource{one, two}}, name); err == nil {
		t.Fatal("duplicate captured hook selected without refusal")
	}
	if _, err := selectCapturedSourceHook(&ingest.Bundle{Resources: []ir.Resource{one},
		Diagnostics: []diag.Diagnostic{{Severity: diag.SeverityError}}}, name); err == nil {
		t.Fatal("error-diagnostic bundle selected without refusal")
	}
	if _, err := selectCapturedSourceHook(&ingest.Bundle{}, name); err == nil {
		t.Fatal("missing captured hook selected without refusal")
	}
	got, err := selectCapturedSourceHook(&ingest.Bundle{Resources: []ir.Resource{one}}, name)
	if err != nil || got != one {
		t.Fatalf("positive selection = %v %v", got, err)
	}
}

func TestHookSourceFIFOBoundedChild(t *testing.T) {
	if os.Getenv("AGENT_MARKETPLACE_HOOK_SOURCE_FIFO_CHILD") != "1" {
		return
	}
	root := os.Getenv("AGENT_MARKETPLACE_HOOK_SOURCE_FIFO_ROOT")
	if root == "" {
		t.Fatal("FIFO source root is missing")
	}
	if _, err := captureHookSourceSnapshot(root, "hooks/hooks.json", "hooks"); err == nil {
		t.Fatal("FIFO source was captured without refusal")
	} else if !errors.Is(err, fsboundary.ErrUnsafeFile) {
		t.Fatalf("FIFO source error = %v, want %v", err, fsboundary.ErrUnsafeFile)
	}
}

func runBoundedHookSourceFIFOCapture(t *testing.T, root string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestHookSourceFIFOBoundedChild$", "-test.v")
	cmd.Env = []string{"AGENT_MARKETPLACE_HOOK_SOURCE_FIFO_CHILD=1", "AGENT_MARKETPLACE_HOOK_SOURCE_FIFO_ROOT=" + root}
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("FIFO source probe exceeded its deadline: %s", output)
	}
	if err != nil {
		t.Fatalf("FIFO source probe: %v: %s", err, output)
	}
	if !strings.Contains(string(output), "--- PASS: TestHookSourceFIFOBoundedChild") {
		t.Fatalf("FIFO source probe did not run its child assertion: %s", output)
	}
}

func TestPrepareIndependentNativeHookProjectionSourceBoundaries(t *testing.T) {
	t.Run("symlinked definition directory refuses", func(t *testing.T) {
		fixture := writeProjectionSource(t, "echo witness")
		repoRoot := t.TempDir()
		sentinel := t.TempDir()
		sentinelFile := filepath.Join(sentinel, "sentinel")
		if err := os.WriteFile(sentinelFile, []byte("external"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.RemoveAll(filepath.Join(fixture.root, "hooks")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(sentinel, filepath.Join(fixture.root, "hooks")); err != nil {
			t.Fatal(err)
		}
		work := skillTarget{skill: fixture.resolved, target: hcc.TargetFactory}
		_, err := prepareNativeHookProjection(work, repoRoot)
		assertHookProjectionRefusal(t, err, repoRoot, "hooks.json")
		if data, err := os.ReadFile(sentinelFile); err != nil || string(data) != "external" {
			t.Fatal("external sentinel changed")
		}
		if entries, err := os.ReadDir(repoRoot); err != nil || len(entries) != 0 {
			t.Fatalf("repoRoot mutated on refusal: %v %v", entries, err)
		}
	})

	t.Run("symlinked source root refuses", func(t *testing.T) {
		fixture := writeProjectionSource(t, "echo witness")
		repoRoot := t.TempDir()
		link := filepath.Join(t.TempDir(), "link")
		if err := os.Symlink(fixture.root, link); err != nil {
			t.Fatal(err)
		}
		resolved := fixture.resolved
		resolved.RegistryURL = link
		resolved.CatalogKey = link
		work := skillTarget{skill: resolved, target: hcc.TargetFactory}
		_, err := prepareNativeHookProjection(work, repoRoot)
		assertHookProjectionRefusal(t, err, repoRoot, "hooks.json")
		if entries, err := os.ReadDir(repoRoot); err != nil || len(entries) != 0 {
			t.Fatalf("repoRoot mutated on refusal: %v %v", entries, err)
		}
	})

	t.Run("definition outside the configured root refuses", func(t *testing.T) {
		fixture := writeProjectionSource(t, "echo witness")
		repoRoot := t.TempDir()
		outside := filepath.Join(t.TempDir(), "hooks.json")
		if err := os.WriteFile(outside, mustRead(t, fixture.definition), 0o644); err != nil {
			t.Fatal(err)
		}
		outsideBefore := mustRead(t, outside)
		resolved := fixture.resolved
		resolved.Path = outside
		work := skillTarget{skill: resolved, target: hcc.TargetFactory}
		_, err := prepareNativeHookProjection(work, repoRoot)
		assertHookProjectionRefusal(t, err, repoRoot, "hooks.json")
		if !bytes.Equal(mustRead(t, outside), outsideBefore) {
			t.Fatal("external definition changed during refusal")
		}
		if entries, err := os.ReadDir(repoRoot); err != nil || len(entries) != 0 {
			t.Fatalf("repoRoot mutated on refusal: %v %v", entries, err)
		}
	})

	t.Run("contained support alias refuses", func(t *testing.T) {
		fixture := writeProjectionSource(t, "echo witness")
		repoRoot := t.TempDir()
		aliasPath := filepath.Join(filepath.Dir(fixture.definition), "alias.sh")
		if err := os.Symlink(filepath.Base(fixture.support), aliasPath); err != nil {
			t.Fatal(err)
		}
		aliasBefore, err := os.Lstat(aliasPath)
		if err != nil {
			t.Fatal(err)
		}
		aliasTarget, err := os.Readlink(aliasPath)
		if err != nil {
			t.Fatal(err)
		}
		supportBefore := mustRead(t, fixture.support)
		work := skillTarget{skill: fixture.resolved, target: hcc.TargetFactory}
		_, err = prepareNativeHookProjection(work, repoRoot)
		assertHookProjectionRefusal(t, err, repoRoot, "hooks.json")
		if !bytes.Equal(mustRead(t, fixture.support), supportBefore) {
			t.Fatal("contained alias target changed during reader refusal")
		}
		aliasAfter, err := os.Lstat(aliasPath)
		if err != nil || !os.SameFile(aliasBefore, aliasAfter) || aliasBefore.Mode() != aliasAfter.Mode() || !aliasBefore.ModTime().Equal(aliasAfter.ModTime()) {
			t.Fatalf("contained alias changed during reader refusal: %v", err)
		}
		if target, err := os.Readlink(aliasPath); err != nil || target != aliasTarget {
			t.Fatalf("contained alias target changed: %q %v", target, err)
		}
		if entries, err := os.ReadDir(repoRoot); err != nil || len(entries) != 0 {
			t.Fatalf("repoRoot mutated on refusal: %v %v", entries, err)
		}
	})

	t.Run("special hardlinked and oversized inputs refuse", func(t *testing.T) {
		fixture := writeProjectionSource(t, "echo witness")
		repoRoot := t.TempDir()
		work := skillTarget{skill: fixture.resolved, target: hcc.TargetFactory}

		fifo := filepath.Join(fixture.root, "hooks", "pipe")
		if err := unix.Mkfifo(fifo, 0o644); err != nil {
			t.Fatalf("mkfifo: %v", err)
		}
		runBoundedHookSourceFIFOCapture(t, fixture.root)
		if info, err := os.Lstat(fifo); err != nil || info.Mode()&os.ModeNamedPipe == 0 {
			t.Fatalf("FIFO probe changed source path: %v %v", info, err)
		}
		if err := os.Remove(fifo); err != nil {
			t.Fatal(err)
		}

		linked := filepath.Join(fixture.root, "hooks", "linked.sh")
		if err := os.Link(fixture.support, linked); err != nil {
			t.Fatalf("hardlink: %v", err)
		}
		_, err := prepareNativeHookProjection(work, repoRoot)
		if !errors.Is(err, fsboundary.ErrUnsafeFile) {
			t.Fatalf("hardlinked support error = %v, want %v", err, fsboundary.ErrUnsafeFile)
		}
		assertHookProjectionRefusal(t, err, repoRoot, "hooks.json")
		if err := os.Remove(linked); err != nil {
			t.Fatal(err)
		}

		oversized := filepath.Join(fixture.root, "hooks", "oversized.bin")
		f, err := os.Create(oversized)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.Truncate(hookSourceFingerprintMaxBytes + 1); err != nil {
			_ = f.Close()
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		_, err = prepareNativeHookProjection(work, repoRoot)
		if !errors.Is(err, fsboundary.ErrTooLarge) {
			t.Fatalf("oversized source error = %v, want %v", err, fsboundary.ErrTooLarge)
		}
		assertHookProjectionRefusal(t, err, repoRoot, "hooks.json")
		if err := os.Remove(oversized); err != nil {
			t.Fatal(err)
		}

		if _, err := prepareNativeHookProjection(work, repoRoot); err != nil {
			t.Fatalf("positive regular-source control: %v", err)
		}
		if entries, err := os.ReadDir(repoRoot); err != nil || len(entries) != 0 {
			t.Fatalf("repoRoot mutated during source preparation: %v %v", entries, err)
		}
	})

}

func TestPrepareIndependentNativeHookProjectionRejectsChangedGeneration(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "guard.sh")
	if err := os.WriteFile(filePath, []byte("AAAAAAAA"), 0o755); err != nil {
		t.Fatal(err)
	}
	dirFD, err := unix.Open(dir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = unix.Close(dirFD) }()
	var total int64
	var before fsboundary.Identity
	var mutationErr error
	_, _, err = readHookSourceSnapshotFileChecked(dirFD, "guard.sh", &total, func(opened *os.File) {
		var identityErr error
		before, identityErr = fsboundary.FileIdentity(opened)
		if identityErr != nil {
			mutationErr = identityErr
			return
		}
		info, statErr := opened.Stat()
		if statErr != nil {
			mutationErr = statErr
			return
		}
		if writeErr := os.WriteFile(filePath, []byte("BBBBBBBB"), 0o755); writeErr != nil {
			mutationErr = writeErr
			return
		}
		if timeErr := os.Chtimes(filePath, info.ModTime(), info.ModTime()); timeErr != nil {
			mutationErr = timeErr
			return
		}
		after, identityErr := fsboundary.FileIdentity(opened)
		if identityErr != nil {
			mutationErr = identityErr
			return
		}
		if !before.SameObject(after) || before.Size != after.Size || before.Mode != after.Mode || before.ModTimeNS != after.ModTimeNS || before.ChangeTimeNS == after.ChangeTimeNS {
			mutationErr = fmt.Errorf("test mutation did not isolate a same-size ctime-only generation change: before=%+v after=%+v", before, after)
		}
	})
	if mutationErr != nil {
		t.Fatal(mutationErr)
	}
	if !errors.Is(err, dependencytxn.ErrGenerationConflict) || !errors.Is(err, fsboundary.ErrGenerationConflict) {
		t.Fatalf("changed descriptor generation error = %v, want dependency transaction and filesystem boundary generation conflicts", err)
	}
	assertHookProjectionRefusal(t, err, dir, "hook-support/guard.sh")
}

func TestPrepareIndependentNativeHookProjectionRejectsContradictoryProvenance(t *testing.T) {
	repoRoot := t.TempDir()
	fixture := writeProjectionSource(t, "echo witness")
	resolved := fixture.resolved
	hook, ok := resolved.Resource.(*ir.Hook)
	if !ok {
		t.Fatal("fixture resource is not a hook")
	}
	mutated := *hook
	mutated.HookSource = "factory"
	resolved.Resource = &mutated
	_, err := prepareNativeHookProjection(skillTarget{skill: resolved, target: hcc.TargetFactory}, repoRoot)
	assertHookProjectionRefusal(t, err, repoRoot, "hooks.json")
	if entries, err := os.ReadDir(repoRoot); err != nil || len(entries) != 0 {
		t.Fatalf("repoRoot mutated on contradictory provenance refusal: %v %v", entries, err)
	}

	native := writeFactoryProjectionSource(t, "Bash", "echo witness")
	if _, err := prepareNativeHookProjection(skillTarget{skill: native.resolved, target: hcc.TargetFactory}, repoRoot); err != nil {
		t.Fatalf("factory-layout canonical source refused: %v", err)
	}
}

func TestPrepareIndependentNativeHookProjectionIdentityIsolation(t *testing.T) {
	first := writeProjectionSource(t, "echo witness")
	second := writeProjectionSource(t, "echo witness")
	repoRoot := t.TempDir()

	one, err := prepareNativeHookProjection(skillTarget{skill: first.resolved, target: hcc.TargetFactory}, repoRoot)
	if err != nil {
		t.Fatalf("first source: %v", err)
	}
	two, err := prepareNativeHookProjection(skillTarget{skill: second.resolved, target: hcc.TargetFactory}, repoRoot)
	if err != nil {
		t.Fatalf("second source: %v", err)
	}
	if one.sourceDigest == two.sourceDigest || one.supportRelBase == two.supportRelBase {
		t.Fatalf("distinct configured sources collided: %q %q", one.sourceDigest, one.supportRelBase)
	}

	renamed := first.resolved
	renamed.InstalledName = "renamed-guard"
	three, err := prepareNativeHookProjection(skillTarget{skill: renamed, target: hcc.TargetFactory}, repoRoot)
	if err != nil {
		t.Fatalf("installed-name source: %v", err)
	}
	if three.sourceDigest == one.sourceDigest || three.supportRelBase == one.supportRelBase {
		t.Fatal("a different installed name shared the source namespace or fingerprint")
	}
	if !strings.HasSuffix(three.supportRelBase, "/renamed-guard") {
		t.Fatalf("supportRelBase = %q, want installed-name component", three.supportRelBase)
	}

	if err := os.WriteFile(first.support, []byte("#!/bin/sh\necho changed\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	changed, err := prepareNativeHookProjection(skillTarget{skill: first.resolved, target: hcc.TargetFactory}, repoRoot)
	if err != nil {
		t.Fatalf("changed support: %v", err)
	}
	if changed.sourceDigest == one.sourceDigest {
		t.Fatal("a support byte change did not invalidate the source digest")
	}

	if err := os.Chmod(first.support, 0o700); err != nil {
		t.Fatal(err)
	}
	remoded, err := prepareNativeHookProjection(skillTarget{skill: first.resolved, target: hcc.TargetFactory}, repoRoot)
	if err != nil {
		t.Fatalf("remoded support: %v", err)
	}
	if remoded.sourceDigest == changed.sourceDigest {
		t.Fatal("a support permission change did not invalidate the source digest")
	}
}

func TestPrepareIndependentNativeHookProjectionMirror(t *testing.T) {
	repoRoot := t.TempDir()
	hook := ir.NewHookWithMatcher("deploy-hook", "PreToolUse", "Bash", "./run.sh", "command", nil, ir.SourceRef{File: "hooks/hooks.json"})
	mirror := func(support []artifact.Artifact) resolver.ResolvedArtifact {
		return resolver.ResolvedArtifact{
			Entry:                   catalog.Entry{Kind: ir.KindHook, Name: "deploy-hook", Path: filepath.Join(t.TempDir(), "artifact.tar.gz"), Resource: hook},
			Source:                  config.RepoCommitted,
			Harnesses:               []hcc.Target{hcc.TargetFactory},
			IsMarketplace:           true,
			MarketplaceInstanceID:   "instance",
			MarketplaceRegistryID:   "registry",
			MarketplaceResourceName: "deploy-hook",
			MarketplaceDigest:       "sha256:" + strings.Repeat("a", 64),
			MarketplaceAcquisition:  config.MarketplaceAcquisitionMirror,
			MarketplaceHookSupport:  support,
		}
	}

	t.Run("verified inventory projects", func(t *testing.T) {
		res := mirror([]artifact.Artifact{
			{Path: "data.bin", Bytes: []byte{0x00, 0xff, 0x0a}, Mode: 0o644},
			{Path: "run.sh", Bytes: []byte("#!/bin/sh\n"), Mode: 0o755},
		})
		projection, err := prepareNativeHookProjection(skillTarget{skill: res, target: hcc.TargetFactory}, repoRoot)
		if err != nil {
			t.Fatalf("mirror prepare: %v", err)
		}
		if len(projection.artifacts) != 1 || projection.artifacts[0].Path != "hooks.json" {
			t.Fatalf("artifacts = %#v", projection.artifacts)
		}
		document := decodeProjectionDocument(t, projection.artifacts[0].Bytes)
		group := document["PreToolUse"].([]any)[0].(map[string]any)
		if group["matcher"] != "Execute" {
			t.Fatalf("matcher = %v, want Execute", group["matcher"])
		}
		if len(projection.support) != 2 || projection.support[0].Path != "data.bin" || projection.support[1].Path != "run.sh" {
			t.Fatalf("support = %#v, want sorted inventory", projection.support)
		}
		if !bytes.Equal(projection.support[0].Bytes, []byte{0x00, 0xff, 0x0a}) || projection.support[0].Mode.Perm() != 0o644 {
			t.Fatalf("binary support = %#v", projection.support[0])
		}
		if projection.support[1].Mode.Perm() != 0o755 {
			t.Fatalf("executable support mode = %04o", projection.support[1].Mode)
		}
		if !strings.HasPrefix(projection.supportRelBase, "hook-support/registry-deploy-hook-") {
			t.Fatalf("supportRelBase = %q, want mirror source namespace", projection.supportRelBase)
		}
		wantDigest := oracleFingerprintMirror("instance", "registry", "hook", "deploy-hook",
			"sha256:"+strings.Repeat("a", 64), "deploy-hook", "factory", "repository")
		if projection.sourceDigest != wantDigest {
			t.Fatalf("mirror digest = %q, want independent oracle %q", projection.sourceDigest, wantDigest)
		}
	})

	t.Run("verified empty inventory is not unknown", func(t *testing.T) {
		res := mirror([]artifact.Artifact{})
		projection, err := prepareNativeHookProjection(skillTarget{skill: res, target: hcc.TargetFactory}, repoRoot)
		if err != nil {
			t.Fatalf("empty verified inventory refused: %v", err)
		}
		if len(projection.support) != 0 {
			t.Fatalf("support = %#v, want empty", projection.support)
		}
	})

	t.Run("unknown inventory refuses", func(t *testing.T) {
		res := mirror(nil)
		_, err := prepareNativeHookProjection(skillTarget{skill: res, target: hcc.TargetFactory}, repoRoot)
		assertHookProjectionRefusal(t, err, repoRoot, "hooks.json")
	})

	for _, tc := range []struct {
		name    string
		support []artifact.Artifact
		wantErr error
	}{
		{name: "duplicate path", support: []artifact.Artifact{{Path: "run.sh", Bytes: []byte("a"), Mode: 0o755}, {Path: "run.sh", Bytes: []byte("b"), Mode: 0o755}}, wantErr: fsboundary.ErrUnsafePath},
		{name: "traversal path", support: []artifact.Artifact{{Path: "../escape", Bytes: []byte("a"), Mode: 0o644}}, wantErr: fsboundary.ErrUnsafePath},
	} {
		t.Run(tc.name+" refuses without writes", func(t *testing.T) {
			root := t.TempDir()
			res := mirror(tc.support)
			_, err := prepareNativeHookProjection(skillTarget{skill: res, target: hcc.TargetFactory}, root)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("support refusal = %v, want %v", err, tc.wantErr)
			}
			assertHookProjectionRefusal(t, err, root, "hooks.json")
			if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
				t.Fatalf("repoRoot mutated: %v %v", entries, err)
			}
		})
	}

	t.Run("unverified digest refuses", func(t *testing.T) {
		res := mirror([]artifact.Artifact{{Path: "run.sh", Bytes: []byte("x"), Mode: 0o755}})
		res.MarketplaceDigest = "sha256:not-a-real-digest"
		_, err := prepareNativeHookProjection(skillTarget{skill: res, target: hcc.TargetFactory}, repoRoot)
		assertHookProjectionRefusal(t, err, repoRoot, "hooks.json")
		res.MarketplaceDigest = ""
		_, err = prepareNativeHookProjection(skillTarget{skill: res, target: hcc.TargetFactory}, repoRoot)
		assertHookProjectionRefusal(t, err, repoRoot, "hooks.json")
	})
}

func TestPrepareIndependentNativeHookProjectionMirrorFromResolver(t *testing.T) {
	support := map[string]artifact.SupportFile{
		"run.sh":   {Data: []byte("#!/bin/sh\necho go\n"), Executable: true},
		"data.bin": {Data: []byte{0x00, 0xff, 0x0a}},
	}
	resolved, digest, archiveDigest := resolveMirrorProjectionHook(t, support)
	if resolved.MarketplaceDigest != digest || resolved.MarketplaceArchiveDigest != archiveDigest {
		t.Fatalf("resolved mirror evidence = content:%q archive:%q, want content:%q archive:%q", resolved.MarketplaceDigest, resolved.MarketplaceArchiveDigest, digest, archiveDigest)
	}
	if resolved.MarketplaceHookSupport == nil || len(resolved.MarketplaceHookSupport) != 2 {
		t.Fatalf("resolved mirror support = %#v, want verified two-file inventory", resolved.MarketplaceHookSupport)
	}

	repoRoot := t.TempDir()
	projection, err := prepareNativeHookProjection(skillTarget{skill: resolved, target: hcc.TargetFactory}, repoRoot)
	if err != nil {
		t.Fatalf("prepareNativeHookProjection: %v", err)
	}
	if len(projection.artifacts) != 1 || projection.artifacts[0].Path != "hooks.json" {
		t.Fatalf("artifacts = %#v, want hooks.json", projection.artifacts)
	}
	if len(projection.support) != 2 || projection.support[0].Path != "data.bin" || projection.support[1].Path != "run.sh" {
		t.Fatalf("support = %#v, want sorted data.bin and run.sh", projection.support)
	}
	if !bytes.Equal(projection.support[0].Bytes, support["data.bin"].Data) || projection.support[0].Mode.Perm() != 0o644 {
		t.Fatalf("binary support = %#v", projection.support[0])
	}
	if !bytes.Equal(projection.support[1].Bytes, support["run.sh"].Data) || projection.support[1].Mode.Perm() != 0o755 {
		t.Fatalf("executable support = %#v", projection.support[1])
	}
	identity := "marketplace:resource/" + resolved.MarketplaceResourceName + "@" + digest
	sum := sha256.Sum256([]byte(identity))
	wantSupportBase := "hook-support/src-deploy-hook-" + hex.EncodeToString(sum[:]) + "/deploy-hook"
	if projection.supportRelBase != wantSupportBase {
		t.Fatalf("supportRelBase = %q, want independent %q", projection.supportRelBase, wantSupportBase)
	}
	wantDigest := oracleFingerprintMirror("mp", "src", "hook", "deploy-hook", digest, "deploy-hook", "factory", "repository")
	if projection.sourceDigest != wantDigest {
		t.Fatalf("sourceDigest = %q, want independent %q", projection.sourceDigest, wantDigest)
	}
	if entries, err := os.ReadDir(repoRoot); err != nil || len(entries) != 0 {
		t.Fatalf("repoRoot mutated: %v %v", entries, err)
	}
}

func TestPrepareIndependentNativeHookProjectionMirrorEmptyInventoryFromResolver(t *testing.T) {
	resolved, digest, archiveDigest := resolveMirrorProjectionHook(t, map[string]artifact.SupportFile{})
	if resolved.MarketplaceDigest != digest || resolved.MarketplaceArchiveDigest != archiveDigest || resolved.MarketplaceHookSupport == nil || len(resolved.MarketplaceHookSupport) != 0 {
		t.Fatalf("resolved mirror evidence/support = %#v", resolved)
	}
	repoRoot := t.TempDir()
	projection, err := prepareNativeHookProjection(skillTarget{skill: resolved, target: hcc.TargetFactory}, repoRoot)
	if err != nil {
		t.Fatalf("prepare verified empty inventory: %v", err)
	}
	if projection.support == nil || len(projection.support) != 0 {
		t.Fatalf("projected support = %#v, want non-nil empty inventory", projection.support)
	}
	if entries, err := os.ReadDir(repoRoot); err != nil || len(entries) != 0 {
		t.Fatalf("repoRoot mutated: %v %v", entries, err)
	}
}

func oracleFingerprintMirror(instanceID, registryID, kind, resourceName, digest, installedName, target, scope string) string {
	buf := []byte("hook-source/mirror/v1\x00")
	for _, s := range []string{instanceID, registryID, kind, resourceName, digest, installedName, target, scope} {
		buf = append(buf, strconv.Itoa(len(s))...)
		buf = append(buf, ':')
		buf = append(buf, s...)
	}
	sum := sha256.Sum256(buf)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func TestPrepareIndependentNativeHookProjectionStagedIndexOnly(t *testing.T) {
	source := t.TempDir()
	originalPath := filepath.Join(source, "hooks", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(originalPath), 0o755); err != nil {
		t.Fatal(err)
	}
	definition := []byte(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo witness"}]}]}}`)
	if err := os.WriteFile(originalPath, definition, 0o644); err != nil {
		t.Fatal(err)
	}
	cat, err := catalog.Load(source, "claude")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
	var hook *ir.Hook
	for i := range cat.Entries {
		if h, ok := cat.Entries[i].Resource.(*ir.Hook); ok {
			hook = h
		}
	}
	if hook == nil {
		t.Fatal("catalog produced no hook")
	}
	canonical, err := artifact.FromIR(hook)
	if err != nil {
		t.Fatal(err)
	}
	archive, _, err := artifact.Encode(canonical)
	if err != nil {
		t.Fatal(err)
	}
	decoded, _, err := artifact.Decode(bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	resource, err := artifact.DecodeResource(decoded.Manifest.Kind, decoded.Payload)
	if err != nil {
		t.Fatal(err)
	}
	decodedHook, ok := resource.(*ir.Hook)
	if !ok {
		t.Fatalf("decoded resource is %T, want hook", resource)
	}

	staged := t.TempDir()
	stagedDefinition := filepath.Join(staged, "flattened.json")
	if err := os.WriteFile(stagedDefinition, definition, 0o640); err != nil {
		t.Fatal(err)
	}
	stagedSupport := filepath.Join(staged, "scripts", "run.sh")
	if err := os.MkdirAll(filepath.Dir(stagedSupport), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stagedSupport, []byte{0x00, 0x41, 0xff, 0x0a}, 0o751); err != nil {
		t.Fatal(err)
	}
	resolved := resolver.ResolvedArtifact{
		Entry:  catalog.Entry{Kind: ir.KindHook, Name: decodedHook.Name(), Path: stagedDefinition, Resource: decodedHook},
		Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory},
		IsMarketplace: true, MarketplaceAcquisition: config.MarketplaceAcquisitionIndexOnly,
		MarketplaceInstanceID: "instance", MarketplaceRegistryID: "registry",
		MarketplaceResourceName: decodedHook.Name(), MarketplaceDigest: "sha256:" + strings.Repeat("a", 64),
		StagedSourceDir: staged,
	}
	repoRoot := t.TempDir()
	projection, err := prepareNativeHookProjection(skillTarget{skill: resolved, target: hcc.TargetFactory}, repoRoot)
	if err != nil {
		t.Fatalf("staged index-only prepare: %v", err)
	}
	if len(projection.artifacts) != 1 || projection.artifacts[0].Path != "hooks.json" {
		t.Fatalf("artifacts = %#v", projection.artifacts)
	}
	handler := decodeProjectionDocument(t, projection.artifacts[0].Bytes)["PreToolUse"].([]any)[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)
	if handler["command"] != "echo witness" {
		t.Fatalf("handler = %v, want echo witness", handler)
	}
	found := false
	for _, file := range projection.support {
		if file.Path == "hooks/scripts/run.sh" && bytes.Equal(file.Bytes, []byte{0x00, 0x41, 0xff, 0x0a}) && file.Mode.Perm() == 0o751 {
			found = true
		}
	}
	if !found {
		t.Fatalf("support = %#v, want staged hooks/scripts/run.sh", projection.support)
	}
	if err := os.RemoveAll(staged); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareNativeHookProjection(skillTarget{skill: resolved, target: hcc.TargetFactory}, repoRoot); err == nil {
		t.Fatal("removed staged source still projected")
	}
}
