package targetprofile

import (
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/nameutil"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/urlutil"
)

const identityInvalidCode = "marketplace.profile.identity-invalid"
const compiledTargetIDCode = "marketplace.profile.compiled-target-id"

type Identity struct {
	TargetID       string `json:"target_id"`
	SourceID       string `json:"source_id"`
	RevisionDigest string `json:"revision_digest"`
}

type Source struct {
	Kind       string
	Origin     string
	Ref        string
	Root       string
	InstanceID string
	RegistryID string
}

func (id Identity) Validate() error {
	invalid := func() error {
		return fmt.Errorf("%s: invalid identity for target %q", identityInvalidCode, id.TargetID)
	}
	if !nameutil.IsTargetIdentifier(id.TargetID) {
		return invalid()
	}
	if target.IsValid(id.TargetID) {
		return fmt.Errorf("%s: target %q is a compiled harness target", compiledTargetIDCode, id.TargetID)
	}
	if _, err := lockfile.ParseDigest(id.RevisionDigest); err != nil {
		return invalid()
	}
	if _, err := ParseSourceID(id.SourceID); err != nil {
		return invalid()
	}
	return nil
}

func ParseSourceID(raw string) (Source, error) {
	invalid := func() (Source, error) {
		return Source{}, fmt.Errorf("%s: invalid profile source identity", identityInvalidCode)
	}
	if raw == "builtin:agent-marketplace" {
		return Source{Kind: "builtin", Origin: "agent-marketplace"}, nil
	}
	if rest, ok := strings.CutPrefix(raw, "marketplace:"); ok {
		if strings.Count(rest, "#") != 1 {
			return invalid()
		}
		endpointRaw, fieldsRaw, _ := strings.Cut(rest, "#")
		fields := strings.Split(fieldsRaw, "/")
		if len(fields) != 3 {
			return invalid()
		}
		endpoint, ok := decodeComponent(endpointRaw)
		if !ok {
			return invalid()
		}
		instanceID, instanceOK := decodeComponent(fields[0])
		registryID, registryOK := decodeComponent(fields[1])
		root, rootOK := decodeComponent(fields[2])
		if !instanceOK || !registryOK || !rootOK || strings.ContainsAny(instanceID+registryID, "/") || !validRelativePath(root) {
			return invalid()
		}
		canonical, err := urlutil.NormalizeEndpointURL(endpoint)
		if err != nil || canonical != endpoint {
			return invalid()
		}
		return Source{Kind: "marketplace", Origin: endpoint, InstanceID: instanceID, RegistryID: registryID, Root: root}, nil
	}
	if rest, ok := strings.CutPrefix(raw, "git:"); ok {
		if strings.Count(rest, "#") != 1 {
			return invalid()
		}
		originRefRaw, rootRaw, _ := strings.Cut(rest, "#")
		if strings.Count(originRefRaw, "@") != 1 {
			return invalid()
		}
		originRaw, refRaw, _ := strings.Cut(originRefRaw, "@")
		origin, originOK := decodeComponent(originRaw)
		ref, refOK := decodeComponent(refRaw)
		root, rootOK := decodeComponent(rootRaw)
		if !originOK || !refOK || !rootOK || !validRelativePath(root) {
			return invalid()
		}
		canonical, err := lockfile.CanonicalGitURLV1(origin)
		if err != nil || canonical != origin {
			return invalid()
		}
		return Source{Kind: "git", Origin: origin, Ref: ref, Root: root}, nil
	}
	if rest, ok := strings.CutPrefix(raw, "directory:"); ok {
		root, ok := decodeComponent(rest)
		if !ok || !filepath.IsAbs(root) || filepath.Clean(root) != root {
			return invalid()
		}
		return Source{Kind: "directory", Root: root}, nil
	}
	return invalid()
}

func decodeComponent(raw string) (string, bool) {
	value, err := url.PathUnescape(raw)
	if err != nil || value == "" || !utf8.ValidString(value) || hasControl(value) || encodeComponent(value) != raw {
		return "", false
	}
	return value, true
}

func encodeComponent(value string) string {
	const hex = "0123456789ABCDEF"
	var encoded strings.Builder
	for _, b := range []byte(value) {
		if b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || b == '-' || b == '.' || b == '_' || b == '~' {
			encoded.WriteByte(b)
		} else {
			encoded.WriteByte('%')
			encoded.WriteByte(hex[b>>4])
			encoded.WriteByte(hex[b&0x0f])
		}
	}
	return encoded.String()
}

func hasControl(value string) bool {
	for _, r := range value {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func validRelativePath(value string) bool {
	cleaned := path.Clean(value)
	return value != "" && !path.IsAbs(value) && cleaned == value && cleaned != "." && cleaned != ".." && !strings.HasPrefix(cleaned, "../")
}
