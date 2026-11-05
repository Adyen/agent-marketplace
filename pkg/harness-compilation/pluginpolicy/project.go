package pluginpolicy

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

// Delivery selects how a plugin reaches a harness: DeliveryDecompose lowers
// every member as a leaf resource; DeliveryNative additionally applies the
// harness catalog's plugin-surface field policies before lowering.
type Delivery string

const (
	DeliveryNative    Delivery = "native"
	DeliveryDecompose Delivery = "decompose"
)

// Roots carries the caller-supplied package generation and plugin data roots
// that ${PLUGIN_ROOT} and ${PLUGIN_DATA} expand to. The generation slice
// supplies real values; an empty value expands to the empty string.
type Roots struct {
	Package string
	Data    string
}

// MemberProjection is the projection of one plugin member: its package key,
// the artifacts the leaf compiler produced, the diagnostics it collected,
// and whether it was blocked.
type MemberProjection struct {
	Key         string
	Artifacts   []artifact.Artifact
	Diagnostics []diag.Diagnostic
	Blocked     bool
}

// Projection is the result of projecting one plugin onto one target.
type Projection struct {
	Plugin      string
	Target      target.Target
	Delivery    Delivery
	Members     []MemberProjection
	Diagnostics []diag.Diagnostic
	Blocked     bool
}

const (
	pluginRootToken = "${PLUGIN_ROOT}"
	pluginDataToken = "${PLUGIN_DATA}"
)

// canonicalInterpolationToken matches every placeholder shape the destination
// translators recognize as canonical MCP interpolation: ${VAR}, ${env:VAR},
// and ${file:PATH}. A plugin member's literal text that matches it would be
// reinterpreted by the destination, and canonical IR offers no literal escape.
var canonicalInterpolationToken = regexp.MustCompile(`\$\{(env:[A-Za-z_][A-Za-z0-9_]*|file:[^{}]*|[A-Za-z_][A-Za-z0-9_]*)\}`)

// HasCanonicalInterpolationConflict reports canonical interpolation other than
// the package-root tokens, whose meaning Agent Plugins preserves.
func HasCanonicalInterpolationConflict(value string) bool {
	for _, token := range canonicalInterpolationToken.FindAllString(value, -1) {
		if token != pluginRootToken && token != pluginDataToken {
			return true
		}
	}
	return false
}

// Project lowers a plugin onto a target. It is pure: it performs no I/O and
// reads every plugin-surface decision from the shipped harness catalog.
func Project(p *ir.Plugin, t target.Target, d Delivery, scope compile.Scope, roots Roots) (*Projection, error) {
	if p == nil {
		return nil, fmt.Errorf("pluginpolicy: plugin must not be nil")
	}
	if d != DeliveryNative && d != DeliveryDecompose {
		return nil, fmt.Errorf("pluginpolicy: delivery %q is not a recognized plugin delivery", d)
	}
	if d == DeliveryNative {
		// The catalog's accepts list describes native loading only;
		// decomposed delivery compiles members to the target like any
		// other resource and needs no package-format acceptance.
		accepted, err := Accepts(t, p.Format)
		if err != nil {
			return nil, err
		}
		if !accepted {
			return nil, diag.NewCodedError(diag.CodePluginFormatUnsupported, diag.SourceRef{},
				fmt.Errorf("target %q does not accept plugin format %q", t, p.Format))
		}
	}
	proj := &Projection{Plugin: p.Name(), Target: t, Delivery: d}
	for _, member := range p.Members {
		mp, err := projectMember(p, member, t, d, scope, roots)
		if err != nil {
			return nil, err
		}
		if mp.Blocked {
			proj.Blocked = true
		}
		proj.Members = append(proj.Members, mp)
	}
	return proj, nil
}

func projectMember(p *ir.Plugin, member ir.PluginMember, t target.Target, d Delivery, scope compile.Scope, roots Roots) (MemberProjection, error) {
	mp := MemberProjection{Key: member.Key()}
	res := member.Resource
	if res == nil {
		return mp, fmt.Errorf("pluginpolicy: member %q carries no resource", mp.Key)
	}
	if d == DeliveryNative {
		surfaced, err := applyPluginSurface(t, res, &mp)
		if err != nil {
			return mp, err
		}
		res = surfaced
	}
	if !mp.Blocked && p.Format == "agent-plugin" {
		if server, ok := res.(*ir.McpServer); ok {
			server = ExpandMemberRuntime(server, roots).(*ir.McpServer)
			res = server
			if carriesLiteralPlaceholder(server) {
				mp.Blocked = true
				mp.Diagnostics = append(mp.Diagnostics, memberDiag(
					diag.CodePluginMemberLiteralUnrepresentable, diag.SeverityError, t, mp.Key,
					"a literal placeholder in this MCP member has no escape on the destination; the member was blocked", memberSource(server)))
			}
		}
	}
	if mp.Blocked {
		return mp, nil
	}
	result, err := compile.Compile(res, compile.Target(t), compile.WithScope(scope))
	if err != nil {
		return mp, fmt.Errorf("pluginpolicy: lowering member %s: %w", mp.Key, err)
	}
	mp.Artifacts = result.Artifacts
	mp.Diagnostics = append(mp.Diagnostics, result.Diagnostics...)
	if diag.HasErrors(result.Diagnostics) {
		mp.Blocked = true
	}
	return mp, nil
}

func ExpandMemberRuntime(resource ir.Resource, roots Roots) ir.Resource {
	server, ok := resource.(*ir.McpServer)
	if !ok {
		return resource
	}
	return expandPluginRuntime(server, roots)
}

// applyPluginSurface applies the catalog's surfaces.plugin.<kind>.<field>
// outcomes to a member before lowering. block-member suppresses the member
// with the catalog's code; omit-field drops the field with a warning. It
// returns the resource to compile, a clone when a field was dropped. A
// catalog lookup error is propagated — a restriction lookup must not fail
// open.
func applyPluginSurface(t target.Target, res ir.Resource, mp *MemberProjection) (ir.Resource, error) {
	policies, err := PluginFieldPolicies(t, res.Kind().String())
	if err != nil {
		return nil, err
	}
	for _, field := range slices.Sorted(maps.Keys(policies)) {
		if !memberHasField(res, field) {
			continue
		}
		outcome := policies[field]
		switch outcome.Outcome {
		case FieldBlockMember:
			mp.Blocked = true
			mp.Diagnostics = append(mp.Diagnostics, memberDiag(
				outcome.Code, diag.SeverityError, t, mp.Key,
				fmt.Sprintf("member field %q is a restriction the destination plugin surface cannot enforce; the member was blocked", field), memberSource(res)))
		case FieldOmitField:
			res = dropMemberField(res, field)
			mp.Diagnostics = append(mp.Diagnostics, memberDiag(
				diag.CodePluginMemberFieldOmitted, diag.SeverityWarning, t, mp.Key,
				fmt.Sprintf("member field %q was omitted by the destination plugin surface", field), memberSource(res)))
		}
	}
	return res, nil
}

// memberHasField reports whether a member carries a plugin-surface field. The
// fields catalogs name (for example permissionMode) are non-modeled fields
// that live in the resource's preserved Extra map.
func memberHasField(res ir.Resource, field string) bool {
	extra := memberExtra(res)
	_, ok := extra[field]
	return ok
}

// dropMemberField returns a clone of res with field removed from Extra; the
// caller's resource is never mutated.
func dropMemberField(res ir.Resource, field string) ir.Resource {
	strip := func(extra map[string]any) map[string]any {
		clone := maps.Clone(extra)
		delete(clone, field)
		return clone
	}
	switch typed := res.(type) {
	case *ir.Skill:
		clone := *typed
		clone.Extra = strip(typed.Extra)
		return &clone
	case *ir.Command:
		clone := *typed
		clone.Extra = strip(typed.Extra)
		return &clone
	case *ir.Subagent:
		clone := *typed
		clone.Extra = strip(typed.Extra)
		return &clone
	case *ir.Hook:
		clone := *typed
		clone.Extra = strip(typed.Extra)
		return &clone
	case *ir.McpServer:
		clone := *typed
		clone.Extra = strip(typed.Extra)
		return &clone
	case *ir.Rule:
		clone := *typed
		clone.Extra = strip(typed.Extra)
		return &clone
	}
	return res
}

func memberExtra(res ir.Resource) map[string]any {
	switch typed := res.(type) {
	case *ir.Skill:
		return typed.Extra
	case *ir.Command:
		return typed.Extra
	case *ir.Subagent:
		return typed.Extra
	case *ir.Hook:
		return typed.Extra
	case *ir.McpServer:
		return typed.Extra
	case *ir.Rule:
		return typed.Extra
	}
	return nil
}

func memberSource(res ir.Resource) diag.SourceRef {
	return diag.SourceRef(compile.SourceOf(res))
}

// expandPluginRuntime expands ${PLUGIN_ROOT} and ${PLUGIN_DATA} in the MCP
// member's args, env values, and cwd — once, non-recursively, never in
// command or env keys (Agent Plugins §9.2). It returns a clone.
func expandPluginRuntime(server *ir.McpServer, roots Roots) *ir.McpServer {
	clone := *server
	replacer := strings.NewReplacer(pluginRootToken, roots.Package, pluginDataToken, roots.Data)
	expand := replacer.Replace
	if len(server.Args) > 0 {
		clone.Args = make([]string, len(server.Args))
		for i, arg := range server.Args {
			clone.Args[i] = expand(arg)
		}
	}
	if len(server.Env) > 0 {
		clone.Env = make(map[string]string, len(server.Env))
		for key, value := range server.Env {
			clone.Env[key] = expand(value)
		}
	}
	clone.Cwd = expand(server.Cwd)
	if raw, ok := server.Extra["cwd"].(string); ok {
		extra := maps.Clone(server.Extra)
		extra["cwd"] = expand(raw)
		clone.Extra = extra
	}
	return &clone
}

// carriesLiteralPlaceholder reports whether the expanded MCP member still
// carries placeholder-like text the destination would reinterpret in the
// fields where plugin expansion applies (args, env values, cwd). ${PLUGIN_*}
// in command is never expanded and never scanned: command is opaque text.
func carriesLiteralPlaceholder(server *ir.McpServer) bool {
	for _, arg := range server.Args {
		if canonicalInterpolationToken.MatchString(arg) {
			return true
		}
	}
	for _, value := range server.Env {
		if canonicalInterpolationToken.MatchString(value) {
			return true
		}
	}
	if canonicalInterpolationToken.MatchString(server.Cwd) {
		return true
	}
	if raw, ok := server.Extra["cwd"].(string); ok && canonicalInterpolationToken.MatchString(raw) {
		return true
	}
	return false
}

func memberDiag(code string, severity diag.Severity, t target.Target, key, message string, source diag.SourceRef) diag.Diagnostic {
	return diag.Diagnostic{
		Code:     code,
		Severity: severity,
		Target:   string(t),
		Resource: key,
		Message:  message,
		Source:   source,
	}
}
