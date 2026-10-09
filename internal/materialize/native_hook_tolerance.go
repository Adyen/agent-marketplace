package materialize

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	compilepkg "github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

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
	in, err := nativeHookFingerprintInput(res, target, machineScope)
	if err != nil {
		return "", err
	}
	return HookSourceFingerprint(in)
}

func nativeHookFingerprintInput(res resolver.ResolvedArtifact, target hcc.Target, machineScope bool) (HookSourceFingerprintInput, error) {
	scope := HookScopeRepository
	if machineScope {
		scope = HookScopeMachine
	}
	name := res.Name
	if res.InstalledName != "" {
		name = res.InstalledName
	}
	identity, err := nativeHookSourceIdentity(res)
	if err != nil {
		return HookSourceFingerprintInput{}, err
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
			return HookSourceFingerprintInput{}, fmt.Errorf("hook native tolerance: staged source root %q is not absolute", res.StagedSourceDir)
		}
		r, err := filepath.Rel(root, filepath.Clean(res.Path))
		if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(os.PathSeparator)) || filepath.IsAbs(r) || r == "." {
			return HookSourceFingerprintInput{}, fmt.Errorf("hook native tolerance: %q is not a definition file under staged source root", res.Path)
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
				return HookSourceFingerprintInput{}, fmt.Errorf("hook native tolerance: %q source %q does not resolve under %q", res.Name, srcFile, res.Path)
			}
			root = srcFile
			for range strings.Split(rel, "/") {
				root = filepath.Dir(root)
			}
		} else {
			root = sourceBundleRoot(res)
			if root == "" {
				return HookSourceFingerprintInput{}, fmt.Errorf("hook native tolerance: %q has no source-layout root", res.Name)
			}
			rel = filepath.ToSlash(srcFile)
			if rel == "" || rel == "." {
				return HookSourceFingerprintInput{}, fmt.Errorf("hook native tolerance: %q has no source-relative definition path", res.Name)
			}
		}
	}
	return HookSourceFingerprintInput{
		SourceIdentity: identity,
		Name:           name,
		Target:         string(target),
		Scope:          scope,
		SourceRoot:     root,
		DefinitionRel:  rel,
	}, nil
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
