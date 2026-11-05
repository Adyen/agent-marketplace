package marketplace

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"runtime"
	"sort"

	"github.com/Adyen/agent-marketplace/internal/redact"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	marketplaceartifact "github.com/Adyen/agent-marketplace/pkg/marketplace/artifact"
)

// requireMatchingRegistryResources checks that the locally rebuilt registry
// revision lists exactly the resources of the attested manifest. Each resource
// must agree on kind, name, content digest, and support-file digests, which
// bind the reconstructed support bytes that content digests alone do not cover.
// Only archive digests are excluded, because the local encoder's digest is not
// the attested one.
func requireMatchingRegistryResources(attested marketplaceartifact.RegistryRevisionManifest, rebuiltPayload []byte) error {
	rebuilt, _, err := marketplaceartifact.DecodeRegistryRevisionManifest(bytes.NewReader(rebuiltPayload))
	if err != nil {
		return fmt.Errorf("decode rebuilt registry revision manifest: %w", err)
	}
	attestedBytes, err := normalizedRegistryResources(attested)
	if err != nil {
		return err
	}
	rebuiltBytes, err := normalizedRegistryResources(rebuilt)
	if err != nil {
		return err
	}
	if !bytes.Equal(attestedBytes, rebuiltBytes) {
		return errors.New("rebuilt registry resources do not match the attested registry revision manifest")
	}
	return nil
}

// normalizedRegistryResources returns the canonical JSON of the resources with
// archive digests cleared and sorted by kind and name.
func normalizedRegistryResources(manifest marketplaceartifact.RegistryRevisionManifest) ([]byte, error) {
	resources := append([]marketplaceartifact.RevisionResource(nil), manifest.Resources...)
	for i := range resources {
		resources[i].ArchiveDigest = ""
	}
	sort.SliceStable(resources, func(i, j int) bool {
		if resources[i].Kind != resources[j].Kind {
			return resources[i].Kind < resources[j].Kind
		}
		return resources[i].Name < resources[j].Name
	})
	raw, err := json.Marshal(resources)
	if err != nil {
		return nil, fmt.Errorf("encode registry resources: %w", err)
	}
	return raw, nil
}

// fetchArchiveBytes returns the archive bytes served for digest and the cache form
// they belong to. It requests the placeholder-form archive, which hashes to digest,
// and falls back to the final artifact route only when the Marketplace answers 404
// for the placeholder form.
func fetchArchiveBytes(ctx context.Context, client *Client, digest string) ([]byte, archiveForm, error) {
	raw, err := client.GetPlaceholderArchive(ctx, digest)
	if err == nil {
		return raw, placeholderForm, nil
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusNotFound {
		return nil, finalForm, err
	}
	raw, err = client.GetArtifact(ctx, digest)
	return raw, finalForm, err
}

// fetchMirrorArchive fetches the archive for digest and caches the bytes under the
// location matching their form. Placeholder bytes never reach the legacy path.
func fetchMirrorArchive(ctx context.Context, client *Client, cache *Cache, digest string) ([]byte, archiveForm, error) {
	raw, form, err := fetchArchiveBytes(ctx, client, digest)
	if err != nil {
		return nil, finalForm, fmt.Errorf("fetch artifact: %w", err)
	}
	if _, err := cache.writeArchive(form, digest, raw); err != nil {
		return nil, finalForm, fmt.Errorf("write artifact cache: %w", err)
	}
	return raw, form, nil
}

// cachedMirrorArchive returns the cached archive for digest. A verified placeholder-
// form copy is preferred over the legacy final-form copy. Without a verified copy,
// an unverified one is returned so the caller can refetch it.
func cachedMirrorArchive(cache *Cache, digest string) ([]byte, bool, archiveForm, error) {
	raw, verified, err := cache.readArchive(placeholderForm, digest)
	if err != nil {
		return nil, false, finalForm, err
	}
	if verified {
		return raw, true, placeholderForm, nil
	}
	legacy, legacyVerified, err := cache.readArchive(finalForm, digest)
	if err != nil {
		return nil, false, finalForm, err
	}
	if legacyVerified || legacy != nil {
		return legacy, legacyVerified, finalForm, nil
	}
	return raw, false, placeholderForm, nil
}

// ArchiveEncodingMismatchError reports a resource whose content is intact but
// whose archive this build cannot verify, because the Marketplace does not serve
// the placeholder form and the archive it serves was compressed differently.
type ArchiveEncodingMismatchError struct {
	Kind           string
	Name           string
	MarketplaceURL string
	Want           string
	Got            string
	Cached         bool
	cause          error
}

// archiveVerifyError returns the friendly diagnostic for an encoding mismatch and
// otherwise reports the verification failure with the given prefix.
func archiveVerifyError(cause error, kind, name, marketplaceURL string, cached bool) error {
	var mismatch *hccartifact.EncodingMismatchError
	if !errors.As(cause, &mismatch) {
		return fmt.Errorf("verify archive digest: %w", cause)
	}
	return &ArchiveEncodingMismatchError{
		Kind: kind, Name: name, MarketplaceURL: redact.URL(marketplaceURL),
		Want: mismatch.Want, Got: mismatch.Got, Cached: cached, cause: cause,
	}
}

func (e *ArchiveEncodingMismatchError) Error() string {
	if e.Cached {
		return fmt.Sprintf("%s %q from %s: cannot verify the cached copy of archive digest %s: it was cached by an agent-marketplace build with a different gzip encoder than this one (%s). The resource content itself is intact. To fix: run 'agent-marketplace sync' while online to replace the cached copy.",
			e.Kind, e.Name, e.MarketplaceURL, e.Want, runtime.Version())
	}
	return fmt.Sprintf("%s %q from %s: cannot verify archive digest %s. This Marketplace does not serve the placeholder archive for it yet, and the archive it does serve was compressed by a different gzip encoder than this agent-marketplace build (%s) uses (re-encoded %s). The resource content itself is intact. To fix: ask the Marketplace operator to upgrade the Marketplace (it backfills placeholder archives automatically), then run 'agent-marketplace sync'.",
		e.Kind, e.Name, e.MarketplaceURL, e.Want, runtime.Version(), e.Got)
}

// Remediation returns the recovery step shown for this diagnostic in the TUI.
func (e *ArchiveEncodingMismatchError) Remediation() string {
	if e.Cached {
		return "Run 'agent-marketplace sync' while online to replace the cached copy."
	}
	return "Ask the Marketplace operator to upgrade the Marketplace (it backfills placeholder archives automatically), then run 'agent-marketplace sync'."
}

func (e *ArchiveEncodingMismatchError) Unwrap() error { return e.cause }
