package materialize

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	compilepkg "github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// nativeHookRootFor returns the authorized destination root that owns the
// documented native hook file for (target, machineScope) — the same root the
// future native writer's ownership store opens on. It deliberately does NOT
// reuse the legacy install root: Windsurf's repository native file
// (.devin/hooks.json) lives under the repository root, not under .windsurf.
func nativeHookRootFor(target hcc.Target, machineScope bool, repoRoot, home string) (string, bool) {
	if _, ok := nativeHookLayoutFor(target, machineScope); !ok {
		return "", false
	}
	base := repoRoot
	if machineScope {
		if home == "" {
			return "", false
		}
		base = home
	}
	switch target {
	case hcc.TargetClaude:
		return filepath.Join(base, ".claude"), true
	case hcc.TargetCursor:
		return filepath.Join(base, ".cursor"), true
	case hcc.TargetFactory:
		return filepath.Join(base, ".factory"), true
	case hcc.TargetWindsurf:
		if machineScope {
			return filepath.Join(home, ".codeium", "windsurf"), true
		}
		return repoRoot, true
	}
	return "", false
}

// nativeHookSourceIdentity derives the spec's stable source identity for one
// selected hook: a git registry occurrence, a canonical local-directory source,
// or an exact Marketplace resource revision. The derivation matches the
// version-1 golden-vector shapes in testdata/hook-source-fingerprint-v1.json:
// Marketplace pins use "marketplace:resource/<name>@<content-digest>", where
// the digest is the pin's verified resource content digest — the exact
// revision coordinate, so a different pinned revision is always a different
// identity. A pin missing its digest yields no identity rather than a guess.
func nativeHookSourceIdentity(res resolver.ResolvedArtifact) (string, error) {
	if res.IsMarketplace {
		digest := strings.TrimSpace(res.MarketplaceDigest)
		if digest == "" {
			return "", fmt.Errorf("hook native tolerance: marketplace content digest for %q is not available", res.Name)
		}
		name := res.MarketplaceResourceName
		if name == "" {
			name = res.Name
		}
		return "marketplace:resource/" + name + "@" + digest, nil
	}
	url := strings.TrimSpace(res.RegistryURL)
	if url == "" {
		return "", fmt.Errorf("hook native tolerance: %q has no configured source identity", res.Name)
	}
	if res.IsLocal() {
		abs, err := filepath.Abs(url)
		if err != nil {
			return "", fmt.Errorf("hook native tolerance: canonicalize local source %q: %w", url, err)
		}
		return "local:" + filepath.Clean(abs), nil
	}
	return "git:" + config.NormalizeGitURL(url) + "@" + strings.TrimSpace(res.Ref) + ":" + config.NormalizeRegistryPath(res.RegistryPath), nil
}

// nativeHookFingerprint computes the selected hook's version-1 source
// fingerprint exactly as the future native writer will: stable source
// identity, installed name, target, scope, the source-layout-relative
// definition path, and the raw source bytes under the source root. Any input
// that cannot be resolved safely yields no fingerprint.
func nativeHookFingerprint(res resolver.ResolvedArtifact, target hcc.Target, machineScope bool) (string, error) {
	if res.Resource == nil || res.Resource.Kind() != ir.KindHook {
		return "", errors.New("hook native tolerance: not a hook resource")
	}
	scope := HookScopeRepository
	if machineScope {
		scope = HookScopeMachine
	}
	name := res.Name
	if res.InstalledName != "" {
		name = res.InstalledName
	}
	if res.IsMarketplace && res.MarketplaceAcquisition == config.MarketplaceAcquisitionMirror {
		// Verified mirror acquisition retains no original source tree: Path
		// is the verified artifact archive, which must never be read as a
		// source layout. The mirror fingerprint is the domain-separated
		// hook-source/mirror/v1 digest over verified resolution metadata —
		// including the content digest the resolver verified the artifact
		// against. A missing or unverified digest fails closed inside the
		// fingerprint and suppresses nothing.
		resource := res.MarketplaceResourceName
		if resource == "" {
			resource = res.Name
		}
		return HookSourceMirrorFingerprint(HookSourceMirrorFingerprintInput{
			InstanceID:    res.MarketplaceInstanceID,
			RegistryID:    res.MarketplaceRegistryID,
			Kind:          res.Resource.Kind().String(),
			ResourceName:  resource,
			Digest:        strings.TrimSpace(res.MarketplaceDigest),
			InstalledName: name,
			Target:        string(target),
			Scope:         scope,
		})
	}
	identity, err := nativeHookSourceIdentity(res)
	if err != nil {
		return "", err
	}
	var root, rel string
	if res.IsMarketplace && res.StagedSourceDir != "" {
		// Index-only/staged Marketplace sources: Path is the definition file
		// inside a copied resource source tree, which may be flatter than the
		// original bundle layout the payload's Source.File records. The
		// staged tree is the only readable source layout and is what the
		// future native writer resolves too, so both sides derive the same
		// definition path relative to the staged root.
		root = filepath.Clean(res.StagedSourceDir)
		if !filepath.IsAbs(root) {
			return "", fmt.Errorf("hook native tolerance: staged source root %q is not absolute", res.StagedSourceDir)
		}
		r, err := filepath.Rel(root, filepath.Clean(res.Path))
		if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(os.PathSeparator)) || filepath.IsAbs(r) || r == "." {
			return "", fmt.Errorf("hook native tolerance: %q is not a definition file under staged source root", res.Path)
		}
		rel = filepath.ToSlash(r)
	} else {
		srcFile := filepath.Clean(filepath.FromSlash(compilepkg.SourceOf(res.Resource).File))
		if filepath.IsAbs(srcFile) {
			// Locked resolution pins the IR SourceRef to the immutable exact
			// snapshot while Entry.Path is rebased to the readable catalog
			// location. Both name the same bundle-relative definition file:
			// the source-relative path is their common trailing components,
			// and the snapshot bytes rooted there are authoritative.
			rel = commonTrailingSlashPath(srcFile, filepath.Clean(res.Path))
			if rel == "" {
				return "", fmt.Errorf("hook native tolerance: %q source %q does not resolve under %q", res.Name, srcFile, res.Path)
			}
			root = srcFile
			for range strings.Split(rel, "/") {
				root = filepath.Dir(root)
			}
		} else {
			root = sourceBundleRoot(res)
			if root == "" {
				return "", fmt.Errorf("hook native tolerance: %q has no source-layout root", res.Name)
			}
			rel = filepath.ToSlash(srcFile)
			if rel == "" || rel == "." {
				return "", fmt.Errorf("hook native tolerance: %q has no source-relative definition path", res.Name)
			}
		}
	}
	return HookSourceFingerprint(HookSourceFingerprintInput{
		SourceIdentity: identity,
		Name:           name,
		Target:         string(target),
		Scope:          scope,
		SourceRoot:     root,
		DefinitionRel:  rel,
	})
}

// commonTrailingSlashPath returns the slash-separated trailing component run
// shared by two paths — for example a snapshot-pinned absolute source file and
// its readable catalog path, whose common suffix is the bundle-relative
// definition path. It returns "" when the paths share no trailing component.
func commonTrailingSlashPath(a, b string) string {
	ac := strings.Split(filepath.ToSlash(a), "/")
	bc := strings.Split(filepath.ToSlash(b), "/")
	var n int
	for n < len(ac) && n < len(bc) && ac[len(ac)-1-n] == bc[len(bc)-1-n] {
		n++
	}
	return strings.Join(ac[len(ac)-n:], "/")
}

// nativeHookInspection is the per-output-key read-only view of a target's
// documented native hook file. entries are the classified rules currently in
// the file; current maps each present entry's hook/v2 identity to its
// canonical value digest; proof is the durable ownership record for exactly
// this file. err is the coded refusal that must fail the target's hooks
// closed before any legacy mutation.
type nativeHookInspection struct {
	root    string
	entries []NativeHookEntry
	current map[ownership.EntryID]string
	proof   ownership.Proof
	err     error
}

// hookSatisfied reports whether the durable record proves that the selected
// hook — identified by its version-1 source fingerprint and its ownership
// resource name — is completely present in the native file. It never equates
// legacy rule bytes with native bytes: the future native writer records this
// fingerprint and resource identity on every entry it emits for the hook, so
// a match requires that EVERY recorded entry carrying (resource, fingerprint)
// is still present and owned-clean. A missing or drifted recorded entry is
// partial proof and suppresses nothing; a recorded entry for a different
// fingerprint or resource is no evidence for this hook at all.
func (ins *nativeHookInspection) hookSatisfied(fingerprint, resource string) bool {
	matched := false
	for _, id := range ins.proof.OwnedIDs() {
		if id.Kind != ownership.KindHook {
			continue
		}
		recorded, ok := ins.proof.Resource(id)
		if !ok || recorded != resource {
			continue
		}
		digest, ok := ins.proof.SourceDigest(id)
		if !ok || digest != fingerprint {
			continue
		}
		valueDigest, present := ins.current[id]
		if !present || ins.proof.Classify(id, valueDigest) != ownership.StatusOwnedClean {
			return false
		}
		matched = true
	}
	return matched
}

// nativeHookInspectionFor inspects the target's native hook file at most once
// per output key. Targets without a documented native hook file get an empty
// inspection (nothing can satisfy them); a failed inspection is cached so
// every hook of the same (target, scope) sees the identical refusal.
func nativeHookInspectionFor(cache map[outputKey]*nativeHookInspection, key outputKey, repoRoot string) *nativeHookInspection {
	if ins, ok := cache[key]; ok {
		return ins
	}
	ins := &nativeHookInspection{}
	cache[key] = ins
	home := ""
	if key.machineScope {
		if resolved, err := os.UserHomeDir(); err == nil {
			home = resolved
		}
	}
	root, ok := nativeHookRootFor(key.target, key.machineScope, repoRoot, home)
	if !ok {
		return ins
	}
	ins.root = root
	store, err := ownership.Open(root)
	if err != nil {
		ins.err = nativeHookRefused(root, "", fmt.Errorf("open ownership store: %w", err))
		return ins
	}
	parsed, err := inspectNativeHookFile(store, root, key.target, key.machineScope)
	if err != nil {
		ins.err = err
		return ins
	}
	ins.entries = parsed.entries
	ins.current = parsed.current
	ins.proof = parsed.proof
	return ins
}

// hookFragmentEventMap decodes one compiled hook fragment into its event map.
// Fragments arrive in two documented envelopes: a bare event map
// ({event: [rules]}, Factory) or one nested under a "hooks" member
// (Claude, Cursor, Windsurf). Any other shape is not a hook fragment this
// function may rewrite and is left entirely alone.
func hookFragmentEventMap(data []byte) (events map[string][]json.RawMessage, nested bool, ok bool) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil || len(root) == 0 {
		return nil, false, false
	}
	if member, has := root["hooks"]; has {
		var inner map[string][]json.RawMessage
		if err := json.Unmarshal(member, &inner); err == nil && inner != nil {
			return inner, true, true
		}
	}
	events = make(map[string][]json.RawMessage, len(root))
	for name, raw := range root {
		trimmed := bytes.TrimSpace(raw)
		if len(trimmed) == 0 || trimmed[0] != '[' {
			return nil, false, false
		}
		var rules []json.RawMessage
		if err := json.Unmarshal(raw, &rules); err != nil {
			return nil, false, false
		}
		events[name] = rules
	}
	return events, false, true
}

// marshalHookFragment re-renders a filtered event map in the same envelope
// the compiler emitted.
func marshalHookFragment(events map[string][]json.RawMessage, nested bool) ([]byte, error) {
	if nested {
		return json.Marshal(map[string]any{"hooks": events})
	}
	return json.Marshal(events)
}

// legacyManagedRulePresent reports whether the existing legacy file already
// holds a MARKED element canonically equal to the desired rule (ignoring the
// _managed_by marker itself). A surviving managed twin must be kept desired:
// dropping it would let the merge delete legacy bytes, which this release is
// forbidden to do. An equal UNMARKED element — the proven native entry itself
// in the Windsurf shared file — does not keep the rule desired: suppressing
// it leaves exactly that element in place, marked-duplicate free.
func legacyManagedRulePresent(legacyFile string, nested bool, event string, rule json.RawMessage) bool {
	raw, err := os.ReadFile(legacyFile)
	if err != nil {
		return false
	}
	events, fileNested, ok := hookFragmentEventMap(raw)
	if !ok || fileNested != nested {
		return false
	}
	want, err := canonicalOwnedJSON(rule)
	if err != nil {
		return false
	}
	for _, existing := range events[event] {
		unmarked, marked := stripManagedByMarker(existing)
		if !marked {
			continue
		}
		canonical, err := canonicalOwnedJSON(unmarked)
		if err != nil {
			continue
		}
		if bytes.Equal(canonical, want) {
			return true
		}
	}
	return false
}

// stripManagedByMarker removes the _managed_by member from one JSON object,
// reporting whether the marker was present, and leaving every other field —
// and non-objects — untouched.
func stripManagedByMarker(raw json.RawMessage) (json.RawMessage, bool) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return raw, false
	}
	if _, ok := obj[managedByField]; !ok {
		return raw, false
	}
	delete(obj, managedByField)
	out, err := json.Marshal(obj)
	if err != nil {
		return raw, false
	}
	return out, true
}

// suppressNativeSatisfiedHookRules drops one hook's compiled fragment
// artifacts when the durable record proves the SAME selected hook — matching
// version-1 source fingerprint and ownership resource identity — is already
// completely present in the target's documented native file. Native rule
// bytes deliberately play no part: PR #403 emits target-native events,
// translated matchers, and rewritten support-command paths, so byte equality
// with the old fragment could never prove anything.
//
// Suppression is all-or-nothing per hook, never per rule: a hook emitting
// more than one native rule is proven only when every recorded entry is
// still owned-clean (partial proof keeps the whole legacy output). A rule
// whose managed legacy twin is still on disk stays desired so merge
// preserves those bytes instead of deleting them.
//
// A hook whose source fingerprint cannot be computed at all suppresses
// nothing — an identity guess could suppress the wrong entry.
func suppressNativeSatisfiedHookRules(res resolver.ResolvedArtifact, key outputKey, legacyRoot string, ins *nativeHookInspection, artifacts []artifact.Artifact) []artifact.Artifact {
	fingerprint, err := nativeHookFingerprint(res, key.target, key.machineScope)
	if err != nil {
		return artifacts
	}
	name := res.Name
	if res.InstalledName != "" {
		name = res.InstalledName
	}
	if !ins.hookSatisfied(fingerprint, name) {
		return artifacts
	}
	out := artifacts[:0]
	for _, a := range artifacts {
		if !a.Fragment {
			out = append(out, a)
			continue
		}
		events, nested, ok := hookFragmentEventMap(a.Bytes)
		if !ok {
			out = append(out, a)
			continue
		}
		legacyFile := filepath.Join(legacyRoot, filepath.FromSlash(a.Path))
		if key.target == hcc.TargetWindsurf && key.machineScope {
			// The machine-scope install root already IS ~/.codeium/windsurf:
			// the compiled ".windsurf/" prefix is a project-scope prefix that
			// machine-scope plans strip before writing.
			legacyFile = filepath.Join(legacyRoot, filepath.FromSlash(strings.TrimPrefix(a.Path, windsurfDir+"/")))
		}
		filtered := make(map[string][]json.RawMessage, len(events))
		for _, event := range sortedKeys(events) {
			var kept []json.RawMessage
			for _, rule := range events[event] {
				if legacyManagedRulePresent(legacyFile, nested, event, rule) {
					kept = append(kept, rule)
				}
			}
			if len(kept) > 0 {
				filtered[event] = kept
			}
		}
		if len(filtered) == 0 {
			// The hook is natively satisfied and no managed legacy twin
			// survives: it contributes nothing to the legacy file.
			continue
		}
		body, marshalErr := marshalHookFragment(filtered, nested)
		if marshalErr != nil {
			out = append(out, a)
			continue
		}
		a.Bytes = body
		out = append(out, a)
	}
	return out
}
