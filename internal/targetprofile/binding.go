package targetprofile

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"
	"io/fs"
	"sort"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/nameutil"
)

type PackageFile struct {
	SourcePath      string
	DestinationPath string
	Mode            fs.FileMode
	Bytes           []byte
}

type Binding struct {
	PackageContentDigest  string   `json:"package_content_digest"`
	DeliveryMappingDigest string   `json:"delivery_mapping_digest"`
	ProfileDigest         string   `json:"profile_digest"`
	ProfileIdentity       Identity `json:"profile_identity"`
}

func BuildBinding(profile Identity, files []PackageFile) (Binding, error) {
	if err := profile.Validate(); err != nil {
		return Binding{}, fmt.Errorf("marketplace.package.binding-invalid: %w", err)
	}
	if len(files) == 0 {
		return Binding{}, fmt.Errorf("marketplace.package.binding-invalid: package file map is empty")
	}

	ordered := append([]PackageFile(nil), files...)
	sources := make(map[string]struct{}, len(ordered))
	destinations := make(map[string]struct{}, len(ordered))
	for _, file := range ordered {
		if !validProfileRelativePath(file.SourcePath) || !validProfileRelativePath(file.DestinationPath) {
			return Binding{}, fmt.Errorf("marketplace.package.binding-invalid: package paths must be clean UTF-8 relative paths")
		}
		if file.Mode != 0o644 && file.Mode != 0o755 {
			return Binding{}, fmt.Errorf("marketplace.package.binding-invalid: unsupported package file mode")
		}
		if _, ok := sources[file.SourcePath]; ok {
			return Binding{}, fmt.Errorf("marketplace.package.binding-invalid: duplicate package source path")
		}
		if _, ok := destinations[file.DestinationPath]; ok {
			return Binding{}, fmt.Errorf("marketplace.package.binding-invalid: duplicate package destination path")
		}
		sources[file.SourcePath] = struct{}{}
		destinations[file.DestinationPath] = struct{}{}
	}
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].SourcePath < ordered[j].SourcePath
	})

	content := sha256.New()
	writeBindingDomain(content, "agent-marketplace/host-package-content/v1")
	mapping := sha256.New()
	writeBindingDomain(mapping, "agent-marketplace/host-package-mapping/v1")
	for _, file := range ordered {
		source := []byte(file.SourcePath)
		writeBindingField(content, source)
		mode := byte(0)
		if file.Mode == 0o755 {
			mode = 1
		}
		writeBindingField(content, []byte{mode})
		writeBindingField(content, file.Bytes)

		writeBindingField(mapping, source)
		writeBindingField(mapping, []byte(file.DestinationPath))
	}

	return Binding{
		PackageContentDigest:  "sha256:" + hex.EncodeToString(content.Sum(nil)),
		DeliveryMappingDigest: "sha256:" + hex.EncodeToString(mapping.Sum(nil)),
		ProfileDigest:         profile.RevisionDigest,
		ProfileIdentity:       profile,
	}, nil
}

func (b Binding) ValidateFor(kind, contentDigest string) error {
	targetID, _, ok := nameutil.SplitPlaceKind(kind)
	if !ok || targetID != b.ProfileIdentity.TargetID {
		return fmt.Errorf("marketplace.package.binding-invalid: scoped kind does not match profile target")
	}
	if err := b.ProfileIdentity.Validate(); err != nil {
		return fmt.Errorf("marketplace.package.binding-invalid: %w", err)
	}
	for _, digest := range []string{b.PackageContentDigest, b.DeliveryMappingDigest, b.ProfileDigest, contentDigest} {
		if !nameutil.IsCanonicalSHA256Digest(digest) {
			return fmt.Errorf("marketplace.package.binding-invalid: lock.digest-invalid: invalid SHA-256 digest %q", digest)
		}
	}
	if b.ProfileDigest != b.ProfileIdentity.RevisionDigest {
		return fmt.Errorf("marketplace.package.binding-invalid: profile digest does not match profile identity")
	}
	if b.PackageContentDigest != contentDigest {
		return fmt.Errorf("marketplace.package.binding-invalid: package content digest does not match")
	}
	return nil
}

func (b Binding) MatchProfile(selected Identity) error {
	if err := b.ProfileIdentity.Validate(); err != nil {
		return fmt.Errorf("marketplace.package.binding-invalid: %w", err)
	}
	if err := selected.Validate(); err != nil {
		return fmt.Errorf("marketplace.package.binding-invalid: %w", err)
	}
	if b.ProfileIdentity != selected {
		return fmt.Errorf("marketplace.package.binding-invalid: selected profile identity does not match assessment")
	}
	return nil
}

func writeBindingDomain(h hash.Hash, domain string) {
	_, _ = h.Write([]byte(domain))
	_, _ = h.Write([]byte{0})
}

func writeBindingField(h hash.Hash, value []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = h.Write(length[:])
	_, _ = h.Write(value)
}
