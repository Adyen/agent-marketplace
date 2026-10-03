package ownership

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

var (
	hookSatisfactionFPAlpha = "sha256:" + strings.Repeat("a", 64)
	hookSatisfactionFPBeta  = "sha256:" + strings.Repeat("b", 64)
	hookSatisfactionFPDelta = "sha256:" + strings.Repeat("d", 64)
	hookSatisfactionFPOwner = "sha256:" + strings.Repeat("e", 64)
)

const (
	hookSatisfactionValueA    = `{"command":"echo alpha","matcher":"Bash","type":"command"}`
	hookSatisfactionValueB    = `{"command":"echo beta","matcher":"Bash","type":"command"}`
	hookSatisfactionValuePost = `{"command":"echo post","type":"command"}`
)

func fixtureHookSatisfaction(event, value string, sources ...HookSourceEvidence) HookSatisfaction {
	valueDigest := Digest([]byte(value))
	return HookSatisfaction{
		Container:      event,
		IdentityDigest: HookIdentityDigest(event, valueDigest),
		ValueDigest:    valueDigest,
		Sources:        append([]HookSourceEvidence(nil), sources...),
	}
}

func satisfactionJSONString(value string) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(data)
}

func rawHookSourceEvidence(t *testing.T, sources []HookSourceEvidence) string {
	t.Helper()
	parts := make([]string, 0, len(sources))
	for _, source := range sources {
		parts = append(parts, `{"resource":`+satisfactionJSONString(source.Resource)+`,"source_digest":`+satisfactionJSONString(source.SourceDigest)+`}`)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func rawHookSatisfaction(t *testing.T, satisfaction HookSatisfaction) string {
	t.Helper()
	return rawHookSatisfactionFields(
		satisfactionJSONString(satisfaction.Container),
		satisfactionJSONString(satisfaction.IdentityDigest),
		satisfactionJSONString(satisfaction.ValueDigest),
		rawHookSourceEvidence(t, satisfaction.Sources),
	)
}

func rawHookSatisfactionFields(container, identity, value, sources string) string {
	return `{"container":` + container + `,"identity_digest":` + identity + `,"value_digest":` + value + `,"sources":` + sources + `}`
}

func rawHookSatisfactionFileState(entries, satisfactions string) string {
	return `{"content_digest":"","entries":` + entries + `,"hook_satisfactions":` + satisfactions + `}`
}

func newHookSatisfactionStore(t *testing.T, native string) (*fixture, *Store) {
	t.Helper()
	f := newFixture(t)
	f.writeAggregate(native)
	return f, f.store()
}

func assertOwnershipFileUnchanged(t *testing.T, path string, before []byte, beforeInfo os.FileInfo) {
	t.Helper()
	after, afterInfo := fileSnapshot(t, path)
	if !bytes.Equal(before, after) || beforeInfo.Mode() != afterInfo.Mode() || !beforeInfo.ModTime().Equal(afterInfo.ModTime()) {
		t.Fatalf("read-only inspection changed %s", path)
	}
}

func directoryEntryCount(t *testing.T, path string) int {
	t.Helper()
	entries, err := os.ReadDir(path)
	if err != nil {
		t.Fatalf("read directory %s: %v", path, err)
	}
	return len(entries)
}

func captureHookSatisfactionLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	previous := slog.Default()
	var output bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &output
}

// spec: hook-native-location-tolerance/satisfaction-only-state-remains-unowned
func TestHookSatisfactionOnlyStateRemainsUnowned(t *testing.T) {
	f, st := newHookSatisfactionStore(t, `{"hooks":{"PreToolUse":[{"matcher":"Execute","hooks":[{"type":"command","command":"echo handwritten"}]}]}}`)
	satisfaction := fixtureHookSatisfaction("PreToolUse", hookSatisfactionValueA,
		HookSourceEvidence{Resource: "beta", SourceDigest: hookSatisfactionFPBeta},
		HookSourceEvidence{Resource: "alpha", SourceDigest: hookSatisfactionFPAlpha},
	)
	writeRawFileState(t, st, f.rel, rawHookSatisfactionFileState("[]", "["+rawHookSatisfaction(t, satisfaction)+"]"))
	stateBefore, stateInfo := fileSnapshot(t, st.StateFile())
	nativeBefore, nativeInfo := fileSnapshot(t, f.absFile)
	proof, err := st.Proof(f.rel)
	if err != nil {
		t.Fatalf("Proof: %v", err)
	}
	if proof.OwnedCount() != 0 || len(proof.OwnedIDs()) != 0 {
		t.Fatalf("satisfaction-only state exposes owned entries: count=%d ids=%v", proof.OwnedCount(), proof.OwnedIDs())
	}
	wantID := EntryID{Kind: KindHook, Container: "PreToolUse", IdentityDigest: satisfaction.IdentityDigest}
	if got := satisfaction.ID(); got != wantID {
		t.Fatalf("satisfaction ID = %+v, want %+v", got, wantID)
	}
	if proof.Classify(wantID, satisfaction.ValueDigest) != StatusUnmanaged {
		t.Fatal("satisfaction-only evidence granted ownership")
	}
	_, hasResource := proof.Resource(wantID)
	_, hasSourceDigest := proof.SourceDigest(wantID)
	if hasResource || hasSourceDigest || proof.HasHookSource(wantID, "alpha", hookSatisfactionFPAlpha) ||
		proof.HasHookSource(wantID, "beta", hookSatisfactionFPBeta) {
		t.Fatal("satisfaction-only evidence widened the owned-only APIs")
	}
	view := proof.HookSatisfactions()
	if len(view) != 1 || view[0].ID() != wantID || view[0].ValueDigest != satisfaction.ValueDigest ||
		!reflect.DeepEqual(view[0].Sources, []HookSourceEvidence{
			{Resource: "alpha", SourceDigest: hookSatisfactionFPAlpha},
			{Resource: "beta", SourceDigest: hookSatisfactionFPBeta},
		}) {
		t.Fatalf("satisfaction view = %+v", view)
	}
	drift := Classify(proof, []EntryValue{{ID: wantID, ValueDigest: satisfaction.ValueDigest}}, nil)
	if len(drift.Clean) != 0 || len(drift.Drifted) != 0 || len(drift.Stale) != 0 ||
		len(drift.Unmanaged) != 1 || drift.Unmanaged[0] != wantID || len(drift.Collisions) != 0 {
		t.Fatalf("satisfaction-only entry affected ownership classification: %+v", drift)
	}
	if _, err := st.Proof(f.rel); err != nil {
		t.Fatalf("repeated Proof: %v", err)
	}
	assertOwnershipFileUnchanged(t, st.StateFile(), stateBefore, stateInfo)
	assertOwnershipFileUnchanged(t, f.absFile, nativeBefore, nativeInfo)
}

func TestHookSatisfactionProofMatchesEveryContributorExactly(t *testing.T) {
	f, st := newHookSatisfactionStore(t, `{"hooks":{"PreToolUse":[],"PostToolUse":[]}}`)
	satisfactionPre := fixtureHookSatisfaction("PreToolUse", hookSatisfactionValueA,
		HookSourceEvidence{Resource: "beta", SourceDigest: hookSatisfactionFPBeta},
		HookSourceEvidence{Resource: "alpha", SourceDigest: hookSatisfactionFPAlpha},
	)
	satisfactionPreOther := fixtureHookSatisfaction("PreToolUse", hookSatisfactionValueB,
		HookSourceEvidence{Resource: "same", SourceDigest: hookSatisfactionFPBeta},
		HookSourceEvidence{Resource: "same", SourceDigest: hookSatisfactionFPAlpha},
	)
	satisfactionPost := fixtureHookSatisfaction("PostToolUse", hookSatisfactionValuePost,
		HookSourceEvidence{Resource: "delta", SourceDigest: hookSatisfactionFPDelta},
	)
	owned := hookEntry("PreToolUse", "owned-resource", hookSatisfactionValueA)
	owned.SourceDigest = hookSatisfactionFPOwner
	entriesJSON, err := json.Marshal([]Entry{owned})
	if err != nil {
		t.Fatal(err)
	}
	rawSatisfactions := "[" + strings.Join([]string{
		rawHookSatisfaction(t, satisfactionPreOther),
		rawHookSatisfaction(t, satisfactionPost),
		rawHookSatisfaction(t, satisfactionPre),
	}, ",") + "]"
	writeRawFileState(t, st, f.rel, rawHookSatisfactionFileState(string(entriesJSON), rawSatisfactions))
	stateBefore, stateInfo := fileSnapshot(t, st.StateFile())

	proof, err := st.Proof(f.rel)
	if err != nil {
		t.Fatalf("Proof: %v", err)
	}
	if proof.OwnedCount() != 1 || !reflect.DeepEqual(proof.OwnedIDs(), []EntryID{owned.ID()}) {
		t.Fatalf("owned API included satisfaction identities: count=%d ids=%v", proof.OwnedCount(), proof.OwnedIDs())
	}
	if satisfactionPre.ID() != owned.ID() {
		t.Fatal("fixture does not share the intended native ID between owned and satisfaction records")
	}
	preA := satisfactionPre
	preA.Sources = []HookSourceEvidence{
		{Resource: "alpha", SourceDigest: hookSatisfactionFPAlpha},
		{Resource: "beta", SourceDigest: hookSatisfactionFPBeta},
	}
	preB := satisfactionPreOther
	preB.Sources = []HookSourceEvidence{
		{Resource: "same", SourceDigest: hookSatisfactionFPAlpha},
		{Resource: "same", SourceDigest: hookSatisfactionFPBeta},
	}
	post := satisfactionPost
	post.Sources = []HookSourceEvidence{{Resource: "delta", SourceDigest: hookSatisfactionFPDelta}}
	preToolRecords := []HookSatisfaction{preA, preB}
	if preToolRecords[0].IdentityDigest > preToolRecords[1].IdentityDigest {
		preToolRecords[0], preToolRecords[1] = preToolRecords[1], preToolRecords[0]
	}
	want := []HookSatisfaction{post, preToolRecords[0], preToolRecords[1]}
	view := proof.HookSatisfactions()
	if !reflect.DeepEqual(view, want) {
		t.Fatalf("satisfaction view = %+v, want %+v", view, want)
	}
	if proof.Classify(owned.ID(), owned.ValueDigest) != StatusOwnedClean {
		t.Fatal("actual owned entry lost its owned-clean classification")
	}
	resource, resourceOK := proof.Resource(owned.ID())
	source, sourceOK := proof.SourceDigest(owned.ID())
	if !resourceOK || resource != "owned-resource" || !sourceOK || source != hookSatisfactionFPOwner {
		t.Fatalf("owned Resource/SourceDigest = %q/%v, %q/%v", resource, resourceOK, source, sourceOK)
	}
	if !proof.HasHookSource(owned.ID(), "owned-resource", hookSatisfactionFPOwner) ||
		proof.HasHookSource(owned.ID(), "alpha", hookSatisfactionFPAlpha) {
		t.Fatal("satisfaction evidence changed the owned-only source API")
	}
	var gotPre HookSatisfaction
	foundPre := false
	for _, record := range view {
		if record.ID() == satisfactionPre.ID() {
			gotPre, foundPre = record, true
		}
	}
	if !foundPre || !reflect.DeepEqual(gotPre.Sources, []HookSourceEvidence{
		{Resource: "alpha", SourceDigest: hookSatisfactionFPAlpha},
		{Resource: "beta", SourceDigest: hookSatisfactionFPBeta},
	}) {
		t.Fatal("contributor evidence did not preserve exact resource/fingerprint pairs")
	}

	view[0].Container = fakeCommand
	view[1].IdentityDigest = fakeCommand
	view[1].Sources[0].Resource = fakeCommand
	view[1].Sources[0].SourceDigest = fakeCommand
	view[1].Sources = append(view[1].Sources, HookSourceEvidence{Resource: fakeCommand, SourceDigest: fakeCommand})
	if again := proof.HookSatisfactions(); !reflect.DeepEqual(again, want) {
		t.Fatalf("mutating an accessor result changed a later proof view: %+v", again)
	}
	assertOwnershipFileUnchanged(t, st.StateFile(), stateBefore, stateInfo)
}

// spec: hook-native-location-tolerance/old-records-remain-readable-without-satisfaction-fields
func TestHookSatisfactionOldRecordsRemainReadableWithoutSatisfactionFields(t *testing.T) {
	hook := hookEntry("PreToolUse", "alpha", hookSatisfactionValueA)
	hook.SourceDigest = hookSatisfactionFPAlpha
	hook.SourceEvidence = []HookSourceEvidence{{Resource: "beta", SourceDigest: hookSatisfactionFPBeta}}
	mcp := mcpEntry("docs", "docs-server", `{"command":"echo docs"}`)
	entriesJSON, err := json.Marshal([]Entry{hook, mcp})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name            string
		satisfactionRaw string
	}{
		{name: "absent"},
		{name: "null", satisfactionRaw: "null"},
		{name: "empty", satisfactionRaw: "[]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, st := newHookSatisfactionStore(t, `{"hooks":{"PreToolUse":[]}}`)
			if tc.satisfactionRaw == "" {
				writeRawState(t, st, f.rel, string(entriesJSON))
			} else {
				writeRawFileState(t, st, f.rel, rawHookSatisfactionFileState(string(entriesJSON), tc.satisfactionRaw))
			}
			stateBefore, stateInfo := fileSnapshot(t, st.StateFile())
			proof, err := st.Proof(f.rel)
			if err != nil {
				t.Fatalf("Proof: %v", err)
			}
			if proof.OwnedCount() != 2 || len(proof.OwnedIDs()) != 2 || len(proof.HookSatisfactions()) != 0 {
				t.Fatalf("old proof changed: owned=%v satisfactions=%v", proof.OwnedIDs(), proof.HookSatisfactions())
			}
			if status := proof.Classify(hook.ID(), hook.ValueDigest); status != StatusOwnedClean {
				t.Fatalf("old hook status = %v, want owned-clean", status)
			}
			if !proof.HasHookSource(hook.ID(), "alpha", hookSatisfactionFPAlpha) ||
				!proof.HasHookSource(hook.ID(), "beta", hookSatisfactionFPBeta) ||
				proof.HasHookSource(hook.ID(), "alpha", hookSatisfactionFPBeta) {
				t.Fatal("existing scalar/multi-source hook evidence changed meaning")
			}
			if resource, ok := proof.Resource(mcp.ID()); !ok || resource != "docs-server" {
				t.Fatalf("existing MCP resource = %q, %v", resource, ok)
			}
			var zero Proof
			if zero.OwnedCount() != 0 || len(zero.HookSatisfactions()) != 0 {
				t.Fatal("zero Proof exposed state")
			}
			assertOwnershipFileUnchanged(t, st.StateFile(), stateBefore, stateInfo)
		})
	}
}

func TestHookSatisfactionInvalidStateRefusesWithoutMutation(t *testing.T) {
	valid := fixtureHookSatisfaction("PreToolUse", hookSatisfactionValueA,
		HookSourceEvidence{Resource: "alpha", SourceDigest: hookSatisfactionFPAlpha})
	event, identity, value := satisfactionJSONString(valid.Container), satisfactionJSONString(valid.IdentityDigest), satisfactionJSONString(valid.ValueDigest)
	sources := rawHookSourceEvidence(t, valid.Sources)
	validRecord := rawHookSatisfaction(t, valid)
	one := func(record string) string { return rawHookSatisfactionFileState("[]", "["+record+"]") }
	many := func(records string) string { return rawHookSatisfactionFileState("[]", "["+records+"]") }
	fields := func(container, identityDigest, valueDigest, sourcesJSON string) string {
		return rawHookSatisfactionFields(container, identityDigest, valueDigest, sourcesJSON)
	}
	pairAlpha := `{"resource":"alpha","source_digest":` + satisfactionJSONString(hookSatisfactionFPAlpha) + `}`
	cases := []struct {
		name       string
		fileState  string
		wantUnsafe bool
	}{
		{name: "missing_container", fileState: many(`{"identity_digest":` + identity + `,"value_digest":` + value + `,"sources":` + sources + `}`), wantUnsafe: true},
		{name: "unsafe_container", fileState: one(fields(satisfactionJSONString(fakeCommand), identity, value, sources)), wantUnsafe: true},
		{name: "bad_identity_digest", fileState: one(fields(event, satisfactionJSONString("not-a-digest"), value, sources)), wantUnsafe: true},
		{name: "bad_value_digest", fileState: one(fields(event, satisfactionJSONString(HookIdentityDigest(valid.Container, "not-a-digest")), satisfactionJSONString("not-a-digest"), sources)), wantUnsafe: true},
		{name: "identity_does_not_match_event_value", fileState: one(fields(event, satisfactionJSONString(HookIdentityDigest("PostToolUse", valid.ValueDigest)), value, sources)), wantUnsafe: true},
		{name: "sources_empty", fileState: one(fields(event, identity, value, "[]")), wantUnsafe: true},
		{name: "sources_null", fileState: one(fields(event, identity, value, "null")), wantUnsafe: true},
		{name: "source_resource_missing", fileState: one(fields(event, identity, value, "[{\"source_digest\":"+satisfactionJSONString(hookSatisfactionFPAlpha)+"}]")), wantUnsafe: true},
		{name: "source_resource_unsafe", fileState: one(fields(event, identity, value, "[{\"resource\":"+satisfactionJSONString(fakeCommand)+",\"source_digest\":"+satisfactionJSONString(hookSatisfactionFPAlpha)+"}]")), wantUnsafe: true},
		{name: "source_digest_missing", fileState: one(fields(event, identity, value, `[{"resource":"alpha"}]`)), wantUnsafe: true},
		{name: "source_digest_malformed", fileState: one(fields(event, identity, value, `[{"resource":"alpha","source_digest":"sha256:zz"}]`)), wantUnsafe: true},
		{name: "duplicate_native_identity", fileState: many(validRecord + "," + validRecord), wantUnsafe: true},
		{name: "duplicate_exact_source_pair", fileState: one(fields(event, identity, value, "["+pairAlpha+","+pairAlpha+"]")), wantUnsafe: true},
		{name: "satisfaction_bucket_wrong_type", fileState: rawHookSatisfactionFileState("[]", satisfactionJSONString(fakeCommand))},
		{name: "satisfaction_record_wrong_type", fileState: many(`"not-a-record"`)},
		{name: "container_wrong_type", fileState: one(fields("7", identity, value, sources))},
		{name: "sources_wrong_type", fileState: one(fields(event, identity, value, satisfactionJSONString(fakeCommand)))},
		{name: "source_not_object", fileState: one(fields(event, identity, value, `["not-an-object"]`))},
		{name: "source_resource_wrong_type", fileState: one(fields(event, identity, value, `[{"resource":7,"source_digest":"`+hookSatisfactionFPAlpha+`"}]`))},
		{name: "collection_duplicate_key_forward", fileState: `{"content_digest":"","entries":[],"hook_satisfactions":[` + validRecord + `],"hook_satisfactions":[]}`},
		{name: "collection_duplicate_key_reverse", fileState: `{"content_digest":"","entries":[],"hook_satisfactions":[],"hook_satisfactions":[` + validRecord + `]}`},
		{name: "collection_alias_forward", fileState: `{"content_digest":"","entries":[],"hook_satisfactions":[` + validRecord + `],"Hook_Satisfactions":[]}`},
		{name: "collection_alias_reverse", fileState: `{"content_digest":"","entries":[],"Hook_Satisfactions":[],"hook_satisfactions":[` + validRecord + `]}`},
		{name: "record_duplicate_container_forward", fileState: one(`{"container":"PreToolUse","container":"PostToolUse","identity_digest":` + identity + `,"value_digest":` + value + `,"sources":` + sources + `}`)},
		{name: "record_duplicate_container_reverse", fileState: one(`{"container":"PostToolUse","container":"PreToolUse","identity_digest":` + identity + `,"value_digest":` + value + `,"sources":` + sources + `}`)},
		{name: "record_container_alias_empty_forward", fileState: one(`{"container":"","Container":"PreToolUse","identity_digest":` + identity + `,"value_digest":` + value + `,"sources":` + sources + `}`)},
		{name: "record_container_alias_empty_reverse", fileState: one(`{"Container":"","container":"PreToolUse","identity_digest":` + identity + `,"value_digest":` + value + `,"sources":` + sources + `}`)},
		{name: "nested_resource_duplicate_forward", fileState: one(fields(event, identity, value, `[{"resource":"alpha","resource":"beta","source_digest":"`+hookSatisfactionFPAlpha+`"}]`))},
		{name: "nested_resource_duplicate_reverse", fileState: one(fields(event, identity, value, `[{"resource":"beta","resource":"alpha","source_digest":"`+hookSatisfactionFPAlpha+`"}]`))},
		{name: "nested_resource_alias_empty_forward", fileState: one(fields(event, identity, value, `[{"resource":"","Resource":"alpha","source_digest":"`+hookSatisfactionFPAlpha+`"}]`))},
		{name: "nested_resource_alias_empty_reverse", fileState: one(fields(event, identity, value, `[{"Resource":"","resource":"alpha","source_digest":"`+hookSatisfactionFPAlpha+`"}]`))},
		{name: "nested_digest_alias_empty_forward", fileState: one(fields(event, identity, value, `[{"resource":"alpha","source_digest":"","Source_Digest":"`+hookSatisfactionFPAlpha+`"}]`))},
		{name: "nested_digest_alias_empty_reverse", fileState: one(fields(event, identity, value, `[{"resource":"alpha","Source_Digest":"","source_digest":"`+hookSatisfactionFPAlpha+`"}]`))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, st := newHookSatisfactionStore(t, `{"hooks":{"PreToolUse":[]}}`)
			writeRawFileState(t, st, f.rel, tc.fileState)
			stateBefore, stateInfo := fileSnapshot(t, st.StateFile())
			recordDirBefore := directoryEntryCount(t, st.recordDir())
			logs := captureHookSatisfactionLogs(t)
			proof, err := st.Proof(f.rel)
			if err == nil || !errors.Is(err, ErrStateCorrupt) {
				t.Fatalf("Proof error = %v, want ErrStateCorrupt", err)
			}
			coded, ok := diag.AsCodedError(err)
			if !ok || coded.Code != diag.CodeOwnershipStateCorrupt || coded.Severity != diag.SeverityError {
				t.Fatalf("corruption diagnostic = %+v", coded)
			}
			if tc.wantUnsafe && !errors.Is(err, ErrUnsafeStateValue) {
				t.Fatalf("Proof error = %v, want ErrUnsafeStateValue in error chain", err)
			}
			if proof.OwnedCount() != 0 || len(proof.OwnedIDs()) != 0 || len(proof.HookSatisfactions()) != 0 {
				t.Fatal("corrupt state exposed usable ownership or satisfaction evidence")
			}
			for _, rejected := range []string{fakeCommand, fakeSecret, fakePrompt, fakeMatcher, "not-a-digest", "sha256:zz", hookSatisfactionFPAlpha} {
				if strings.Contains(err.Error(), rejected) || strings.Contains(logs.String(), rejected) {
					t.Fatal("state refusal exposed rejected data")
				}
			}
			assertOwnershipFileUnchanged(t, st.StateFile(), stateBefore, stateInfo)
			if directoryEntryCount(t, st.recordDir()) != recordDirBefore {
				t.Fatal("read-only refusal created state-root residue")
			}
		})
	}
}
