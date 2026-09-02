package lockfile

import "fmt"

const APIVersionV1 = "agent-marketplace-lock/v1"

type Scope string

const (
	ScopeOrganization Scope = "organization"
	ScopeUser         Scope = "user"
	ScopeRepo         Scope = "repo"
	ScopeLocal        Scope = "local"
)

func (s Scope) Valid() bool {
	switch s {
	case ScopeOrganization, ScopeUser, ScopeRepo, ScopeLocal:
		return true
	default:
		return false
	}
}

func ParseScope(value string) (Scope, error) {
	s := Scope(value)
	if !s.Valid() {
		return "", coded(CodeInvalidScope, "invalid lock scope %q", value)
	}
	return s, nil
}

type Document struct {
	APIVersion   string        `yaml:"apiVersion"`
	Scope        Scope         `yaml:"scope"`
	Registries   []GitRegistry `yaml:"registries,omitempty"`
	Marketplaces []Marketplace `yaml:"marketplaces,omitempty"`
}

type GitRegistry struct {
	URL    string    `yaml:"url"`
	Ref    string    `yaml:"ref"`
	Commit GitOID    `yaml:"commit"`
	Roots  []GitRoot `yaml:"roots,omitempty"`
}

type GitRoot struct {
	Path           string        `yaml:"path"`
	SourceHarness  string        `yaml:"source_harness"`
	RevisionSchema string        `yaml:"revision_schema"`
	RevisionDigest Digest        `yaml:"revision_digest"`
	Resources      []GitResource `yaml:"resources,omitempty"`
}

type GitResource struct {
	Kind          string            `yaml:"kind"`
	Name          string            `yaml:"name"`
	ContentDigest Digest            `yaml:"content_digest"`
	ArchiveDigest Digest            `yaml:"archive_digest"`
	SupportFiles  map[string]Digest `yaml:"support_files,omitempty"`
}

type Marketplace struct {
	URL         string                  `yaml:"url"`
	InstanceID  string                  `yaml:"instance_id"`
	Collections []MarketplaceCollection `yaml:"collections,omitempty"`
	Resources   []MarketplaceResource   `yaml:"resources,omitempty"`
}

type MarketplaceCollection struct {
	SourceID string           `yaml:"source_id"`
	Name     string           `yaml:"name"`
	Revision Digest           `yaml:"revision"`
	Direct   bool             `yaml:"direct,omitempty"`
	Members  []CollectionEdge `yaml:"members,omitempty"`
}

type CollectionEdge struct {
	SourceID string `yaml:"source_id"`
	Kind     string `yaml:"kind,omitempty"`
	Name     string `yaml:"name"`
	Revision Digest `yaml:"revision"`
}

type Acquisition string

const (
	AcquisitionMirror    Acquisition = "mirror"
	AcquisitionIndexOnly Acquisition = "index-only"
)

type MarketplaceResource struct {
	SourceID             string           `yaml:"source_id"`
	Kind                 string           `yaml:"kind"`
	Name                 string           `yaml:"name"`
	Revision             Digest           `yaml:"revision"`
	Acquisition          Acquisition      `yaml:"acquisition"`
	ArchiveDigest        Digest           `yaml:"archive_digest,omitempty"`
	SourceCommit         GitOID           `yaml:"source_commit,omitempty"`
	SourceManifestDigest Digest           `yaml:"source_manifest_digest,omitempty"`
	Direct               bool             `yaml:"direct,omitempty"`
	SelectedBy           []CollectionEdge `yaml:"selected_by,omitempty"`
}

type Digest string

func ParseDigest(value string) (Digest, error) {
	if !validHexIdentifier(value, "sha256", 64) {
		return "", coded(CodeInvalidDigest, "invalid SHA-256 digest %q", value)
	}
	return Digest(value), nil
}

func (d Digest) String() string               { return string(d) }
func (d Digest) MarshalText() ([]byte, error) { return []byte(d), nil }
func (d *Digest) UnmarshalText(text []byte) error {
	parsed, err := ParseDigest(string(text))
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}

func require(value, field string) error {
	if value == "" {
		return coded(CodeInvalidDocument, "%s is required", field)
	}
	return nil
}

func validateDigest(d Digest, field string) error {
	if _, err := ParseDigest(string(d)); err != nil {
		return fmt.Errorf("%s: %w", field, err)
	}
	return nil
}
