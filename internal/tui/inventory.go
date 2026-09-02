package tui

import "github.com/charmbracelet/bubbles/key"

type ScreenID string

type ActionID string

type StateID string

const QAInventorySchemaVersion = 1

const (
	ScreenCatalog          ScreenID = "catalog"
	ScreenDetail           ScreenID = "detail"
	ScreenHelp             ScreenID = "help"
	ScreenDiagnostics      ScreenID = "diagnostics"
	ScreenQuitConfirmation ScreenID = "quit-confirmation"
	ScreenResourceToggle   ScreenID = "resource-toggle"
	ScreenFirstRun         ScreenID = "first-run"
	ScreenMigration        ScreenID = "migration"
	ScreenHarnessSelection ScreenID = "harness-selection"
	ScreenEditConfig       ScreenID = "edit-config"
	ScreenResizeRequired   ScreenID = "resize-required"
)

const (
	ActionUp                ActionID = "up"
	ActionDown              ActionID = "down"
	ActionTop               ActionID = "top"
	ActionBottom            ActionID = "bottom"
	ActionNextTab           ActionID = "next-tab"
	ActionPrevTab           ActionID = "prev-tab"
	ActionTabScope          ActionID = "tab-scope"
	ActionTabRegistry       ActionID = "tab-registry"
	ActionTabStatus         ActionID = "tab-status"
	ActionSearch            ActionID = "search"
	ActionClear             ActionID = "clear"
	ActionEnter             ActionID = "enter"
	ActionSpace             ActionID = "space"
	ActionCollapse          ActionID = "collapse"
	ActionExpand            ActionID = "expand"
	ActionReload            ActionID = "reload"
	ActionSync              ActionID = "sync"
	ActionUpdate            ActionID = "update"
	ActionHarness           ActionID = "harness"
	ActionEditConfig        ActionID = "edit-config"
	ActionFoldPrefix        ActionID = "fold-prefix"
	ActionFoldToggle        ActionID = "fold-toggle"
	ActionFoldAll           ActionID = "fold-all"
	ActionExpandAll         ActionID = "expand-all"
	ActionHelp              ActionID = "help"
	ActionErrors            ActionID = "errors"
	ActionQuit              ActionID = "quit"
	ActionToggleGlobal      ActionID = "toggle-global"
	ActionToggleProject     ActionID = "toggle-project"
	ActionToggleLocal       ActionID = "toggle-local"
	ActionToggleExclude     ActionID = "toggle-exclude"
	ActionToggleCancel      ActionID = "toggle-cancel"
	ActionHarnessGlobal     ActionID = "harness-global"
	ActionHarnessProject    ActionID = "harness-project"
	ActionHarnessSave       ActionID = "harness-save"
	ActionHarnessCancel     ActionID = "harness-cancel"
	ActionMigrationRun      ActionID = "migration-run"
	ActionMigrationLeave    ActionID = "migration-leave"
	ActionEditConfigGlobal  ActionID = "edit-config-global"
	ActionEditConfigProject ActionID = "edit-config-project"
	ActionEditConfigLocal   ActionID = "edit-config-local"
	ActionEditConfigCancel  ActionID = "edit-config-cancel"
	ActionDetailBack        ActionID = "detail-back"
	ActionSearchConfirm     ActionID = "search-confirm"
	ActionSearchBackspace   ActionID = "search-backspace"
	ActionConfirmYes        ActionID = "confirm-yes"
	ActionConfirmNo         ActionID = "confirm-no"
	ActionConfirmCancel     ActionID = "confirm-cancel"
)

const (
	StateTabScope             StateID = "tab.scope"
	StateTabRegistry          StateID = "tab.registry"
	StateTabStatus            StateID = "tab.status"
	StateInlineDetail         StateID = "detail.inline"
	StateSearchActive         StateID = "search.active"
	StateSearchNoMatch        StateID = "search.no-match"
	StateDetailLoading        StateID = "detail.loading"
	StateEditorSuspended      StateID = "editor.suspended"
	StateInitialLoading       StateID = "loading.initial"
	StateWarmCacheRefresh     StateID = "loading.warm-cache-refresh"
	StateOperationActive      StateID = "operation.active"
	StateToastVisible         StateID = "toast.visible"
	StateErrorCount           StateID = "diagnostics.errors"
	StateWarningCount         StateID = "diagnostics.warnings"
	StateEmptyCatalog         StateID = "catalog.empty"
	StateDegradedStartup      StateID = "startup.degraded"
	StateTerminalResizeNeeded StateID = "terminal.resize-required"
	StateDriftUnavailable     StateID = "drift.unavailable"
	StateDriftInSync          StateID = "drift.in-sync"
	StateDriftOutOfSync       StateID = "drift.out-of-sync"
)

type QAAction struct {
	ID              ActionID `json:"id"`
	Prefix          ActionID `json:"prefix,omitempty"`
	Keys            []string `json:"keys"`
	HelpKey         string   `json:"helpKey"`
	HelpDescription string   `json:"helpDescription"`
}

type QATarget struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type QAInventorySnapshot struct {
	SchemaVersion int        `json:"schemaVersion"`
	Screens       []ScreenID `json:"screens"`
	Actions       []QAAction `json:"actions"`
	States        []StateID  `json:"states"`
	Targets       []QATarget `json:"targets"`
}

type qaScreenRegistration struct {
	pane pane
	id   ScreenID
}

type qaActionRegistration struct {
	id      ActionID
	prefix  ActionID
	binding *key.Binding
}

var qaTabStateIDs = map[tab]StateID{
	tabScope:    StateTabScope,
	tabRegistry: StateTabRegistry,
	tabStatus:   StateTabStatus,
}

var qaTransientStateIDs = []StateID{
	StateInlineDetail,
	StateSearchActive,
	StateSearchNoMatch,
	StateDetailLoading,
	StateEditorSuspended,
	StateInitialLoading,
	StateWarmCacheRefresh,
	StateOperationActive,
	StateToastVisible,
	StateErrorCount,
	StateWarningCount,
	StateEmptyCatalog,
	StateDegradedStartup,
	StateTerminalResizeNeeded,
	StateDriftUnavailable,
	StateDriftInSync,
	StateDriftOutOfSync,
}

func qaScreenRegistrations() []qaScreenRegistration {
	return []qaScreenRegistration{
		{pane: paneList, id: ScreenCatalog},
		{pane: paneDetail, id: ScreenDetail},
		{pane: paneHelp, id: ScreenHelp},
		{pane: paneErrors, id: ScreenDiagnostics},
		{pane: paneConfirm, id: ScreenQuitConfirmation},
		{pane: paneToggleModal, id: ScreenResourceToggle},
		{pane: paneInitModal, id: ScreenFirstRun},
		{pane: paneMigrationModal, id: ScreenMigration},
		{pane: paneHarnessModal, id: ScreenHarnessSelection},
		{pane: paneEditConfigModal, id: ScreenEditConfig},
	}
}

func qaActionRegistrations(keys *keyMap) []qaActionRegistration {
	return []qaActionRegistration{
		{id: ActionUp, binding: &keys.Up},
		{id: ActionDown, binding: &keys.Down},
		{id: ActionTop, binding: &keys.Top},
		{id: ActionBottom, binding: &keys.Bottom},
		{id: ActionNextTab, binding: &keys.NextTab},
		{id: ActionPrevTab, binding: &keys.PrevTab},
		{id: ActionTabScope, binding: &keys.Tab1},
		{id: ActionTabRegistry, binding: &keys.Tab2},
		{id: ActionTabStatus, binding: &keys.Tab3},
		{id: ActionSearch, binding: &keys.Search},
		{id: ActionClear, binding: &keys.Clear},
		{id: ActionEnter, binding: &keys.Enter},
		{id: ActionSpace, binding: &keys.Space},
		{id: ActionCollapse, binding: &keys.Collapse},
		{id: ActionExpand, binding: &keys.Expand},
		{id: ActionReload, binding: &keys.Reload},
		{id: ActionSync, binding: &keys.Sync},
		{id: ActionUpdate, binding: &keys.Update},
		{id: ActionHarness, binding: &keys.Harness},
		{id: ActionEditConfig, binding: &keys.EditConfig},
		{id: ActionFoldPrefix, binding: &keys.FoldPrefix},
		{id: ActionFoldToggle, prefix: ActionFoldPrefix, binding: &keys.FoldToggle},
		{id: ActionFoldAll, prefix: ActionFoldPrefix, binding: &keys.FoldAll},
		{id: ActionExpandAll, prefix: ActionFoldPrefix, binding: &keys.ExpandAll},
		{id: ActionHelp, binding: &keys.Help},
		{id: ActionErrors, binding: &keys.Errors},
		{id: ActionQuit, binding: &keys.Quit},
		{id: ActionToggleGlobal, binding: &keys.ToggleGlobal},
		{id: ActionToggleProject, binding: &keys.ToggleProject},
		{id: ActionToggleLocal, binding: &keys.ToggleLocal},
		{id: ActionToggleExclude, binding: &keys.ToggleExclude},
		{id: ActionToggleCancel, binding: &keys.ToggleCancel},
		{id: ActionHarnessGlobal, binding: &keys.HarnessGlobal},
		{id: ActionHarnessProject, binding: &keys.HarnessProject},
		{id: ActionHarnessSave, binding: &keys.HarnessSave},
		{id: ActionHarnessCancel, binding: &keys.HarnessCancel},
		{id: ActionMigrationRun, binding: &keys.MigrationRun},
		{id: ActionMigrationLeave, binding: &keys.MigrationLeave},
		{id: ActionEditConfigGlobal, binding: &keys.EditConfigGlobal},
		{id: ActionEditConfigProject, binding: &keys.EditConfigProject},
		{id: ActionEditConfigLocal, binding: &keys.EditConfigLocal},
		{id: ActionEditConfigCancel, binding: &keys.EditConfigCancel},
		{id: ActionDetailBack, binding: &keys.DetailBack},
		{id: ActionSearchConfirm, binding: &keys.SearchConfirm},
		{id: ActionSearchBackspace, binding: &keys.SearchBackspace},
		{id: ActionConfirmYes, binding: &keys.ConfirmYes},
		{id: ActionConfirmNo, binding: &keys.ConfirmNo},
		{id: ActionConfirmCancel, binding: &keys.ConfirmCancel},
	}
}

func QAInventory() QAInventorySnapshot {
	screenRegistrations := qaScreenRegistrations()
	screens := make([]ScreenID, 0, len(screenRegistrations)+1)
	for _, registration := range screenRegistrations {
		screens = append(screens, registration.id)
	}
	screens = append(screens, ScreenResizeRequired)

	keys := defaultKeyMap
	actionRegistrations := qaActionRegistrations(&keys)
	actions := make([]QAAction, 0, len(actionRegistrations))
	for _, registration := range actionRegistrations {
		help := registration.binding.Help()
		actions = append(actions, QAAction{
			ID:              registration.id,
			Prefix:          registration.prefix,
			Keys:            append([]string(nil), registration.binding.Keys()...),
			HelpKey:         help.Key,
			HelpDescription: help.Desc,
		})
	}

	states := make([]StateID, 0, len(allTabs)+len(qaTransientStateIDs))
	for _, current := range allTabs {
		states = append(states, qaTabStateIDs[current])
	}
	states = append(states, qaTransientStateIDs...)

	targets := make([]QATarget, len(knownHarnessOptions))
	for index, target := range knownHarnessOptions {
		targets[index] = QATarget(target)
	}

	return QAInventorySnapshot{SchemaVersion: QAInventorySchemaVersion, Screens: screens, Actions: actions, States: states, Targets: targets}
}
