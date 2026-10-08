package tui

import (
	"context"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	"charm.land/huh/v2"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/telemetry"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
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
	Artifact          resolver.ResolvedArtifact
	DetailSource      *detailSourceSnapshot // immutable bounded copy captured while the exact snapshot lease is held
	IsAvailable       bool                  // not in resolved (available but not enabled)
	IsExcluded        bool                  // legacy: skill is in localCfg.Exclude (mirrors ExcludedLocally)
	IsMalformed       bool
	MalformedMsg      string
	UnavailableReason string
	DriftItem         *materialize.DriftItem // nil if in sync or available-only

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

	PendingVerification bool

	// searchWords is the cached, sorted and deduplicated set of lowercased
	// words from the name and metadata that query terms prefix-match.
	// Populated lazily by ensureSearchable so applyFilter does not rebuild
	// it on every keystroke while the user is typing in the search input.
	searchWords     []string
	searchReady     bool
	searchName      string
	searchRanked    bool
	searchNameMatch bool
	searchScore     int
	// searchMatchRunes holds rune indexes into the sanitized name that the
	// query genuinely matched; it is empty for metadata-only matches.
	searchMatchRunes []int

	labelMemo *rowLabelMemo
}

type rowLabelMemo struct {
	depth   int
	width   int
	compact bool
	color   bool
	height  int
	valid   bool
}

func (r *row) ensureLabelMemo() {
	if r.labelMemo == nil {
		r.labelMemo = &rowLabelMemo{}
	}
}

// ensureSearchable populates the search index on first use and
// short-circuits on subsequent calls. Safe to call from any code path that
// is about to filter the row; buildRows and test helpers can skip the call
// because the first applyFilter invocation will fill it in.
func (r *row) ensureSearchable() {
	if r.searchReady {
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
		metadataKeys := make([]string, 0, len(r.MarketplaceDiscovery.Item.Metadata))
		for key := range r.MarketplaceDiscovery.Item.Metadata {
			metadataKeys = append(metadataKeys, key)
		}
		sort.Strings(metadataKeys)
		for _, key := range metadataKeys {
			b.WriteByte(' ')
			b.WriteString(key)
			b.WriteByte(' ')
			b.WriteString(r.MarketplaceDiscovery.Item.Metadata[key])
		}
	}
	r.searchName = strings.ToLower(sanitizeTerminalData(r.Artifact.Name, terminalSingleLine))
	words := searchTerms(b.String())
	slices.Sort(words)
	r.searchWords = slices.Compact(words)
	r.searchReady = true
}

// searchTerms splits text into lowercased words at every character that is
// neither a letter nor a digit. Queries and searchable metadata share it so
// "platform.git" matches the words of a registry URL.
func searchTerms(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(c rune) bool {
		return !unicode.IsLetter(c) && !unicode.IsDigit(c)
	})
}

// hasWordWithPrefix reports whether any of the row's search words begins
// with term.
func (r *row) hasWordWithPrefix(term string) bool {
	i, _ := slices.BinarySearch(r.searchWords, term)
	return i < len(r.searchWords) && strings.HasPrefix(r.searchWords[i], term)
}

func (r row) canOverrideMarketplace() bool {
	return r.Artifact.IsMarketplace && r.Artifact.Source == config.Organization && !r.Artifact.Required
}

// rowStatus is the authoritative result of a row's status precedence: one
// decision derives the ASCII glyph, the color-profile symbol, and the status
// label so the three can never diverge.
type rowStatus struct {
	glyph  string
	symbol string
	label  string
}

func (r row) status() rowStatus {
	if r.PendingVerification {
		label := "enabled in config; verification pending"
		if r.IsExcluded {
			label = "excluded; verification pending"
		} else if r.IsAvailable {
			label = "available; verification pending"
		}
		if r.IsAvailable {
			return rowStatus{"[ ]", "○", label}
		}
		return rowStatus{"[x]", "●", label}
	}
	if r.IsMalformed {
		return rowStatus{"[!]", "✗", "malformed: " + r.MalformedMsg}
	}
	if r.UnavailableReason != "" {
		return rowStatus{"[!]", "✗", "unavailable: " + r.UnavailableReason}
	}
	if r.IsAvailable {
		if r.IsExcluded {
			return rowStatus{"[ ]", "○", "excluded"}
		}
		if r.MarketplaceDiscovery != nil {
			label := string(r.MarketplaceDiscovery.Item.Availability)
			if label == "" {
				label = "unknown"
			}
			if r.MarketplaceDiscovery.CacheState == marketplace.DiscoveryCacheStale {
				label += ", stale"
			}
			return rowStatus{"[ ]", "○", label + " via Marketplace"}
		}
		return rowStatus{"[ ]", "○", "available"}
	}
	if r.Artifact.Required {
		if r.Artifact.Source == config.Organization {
			return rowStatus{"[R]", "■", "required (org)"}
		}
		return rowStatus{"[R]", "■", "required"}
	}
	if r.DriftItem != nil {
		switch r.DriftItem.Reason {
		case materialize.DriftMissing:
			return rowStatus{"[*]", "◐", "enabled (pending sync)"}
		case materialize.DriftOrphaned:
			return rowStatus{"[-]", "◌", "orphaned"}
		case materialize.DriftBroken:
			return rowStatus{"[!]", "✗", "broken"}
		}
	}
	return rowStatus{"[x]", "●", "enabled"}
}

func (r row) Glyph() string { return r.status().glyph }

func (r row) StatusText() string { return r.status().label }

type Model struct {
	repoRoot            string
	workspace           pipeline.Workspace
	homeDir             string
	rows                []row
	authoritativeRows   []row
	provisionalRows     []row
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
	assessmentPending   bool

	pane                pane
	driftReport         materialize.DriftReport
	driftAssessment     driftAssessment
	diagnostics         diagnosticStore
	errs                []error
	warnings            []error
	syncing             bool
	refreshing          bool
	requests            requestState
	configInvalid       bool
	rowsStale           bool
	dependencyLifecycle DependencyLifecycle

	// Cached config state for toggle/sync
	mergedCfg        *config.Config
	resolved         []resolver.ResolvedArtifact
	localExcludes    []string
	globalHarnesses  []string
	projectHarnesses []string

	listOffset       int // first visible flatItems index in the left pane
	listHeightPrefix []int
	listHeightWidth  int
	width            int
	height           int

	// foldedSections is the single status-grouped list's fold state, keyed by
	// section path ("status:enabled", ...).
	foldedSections map[string]bool

	queryFoldSections map[string]bool
	queryFoldQuery    string

	// flatItems is the render-ready list computed from filteredRows +
	// foldState. Cursor indexes into this slice. Call rebuildFlatItems
	// whenever rows, filter, or fold state change.
	flatItems []flatItem

	// zPending is true after the user presses 'z' and awaits a follow-up
	// key (a/M/R) to complete a vim-style fold command.
	zPending bool

	// palette is the command-palette overlay state. It opens only in the
	// list pane with no active search input, modal, overlay, or pending
	// fold prefix; see palette.go.
	palette paletteState

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
	// updateReload is the superseding snapshot generation that must be
	// accepted (or fail) before a stale updateDoneMsg's operation ends;
	// updatePendingErr retains that update's own error for the verdict.
	updateReload          requestGeneration
	updatePendingErr      error
	updatePendingWarnings []error
	// syncReload is the snapshot generation scheduled by syncDoneMsg that
	// must be accepted (or fail) before the sync operation ends;
	// syncPendingErr retains the pipeline's own error for the verdict.
	syncReload          requestGeneration
	syncPendingErr      error
	syncPendingWarnings []error

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

	previewSel previewSelection

	// toast/toastExp mirror the newest visible toast for the single-line
	// footer segment; toastQueue holds the whole visible stack (newest
	// last, bounded by toastStackMax) rendered as a top-right layer.
	toast      string
	toastExp   time.Time
	toastQueue []queuedToast

	// progress is the active operation's phase observation source;
	// operationPhase is the newest accepted phase label (empty = generic
	// running), and progressLabel names the operation for the status bar.
	progress       *operationProgressSource
	progressLabel  string
	operationPhase string

	confirmMsg string

	// Huh forms owned by the interactive modals (AC4). Each form carries
	// its option state; letter accelerators are routed before delegation.
	toggleForm       *huh.Form
	toggleFormFor    string
	toggleChoice     *string
	migrationForm    *huh.Form
	migrationValue   *bool
	editConfigForm   *huh.Form
	editConfigChoice *string
	syncQuitForm     *huh.Form
	syncQuitValue    *bool

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

	// isDark records the terminal background reported by the most recent
	// tea.BackgroundColorMsg; theme carries the semantic colors built from
	// it so every model renders against its own reported background.
	isDark bool
	theme  theme
}
