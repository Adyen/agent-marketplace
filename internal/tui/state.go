package tui

import (
	"context"
	"strings"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/telemetry"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
)

type driftAssessment uint8

const (
	driftUnknown driftAssessment = iota
	driftReady
	driftFailed
)

type pane int

const (
	paneList pane = iota
	paneDetail
	paneHelp
	paneErrors
	paneConfirm
	paneToggleModal
	paneInitModal
	paneMigrationModal
	paneHarnessModal
	paneEditConfigModal
	paneMarketplaceConsent
)

type marketplaceSelection struct {
	Entry config.MarketplaceEntry
	Pin   config.MarketplaceResourcePin
}

type marketplaceDiscovery struct {
	Endpoint   config.MarketplaceEntry
	Item       mpapi.ResourceListItem
	CacheState marketplace.DiscoveryCacheState
	ObservedAt time.Time
}

type row struct {
	Artifact     resolver.ResolvedArtifact
	DetailSource *detailSourceSnapshot // immutable bounded copy captured while the exact snapshot lease is held
	IsAvailable  bool                  // not in resolved (available but not enabled)
	IsExcluded   bool                  // legacy: skill is in localCfg.Exclude (mirrors ExcludedLocally)
	IsMalformed  bool
	MalformedMsg string
	DriftItem    *materialize.DriftItem // nil if in sync or available-only

	// Per-scope enablement: which config file(s) enable this skill.
	// A skill may be enabled in multiple scopes simultaneously.
	EnabledGlobal   bool
	EnabledProject  bool
	EnabledLocal    bool
	ExcludedLocally bool
	ExcludedByUser  bool

	// Source registry of the skill. For available (non-enabled) skills, set
	// to the first registry whose catalog contained the skill, in cfg order.
	RegistryURL   string
	RegistryName  string // display-friendly form: last path segment, .git stripped
	RegistryPath  string // path subdirectory from the registry entry (for config writes)
	RegistryRef   string // ref from the registry entry (for config writes)
	Harnesses     []string
	HarnessSource string

	MarketplaceSelections     map[config.Source]marketplaceSelection
	MarketplaceCollectionOnly bool
	MarketplaceDiscovery      *marketplaceDiscovery

	// searchableLower is a cached lowercased concatenation of the fields
	// applyFilter scans (name + tags + registry URL). Populated lazily by
	// ensureSearchable so applyFilter does not re-allocate this string on
	// every keystroke while the user is typing in the search input.
	searchableLower string
}

// ensureSearchable populates searchableLower on first use and short-circuits
// on subsequent calls. Safe to call from any code path that is about to
// filter the row; buildRows and test helpers can skip the call because the
// first applyFilter invocation will fill it in.
func (r *row) ensureSearchable() {
	if r.searchableLower != "" {
		return
	}
	size := len(r.Artifact.Name) + len(r.Artifact.RegistryURL) + 2
	for _, t := range r.Artifact.Tags {
		size += len(t) + 1
	}
	var b strings.Builder
	b.Grow(size)
	b.WriteString(r.Artifact.Name)
	b.WriteByte(' ')
	for i, t := range r.Artifact.Tags {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(t)
	}
	b.WriteByte(' ')
	b.WriteString(r.Artifact.RegistryURL)
	b.WriteByte(' ')
	b.WriteString(r.Artifact.Description)
	if r.MarketplaceDiscovery != nil {
		b.WriteByte(' ')
		b.WriteString(marketplaceEndpointDisplay(r.MarketplaceDiscovery.Endpoint))
		b.WriteByte(' ')
		b.WriteString(r.MarketplaceDiscovery.Item.RegistrySlug)
		b.WriteByte(' ')
		b.WriteString(string(r.MarketplaceDiscovery.Item.Availability))
		for key, value := range r.MarketplaceDiscovery.Item.Metadata {
			b.WriteByte(' ')
			b.WriteString(key)
			b.WriteByte(' ')
			b.WriteString(value)
		}
	}
	r.searchableLower = strings.ToLower(b.String())
	if r.searchableLower == "" {
		// Guarantee a non-empty sentinel so the cached-check above keeps
		// short-circuiting even for fully-empty rows.
		r.searchableLower = " "
	}
}

func (r row) canOverrideMarketplace() bool {
	return r.Artifact.IsMarketplace && r.Artifact.Source == config.Organization && !r.Artifact.Required
}

func (r row) Glyph() string {
	if r.IsMalformed {
		return "[!]"
	}
	if r.IsAvailable {
		return "[ ]"
	}
	if r.Artifact.Required {
		return "[R]"
	}
	if r.DriftItem != nil {
		switch r.DriftItem.Reason {
		case materialize.DriftMissing:
			return "[*]"
		case materialize.DriftOrphaned:
			return "[-]"
		case materialize.DriftBroken:
			return "[!]"
		}
	}
	return "[x]"
}

func (r row) StatusText() string {
	if r.IsMalformed {
		return "malformed: " + r.MalformedMsg
	}
	if r.IsAvailable {
		if r.IsExcluded {
			return "excluded"
		}
		if r.MarketplaceDiscovery != nil {
			status := string(r.MarketplaceDiscovery.Item.Availability)
			if status == "" {
				status = "unknown"
			}
			if r.MarketplaceDiscovery.CacheState == marketplace.DiscoveryCacheStale {
				status += ", stale"
			}
			return status + " via Marketplace"
		}
		return "available"
	}
	if r.Artifact.Required {
		if r.Artifact.Source == config.Organization {
			return "required (org)"
		}
		return "required"
	}
	if r.DriftItem != nil {
		switch r.DriftItem.Reason {
		case materialize.DriftMissing:
			return "enabled (pending sync)"
		case materialize.DriftOrphaned:
			return "orphaned"
		case materialize.DriftBroken:
			return "broken"
		}
	}
	return "enabled"
}

type Model struct {
	repoRoot            string
	homeDir             string
	rows                []row
	authoritativeRows   []row
	discoveryRows       []row
	browseDiscoveryRows []row
	discoveryOutcomes   []marketplace.DiscoveryOutcome
	discoveryCancel     context.CancelFunc
	discoveryLoading    bool
	discoverySequence   uint64
	discoveryErrs       []error
	filteredRows        []row
	filterScratch       []row
	cursor              int
	filter              string
	filterActive        bool
	searchInput         textinput.Model
	loading             bool

	pane                pane
	driftReport         materialize.DriftReport
	driftAssessment     driftAssessment
	diagnostics         diagnosticStore
	errs                []error
	warnings            []error
	syncing             bool
	refreshing          bool
	requests            requestState
	rowsStale           bool
	dependencyLifecycle DependencyLifecycle

	// Cached config state for toggle/sync
	mergedCfg        *config.Config
	resolved         []resolver.ResolvedArtifact
	localExcludes    []string
	globalHarnesses  []string
	projectHarnesses []string

	listOffset       int // first visible flatItems index in the left pane (current tab)
	listHeightPrefix []int
	listHeightWidth  int
	width            int
	height           int

	// Top tab bar and per-tab navigation state. When switching tabs we swap
	// cursor/listOffset/filter in+out of tabStates so each tab keeps its
	// own position and fold state independently.
	activeTab tab
	tabStates map[tab]*tabViewState

	// flatItems is the render-ready list computed from filteredRows +
	// current tab + current foldState. Cursor indexes into this slice.
	// Call rebuildFlatItems whenever rows, tab, filter, or fold state change.
	flatItems []flatItem

	// zPending is true after the user presses 'z' and awaits a follow-up
	// key (a/M/R) to complete a vim-style fold command.
	zPending bool

	// modalSkill is the row whose state is displayed in paneToggleModal.
	// Captured at modal-open time so rapid reloads can't change what the
	// user is acting on mid-interaction.
	modalSkill                  row
	marketplaceInstallOptions   pipeline.InstallOptions
	marketplacePreparedInstall  *pipeline.PreparedMarketplaceInstall
	marketplaceInstallOperation operationID
	marketplaceInstallReload    requestGeneration
	marketplaceInstallPreview   string
	harnessModal                harnessModalState
	initModal                   initModalState
	migrationPrompt             migrationPrompt
	migrationDismissed          bool
	operations                  operationState
	pendingToggleOperation      operationID
	pendingToggleReload         requestGeneration

	// pendingExcludeAction holds a toggleAction that needs to emit
	// excludeCompetingRegistriesCmd after the primary toggle succeeds.
	// Set when enabling a colliding resource; cleared on configWrittenMsg.
	pendingExcludeAction  *toggleAction
	selectionAfterDisable string

	// spinner provides an animated indicator in the footer while
	// loading, refreshing, or syncing.
	spinner spinner.Model

	detailViewport             viewport.Model
	helpViewport               viewport.Model
	errorsViewport             viewport.Model
	marketplaceConsentViewport viewport.Model

	// detailKey is the stable row identity of the resource whose full detail
	// pane is open. It keeps an accepted snapshot rebuild anchored to the same
	// resource even when rows are inserted or reordered.
	detailKey          string
	detailRenderWidth  int
	detailStickyBottom bool
	bundle             *bundleTree
	bundleOffset       int
	detailSelectedPath string
	detailLegacyRender bool

	toast    string
	toastExp time.Time

	confirmMsg string

	// quitAfterSync is set when the user confirms "sync+quit" in the
	// confirm dialog. The syncDoneMsg handler checks this flag and emits
	// tea.Quit only after the sync has actually completed, avoiding the
	// race where tea.Quit in a tea.Batch kills the program before the
	// sync goroutine finishes.
	quitAfterSync bool

	// syncQuitErr holds the sync error (if any) from the sync that
	// triggered a quit. Run() checks this after the program exits so
	// failed sync+quit flows surface the error to the caller instead of
	// exiting cleanly.
	syncQuitErr error

	// ctx carries the command/root span context through the TUI lifecycle.
	// It is set by Run and used by action and session span instrumentation.
	ctx context.Context

	// sessionSpan is the TUI session span; it is ended when Run returns.
	sessionSpan telemetry.Span

	// keys is the centralized keymap; the single source of truth for all
	// key bindings and their help text. Both the footer hint line and the
	// ? help overlay are driven from this keymap via the help model.
	keys keyMap
	help help.Model
}
