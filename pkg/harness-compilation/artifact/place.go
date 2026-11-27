package artifact

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/targetprofile"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/nameutil"
)

type placePayload struct {
	Name  string             `json:"name"`
	Files []placePayloadFile `json:"files"`
}

type placePayloadFile struct {
	SourcePath      string      `json:"source_path"`
	DestinationPath string      `json:"destination_path"`
	Mode            fs.FileMode `json:"mode"`
	Bytes           []byte      `json:"bytes"`
}

func NewPlaceArtifact(kind, name string, profile targetprofile.Identity, files []targetprofile.PackageFile) (CanonicalArtifact, error) {
	binding, err := targetprofile.BuildBinding(profile, files)
	if err != nil {
		return CanonicalArtifact{}, err
	}
	payload, err := marshalPlacePayload(name, files)
	if err != nil {
		return CanonicalArtifact{}, fmt.Errorf("marshal place package payload: %w", err)
	}

	a := CanonicalArtifact{
		Manifest: CanonicalManifest{
			SchemaVersion:   CanonicalArtifactSchemaV1,
			ArtifactVersion: CanonicalArtifactSchemaV1,
			Kind:            kind,
			Name:            name,
			ContentDigest:   binding.PackageContentDigest,
			Provenance: CanonicalProvenance{
				Generator: "agent-marketplace canonical artifact v1",
			},
			SupportedSchemas:    []string{CanonicalArtifactSchemaV1},
			HostPackageIdentity: &binding,
		},
		Payload: payload,
	}
	if err := a.Manifest.Validate(); err != nil {
		return CanonicalArtifact{}, fmt.Errorf("invalid place manifest: %w", err)
	}
	if err := a.validateLimits(DefaultArchiveLimits()); err != nil {
		return CanonicalArtifact{}, err
	}
	return a, nil
}

func (a CanonicalArtifact) PlaceFiles() ([]targetprofile.PackageFile, error) {
	if _, _, scoped := nameutil.SplitPlaceKind(a.Manifest.Kind); !scoped {
		return nil, errors.New("place files require a scoped resource kind")
	}
	if !nameutil.IsPlacePackageName(a.Manifest.Name) {
		return nil, fmt.Errorf("invalid place package name %q", a.Manifest.Name)
	}
	if a.Manifest.HostPackageIdentity == nil {
		return nil, errors.New("place manifest host_package_identity is required")
	}
	if err := a.Manifest.HostPackageIdentity.ValidateFor(a.Manifest.Kind, a.Manifest.ContentDigest); err != nil {
		return nil, fmt.Errorf("invalid place package identity: %w", err)
	}
	if len(a.Assets) != 0 || len(a.Licenses) != 0 || len(a.Support) != 0 {
		return nil, errors.New("place package archive contains unsupported members")
	}
	if int64(len(a.Payload)) > DefaultArchiveLimits().MaxPayloadBytes {
		return nil, fmt.Errorf("place package payload exceeds max bytes %d", DefaultArchiveLimits().MaxPayloadBytes)
	}

	var payload placePayload
	if err := json.Unmarshal(a.Payload, &payload); err != nil {
		return nil, fmt.Errorf("decode place package payload: %w", err)
	}
	if payload.Name != a.Manifest.Name {
		return nil, errors.New("place package payload name does not match manifest")
	}
	files := make([]targetprofile.PackageFile, len(payload.Files))
	for i, file := range payload.Files {
		files[i] = targetprofile.PackageFile{
			SourcePath:      file.SourcePath,
			DestinationPath: file.DestinationPath,
			Mode:            file.Mode,
			Bytes:           file.Bytes,
		}
	}
	binding, err := targetprofile.BuildBinding(a.Manifest.HostPackageIdentity.ProfileIdentity, files)
	if err != nil {
		return nil, fmt.Errorf("validate place package files: %w", err)
	}
	if binding != *a.Manifest.HostPackageIdentity {
		return nil, errors.New("place package identity does not match payload")
	}
	canonical, err := marshalPlacePayload(a.Manifest.Name, files)
	if err != nil {
		return nil, fmt.Errorf("marshal place package payload: %w", err)
	}
	if !bytes.Equal(canonical, a.Payload) {
		return nil, errors.New("place package payload is not canonical")
	}
	return files, nil
}

func marshalPlacePayload(name string, files []targetprofile.PackageFile) ([]byte, error) {
	payload := placePayload{
		Name:  name,
		Files: make([]placePayloadFile, len(files)),
	}
	for i, file := range files {
		data := file.Bytes
		if data == nil {
			data = []byte{}
		}
		payload.Files[i] = placePayloadFile{
			SourcePath:      file.SourcePath,
			DestinationPath: file.DestinationPath,
			Mode:            file.Mode,
			Bytes:           data,
		}
	}
	slices.SortFunc(payload.Files, func(a, b placePayloadFile) int {
		return strings.Compare(a.SourcePath, b.SourcePath)
	})
	return json.Marshal(payload)
}
