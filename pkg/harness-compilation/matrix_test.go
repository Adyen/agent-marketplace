package harnesscompile

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/testutil"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

const (
	matrixClassIdentity = "identity"
	matrixClassMarquee  = "marquee"
	matrixClassCross    = "cross"
	matrixClassDrop     = "drop"
	matrixPluginRoot    = "/plugins/demo"
)

var matrixHarnesses = targetmeta.ConversionTargets()

type matrixSpec struct {
	Cells []matrixCell `yaml:"cells"`
}

type matrixCell struct {
	From          Harness  `yaml:"from"`
	To            Harness  `yaml:"to"`
	Class         string   `yaml:"class"`
	Diagnostics   []string `yaml:"diagnostics"`
	AllowedLosses []string `yaml:"allowedLosses"`
}

type canonicalFixture struct {
	Kind            string            `json:"kind"`
	Name            string            `json:"name"`
	Description     string            `json:"description,omitempty"`
	Body            string            `json:"body,omitempty"`
	Triggers        []string          `json:"triggers,omitempty"`
	SupportFiles    []string          `json:"supportFiles,omitempty"`
	AllowedTools    []string          `json:"allowedTools,omitempty"`
	Permissions     map[string]any    `json:"permissions,omitempty"`
	PermissionsSet  bool              `json:"permissionsSet,omitempty"`
	Model           string            `json:"model,omitempty"`
	Tools           []string          `json:"tools,omitempty"`
	Effort          string            `json:"effort,omitempty"`
	DisallowedTools []string          `json:"disallowedTools,omitempty"`
	Isolation       string            `json:"isolation,omitempty"`
	MaxNesting      *int              `json:"maxNesting,omitempty"`
	Event           string            `json:"event,omitempty"`
	Matcher         string            `json:"matcher,omitempty"`
	Command         string            `json:"command,omitempty"`
	Type            string            `json:"type,omitempty"`
	Args            []string          `json:"args,omitempty"`
	Env             map[string]string `json:"env,omitempty"`
	URL             string            `json:"url,omitempty"`
	Transport       string            `json:"transport,omitempty"`
	Headers         map[string]string `json:"headers,omitempty"`
	Disabled        bool              `json:"disabled,omitempty"`
	DisabledSet     bool              `json:"disabledSet,omitempty"`
	OAuthClientID   string            `json:"oauthClientId,omitempty"`
	OAuthSecret     string            `json:"oauthClientSecret,omitempty"`
	OAuthResource   string            `json:"oauthResource,omitempty"`
	Timeout         *int              `json:"timeout,omitempty"`
}

func TestMatrix_Skill(t *testing.T) {
	runMatrixKind(t, "skill")
}

func TestMatrix_Command(t *testing.T) {
	runMatrixKind(t, "command")
}

func TestMatrix_Subagent(t *testing.T) {
	runMatrixKind(t, "subagent")
}

func TestMatrix_Hook(t *testing.T) {
	runMatrixKind(t, "hook")
}

func TestMatrix_MCPServer(t *testing.T) {
	runMatrixKind(t, "mcpserver")
}

func TestMatrix_RoundTripSameHarness(t *testing.T) {
	skipMatrixIfRequested(t)

	for _, kind := range []string{"skill", "command", "subagent", "hook", "mcpserver"} {
		for _, harness := range matrixHarnesses {
			t.Run(kind+"/"+string(harness), func(t *testing.T) {
				spec := loadMatrixSpec(t, kind)
				cell := spec.mustCell(t, harness, harness)
				if cell.Class == matrixClassDrop {
					t.Skipf("%s/%s is a documented drop cell", kind, harness)
				}

				src := matrixInputFS(t, kind, harness)
				bundle, err := IngestBundle(src, harness)
				if err != nil {
					t.Fatalf("IngestBundle(%s, %s): %v", kind, harness, err)
				}
				resource := requireSingleResource(t, kind, harness, bundle.Resources)

				result, err := Convert(src, harness, harness, WithPluginRoot(matrixPluginRoot))
				if err != nil {
					t.Fatalf("Convert(%s %s→%s): %v", kind, harness, harness, err)
				}

				outDir := t.TempDir()
				if _, err := materializeMatrixResult(result, outDir, harness); err != nil {
					t.Fatalf("Materialize(%s %s→%s): %v", kind, harness, harness, err)
				}

				reingested, err := IngestBundle(os.DirFS(outDir), harness)
				if err != nil {
					t.Fatalf("re-ingest %s %s→%s: %v", kind, harness, harness, err)
				}
				got := requireSingleResource(t, kind, harness, reingested.Resources)

				assertCanonicalEqual(t, normalizeResource(resource), normalizeResource(got), nil)
			})
		}
	}
}

func TestMatrix_CrossHarnessLossProfile(t *testing.T) {
	skipMatrixIfRequested(t)

	for _, kind := range []string{"skill", "command", "subagent", "hook", "mcpserver"} {
		spec := loadMatrixSpec(t, kind)
		want := loadCanonicalFixture(t, kind)
		for _, cell := range spec.Cells {
			cell := cell
			if cell.Class == matrixClassDrop {
				continue
			}
			t.Run(kind+"/"+string(cell.From)+"-to-"+string(cell.To), func(t *testing.T) {
				src := matrixInputFS(t, kind, cell.From)
				result, err := Convert(src, cell.From, cell.To, WithPluginRoot(matrixPluginRoot))
				if err != nil {
					t.Fatalf("Convert(%s %s→%s): %v", kind, cell.From, cell.To, err)
				}

				assertDiagnosticCodesMatch(t, result.Diagnostics, cell.Diagnostics)
				outDir := t.TempDir()
				if hasErrorDiagnostic(result.Diagnostics) {
					if len(result.Artifacts) != 0 {
						t.Fatalf("security/error diagnostics must suppress artifacts, got %d", len(result.Artifacts))
					}
					if _, err := Materialize(result, outDir, WithWriteMode(WriteModeOverwrite)); err == nil {
						t.Fatal("Materialize should reject results containing error diagnostics")
					}
					return
				}
				if _, err := materializeMatrixResult(result, outDir, cell.To); err != nil {
					t.Fatalf("Materialize(%s %s→%s): %v", kind, cell.From, cell.To, err)
				}
				reingested, err := IngestBundle(os.DirFS(outDir), cell.To)
				if err != nil {
					t.Fatalf("re-ingest %s %s→%s: %v", kind, cell.From, cell.To, err)
				}
				got := requireSingleResource(t, kind, cell.To, reingested.Resources)
				assertCanonicalEqual(t, want, normalizeResource(got), cell.AllowedLosses)
			})
		}
	}
}

func runMatrixKind(t *testing.T, kind string) {
	t.Helper()
	skipMatrixIfRequested(t)

	spec := loadMatrixSpec(t, kind)
	if len(spec.Cells) != len(matrixHarnesses)*len(matrixHarnesses) {
		t.Fatalf("matrix spec %s has %d cells, want %d", kind, len(spec.Cells), len(matrixHarnesses)*len(matrixHarnesses))
	}

	for _, cell := range spec.Cells {
		cell := cell
		t.Run(string(cell.From)+"-to-"+string(cell.To), func(t *testing.T) {
			src := matrixInputFS(t, kind, cell.From)
			bundle, err := IngestBundle(src, cell.From)
			if err != nil {
				t.Fatalf("IngestBundle(%s, %s): %v", kind, cell.From, err)
			}

			result, err := Convert(src, cell.From, cell.To, WithPluginRoot(matrixPluginRoot))
			if err != nil {
				t.Fatalf("Convert(%s %s→%s): %v", kind, cell.From, cell.To, err)
			}

			assertDiagnosticCodesMatch(t, result.Diagnostics, cell.Diagnostics)
			if hasErrorDiagnostic(result.Diagnostics) {
				if len(result.Artifacts) != 0 {
					t.Fatalf("security/error diagnostics must suppress artifacts, got %d", len(result.Artifacts))
				}
				return
			}

			switch cell.Class {
			case matrixClassIdentity, matrixClassMarquee:
				requireSingleResource(t, kind, cell.From, bundle.Resources)
				outDir := t.TempDir()
				if _, err := materializeMatrixResult(result, outDir, cell.To); err != nil {
					t.Fatalf("Materialize(%s %s→%s): %v", kind, cell.From, cell.To, err)
				}
				testutil.AssertGoldenDir(t, outDir, matrixExpectedDir(kind, cell.From, cell.To))
			case matrixClassCross:
				requireSingleResource(t, kind, cell.From, bundle.Resources)
				assertArtifactShapeMatches(t, result, matrixExpectedDir(kind, cell.From, cell.To), cell.To)
			case matrixClassDrop:
				if len(result.Artifacts) != 0 {
					t.Fatalf("drop cell %s %s→%s produced %d artifacts, want 0", kind, cell.From, cell.To, len(result.Artifacts))
				}
			default:
				t.Fatalf("unknown matrix class %q for %s %s→%s", cell.Class, kind, cell.From, cell.To)
			}
		})
	}
}

func skipMatrixIfRequested(t *testing.T) {
	t.Helper()
	if os.Getenv("SKIP_MATRIX") == "1" {
		t.Skip("SKIP_MATRIX=1")
	}
}

func loadMatrixSpec(t *testing.T, kind string) matrixSpec {
	t.Helper()
	path := filepath.Join("testdata", "matrix", kind, "matrix.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var spec matrixSpec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return spec
}

func (s matrixSpec) mustCell(t *testing.T, from, to Harness) matrixCell {
	t.Helper()
	for _, cell := range s.Cells {
		if cell.From == from && cell.To == to {
			return cell
		}
	}
	t.Fatalf("missing matrix cell %s→%s", from, to)
	return matrixCell{}
}

func loadCanonicalFixture(t *testing.T, kind string) canonicalFixture {
	t.Helper()
	path := filepath.Join("testdata", "matrix", kind, "canonical_ir.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var fixture canonicalFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return fixture
}

func matrixInputFS(t *testing.T, kind string, from Harness) fs.FS {
	t.Helper()
	root := filepath.Join("testdata", "matrix", kind, "inputs", string(from))
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("stat %s: %v", root, err)
	}
	return os.DirFS(root)
}

func matrixExpectedDir(kind string, from, to Harness) string {
	return filepath.Join("testdata", "matrix", kind, "expected", string(from)+"-to-"+string(to))
}

func requireSingleResource(t *testing.T, kind string, from Harness, resources []ir.Resource) ir.Resource {
	t.Helper()
	if len(resources) != 1 {
		t.Fatalf("%s/%s resources = %d, want 1", kind, from, len(resources))
	}
	return resources[0]
}

func hasErrorDiagnostic(diags []Diagnostic) bool {
	for _, diagnostic := range diags {
		if diagnostic.Severity == SeverityError {
			return true
		}
	}
	return false
}

func assertDiagnosticCodesMatch(t *testing.T, diags []Diagnostic, want []string) {
	t.Helper()
	got := make([]string, 0, len(diags))
	for _, diagnostic := range diags {
		// Rich-field fixtures may contain legacy same-name extras; these are
		// covered by dedicated precedence tests and do not change artifact shape.
		if diagnostic.Code == "compile.canonical-field-overrides-extra" {
			continue
		}
		got = append(got, diagnostic.Code)
	}
	slices.Sort(got)
	want = append([]string(nil), want...)
	slices.Sort(want)
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("diagnostic codes = %v, want %v", got, want)
	}
}

func assertArtifactShapeMatches(t *testing.T, result *Result, expectedDir string, target Harness) {
	t.Helper()
	outDir := t.TempDir()
	if _, err := materializeMatrixResult(result, outDir, target); err != nil {
		t.Fatalf("Materialize(shape): %v", err)
	}
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		testutil.AssertGoldenDir(t, outDir, expectedDir)
		return
	}
	got := walkFiles(t, outDir)
	want := walkFiles(t, expectedDir)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("artifact shape = %v, want %v", got, want)
	}
}

func materializeMatrixResult(result *Result, outDir string, target Harness) (*WriteReport, error) {
	// Matrix fixtures contain one resource and materialize into isolated empty
	// roots, so consumer-owned reconciliation and aggregate merging are absent.
	fixtureResult := *result
	fixtureResult.Artifacts = append([]Artifact(nil), result.Artifacts...)
	for i := range fixtureResult.Artifacts {
		artifact := &fixtureResult.Artifacts[i]
		policy, _ := artifact.Policy()
		if artifact.ExternalReconciliationRequired || policy.IsExternal() {
			artifact.ExternalReconciliationRequired = false
			artifact.Reconciliation = ReconciliationPolicy{}
			artifact.Fragment = false
			artifact.MergeWithExisting = false
		}
	}
	report, err := Materialize(&fixtureResult, outDir, WithWriteMode(WriteModeOverwrite))
	if err != nil {
		return nil, err
	}
	if target == HarnessCodex {
		// Codex skills and commands are shared under .agents, but Codex ingest
		// deliberately requires the ambient .codex workspace marker.
		if err := os.MkdirAll(filepath.Join(outDir, ".codex"), 0o755); err != nil {
			return nil, err
		}
	}
	return report, nil
}

func walkFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	slices.Sort(files)
	return files
}

func normalizeResource(resource ir.Resource) canonicalFixture {
	switch typed := resource.(type) {
	case *ir.Skill:
		return canonicalFixture{
			Kind:           "skill",
			Name:           typed.Name(),
			Description:    typed.Description,
			Body:           typed.Body,
			Triggers:       append([]string(nil), typed.Triggers...),
			SupportFiles:   append([]string(nil), typed.SupportFiles...),
			AllowedTools:   append([]string(nil), typed.AllowedTools...),
			Permissions:    permissionFixture(typed),
			PermissionsSet: typed.PermissionsSet,
		}
	case *ir.Command:
		return canonicalFixture{
			Kind:        "command",
			Name:        typed.Name(),
			Description: typed.Description,
			Body:        typed.Body,
		}
	case *ir.Subagent:
		return canonicalFixture{
			Kind:            "subagent",
			Name:            typed.Name(),
			Description:     typed.Description,
			Body:            typed.Body,
			Model:           typed.Model,
			Tools:           append([]string(nil), typed.Tools...),
			Effort:          typed.Effort,
			DisallowedTools: append([]string(nil), typed.DisallowedTools...),
			Isolation:       typed.Isolation,
			MaxNesting:      typed.MaxNesting,
		}
	case *ir.Hook:
		return canonicalFixture{
			Kind:    "hook",
			Name:    typed.Name(),
			Event:   typed.Event,
			Matcher: typed.Matcher,
			Command: typed.Command,
			Type:    typed.Type,
			Timeout: typed.Timeout,
		}
	case *ir.McpServer:
		env := make(map[string]string, len(typed.Env))
		for key, value := range typed.Env {
			env[key] = value
		}
		return canonicalFixture{
			Kind:          "mcpserver",
			Name:          typed.Name(),
			Command:       typed.Command,
			Args:          append([]string(nil), typed.Args...),
			Env:           env,
			URL:           typed.URL,
			Transport:     typed.Transport,
			Headers:       typed.Headers,
			Disabled:      typed.Disabled,
			DisabledSet:   typed.DisabledSet,
			OAuthClientID: typed.OAuthClientID,
			OAuthSecret:   typed.OAuthClientSecret,
			OAuthResource: typed.OAuthResource,
		}
	default:
		panic(fmt.Sprintf("unsupported resource type %T", resource))
	}
}

func permissionFixture(skill *ir.Skill) map[string]any {
	if !skill.PermissionsSet {
		return nil
	}
	value := map[string]any{}
	if skill.Permissions.Allow != nil {
		value["allow"] = skill.Permissions.Allow
	}
	if skill.Permissions.Ask != nil {
		value["ask"] = skill.Permissions.Ask
	}
	if skill.Permissions.Deny != nil {
		value["deny"] = skill.Permissions.Deny
	}
	for key, nested := range skill.Permissions.Extra {
		value[key] = nested
	}
	return value
}

func assertCanonicalEqual(t *testing.T, want, got canonicalFixture, allowedLosses []string) {
	t.Helper()
	wantMap := canonicalMap(t, want)
	gotMap := canonicalMap(t, got)
	for _, field := range allowedLosses {
		delete(wantMap, field)
		delete(gotMap, field)
	}
	if !reflect.DeepEqual(gotMap, wantMap) {
		gotJSON, _ := json.MarshalIndent(gotMap, "", "  ")
		wantJSON, _ := json.MarshalIndent(wantMap, "", "  ")
		t.Fatalf("canonical IR mismatch\n--- got ---\n%s\n--- want ---\n%s", strings.TrimSpace(string(gotJSON)), strings.TrimSpace(string(wantJSON)))
	}
}

func canonicalMap(t *testing.T, fixture canonicalFixture) map[string]any {
	t.Helper()
	data, err := json.Marshal(fixture)
	if err != nil {
		t.Fatalf("marshal canonical fixture: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal canonical fixture: %v", err)
	}
	return decoded
}
