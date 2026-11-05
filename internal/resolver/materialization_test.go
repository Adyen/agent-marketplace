package resolver

import (
	"reflect"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func materializationArtifact(name, body string, kind ir.ResourceKind, source config.Source, targets []hcc.Target) ResolvedArtifact {
	var resource ir.Resource
	switch kind {
	case ir.KindSkill:
		resource = ir.NewSkill(name, "test", body, nil, nil, ir.SourceRef{})
	case ir.KindCommand:
		resource = ir.NewCommand(name, "test", body, nil, ir.SourceRef{})
	case ir.KindSubagent:
		resource = ir.NewSubagent(name, "test", body, "", nil, "", nil, "", nil, ir.SourceRef{})
	case ir.KindHook:
		resource = ir.NewHook(name, "SessionStart", body, "command", nil, ir.SourceRef{})
	case ir.KindMcpServer:
		resource = ir.NewMcpServer(name, body, nil, nil, nil, ir.SourceRef{})
	}
	return ResolvedArtifact{Entry: catalog.Entry{Kind: kind, Name: name, Resource: resource}, RegistryURL: "https://example.invalid/" + body, Source: source, Harnesses: targets}
}

func TestMaterializationWinnersPreservesInputAndPartialTargets(t *testing.T) {
	firstTargets := []hcc.Target{hcc.TargetFactory, hcc.TargetClaude}
	secondTargets := []hcc.Target{hcc.TargetFactory, hcc.TargetGemini}
	first := materializationArtifact("shared", "first", ir.KindSkill, config.RepoCommitted, firstTargets)
	first.FilePrecedence = 1
	second := materializationArtifact("shared", "second", ir.KindSkill, config.RepoCommitted, secondTargets)
	second.FilePrecedence = 2
	beforeFirst := append([]hcc.Target(nil), firstTargets...)
	beforeSecond := append([]hcc.Target(nil), secondTargets...)
	out := MaterializationWinners([]ResolvedArtifact{first, second})
	if !reflect.DeepEqual(firstTargets, beforeFirst) || !reflect.DeepEqual(secondTargets, beforeSecond) {
		t.Fatalf("input targets changed: %v / %v", firstTargets, secondTargets)
	}
	if len(out) != 2 || !reflect.DeepEqual(out[0].Harnesses, []hcc.Target{hcc.TargetClaude}) || !reflect.DeepEqual(out[1].Harnesses, []hcc.Target{hcc.TargetFactory, hcc.TargetGemini}) {
		t.Fatalf("partial target projection = %#v", out)
	}
}

func TestMaterializationWinnersLegacyEmptyPriorityAndOrdering(t *testing.T) {
	empty := materializationArtifact("empty", "empty", ir.KindSkill, config.RepoCommitted, []hcc.Target{})
	legacy := materializationArtifact("legacy", "legacy", ir.KindSkill, config.RepoCommitted, nil)
	lower := materializationArtifact("shared", "lower", ir.KindSkill, config.UserPersonal, []hcc.Target{hcc.TargetFactory})
	higher := materializationArtifact("shared", "higher", ir.KindSkill, config.Organization, []hcc.Target{hcc.TargetFactory})
	higher.Required = true
	tiedFirst := materializationArtifact("tied", "first", ir.KindSkill, config.RepoCommitted, []hcc.Target{hcc.TargetFactory})
	tiedSecond := materializationArtifact("tied", "second", ir.KindSkill, config.RepoCommitted, []hcc.Target{hcc.TargetFactory})
	tiedSecond.RegistryURL = tiedFirst.RegistryURL
	out := MaterializationWinners([]ResolvedArtifact{empty, legacy, lower, higher, tiedFirst, tiedSecond})
	if len(out) != 3 || out[0].Name != "legacy" || !reflect.DeepEqual(out[0].Harnesses, []hcc.Target{hcc.TargetClaude}) || out[1].Resource.Name() != "shared" || out[1].RegistryURL != higher.RegistryURL || out[2].RegistryURL != tiedFirst.RegistryURL {
		t.Fatalf("projection = %#v", out)
	}
}

func TestMaterializationWinnersNilBlocksFallbackAndUnresolvedDoesNot(t *testing.T) {
	nilWinner := ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "blocked"}, RegistryURL: "https://example.invalid/high", Source: config.Organization, Required: true, Harnesses: []hcc.Target{hcc.TargetFactory}}
	fallback := materializationArtifact("blocked", "fallback", ir.KindSkill, config.UserPersonal, []hcc.Target{hcc.TargetFactory})
	metadata := materializationArtifact("visible", "metadata", ir.KindSkill, config.Organization, []hcc.Target{hcc.TargetFactory})
	metadata.Unresolved = true
	valid := materializationArtifact("visible", "valid", ir.KindSkill, config.UserPersonal, []hcc.Target{hcc.TargetFactory})
	out := MaterializationWinners([]ResolvedArtifact{fallback, nilWinner, metadata, valid})
	if len(out) != 2 || out[0].Resource != nil || out[0].Name != "blocked" || out[1].Resource == nil || out[1].Name != "visible" {
		t.Fatalf("nil/unresolved projection = %#v", out)
	}
}

func TestMaterializationWinnersPreservesScopesAndKinds(t *testing.T) {
	machine := materializationArtifact("shared", "machine", ir.KindSkill, config.UserPersonal, []hcc.Target{hcc.TargetFactory})
	project := materializationArtifact("shared", "project", ir.KindSkill, config.RepoCommitted, []hcc.Target{hcc.TargetFactory})
	otherKind := materializationArtifact("shared", "mcp", ir.KindMcpServer, config.RepoCommitted, []hcc.Target{hcc.TargetFactory})
	out := MaterializationWinners([]ResolvedArtifact{machine, project, otherKind})
	if len(out) != 3 {
		t.Fatalf("scope/kind controls projected to %d", len(out))
	}
}

func TestMaterializationWinnersMarketplaceURLTieBreak(t *testing.T) {
	for _, tc := range []struct {
		name                string
		marketplaceURL      string
		marketplaceOrigin   string
		marketplaceRequired bool
		want                string
	}{
		{name: "marketplace endpoint wins over acquisition origin", marketplaceURL: "https://a.example", marketplaceOrigin: "https://z.example/repo.git", want: "marketplace"},
		{name: "Git wins over marketplace endpoint", marketplaceURL: "https://z.example", marketplaceOrigin: "https://a.example/repo.git", want: "git"},
		{name: "empty marketplace origin uses endpoint", marketplaceURL: "https://z.example", want: "git"},
		{name: "mirror URLs agree", marketplaceURL: "https://a.example", marketplaceOrigin: "https://a.example", want: "marketplace"},
		{name: "required marketplace wins before URL", marketplaceURL: "https://z.example", marketplaceOrigin: "https://z.example/repo.git", marketplaceRequired: true, want: "marketplace"},
	} {
		for _, reverse := range []bool{false, true} {
			t.Run(tc.name+map[bool]string{false: "/forward", true: "/reverse"}[reverse], func(t *testing.T) {
				marketplace := materializationArtifact("shared", "marketplace", ir.KindSkill, config.RepoCommitted, []hcc.Target{hcc.TargetFactory})
				marketplace.CatalogKey = "marketplace"
				marketplace.IsMarketplace = true
				marketplace.MarketplaceURL = tc.marketplaceURL
				marketplace.RegistryURL = tc.marketplaceOrigin
				marketplace.Required = tc.marketplaceRequired
				git := materializationArtifact("shared", "git", ir.KindSkill, config.RepoCommitted, []hcc.Target{hcc.TargetFactory})
				git.CatalogKey = "git"
				git.RegistryURL = "https://m.example/repo.git"
				input := []ResolvedArtifact{marketplace, git}
				if reverse {
					input[0], input[1] = input[1], input[0]
				}
				out := MaterializationWinners(input)
				if len(out) != 1 || out[0].CatalogKey != tc.want {
					t.Fatalf("winner = %#v, want %q", out, tc.want)
				}
			})
		}
	}
}

func TestMaterializationWinnersPreservesAdditiveHooks(t *testing.T) {
	first := materializationArtifact("startup", "first", ir.KindHook, config.UserPersonal, []hcc.Target{hcc.TargetFactory})
	second := materializationArtifact("startup", "second", ir.KindHook, config.Organization, []hcc.Target{hcc.TargetFactory})
	out := MaterializationWinners([]ResolvedArtifact{first, second})
	if len(out) != 2 {
		t.Fatalf("additive hooks projected to %d", len(out))
	}
}

func TestMaterializationProjectionSharedSkillCommandPath(t *testing.T) {
	targets := []hcc.Target{hcc.TargetDevin, hcc.TargetCodex, hcc.TargetClaude}
	required := materializationArtifact("audit", "org", ir.KindSkill, config.Organization, targets)
	required.Required = true
	command := materializationArtifact("audit", "user", ir.KindCommand, config.UserPersonal, targets)
	for name, input := range map[string][]ResolvedArtifact{
		"skill-first":   {required, command},
		"command-first": {command, required},
	} {
		t.Run(name, func(t *testing.T) {
			out, displaced := MaterializationProjection(input)
			byKind := map[ir.ResourceKind][]hcc.Target{}
			for _, a := range out {
				byKind[a.Resource.Kind()] = a.Harnesses
			}
			if !reflect.DeepEqual(byKind[ir.KindSkill], targets) || !reflect.DeepEqual(byKind[ir.KindCommand], []hcc.Target{hcc.TargetClaude}) {
				t.Fatalf("projection = %v", byKind)
			}
			paths := map[hcc.Target]string{}
			for _, d := range displaced {
				winnerSource, displacedSource := d.Sources()
				if d.Winner.Resource.Kind() != ir.KindSkill || d.Displaced.Resource.Kind() != ir.KindCommand || !d.MachineScope || input[d.DisplacedIndex].Resource.Kind() != ir.KindCommand ||
					winnerSource != "https://example.invalid/org" || displacedSource != "https://example.invalid/user" {
					t.Fatalf("displacement = %+v (%s, %s)", d, winnerSource, displacedSource)
				}
				winnerPath, displacedPath := d.Paths()
				if winnerPath != displacedPath {
					t.Fatalf("same-name paths differ: %q, %q", winnerPath, displacedPath)
				}
				paths[d.Target] = displacedPath
			}
			want := map[hcc.Target]string{hcc.TargetDevin: "skills/audit/SKILL.md", hcc.TargetCodex: ".agents/skills/audit/SKILL.md"}
			if !reflect.DeepEqual(paths, want) {
				t.Fatalf("displaced paths = %v, want %v", paths, want)
			}
		})
	}
}

func TestMaterializationProjectionSharedPathTieIsOrderIndependent(t *testing.T) {
	targets := []hcc.Target{hcc.TargetDevin}
	skill := materializationArtifact("review", "b-skill", ir.KindSkill, config.UserPersonal, targets)
	command := materializationArtifact("review", "a-command", ir.KindCommand, config.UserPersonal, targets)
	for _, input := range [][]ResolvedArtifact{{skill, command}, {command, skill}} {
		out, displaced := MaterializationProjection(input)
		if len(out) != 1 || out[0].Resource.Kind() != ir.KindCommand || len(displaced) != 1 || displaced[0].Displaced.Resource.Kind() != ir.KindSkill {
			t.Fatalf("equal-precedence winner must follow source order, not input order: out=%v displaced=%v", out, displaced)
		}
	}
}

func TestMaterializationProjectionKeepsSeparatePathsAndSameKindRivals(t *testing.T) {
	targets := []hcc.Target{hcc.TargetDevin, hcc.TargetClaude}
	skill := materializationArtifact("shared", "skill", ir.KindSkill, config.UserPersonal, targets)
	mcp := materializationArtifact("shared", "mcp", ir.KindMcpServer, config.UserPersonal, targets)
	subagent := materializationArtifact("shared", "subagent", ir.KindSubagent, config.UserPersonal, targets)
	rival := materializationArtifact("shared", "rival", ir.KindSkill, config.RepoCommitted, targets)
	rivalLoser := materializationArtifact("shared", "rival-loser", ir.KindSkill, config.RepoCommitted, targets)
	out, displaced := MaterializationProjection([]ResolvedArtifact{skill, mcp, subagent, rival, rivalLoser})
	if len(out) != 4 || len(displaced) != 0 {
		t.Fatalf("separate paths and same-kind rivals: out=%d displaced=%v", len(out), displaced)
	}
}

// The alias table is derived from the target layouts; this independent
// expectation makes any layout change that lets two kinds share a path, or
// stop sharing one, a deliberate review decision.
func TestSharedPathKindsMatchesTargetLayouts(t *testing.T) {
	want := map[hcc.Target]map[ir.ResourceKind]ir.ResourceKind{
		hcc.TargetDevin: {ir.KindSkill: ir.KindSkill, ir.KindCommand: ir.KindSkill},
		hcc.TargetCodex: {ir.KindSkill: ir.KindSkill, ir.KindCommand: ir.KindSkill},
	}
	if got := sharedPathKinds(); !reflect.DeepEqual(got, want) {
		t.Fatalf("sharedPathKinds() = %v, want %v", got, want)
	}
}

func TestMaterializationProjectionCaseVariantSharedPath(t *testing.T) {
	targets := []hcc.Target{hcc.TargetDevin, hcc.TargetCodex, hcc.TargetClaude}
	for _, tc := range []struct {
		name                       string
		requiredKind, optionalKind ir.ResourceKind
		requiredName, optionalName string
		wantDevin, wantCodex       [2]string
	}{
		{"required skill keeps the path", ir.KindSkill, ir.KindCommand, "audit", "Audit",
			[2]string{"skills/audit/SKILL.md", "skills/Audit/SKILL.md"}, [2]string{".agents/skills/audit/SKILL.md", ".agents/skills/Audit/SKILL.md"}},
		{"required command keeps the path", ir.KindCommand, ir.KindSkill, "Audit", "audit",
			[2]string{"skills/Audit/SKILL.md", "skills/audit/SKILL.md"}, [2]string{".agents/skills/Audit/SKILL.md", ".agents/skills/audit/SKILL.md"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			required := materializationArtifact(tc.requiredName, "org", tc.requiredKind, config.Organization, targets)
			required.Required = true
			optional := materializationArtifact(tc.optionalName, "user", tc.optionalKind, config.UserPersonal, targets)
			for _, input := range [][]ResolvedArtifact{{required, optional}, {optional, required}} {
				out, displaced := MaterializationProjection(input)
				byKind := map[ir.ResourceKind][]hcc.Target{}
				for _, a := range out {
					byKind[a.Resource.Kind()] = a.Harnesses
				}
				if !reflect.DeepEqual(byKind[tc.requiredKind], targets) || !reflect.DeepEqual(byKind[tc.optionalKind], []hcc.Target{hcc.TargetClaude}) {
					t.Fatalf("projection = %v", byKind)
				}
				paths := map[hcc.Target][2]string{}
				for _, d := range displaced {
					if d.Winner.Resource.Kind() != tc.requiredKind || d.Displaced.Resource.Kind() != tc.optionalKind || input[d.DisplacedIndex].Resource.Kind() != tc.optionalKind {
						t.Fatalf("displacement = %+v", d)
					}
					winnerPath, displacedPath := d.Paths()
					paths[d.Target] = [2]string{winnerPath, displacedPath}
				}
				if want := map[hcc.Target][2]string{hcc.TargetDevin: tc.wantDevin, hcc.TargetCodex: tc.wantCodex}; !reflect.DeepEqual(paths, want) {
					t.Fatalf("displaced paths = %v, want %v", paths, want)
				}
			}
		})
	}
}

func TestMaterializationProjectionLeavesSameKindCaseVariantsToRefusal(t *testing.T) {
	targets := []hcc.Target{hcc.TargetDevin, hcc.TargetCodex}
	upper := materializationArtifact("Deploy", "upper", ir.KindCommand, config.UserPersonal, targets)
	lower := materializationArtifact("deploy", "lower", ir.KindCommand, config.UserPersonal, targets)
	out, displaced := MaterializationProjection([]ResolvedArtifact{upper, lower})
	if len(out) != 2 || len(displaced) != 0 {
		t.Fatalf("same-kind case variants must both reach the case-only refusal: out=%d displaced=%v", len(out), displaced)
	}
}

func TestMaterializationProjectionKeysSharedPathsByInstalledName(t *testing.T) {
	targets := []hcc.Target{hcc.TargetDevin}
	required := materializationArtifact("audit", "org", ir.KindSkill, config.Organization, targets)
	required.Required = true
	aliased := materializationArtifact("published-name", "user", ir.KindCommand, config.UserPersonal, targets)
	aliased.InstalledName = "audit"
	out, displaced := MaterializationProjection([]ResolvedArtifact{aliased, required})
	if len(out) != 1 || out[0].Resource.Kind() != ir.KindSkill || len(displaced) != 1 {
		t.Fatalf("installed name must decide the shared path: out=%v displaced=%v", out, displaced)
	}
	if _, path := displaced[0].Paths(); path != "skills/audit/SKILL.md" {
		t.Fatalf("displaced path = %q", path)
	}
}

func TestMaterializationProjectionRequiredKeepsSameKindCaseVariant(t *testing.T) {
	targets := []hcc.Target{hcc.TargetClaude}
	organization := func(name string, required bool) ResolvedArtifact {
		a := materializationArtifact(name, "org-"+name, ir.KindCommand, config.Organization, targets)
		a.Required = required
		return a
	}
	user := func(name string, required bool) ResolvedArtifact {
		a := materializationArtifact(name, "user-"+name, ir.KindCommand, config.UserPersonal, targets)
		a.Required = required
		return a
	}
	for _, tc := range []struct {
		name          string
		input         []ResolvedArtifact
		winner, loser string
	}{
		{"required beats an optional variant", []ResolvedArtifact{organization("deploy", true), user("Deploy", false)}, "deploy", "Deploy"},
		{"optional variant listed first", []ResolvedArtifact{user("Deploy", false), organization("deploy", true)}, "deploy", "Deploy"},
		{"required user command beats an optional organization variant", []ResolvedArtifact{organization("deploy", false), user("Deploy", true)}, "Deploy", "deploy"},
		// Between requirements the layer precedence of name collisions decides,
		// as it does for equal names: the user layer outranks the organization.
		{"layer precedence decides between requirements", []ResolvedArtifact{organization("deploy", true), user("Deploy", true)}, "Deploy", "deploy"},
		{"equal requirements are left to the refusal", []ResolvedArtifact{organization("deploy", true), organization("Deploy", true)}, "", ""},
		{"no requirement is left to the refusal", []ResolvedArtifact{organization("deploy", false), user("Deploy", false)}, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, displaced := MaterializationProjection(tc.input)
			if tc.winner == "" {
				if len(out) != 2 || len(displaced) != 0 {
					t.Fatalf("both must reach the case-only refusal: out=%d displaced=%v", len(out), displaced)
				}
				return
			}
			if len(out) != 1 || out[0].Name != tc.winner || len(displaced) != 1 || displaced[0].Displaced.Name != tc.loser || displaced[0].Winner.Name != tc.winner {
				t.Fatalf("out=%v displaced=%v, want %s over %s", out, displaced, tc.winner, tc.loser)
			}
			winnerPath, loserPath := displaced[0].Paths()
			if winnerPath != "commands/"+tc.winner+".md" || loserPath != "commands/"+tc.loser+".md" {
				t.Fatalf("paths = %q, %q", winnerPath, loserPath)
			}
		})
	}
}
