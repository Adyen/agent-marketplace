package resolver

import (
	"fmt"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/redact"
	"github.com/Adyen/agent-marketplace/internal/skill"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// PluginEdge kinds recorded on a resolved plugin selection.
const (
	PluginEdgeDirect     = "direct"
	PluginEdgeCollection = "collection"
)

// PluginEdge is one selection edge: a direct request or a
// collection-to-plugin binding. The plugin-to-member edge set is
// PluginSelection.Members.
type PluginEdge struct {
	Kind       string
	Collection string
}

// PluginSelection is the resolved selection state of one plugin artifact:
// its selection edges and its members after exact request exclusions.
type PluginSelection struct {
	Edges      []PluginEdge
	Members    []ir.PluginMember
	Exclusions []string
}

// PluginMemberDirectSelectionError refuses a leaf locator that names a
// member of a format source; only the plugin itself is selectable.
type PluginMemberDirectSelectionError struct {
	Kind       ir.ResourceKind
	Name       string
	PluginName string
	URL        string
}

func (e *PluginMemberDirectSelectionError) Error() string {
	return fmt.Sprintf("plugin.member-direct-selection: %s %q is a member of plugin/%s at %s; select the plugin instead",
		skill.KindLabel(e.Kind), e.Name, e.PluginName, redact.URL(e.URL))
}

// PluginUnknownExclusionError refuses a plugin request exclusion that names
// no member of the plugin.
type PluginUnknownExclusionError struct {
	PluginName string
	Exclusion  string
	URL        string
}

func (e *PluginUnknownExclusionError) Error() string {
	return fmt.Sprintf("plugin.member.unknown-exclusion: plugin/%s at %s has no member %q",
		e.PluginName, redact.URL(e.URL), e.Exclusion)
}

// PluginEntryRefusalError reports the diagnostic attached to a catalog entry
// that is not installable (refused source type or duplicate name).
type PluginEntryRefusalError struct {
	Name       string
	URL        string
	Diagnostic *diag.Diagnostic
}

func (e *PluginEntryRefusalError) Error() string {
	if e.Diagnostic != nil && e.Diagnostic.Code != "" {
		return fmt.Sprintf("%s: plugin/%s at %s: %s", e.Diagnostic.Code, e.Name, redact.URL(e.URL), e.Diagnostic.Message)
	}
	return fmt.Sprintf("plugin/%s at %s is not installable", e.Name, redact.URL(e.URL))
}

// pluginMemberIndex maps member kind/name selections to the plugin that
// carries them, for direct-member refusals.
func pluginMemberIndex(entries []catalog.Entry) map[resourceSelection]string {
	var index map[resourceSelection]string
	for _, entry := range entries {
		if entry.Kind != ir.KindPlugin || entry.Plugin == nil {
			continue
		}
		for _, member := range entry.Plugin.Members {
			if member.Resource == nil {
				continue
			}
			if index == nil {
				index = map[resourceSelection]string{}
			}
			index[resourceSelection{kind: member.Resource.Kind(), name: member.Resource.Name()}] = entry.Name
		}
	}
	return index
}

// pluginRequestsFor returns the options of each explicit plugin request for
// name in the entry's ordered decision stream.
func pluginRequestsFor(entry config.RegistryEntry, name string) []config.V4ResourceOptions {
	var out []config.V4ResourceOptions
	for _, decision := range entry.V4Decisions {
		if decision.Action != config.V4SelectionExplicit || decision.Request == nil {
			continue
		}
		if decision.Request.Kind == "plugin" && decision.Request.Name == name {
			out = append(out, decision.Request.Options)
		}
	}
	return out
}

// pluginMemberExclusionMatch reports whether exclusion names member exactly.
func pluginMemberExclusionMatch(exclusion string, member ir.PluginMember) bool {
	kind, name, ok := strings.Cut(exclusion, "/")
	if !ok || member.Resource == nil {
		return false
	}
	return v4DecisionKind(kind) == member.Resource.Kind() && name == member.Resource.Name()
}

// applyPluginRequestOptions folds authored request options into the
// selection: exclusions are exact kind/name member locators; unknown entries
// refuse.
func applyPluginRequestOptions(selection *PluginSelection, name string, options []config.V4ResourceOptions, entry config.RegistryEntry) error {
	var excluded []string
	for _, opts := range options {
		excluded = append(excluded, opts.Exclude...)
	}
	members := selection.Members
	for _, exclusion := range excluded {
		matched := false
		kept := make([]ir.PluginMember, 0, len(members))
		for _, member := range members {
			if pluginMemberExclusionMatch(exclusion, member) {
				matched = true
				continue
			}
			kept = append(kept, member)
		}
		if !matched {
			return &PluginUnknownExclusionError{PluginName: name, Exclusion: exclusion, URL: entry.URL}
		}
		selection.Exclusions = append(selection.Exclusions, exclusion)
		members = kept
	}
	selection.Members = members
	return nil
}
