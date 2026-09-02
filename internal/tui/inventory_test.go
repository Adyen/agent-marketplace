package tui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
	"github.com/charmbracelet/bubbles/key"
)

var qaKeyMapFieldAllowlist = map[string]string{}

func TestQAScreenInventoryCoversEveryPaneExactlyOnce(t *testing.T) {
	paneNames := declaredPaneNames(t)
	registrations := qaScreenRegistrations()
	if len(registrations) != len(paneNames) {
		t.Fatalf("screen registrations = %d, declared panes = %d (%v)", len(registrations), len(paneNames), paneNames)
	}

	byPane := make(map[pane]ScreenID, len(registrations))
	byID := make(map[ScreenID]pane, len(registrations))
	for _, registration := range registrations {
		if previous, ok := byPane[registration.pane]; ok {
			t.Fatalf("pane %d is registered as both %q and %q", registration.pane, previous, registration.id)
		}
		if previous, ok := byID[registration.id]; ok {
			t.Fatalf("screen ID %q is registered for both pane %d and pane %d", registration.id, previous, registration.pane)
		}
		byPane[registration.pane] = registration.id
		byID[registration.id] = registration.pane
	}
	for value, name := range paneNames {
		if _, ok := byPane[pane(value)]; !ok {
			t.Errorf("pane %s (value %d) has no screen registration", name, value)
		}
	}
	wantByPane := map[pane]ScreenID{
		paneList:            "catalog",
		paneDetail:          "detail",
		paneHelp:            "help",
		paneErrors:          "diagnostics",
		paneConfirm:         "quit-confirmation",
		paneToggleModal:     "resource-toggle",
		paneInitModal:       "first-run",
		paneMigrationModal:  "migration",
		paneHarnessModal:    "harness-selection",
		paneEditConfigModal: "edit-config",
	}
	if !reflect.DeepEqual(byPane, wantByPane) {
		t.Errorf("pane screen registrations = %v, want %v", byPane, wantByPane)
	}
	wantScreens := make([]ScreenID, 0, len(registrations)+1)
	for _, registration := range registrations {
		wantScreens = append(wantScreens, registration.id)
	}
	wantScreens = append(wantScreens, ScreenResizeRequired)
	if got := QAInventory().Screens; !reflect.DeepEqual(got, wantScreens) {
		t.Errorf("QA screens = %v, want pane screens plus resize-required %v", got, wantScreens)
	}
}

func TestQATopLevelRoutersHandleEveryNonDefaultPane(t *testing.T) {
	want := make(map[string]bool)
	for _, name := range declaredPaneNames(t) {
		if name != "paneList" {
			want[name] = true
		}
	}
	for _, method := range []string{"View", "handleKey"} {
		got := paneCasesInMethod(t, method)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s pane cases = %v, want %v; paneList is the default route", method, sortedKeys(got), sortedKeys(want))
		}
	}
}

func TestQAActionInventoryCoversEveryKeyMapFieldExactlyOnce(t *testing.T) {
	keys := defaultKeyMap
	value := reflect.ValueOf(&keys).Elem()
	typ := value.Type()
	bindingType := reflect.TypeOf(key.Binding{})
	registrations := qaActionRegistrations(&keys)
	registeredPointers := make(map[uintptr]ActionID, len(registrations))
	registeredIDs := make(map[ActionID]bool, len(registrations))

	for _, registration := range registrations {
		if registration.binding == nil {
			t.Fatalf("action %q has a nil key binding", registration.id)
		}
		pointer := reflect.ValueOf(registration.binding).Pointer()
		if previous, ok := registeredPointers[pointer]; ok {
			t.Fatalf("key binding is registered as both %q and %q", previous, registration.id)
		}
		if registeredIDs[registration.id] {
			t.Fatalf("action ID %q is registered more than once", registration.id)
		}
		registeredPointers[pointer] = registration.id
		registeredIDs[registration.id] = true
	}

	for index := 0; index < typ.NumField(); index++ {
		field := typ.Field(index)
		reason := strings.TrimSpace(qaKeyMapFieldAllowlist[field.Name])
		if field.Type != bindingType {
			if reason == "" {
				t.Errorf("keyMap.%s has type %s and no reasoned QA inventory allowlist entry", field.Name, field.Type)
			}
			continue
		}
		pointer := value.Field(index).Addr().Pointer()
		_, registered := registeredPointers[pointer]
		if !registered && reason == "" {
			t.Errorf("keyMap.%s has neither an ActionID registration nor a reasoned allowlist entry", field.Name)
		}
		if registered && reason != "" {
			t.Errorf("keyMap.%s is both registered and allowlisted", field.Name)
		}
	}
	for field, reason := range qaKeyMapFieldAllowlist {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("keyMap.%s has an empty QA inventory allowlist reason", field)
		}
		if _, ok := typ.FieldByName(field); !ok {
			t.Errorf("QA inventory allowlist references unknown keyMap.%s", field)
		}
	}
	if len(registrations)+len(qaKeyMapFieldAllowlist) != typ.NumField() {
		t.Errorf("action registrations + allowlist = %d, keyMap fields = %d", len(registrations)+len(qaKeyMapFieldAllowlist), typ.NumField())
	}
	inventory := QAInventory().Actions
	if len(inventory) != len(registrations) {
		t.Fatalf("inventory actions = %d, registrations = %d", len(inventory), len(registrations))
	}
	for index, registration := range registrations {
		help := registration.binding.Help()
		want := QAAction{
			ID:              registration.id,
			Prefix:          registration.prefix,
			Keys:            registration.binding.Keys(),
			HelpKey:         help.Key,
			HelpDescription: help.Desc,
		}
		if !reflect.DeepEqual(inventory[index], want) {
			t.Errorf("inventory action %d = %+v, want keymap-derived %+v", index, inventory[index], want)
		}
	}
}

func TestQAFoldActionsRetainTheirPrefix(t *testing.T) {
	want := map[ActionID]ActionID{
		ActionFoldToggle: ActionFoldPrefix,
		ActionFoldAll:    ActionFoldPrefix,
		ActionExpandAll:  ActionFoldPrefix,
	}
	for _, action := range QAInventory().Actions {
		if prefix, ok := want[action.ID]; ok {
			if action.Prefix != prefix {
				t.Errorf("action %q prefix = %q, want %q", action.ID, action.Prefix, prefix)
			}
			delete(want, action.ID)
		}
	}
	if len(want) != 0 {
		t.Errorf("prefixed actions absent from inventory: %v", want)
	}
}

func TestQAStateInventoryCoversEveryTabAndUsesFiniteIDs(t *testing.T) {
	if len(qaTransientStateIDs) == 0 {
		t.Fatal("transient state inventory is empty")
	}
	if len(qaTabStateIDs) != len(allTabs) {
		t.Fatalf("tab state registrations = %d, tabs = %d", len(qaTabStateIDs), len(allTabs))
	}
	seenTabs := make(map[tab]bool, len(allTabs))
	for _, current := range allTabs {
		id, ok := qaTabStateIDs[current]
		if !ok {
			t.Errorf("tab %v has no StateID registration", current)
		}
		if seenTabs[current] {
			t.Errorf("tab %v appears more than once in allTabs", current)
		}
		seenTabs[current] = true
		if id == "" {
			t.Errorf("tab %v has an empty StateID", current)
		}
	}
	want := make([]StateID, 0, len(allTabs)+len(qaTransientStateIDs))
	for _, current := range allTabs {
		want = append(want, qaTabStateIDs[current])
	}
	want = append(want, qaTransientStateIDs...)
	for current := range qaTabStateIDs {
		if !seenTabs[current] {
			t.Errorf("StateID registered for tab %v that is absent from allTabs", current)
		}
	}
	if got := QAInventory().States; !reflect.DeepEqual(got, want) {
		t.Errorf("QA states = %v, want tab states followed by finite transient states %v", got, want)
	}
}

func TestQAInventoryIDsAndBindingsAreStable(t *testing.T) {
	inventory := QAInventory()
	if QAInventorySchemaVersion != 1 {
		t.Fatalf("QA inventory schema version constant = %d, want 1", QAInventorySchemaVersion)
	}
	if inventory.SchemaVersion != QAInventorySchemaVersion {
		t.Fatalf("QA inventory schema version = %d, want %d", inventory.SchemaVersion, QAInventorySchemaVersion)
	}
	idPattern := regexp.MustCompile(`^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)*$`)
	seen := make(map[string]string)
	assertID := func(kind, id string) {
		t.Helper()
		if !idPattern.MatchString(id) {
			t.Errorf("%s ID %q does not use the stable lowercase dotted/kebab convention", kind, id)
		}
		key := kind + "\x00" + id
		if previous, ok := seen[key]; ok {
			t.Errorf("duplicate %s ID %q (first seen as %s)", kind, id, previous)
		}
		seen[key] = kind
	}
	for _, id := range inventory.Screens {
		assertID("screen", string(id))
	}
	actionIDs := make(map[ActionID]bool, len(inventory.Actions))
	for _, action := range inventory.Actions {
		assertID("action", string(action.ID))
		actionIDs[action.ID] = true
		if len(action.Keys) == 0 || action.HelpKey == "" || action.HelpDescription == "" {
			t.Errorf("action %q did not derive complete keys/help from defaultKeyMap: %+v", action.ID, action)
		}
	}
	for _, action := range inventory.Actions {
		if action.Prefix != "" && (!actionIDs[action.Prefix] || action.Prefix == action.ID) {
			t.Errorf("action %q has invalid prefix %q", action.ID, action.Prefix)
		}
	}
	for _, id := range inventory.States {
		assertID("state", string(id))
	}
	for _, target := range inventory.Targets {
		assertID("target", target.ID)
		if target.Label == "" {
			t.Errorf("target %q has an empty label", target.ID)
		}
	}
}

func TestQAInventoryHasStableOrderedIDs(t *testing.T) {
	inventory := QAInventory()
	tests := []struct {
		name string
		got  []string
		want []string
	}{
		{
			name: "screens",
			got:  screenIDStrings(inventory.Screens),
			want: []string{
				"catalog", "detail", "help", "diagnostics", "quit-confirmation",
				"resource-toggle", "first-run", "migration", "harness-selection",
				"edit-config", "resize-required",
			},
		},
		{
			name: "actions",
			got:  actionIDStrings(inventory.Actions),
			want: []string{
				"up", "down", "top", "bottom", "next-tab", "prev-tab",
				"tab-scope", "tab-registry", "tab-status", "search", "clear",
				"enter", "space", "collapse", "expand", "reload", "sync",
				"update", "harness", "edit-config", "fold-prefix", "fold-toggle",
				"fold-all", "expand-all", "help", "errors", "quit", "toggle-global",
				"toggle-project", "toggle-local", "toggle-exclude", "toggle-cancel",
				"harness-global", "harness-project", "harness-save", "harness-cancel",
				"migration-run", "migration-leave", "edit-config-global",
				"edit-config-project", "edit-config-local", "edit-config-cancel",
				"detail-back", "search-confirm", "search-backspace", "confirm-yes",
				"confirm-no", "confirm-cancel",
			},
		},
		{
			name: "states",
			got:  stateIDStrings(inventory.States),
			want: []string{
				"tab.scope", "tab.registry", "tab.status", "detail.inline",
				"search.active", "search.no-match", "detail.loading", "editor.suspended",
				"loading.initial", "loading.warm-cache-refresh", "operation.active",
				"toast.visible", "diagnostics.errors", "diagnostics.warnings",
				"catalog.empty", "startup.degraded", "terminal.resize-required",
				"drift.unavailable", "drift.in-sync", "drift.out-of-sync",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !reflect.DeepEqual(test.got, test.want) {
				t.Fatalf("ordered IDs = %v, want %v", test.got, test.want)
			}
		})
	}
}

func TestEveryDeclaredQAIDConstantIsInventoried(t *testing.T) {
	inventory := QAInventory()
	tests := []struct {
		typeName string
		ids      []string
	}{
		{typeName: "ScreenID", ids: screenIDStrings(inventory.Screens)},
		{typeName: "ActionID", ids: actionIDStrings(inventory.Actions)},
		{typeName: "StateID", ids: stateIDStrings(inventory.States)},
	}
	for _, test := range tests {
		t.Run(test.typeName, func(t *testing.T) {
			declared := declaredTypedStringConstants(t, test.typeName)
			inventoried := make(map[string]bool, len(test.ids))
			for _, id := range test.ids {
				inventoried[id] = true
			}
			if len(declared) != len(test.ids) {
				t.Errorf("declared %s constants = %d, inventoried IDs = %d", test.typeName, len(declared), len(test.ids))
			}
			for name, id := range declared {
				if !inventoried[id] {
					t.Errorf("%s %s = %q is absent from QAInventory", test.typeName, name, id)
				}
			}
			for _, id := range test.ids {
				found := false
				for _, declaredID := range declared {
					found = found || declaredID == id
				}
				if !found {
					t.Errorf("inventoried %s ID %q has no typed constant", test.typeName, id)
				}
			}
		})
	}
}

func TestQAInventoryDerivesUITargetIdentityAndOrderFromMetadata(t *testing.T) {
	definitions := targetmeta.Definitions()
	sort.SliceStable(definitions, func(i, j int) bool {
		return definitions[i].UIOrder < definitions[j].UIOrder
	})
	var want []QATarget
	for _, definition := range definitions {
		if definition.UI {
			want = append(want, QATarget{ID: string(definition.Target), Label: definition.Label})
		}
	}
	if got := QAInventory().Targets; !reflect.DeepEqual(got, want) {
		t.Fatalf("QA targets = %+v, want target metadata order %+v", got, want)
	}
}

func TestQAInventoryReturnsIndependentCopies(t *testing.T) {
	first := QAInventory()
	first.Screens[0] = "changed"
	first.Actions[0].Keys[0] = "changed"
	first.States[0] = "changed"
	first.Targets[0].ID = "changed"

	second := QAInventory()
	if second.Screens[0] == "changed" || second.Actions[0].Keys[0] == "changed" || second.States[0] == "changed" || second.Targets[0].ID == "changed" {
		t.Fatal("QAInventory returned storage shared with a previous snapshot")
	}
}

func screenIDStrings(ids []ScreenID) []string {
	values := make([]string, len(ids))
	for index, id := range ids {
		values[index] = string(id)
	}
	return values
}

func actionIDStrings(actions []QAAction) []string {
	values := make([]string, len(actions))
	for index, action := range actions {
		values[index] = string(action.ID)
	}
	return values
}

func stateIDStrings(ids []StateID) []string {
	values := make([]string, len(ids))
	for index, id := range ids {
		values[index] = string(id)
	}
	return values
}

func declaredTypedStringConstants(t *testing.T, typeName string) map[string]string {
	t.Helper()
	declared := make(map[string]string)
	for _, file := range productionTUIFiles(t) {
		for _, spec := range constantSpecsOfType(file, typeName) {
			if len(spec.Names) != 1 || len(spec.Values) != 1 {
				t.Fatalf("%s constants must use one explicit string literal per declaration", typeName)
			}
			literal, ok := spec.Values[0].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				t.Fatalf("%s %s must use an explicit string literal", typeName, spec.Names[0].Name)
			}
			value, err := strconv.Unquote(literal.Value)
			if err != nil {
				t.Fatalf("decode %s %s: %v", typeName, spec.Names[0].Name, err)
			}
			declared[spec.Names[0].Name] = value
		}
	}
	if len(declared) == 0 {
		t.Fatalf("no %s constants found", typeName)
	}
	return declared
}

func declaredPaneNames(t *testing.T) []string {
	t.Helper()
	var names []string
	for _, file := range productionTUIFiles(t) {
		for _, spec := range constantSpecsOfType(file, "pane") {
			for _, name := range spec.Names {
				names = append(names, name.Name)
			}
		}
	}
	if len(names) == 0 {
		t.Fatal("pane constant declaration not found")
	}
	return names
}

func constantSpecsOfType(file *ast.File, typeName string) []*ast.ValueSpec {
	var specs []*ast.ValueSpec
	for _, declaration := range file.Decls {
		group, ok := declaration.(*ast.GenDecl)
		if !ok || group.Tok != token.CONST {
			continue
		}
		currentType := ""
		for _, item := range group.Specs {
			spec := item.(*ast.ValueSpec)
			if spec.Type != nil {
				identifier, ok := spec.Type.(*ast.Ident)
				if !ok {
					currentType = ""
				} else {
					currentType = identifier.Name
				}
			} else if len(spec.Values) > 0 {
				currentType = ""
			}
			if currentType == typeName {
				specs = append(specs, spec)
			}
		}
	}
	return specs
}

func productionTUIFiles(t *testing.T) []*ast.File {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate TUI inventory test source")
	}
	directory := filepath.Dir(source)
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read TUI package: %v", err)
	}
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() && filepath.Ext(name) == ".go" && !strings.HasSuffix(name, "_test.go") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	files := make([]*ast.File, 0, len(names))
	for _, name := range names {
		files = append(files, parseInventoryFile(t, filepath.Join(directory, name)))
	}
	return files
}

func paneCasesInMethod(t *testing.T, method string) map[string]bool {
	t.Helper()
	cases := make(map[string]bool)
	found := false
	for _, file := range productionTUIFiles(t) {
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Name.Name != method || function.Recv == nil {
				continue
			}
			found = true
			ast.Inspect(function.Body, func(node ast.Node) bool {
				statement, ok := node.(*ast.SwitchStmt)
				if !ok || !isPaneSelector(statement.Tag) {
					return true
				}
				for _, item := range statement.Body.List {
					clause := item.(*ast.CaseClause)
					for _, expression := range clause.List {
						if identifier, ok := expression.(*ast.Ident); ok {
							cases[identifier.Name] = true
						}
					}
				}
				return true
			})
		}
	}
	if !found {
		t.Fatalf("method %s not found", method)
	}
	return cases
}

func isPaneSelector(expression ast.Expr) bool {
	selector, ok := expression.(*ast.SelectorExpr)
	return ok && selector.Sel.Name == "pane"
}

func parseInventoryFile(t *testing.T, name string) *ast.File {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return file
}

func sortedKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for value := range values {
		keys = append(keys, value)
	}
	sort.Strings(keys)
	return keys
}
