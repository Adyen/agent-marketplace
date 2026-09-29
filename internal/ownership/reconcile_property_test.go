package ownership

import (
	"sort"
	"testing"

	"pgregory.net/rapid"
)

var (
	propMcpKeys     = []string{"alpha", "beta", "gamma"}
	propHookEvents  = []string{"PreToolUse", "PostToolUse"}
	propHookIDParts = []string{"h1", "h2"}
	propDigests     = []string{"sha256:d1", "sha256:d2", "sha256:d3"}
)

type propEntry struct {
	id             EntryID
	owned          bool
	ownedDigest    string
	existing       bool
	existingDigest string
	desired        bool
	desiredDigest  string
}

type propScenario struct {
	proof    Proof
	existing []EntryValue
	desired  []EntryValue
	model    map[string]propEntry
}

func propDrawID(t *rapid.T) EntryID {
	if rapid.Bool().Draw(t, "is-hook") {
		event := rapid.SampledFrom(propHookEvents).Draw(t, "event")
		part := rapid.SampledFrom(propHookIDParts).Draw(t, "hook-id")
		return EntryID{Kind: KindHook, Container: event, IdentityDigest: part}
	}
	key := rapid.SampledFrom(propMcpKeys).Draw(t, "key")
	return EntryID{Kind: KindMcpServer, Container: "mcpServers", Key: key, IdentityDigest: McpIdentityDigest(key)}
}

func propDrawModel(t *rapid.T) map[string]propEntry {
	n := rapid.IntRange(1, 7).Draw(t, "entries")
	model := make(map[string]propEntry, n)
	for len(model) < n {
		e := propEntry{id: propDrawID(t)}
		if _, dup := model[e.id.mapKey()]; dup {
			continue
		}
		e.owned = rapid.Bool().Draw(t, "owned")
		if e.owned {
			e.ownedDigest = rapid.SampledFrom(propDigests).Draw(t, "owned-digest")
		}
		e.existing = rapid.Bool().Draw(t, "existing")
		if e.existing {
			if e.owned && rapid.Bool().Draw(t, "existing-matches-owned") {
				e.existingDigest = e.ownedDigest
			} else {
				e.existingDigest = rapid.SampledFrom(propDigests).Draw(t, "existing-digest")
			}
		}
		e.desired = rapid.Bool().Draw(t, "desired")
		if e.desired {
			if e.existing && rapid.Bool().Draw(t, "desired-matches-existing") {
				e.desiredDigest = e.existingDigest
			} else {
				e.desiredDigest = rapid.SampledFrom(propDigests).Draw(t, "desired-digest")
			}
		}
		model[e.id.mapKey()] = e
	}
	return model
}

func propModelKeys(model map[string]propEntry) []string {
	keys := make([]string, 0, len(model))
	for k := range model {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func propBuild(model map[string]propEntry, zeroProof bool) propScenario {
	s := propScenario{model: model}
	keys := propModelKeys(model)
	if !zeroProof {
		s.proof.file = "mcp_config.json"
		s.proof.entries = make(map[string]Entry)
		for _, k := range keys {
			e := model[k]
			if e.owned {
				s.proof.entries[k] = Entry{
					Kind: e.id.Kind, Container: e.id.Container, Key: e.id.Key,
					IdentityDigest: e.id.IdentityDigest, ValueDigest: e.ownedDigest,
					Resource: "res",
				}
			}
		}
	}
	for _, k := range keys {
		e := model[k]
		if e.existing {
			s.existing = append(s.existing, EntryValue{ID: e.id, ValueDigest: e.existingDigest})
		}
		if e.desired {
			s.desired = append(s.desired, EntryValue{ID: e.id, ValueDigest: e.desiredDigest})
		}
	}
	return s
}

func propShuffle(t *rapid.T, evs []EntryValue) {
	for i := len(evs) - 1; i > 0; i-- {
		j := rapid.IntRange(0, i).Draw(t, "perm")
		evs[i], evs[j] = evs[j], evs[i]
	}
}

func propKeys(ids []EntryID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.mapKey()
	}
	sort.Strings(out)
	return out
}

func propWantKeys(t *rapid.T, model map[string]propEntry, want func(propEntry) bool) []string {
	var out []string
	for k, e := range model {
		if want(e) {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func propAdoptionKeys(cs []AdoptionCandidate) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.id.mapKey()
	}
	return out
}

func propConflictKeys(cs []ConflictCandidate) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.id.mapKey()
	}
	return out
}

func propAdoptionSeq(cs []AdoptionCandidate) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.id.mapKey() + "|" + c.ValueDigest()
	}
	return out
}

func propConflictSeq(cs []ConflictCandidate) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.id.mapKey() + "|" + c.ExistingValueDigest() + "|" + c.DesiredValueDigest()
	}
	return out
}

func propAssertKeys(t *rapid.T, what string, got []string, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s = %v, want %v", what, got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%s = %v, want %v", what, got, want)
		}
	}
}

func propAssertSorted(t *rapid.T, what string, ids []EntryID) {
	t.Helper()
	for i := 1; i < len(ids); i++ {
		if ids[i-1].mapKey() > ids[i].mapKey() {
			t.Fatalf("%s is not sorted: %v", what, propKeys(ids))
		}
	}
}

func propCheck(t *rapid.T, s propScenario, zeroProof bool) {
	m := s.model
	report := Classify(s.proof, s.existing, s.desired)

	propAssertKeys(t, "clean", propKeys(report.Clean),
		propWantKeys(t, m, func(e propEntry) bool { return e.existing && e.owned && e.existingDigest == e.ownedDigest }))
	propAssertKeys(t, "drifted", propKeys(report.Drifted),
		propWantKeys(t, m, func(e propEntry) bool { return e.existing && e.owned && e.existingDigest != e.ownedDigest }))
	propAssertKeys(t, "unmanaged", propKeys(report.Unmanaged),
		propWantKeys(t, m, func(e propEntry) bool { return e.existing && !e.owned }))
	nExisting := propWantKeys(t, m, func(e propEntry) bool { return e.existing })
	if len(report.Clean)+len(report.Drifted)+len(report.Unmanaged) != len(nExisting) {
		t.Fatalf("classes do not partition existing: %+v", report)
	}

	propAssertKeys(t, "stale", propKeys(report.Stale),
		propWantKeys(t, m, func(e propEntry) bool { return e.owned && e.existing && !e.desired }))

	propAssertKeys(t, "collisions", propKeys(report.Collisions),
		propWantKeys(t, m, func(e propEntry) bool { return e.desired && e.existing && !e.owned }))

	if zeroProof {
		if len(report.Clean) != 0 || len(report.Drifted) != 0 || len(report.Stale) != 0 {
			t.Fatalf("zero proof produced owned or stale entries: %+v", report)
		}
		if len(report.Unmanaged) != len(nExisting) {
			t.Fatalf("zero proof did not mark every existing entry unmanaged: %+v", report)
		}
	}

	adopt := ExactAdoptionCandidates(s.proof, s.existing, s.desired)
	conflict := ValueConflictCandidates(s.proof, s.existing, s.desired)
	for _, c := range adopt {
		e := m[c.id.mapKey()]
		if c.ValueDigest() != e.existingDigest || c.ValueDigest() != e.desiredDigest {
			t.Fatalf("adoption digest mismatch for %v", c.EntryID())
		}
	}
	for _, c := range conflict {
		e := m[c.id.mapKey()]
		if c.ExistingValueDigest() != e.existingDigest || c.DesiredValueDigest() != e.desiredDigest || !c.Differs() {
			t.Fatalf("conflict digest mismatch for %v", c.EntryID())
		}
	}
	propAssertKeys(t, "adoption", propAdoptionKeys(adopt),
		propWantKeys(t, m, func(e propEntry) bool {
			return e.existing && !e.owned && e.desired && e.existingDigest == e.desiredDigest
		}))
	propAssertKeys(t, "conflict", propConflictKeys(conflict),
		propWantKeys(t, m, func(e propEntry) bool {
			return e.existing && !e.owned && e.desired && e.existingDigest != e.desiredDigest
		}))
	for _, a := range propAdoptionKeys(adopt) {
		for _, c := range propConflictKeys(conflict) {
			if a == c {
				t.Fatalf("adoption and conflict overlap on %v", a)
			}
		}
	}

	propAssertSorted(t, "clean", report.Clean)
	propAssertSorted(t, "drifted", report.Drifted)
	propAssertSorted(t, "unmanaged", report.Unmanaged)
	propAssertSorted(t, "stale", report.Stale)
	propAssertSorted(t, "collisions", report.Collisions)

	shuffledExisting := append([]EntryValue(nil), s.existing...)
	shuffledDesired := append([]EntryValue(nil), s.desired...)
	propShuffle(t, shuffledExisting)
	propShuffle(t, shuffledDesired)
	again := Classify(s.proof, shuffledExisting, shuffledDesired)
	for name, pair := range map[string][2][]EntryID{
		"clean":      {report.Clean, again.Clean},
		"drifted":    {report.Drifted, again.Drifted},
		"unmanaged":  {report.Unmanaged, again.Unmanaged},
		"stale":      {report.Stale, again.Stale},
		"collisions": {report.Collisions, again.Collisions},
	} {
		propAssertKeys(t, "permuted "+name, propKeys(pair[0]), propKeys(pair[1]))
	}
	propAssertKeys(t, "permuted adoption", propAdoptionSeq(adopt),
		propAdoptionSeq(ExactAdoptionCandidates(s.proof, shuffledExisting, shuffledDesired)))
	propAssertKeys(t, "permuted conflict", propConflictSeq(conflict),
		propConflictSeq(ValueConflictCandidates(s.proof, shuffledExisting, shuffledDesired)))
}

func propConstrain(model map[string]propEntry, f func(*propEntry)) {
	k := propModelKeys(model)[0]
	e := model[k]
	f(&e)
	model[k] = e
}

func TestClassifyProperties(t *testing.T) {
	cases := map[string]func(map[string]propEntry) bool{
		"zero-proof": func(m map[string]propEntry) bool {
			for k, e := range m {
				e.owned = false
				e.ownedDigest = ""
				m[k] = e
			}
			propConstrain(m, func(e *propEntry) { e.existing = true })
			return true
		},
		"drifted": func(m map[string]propEntry) bool {
			propConstrain(m, func(e *propEntry) {
				e.owned, e.existing = true, true
				e.ownedDigest, e.existingDigest = propDigests[0], propDigests[1]
			})
			return false
		},
		"stale": func(m map[string]propEntry) bool {
			propConstrain(m, func(e *propEntry) {
				e.owned, e.existing, e.desired = true, true, false
				e.ownedDigest, e.existingDigest = propDigests[0], propDigests[0]
			})
			return false
		},
		"collision": func(m map[string]propEntry) bool {
			propConstrain(m, func(e *propEntry) {
				e.owned, e.existing, e.desired = false, true, true
			})
			return false
		},
		"adoption": func(m map[string]propEntry) bool {
			propConstrain(m, func(e *propEntry) {
				e.owned, e.existing, e.desired = false, true, true
				e.existingDigest, e.desiredDigest = propDigests[0], propDigests[0]
			})
			return false
		},
		"conflict": func(m map[string]propEntry) bool {
			propConstrain(m, func(e *propEntry) {
				e.owned, e.existing, e.desired = false, true, true
				e.existingDigest, e.desiredDigest = propDigests[0], propDigests[1]
			})
			return false
		},
		"mixed": func(m map[string]propEntry) bool { return false },
	}
	for name, constrain := range cases {
		constrain := constrain
		t.Run(name, rapid.MakeCheck(func(t *rapid.T) {
			model := propDrawModel(t)
			zeroProof := constrain(model)
			propCheck(t, propBuild(model, zeroProof), zeroProof)
		}))
	}
}
