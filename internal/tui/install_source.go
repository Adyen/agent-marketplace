package tui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/giturl"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/skill"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/charmbracelet/x/ansi"
)

type installSourceStep uint8

const (
	installSourceStepSource installSourceStep = iota
	installSourceStepPick
	installSourceStepReview
)

type installSourceValues struct {
	url  string
	ref  string
	path string
}

type installSourceModal struct {
	step        installSourceStep
	values      *installSourceValues
	form        *huh.Form
	preview     *pipeline.SourcePreview
	picked      map[string]bool
	cursor      int
	scope       scope
	filter      string
	filterInput textinput.Model
	filtering   bool
	error       string
	notice      string
	loading     bool
	details     bool
	generation  requestGeneration
	cancel      context.CancelFunc
}

type installSourceChoice struct {
	kind        ir.ResourceKind
	name        string
	description string
	malformed   string
	bundlePath  string
}

type installSourcePreviewLoadedMsg struct {
	generation requestGeneration
	preview    *pipeline.SourcePreview
	err        error
}

type installSourceInstallMsg struct {
	operation operationID
	options   pipeline.InstallOptions
	result    *pipeline.InstallResult
	err       error
}

func (m Model) openInstallSource() (tea.Model, tea.Cmd) {
	if m.configInvalid {
		return m, invalidConfigToastCmd()
	}
	if m.operations.active != nil || m.syncing {
		return m, operationBlockedCmd()
	}
	values := &installSourceValues{}
	filter := textinput.New()
	filter.Prompt = "/ "
	filter.SetWidth(modalInnerWidth(m))
	m.installSource = installSourceModal{
		step: installSourceStepSource, values: values, picked: make(map[string]bool),
		scope: scopeProject, filterInput: filter,
	}
	m.buildInstallSourceForm()
	m.pane = paneInstallSource
	return m, nil
}

func (m *Model) buildInstallSourceForm() {
	if m.installSource.values == nil {
		m.installSource.values = &installSourceValues{}
	}
	values := m.installSource.values
	fields := []huh.Field{
		huh.NewInput().Title("URL").Key("url").Placeholder("Git URL, web URL, or directory").Value(&values.url),
		huh.NewInput().Title("Ref").Key("ref").Placeholder("default branch").Value(&values.ref),
		huh.NewInput().Title("Path").Key("path").Placeholder("registry root").Value(&values.path),
	}
	form := huh.NewForm(huh.NewGroup(fields...)).
		WithKeyMap(modalFormKeyMap()).
		WithWidth(modalInnerWidth(*m)).
		WithTheme(huh.ThemeFunc(func(bool) *huh.Styles { return huhTheme(m.currentTheme()) })).
		WithShowHelp(false).
		WithShowErrors(true)
	drainFormCmd(form, form.Init())
	m.installSource.form = form
}

func (m *Model) resizeInstallSourceForm() {
	if m.installSource.form != nil {
		m.installSource.form.WithWidth(modalInnerWidth(*m))
	}
	m.installSource.filterInput.SetWidth(modalInnerWidth(*m) - 2)
}

func updateInstallSourceForm(s *installSourceModal, msg tea.Msg) tea.Cmd {
	if s.form == nil {
		return nil
	}
	updated, cmd := s.form.Update(msg)
	if form, ok := updated.(*huh.Form); ok {
		s.form = form
	}
	return cmd
}

var installSourceFieldKeys = []string{"url", "ref", "path"}

// moveInstallSourceFocus moves focus across the Source fields, wrapping at the
// ends when wrap is set. huh completes a single-group form when Tab leaves its
// last field, so the step goes back through the fields instead of asking huh
// to advance past the end. Focus moves synchronously inside huh; only the
// returned cursor commands are async.
func moveInstallSourceFocus(s *installSourceModal, delta int, wrap bool) tea.Cmd {
	if s.form == nil {
		return nil
	}
	n := len(installSourceFieldKeys)
	index := slices.Index(installSourceFieldKeys, s.form.GetFocusedField().GetKey())
	target := index + delta
	if wrap {
		target = (target + n) % n
	} else {
		target = min(max(target, 0), n-1)
	}
	var cmds []tea.Cmd
	for ; index < target; index++ {
		cmds = append(cmds, s.form.NextField())
	}
	for ; index > target; index-- {
		cmds = append(cmds, s.form.PrevField())
	}
	return tea.Batch(cmds...)
}

func renderInstallSourceModal(m Model) string {
	s := m.installSource
	t := m.currentTheme()
	title := titleStyle(t).Render("Install from source")
	stepParts := []string{"① Source", "② Pick", "③ Review"}
	for i := 0; i < int(s.step); i++ {
		stepParts[i] = "✓ " + []string{"Source", "Pick", "Review"}[i]
	}
	stepLine := strings.Join(stepParts, "   ")
	if compactTerminalLayout(m) {
		stepLine = fmt.Sprintf("%d/3", s.step+1)
	}
	body := []string{stepLine}
	hints := []footerHint{{"esc", "cancel"}}
	switch s.step {
	case installSourceStepSource:
		if s.form != nil {
			body = append(body, "", s.form.View())
		}
		body = append(body, "", "Web URLs fill ref and path automatically.", "Nothing is written yet.")
		if s.loading {
			body = append(body, "", "Previewing source…")
		}
		if s.error != "" {
			body = append(body, "", styleErrorTitle.Render(sanitizeTerminalData(s.error, terminalSingleLine)))
		}
		hints = append(hints, footerHint{"tab/↑↓", "field"}, footerHint{"enter", "preview"})
	case installSourceStepPick:
		body = append(body, installSourceHeader(s))
		if sourceAlreadyConfigured(m, s.preview) {
			body = append(body, "This source is already configured; picks remain available.")
		}
		if s.notice != "" {
			body = append(body, styleWarningMsg.Render(sanitizeTerminalData(s.notice, terminalSingleLine)))
		}
		if s.error != "" {
			body = append(body, styleErrorTitle.Render(sanitizeTerminalData(s.error, terminalSingleLine)))
		}
		if s.loading {
			body = append(body, "Previewing source…")
		}
		if s.preview != nil && len(s.preview.BundlePaths) > 0 {
			body = append(body, "Several bundles found; choose a path to preview.")
		} else if s.filtering {
			body = append(body, s.filterInput.View())
		} else if s.filter != "" {
			body = append(body, "Filter: "+sanitizeTerminalData(s.filter, terminalSingleLine))
		}
		prefix := append([]string(nil), body...)
		suffix := make([]string, 0)
		choices := installSourceFilteredChoices(s)
		if s.preview != nil && len(s.preview.BundlePaths) == 0 && len(installSourceChoices(s.preview)) == 0 {
			suffix = append(suffix, "Nothing discoverable under this source. Check that it points at the directory containing skills/, commands/ or a plugin manifest.")
		}
		suffix = append(suffix, fmt.Sprintf("%d selected", installSourceSelectedCount(s)))
		if malformed := installSourceMalformedCount(s.preview); malformed > 0 {
			suffix = append(suffix, fmt.Sprintf("⚠ %d malformed resource(s) skipped · d: details", malformed))
		}
		if s.details && s.preview != nil {
			for _, warning := range s.preview.Warnings {
				suffix = append(suffix, "• "+sanitizeTerminalData(warning.Error(), terminalSingleLine))
			}
			for _, choice := range installSourceChoices(s.preview) {
				if choice.malformed != "" {
					suffix = append(suffix, "• "+sanitizeTerminalData(choice.name+": "+choice.malformed, terminalSingleLine))
				}
			}
		}
		start, end := installSourceChoiceWindow(m, s, len(choices), prefix, suffix)
		body = append(prefix, installSourceChoiceLines(m, s, choices, start, end)...)
		body = append(body, suffix...)
		hints = append(hints, footerHint{"space", "toggle"}, footerHint{"a", "all"}, footerHint{"/", "filter"}, footerHint{"→/enter", "review"}, footerHint{"←/⌫", "back"}, footerHint{"d", "details"})
	case installSourceStepReview:
		body = append(body, installSourceReviewLines(m, s)...)
		hints = append(hints, footerHint{"↑↓/g/p/l", "scope"}, footerHint{"y/enter", "install"}, footerHint{"←/⌫", "back"})
	}
	return modalFrame(m, t.accent, title, body, hints)
}

func installSourceHeader(s installSourceModal) string {
	if s.preview == nil {
		return ""
	}
	kindCounts := map[ir.ResourceKind]int{}
	validCount := 0
	for _, choice := range installSourceChoices(s.preview) {
		if choice.malformed == "" {
			kindCounts[choice.kind]++
			validCount++
		}
	}
	counts := make([]string, 0, len(kindCounts))
	for _, kind := range installSourceKindOrder {
		if kindCounts[kind] > 0 {
			counts = append(counts, fmt.Sprintf("%d %s", kindCounts[kind], skill.KindLabel(kind)))
		}
	}
	commit := "directory"
	if s.preview.Commit != "" {
		commit = sanitizeTerminalData(shortCommit(s.preview.Commit), terminalSingleLine)
	}
	ref := s.preview.Ref
	if ref == "" {
		ref = "default branch"
	}
	ref = sanitizeTerminalData(ref, terminalSingleLine)
	return fmt.Sprintf("%d resources at %s on %s · %s", validCount, commit, ref, strings.Join(counts, " · "))
}

var installSourceKindOrder = []ir.ResourceKind{
	ir.KindSkill, ir.KindCommand, ir.KindSubagent, ir.KindHook, ir.KindMcpServer, ir.KindCollection, ir.KindRule,
}

func installSourceChoices(preview *pipeline.SourcePreview) []installSourceChoice {
	if preview == nil {
		return nil
	}
	if len(preview.BundlePaths) > 0 {
		choices := make([]installSourceChoice, 0, len(preview.BundlePaths))
		for _, path := range preview.BundlePaths {
			choices = append(choices, installSourceChoice{bundlePath: path, name: path})
		}
		return choices
	}
	choices := make([]installSourceChoice, 0)
	for _, loaded := range preview.Catalogs {
		for _, entry := range loaded.Entries {
			choices = append(choices, installSourceChoice{kind: entry.Kind, name: entry.Name, description: entry.Description})
		}
	}
	for _, col := range preview.Collections {
		choices = append(choices, installSourceChoice{kind: ir.KindCollection, name: col.Name, description: col.Description})
	}
	for _, loaded := range preview.Catalogs {
		for _, malformed := range loaded.Malformed {
			kind := ir.KindSkill
			if parsed, ok := skill.ParseKind(malformed.Kind); ok {
				kind = parsed
			}
			name := malformed.DirName
			if name == "" {
				name = malformed.DeclaredName
			}
			message := ""
			if malformed.Error != nil {
				message = malformed.Error.Error()
			}
			choices = append(choices, installSourceChoice{kind: kind, name: name, malformed: message})
		}
	}
	sort.SliceStable(choices, func(i, j int) bool {
		if choices[i].bundlePath != choices[j].bundlePath {
			return choices[i].bundlePath < choices[j].bundlePath
		}
		if choices[i].kind != choices[j].kind {
			return choices[i].kind < choices[j].kind
		}
		if (choices[i].malformed == "") != (choices[j].malformed == "") {
			return choices[i].malformed == ""
		}
		return choices[i].name < choices[j].name
	})
	return choices
}

func installSourceChoiceKey(choice installSourceChoice) string {
	return choice.kind.String() + "\x00" + choice.name
}

func installSourceFilteredChoices(s installSourceModal) []installSourceChoice {
	choices := installSourceChoices(s.preview)
	if s.filter == "" {
		return choices
	}
	filter := strings.ToLower(s.filter)
	return slices.DeleteFunc(choices, func(choice installSourceChoice) bool {
		return !strings.Contains(strings.ToLower(choice.name+" "+choice.description+" "+choice.malformed+" "+choice.bundlePath), filter)
	})
}

func installSourceChoiceWindow(m Model, s installSourceModal, choiceCount int, prefix, suffix []string) (int, int) {
	if choiceCount == 0 {
		return 0, 0
	}
	visibleRows := max(1, m.height-modalFrameRows-1-ansiLineCount(prefix, modalInnerWidth(m))-ansiLineCount(suffix, modalInnerWidth(m)))
	if choiceCount <= visibleRows {
		return 0, choiceCount
	}
	cursor := min(max(0, s.cursor), choiceCount-1)
	start := min(max(0, cursor-visibleRows/2), choiceCount-visibleRows)
	return start, start + visibleRows
}

func ansiLineCount(lines []string, width int) int {
	count := 0
	for _, line := range lines {
		count += len(strings.Split(ansi.Wrap(line, max(1, width), ""), "\n"))
	}
	return count
}

func installSourceChoiceLines(m Model, s installSourceModal, choices []installSourceChoice, start, end int) []string {
	if start >= end || start < 0 || end > len(choices) {
		return nil
	}
	lines := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		choice := choices[i]
		cursor := " "
		if i == s.cursor {
			cursor = "›"
		}
		if choice.bundlePath != "" {
			lines = append(lines, fmt.Sprintf("%s [ ] %s", cursor, sanitizeTerminalData(choice.bundlePath, terminalSingleLine)))
			continue
		}
		if choice.malformed != "" {
			lines = append(lines, fmt.Sprintf("%s [!] %s %s · malformed", cursor, skill.KindLabel(choice.kind), sanitizeTerminalData(choice.name, terminalSingleLine)))
			continue
		}
		selected := " "
		if s.picked[installSourceChoiceKey(choice)] {
			selected = "x"
		}
		status := installSourceBadge(m, s.preview.URL, choice)
		description := ""
		if !compactTerminalLayout(m) {
			description = sanitizeTerminalData(choice.description, terminalSingleLine)
		}
		line := fmt.Sprintf("%s [%s] %-10s %-22s %s", cursor, selected, skill.KindLabel(choice.kind), sanitizeTerminalData(choice.name, terminalSingleLine), description)
		if status != "" {
			line += " · " + status
		}
		lines = append(lines, ansi.Truncate(line, max(1, modalInnerWidth(m)-2), "…"))
	}
	return lines
}

func installSourceBadge(m Model, url string, choice installSourceChoice) string {
	for _, installed := range m.rows {
		if installed.Artifact.Kind != choice.kind || installed.Artifact.Name != choice.name {
			continue
		}
		sourceURL := installed.RegistryURL
		if sourceURL == "" {
			sourceURL = installed.Artifact.RegistryURL
		}
		if config.NormalizeGitURL(sourceURL) == config.NormalizeGitURL(url) {
			var scopes []string
			if installed.EnabledGlobal {
				scopes = append(scopes, "G")
			}
			if installed.EnabledProject {
				scopes = append(scopes, "P")
			}
			if installed.EnabledLocal {
				scopes = append(scopes, "L")
			}
			if len(scopes) > 0 {
				return "✓ already installed (" + strings.Join(scopes, "") + ")"
			}
			return "✓ already installed"
		}
		name := installed.RegistryName
		if name == "" {
			name = registryDisplayName(sourceURL)
		}
		return "conflicts: installed from " + sanitizeTerminalData(name, terminalSingleLine)
	}
	return ""
}

func sourceAlreadyConfigured(m Model, preview *pipeline.SourcePreview) bool {
	if preview == nil || m.mergedCfg == nil {
		return false
	}
	source := config.RegistryEntry{URL: preview.URL}
	for _, entry := range m.mergedCfg.Registries {
		if entry.IsLocal() != source.IsLocal() {
			continue
		}
		if entry.IsLocal() {
			if filepath.Clean(entry.URL) == filepath.Clean(preview.URL) && config.NormalizeRegistryPath(entry.Path) == config.NormalizeRegistryPath(preview.Path) {
				return true
			}
			continue
		}
		if config.NormalizeGitURL(entry.URL) == config.NormalizeGitURL(preview.URL) &&
			strings.TrimSpace(entry.Ref) == strings.TrimSpace(preview.Ref) &&
			config.NormalizeRegistryPath(entry.Path) == config.NormalizeRegistryPath(preview.Path) {
			return true
		}
	}
	return false
}

func installSourceReviewLines(m Model, s installSourceModal) []string {
	var lines []string
	for _, choice := range installSourceChoices(s.preview) {
		if !s.picked[installSourceChoiceKey(choice)] || choice.malformed != "" {
			continue
		}
		if status := installSourceBadge(m, s.preview.URL, choice); strings.HasPrefix(status, "conflicts:") {
			lines = append(lines, "Collision warning: "+status)
		}
	}
	for _, option := range []struct {
		scope scope
		key   string
		name  string
	}{
		{scopeGlobal, "g", "User"}, {scopeProject, "p", "Project"}, {scopeLocal, "l", "Local"},
	} {
		path, err := installSourceConfigPath(m, option.scope)
		if err != nil {
			path = "configuration path unavailable"
		}
		entry := config.RegistryEntry{URL: s.preview.URL, Ref: s.preview.Ref, Path: s.preview.Path}
		var reason string
		if option.scope == scopeGlobal {
			if err := config.ValidateGlobalRegistryWrite(entry); err != nil {
				reason = err.Error()
			}
		}
		marker := " "
		if s.scope == option.scope {
			marker = "›"
		}
		line := fmt.Sprintf("%s [%s] %s · %s", marker, option.key, option.name, shortenPath(path, m.homeDir, modalInnerWidth(m)-24))
		if reason != "" {
			line += " (disabled: " + sanitizeTerminalData(reason, terminalSingleLine) + ")"
		}
		lines = append(lines, line)
	}
	lines = append(lines, "", "Selected resources:")
	for _, choice := range installSourceChoices(s.preview) {
		if s.picked[installSourceChoiceKey(choice)] && choice.malformed == "" {
			lines = append(lines, fmt.Sprintf("• %s %s", skill.KindLabel(choice.kind), sanitizeTerminalData(choice.name, terminalSingleLine)))
		}
	}
	path, _ := installSourceConfigPath(m, s.scope)
	lines = append(lines, "", "Writes: "+shortenPath(path, m.homeDir, modalInnerWidth(m)-8))
	if s.preview.Commit != "" && !giturl.IsFullObjectID(strings.TrimSpace(s.preview.Ref)) {
		lines = append(lines, fmt.Sprintf("%s is a branch; it can move before install. Use a commit SHA in Ref to pin.", sanitizeTerminalData(s.preview.Ref, terminalSingleLine)))
	}
	if s.error != "" {
		lines = append(lines, styleErrorTitle.Render(sanitizeTerminalData(s.error, terminalSingleLine)))
	}
	return lines
}

func installSourceConfigPath(m Model, target scope) (string, error) {
	root := m.repoRoot
	if target == scopeLocal {
		root = m.workspace.LocalConfigDir()
	}
	return pipeline.ScopedConfigPath(root, pipelineScope(target))
}

func installSourceSelectedCount(s installSourceModal) int {
	count := 0
	for _, choice := range installSourceChoices(s.preview) {
		if choice.malformed == "" && choice.bundlePath == "" && s.picked[installSourceChoiceKey(choice)] {
			count++
		}
	}
	return count
}

func (m Model) handleInstallSourceKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := &m.installSource
	switch s.step {
	case installSourceStepSource:
		if key.Matches(msg, m.keys.Clear) {
			return m.cancelInstallSourceModal()
		}
		if key.Matches(msg, m.keys.Enter) {
			if s.values == nil || strings.TrimSpace(s.values.url) == "" {
				s.error = "enter a source URL or directory"
				return m, nil
			}
			if s.preview != nil && installSourceUnchanged(*s) {
				s.step, s.error, s.cursor = installSourceStepPick, "", 0
				return m, nil
			}
			return m.startInstallSourcePreview(false)
		}
		switch msg.String() {
		case "tab":
			return m, moveInstallSourceFocus(s, 1, true)
		case "shift+tab":
			return m, moveInstallSourceFocus(s, -1, true)
		case "down":
			return m, moveInstallSourceFocus(s, 1, false)
		case "up":
			return m, moveInstallSourceFocus(s, -1, false)
		}
		return m, updateInstallSourceForm(s, msg)
	case installSourceStepPick:
		if s.filtering {
			if key.Matches(msg, m.keys.Clear) {
				s.filter = ""
				s.filterInput.SetValue("")
				s.filterInput.Blur()
				s.filtering = false
				s.cursor = 0
				return m, nil
			}
			if key.Matches(msg, m.keys.SearchConfirm) {
				s.filter = strings.TrimSpace(s.filterInput.Value())
				s.filtering = false
				s.filterInput.Blur()
				s.cursor = 0
				return m, nil
			}
			s.filterInput, _ = s.filterInput.Update(msg)
			return m, nil
		}
		if key.Matches(msg, m.keys.Quit) || key.Matches(msg, m.keys.Clear) {
			return m.cancelInstallSourceModal()
		}
		if key.Matches(msg, m.keys.Search) {
			s.filtering = true
			s.filterInput.SetValue(s.filter)
			return m, s.filterInput.Focus()
		}
		if key.Matches(msg, m.keys.Enter) || msg.String() == "right" {
			return m.advanceInstallSourcePick()
		}
		switch msg.String() {
		case "backspace", "left":
			s.step = installSourceStepSource
			s.error = ""
			s.cursor = 0
			m.buildInstallSourceForm()
			return m, nil
		case "d", "D":
			s.details = !s.details
			return m, nil
		case "a", "A":
			return m.toggleAllInstallSourceChoices()
		case " ", "space":
			return m.toggleInstallSourceChoice()
		}
		if key.Matches(msg, m.keys.Up) || msg.String() == "k" {
			s.cursor = max(0, s.cursor-1)
			return m, nil
		}
		if key.Matches(msg, m.keys.Down) || msg.String() == "j" {
			s.cursor = min(max(0, len(installSourceFilteredChoices(*s))-1), s.cursor+1)
			return m, nil
		}
		return m, nil
	case installSourceStepReview:
		if key.Matches(msg, m.keys.Enter) || key.Matches(msg, m.keys.ConfirmYes) {
			if err := installSourceScopeError(*s, s.scope); err != nil {
				s.error = err.Error()
				return m, nil
			}
			return m.startInstallSourceInstall()
		}
		if key.Matches(msg, m.keys.Quit) || key.Matches(msg, m.keys.Clear) {
			return m.cancelInstallSourceModal()
		}
		switch msg.String() {
		case "backspace", "left":
			s.step = installSourceStepPick
			s.error = ""
			return m, nil
		case "up", "k":
			index := slices.Index(installSourceScopes, s.scope)
			s.selectScope(installSourceScopes[max(0, index-1)])
		case "down", "j":
			index := slices.Index(installSourceScopes, s.scope)
			s.selectScope(installSourceScopes[min(index+1, len(installSourceScopes)-1)])
		case "g":
			s.selectScope(scopeGlobal)
		case "p":
			s.selectScope(scopeProject)
		case "l":
			s.selectScope(scopeLocal)
		}
	}
	return m, nil
}

var installSourceScopes = []scope{scopeGlobal, scopeProject, scopeLocal}

func (s *installSourceModal) selectScope(target scope) {
	if err := installSourceScopeError(*s, target); err != nil {
		s.error = err.Error()
		return
	}
	s.scope, s.error = target, ""
}

func installSourceUnchanged(s installSourceModal) bool {
	return strings.TrimSpace(s.values.url) == s.preview.URL &&
		strings.TrimSpace(s.values.ref) == s.preview.Ref &&
		strings.TrimSpace(s.values.path) == s.preview.Path
}

func (m Model) cancelInstallSourceModal() (tea.Model, tea.Cmd) {
	m.cancelInstallSourcePreview()
	m.installSource = installSourceModal{}
	m.pane = paneList
	return m, nil
}

func (m Model) toggleInstallSourceChoice() (tea.Model, tea.Cmd) {
	choices := installSourceFilteredChoices(m.installSource)
	if m.installSource.cursor < 0 || m.installSource.cursor >= len(choices) {
		return m, nil
	}
	choice := choices[m.installSource.cursor]
	if choice.bundlePath != "" {
		m.installSource.values.path = choice.bundlePath
		return m.startInstallSourcePreview(false)
	}
	if choice.malformed != "" {
		return m, nil
	}
	key := installSourceChoiceKey(choice)
	m.installSource.picked[key] = !m.installSource.picked[key]
	return m, nil
}

func (m Model) toggleAllInstallSourceChoices() (tea.Model, tea.Cmd) {
	choices := installSourceFilteredChoices(m.installSource)
	allSelected := true
	for _, choice := range choices {
		if choice.bundlePath == "" && choice.malformed == "" && !m.installSource.picked[installSourceChoiceKey(choice)] {
			allSelected = false
			break
		}
	}
	for _, choice := range choices {
		if choice.bundlePath == "" && choice.malformed == "" {
			m.installSource.picked[installSourceChoiceKey(choice)] = !allSelected
		}
	}
	return m, nil
}

func (m Model) advanceInstallSourcePick() (tea.Model, tea.Cmd) {
	s := &m.installSource
	if s.preview == nil || len(s.preview.BundlePaths) == 0 && len(installSourceChoices(s.preview)) == 0 {
		return m, nil
	}
	if len(s.preview.BundlePaths) > 0 {
		choices := installSourceFilteredChoices(*s)
		if s.cursor < 0 || s.cursor >= len(choices) {
			return m, nil
		}
		s.values.path = choices[s.cursor].bundlePath
		return m.startInstallSourcePreview(false)
	}
	if installSourceSelectedCount(*s) == 0 {
		s.error = "pick at least one resource"
		return m, nil
	}
	s.step = installSourceStepReview
	s.error = ""
	return m, nil
}

func installSourceScopeError(s installSourceModal, target scope) error {
	if s.preview == nil {
		return errors.New("preview the source before choosing an install scope")
	}
	if target == scopeGlobal {
		return config.ValidateGlobalRegistryWrite(config.RegistryEntry{URL: s.preview.URL, Ref: s.preview.Ref, Path: s.preview.Path})
	}
	return nil
}

func (m Model) startInstallSourcePreview(preserveNotice bool) (tea.Model, tea.Cmd) {
	s := &m.installSource
	m.cancelInstallSourcePreview()
	if !preserveNotice {
		s.notice = ""
	}
	s.error = ""
	s.preview = nil
	s.loading = true
	s.step = installSourceStepPick
	generation := m.beginRequest(requestInstallPreview)
	s.generation = generation
	ctx, cancel := context.WithCancel(ctxOrBackground(m.ctx))
	s.cancel = cancel
	return m, loadInstallSourcePreview(ctx, m.workspace, s.values.url, s.values.ref, s.values.path, generation)
}

func loadInstallSourcePreview(ctx context.Context, ws pipeline.Workspace, url, ref, path string, generation requestGeneration) tea.Cmd {
	return func() tea.Msg {
		preview, err := pipeline.PreviewSource(ctx, ws, url, ref, path)
		return installSourcePreviewLoadedMsg{generation: generation, preview: &preview, err: err}
	}
}

func (m Model) updateInstallSourcePreview(msg installSourcePreviewLoadedMsg) (tea.Model, tea.Cmd) {
	if m.pane != paneInstallSource || !m.acceptsRequest(requestInstallPreview, msg.generation) {
		return m, nil
	}
	s := &m.installSource
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.loading = false
	if msg.err != nil {
		message := sanitizeTerminalData(msg.err.Error(), terminalSingleLine)
		s.error = message
		var rejected *pipeline.SourceRejectedError
		if errors.As(msg.err, &rejected) {
			s.step = installSourceStepSource
			return m, nil
		}
		return m, nil
	}
	s.preview = msg.preview
	s.form = nil
	s.values.url, s.values.ref, s.values.path = msg.preview.URL, msg.preview.Ref, msg.preview.Path
	available := make(map[string]bool)
	for _, choice := range installSourceChoices(msg.preview) {
		if choice.bundlePath == "" && choice.malformed == "" {
			available[installSourceChoiceKey(choice)] = true
		}
	}
	for key := range s.picked {
		if !available[key] {
			delete(s.picked, key)
		}
	}
	s.error = ""
	s.cursor = 0
	s.step = installSourceStepPick
	return m, nil
}

func (m Model) startInstallSourceInstall() (tea.Model, tea.Cmd) {
	locators := make([]config.ResourceLocator, 0, installSourceSelectedCount(m.installSource))
	for _, choice := range installSourceChoices(m.installSource.preview) {
		if choice.malformed == "" && choice.bundlePath == "" && m.installSource.picked[installSourceChoiceKey(choice)] {
			locators = append(locators, config.ResourceLocator{Kind: choice.kind, Name: choice.name})
		}
	}
	if len(locators) == 0 {
		m.installSource.error = "pick at least one resource"
		return m, nil
	}
	localConfigDir := ""
	if m.workspace.LocalConfigDir() != m.workspace.RepoRoot() {
		localConfigDir = m.workspace.LocalConfigDir()
	}
	options := pipeline.InstallOptions{
		RepoRoot: m.repoRoot, LocalConfigDir: localConfigDir,
		FromURL: m.installSource.preview.URL, FromRef: m.installSource.preview.Ref, FromPath: m.installSource.preview.Path,
		Locators: locators, ExpectedCommit: m.installSource.preview.Commit, Scope: pipelineScope(m.installSource.scope), ScopeSpecified: true,
		DependencyReconcile: m.dependencyLifecycle.InstallReconcile,
		PrepareDependencies: m.dependencyLifecycle.InstallPrepare,
	}
	operation, ok := m.beginOperation(operationMutation)
	if !ok {
		return m, operationBlockedCmd()
	}
	m.pane = paneList
	m.marketplaceInstallOptions = options
	m.marketplaceInstallOperation = operation
	m.marketplaceInstallIsSource = true
	m.beginOperationProgressFor(operation, "install", m.requests.active(requestSnapshot))
	return m, tea.Batch(runInstallSource(m.observedContext(ctxOrBackground(m.ctx)), operation, options), m.operationProgressCmd())
}

func runInstallSource(ctx context.Context, operation operationID, options pipeline.InstallOptions) tea.Cmd {
	return func() tea.Msg {
		result, err := pipeline.Install(ctx, options)
		return installSourceInstallMsg{operation: operation, options: options, result: result, err: err}
	}
}

func (m Model) updateInstallSourceInstall(msg installSourceInstallMsg) (tea.Model, tea.Cmd) {
	if !m.acceptsOperation(msg.operation) {
		return m, nil
	}
	if msg.err != nil {
		var moved *pipeline.SourceMovedError
		if errors.As(msg.err, &moved) {
			m.clearOperationProgress()
			m.finishOperation(msg.operation)
			m.marketplaceInstallOperation = 0
			m.marketplaceInstallIsSource = false
			m.pane = paneInstallSource
			m.installSource.step = installSourceStepPick
			m.installSource.notice = fmt.Sprintf("%s moved from %s to %s; preview again",
				sanitizeTerminalData(moved.Ref, terminalSingleLine),
				sanitizeTerminalData(shortCommit(moved.Expected), terminalSingleLine),
				sanitizeTerminalData(shortCommit(moved.Actual), terminalSingleLine))
			m.installSource.error = ""
			return m.startInstallSourcePreview(true)
		}
		m.clearOperationProgress()
		m.finishOperation(msg.operation)
		m.marketplaceInstallOperation = 0
		m.marketplaceInstallIsSource = false
		m.recordOperationFailure(operationMutation, diagnosticMutation, msg.err)
		m.pushToast("Source install failed: "+sanitizeTerminalData(msg.err.Error(), terminalSingleLine), 5*time.Second)
		return m, toastExpiryCmd(5 * time.Second)
	}
	generation := m.beginRequest(requestSnapshot)
	m.marketplaceInstallReload = generation
	if m.progress != nil {
		m.operationPhase = string(pipeline.PhaseReload)
	}
	return m, loadSnapshot(ctxOrBackground(m.ctx), m.workspace, m.homeDir, generation)
}

func (m *Model) cancelInstallSourcePreview() {
	if m.installSource.cancel != nil {
		m.installSource.cancel()
		m.installSource.cancel = nil
	}
	m.cancelRequest(requestInstallPreview)
	m.installSource.loading = false
}

func shortCommit(commit string) string {
	if len(commit) > 8 {
		return commit[:8]
	}
	return commit
}

func installSourceMalformedCount(preview *pipeline.SourcePreview) int {
	if preview == nil {
		return 0
	}
	count := 0
	for _, choice := range installSourceChoices(preview) {
		if choice.malformed != "" {
			count++
		}
	}
	return count
}
