package session

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

const (
	marketplaceArtifactName   = "artifact.tar.gz"
	marketplaceVerifiedMarker = "verified.marker"

	// maxMarketplaceArtifactBytes bounds the compressed archive read for a
	// detail preview. It matches the canonical artifact total-size budget so
	// a corrupt or hostile cache object cannot exhaust TUI memory.
	maxMarketplaceArtifactBytes = 64 << 20
)

func ReadVerifiedMarketplaceArtifact(artifactPath string) (hccartifact.CanonicalArtifact, error) {
	info, err := os.Lstat(artifactPath)
	if err != nil {
		return hccartifact.CanonicalArtifact{}, errors.New("marketplace artifact is unavailable")
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return hccartifact.CanonicalArtifact{}, fmt.Errorf("marketplace artifact is not a regular file")
	}
	marker, err := os.Lstat(filepath.Join(filepath.Dir(artifactPath), marketplaceVerifiedMarker))
	if err != nil {
		return hccartifact.CanonicalArtifact{}, errors.New("marketplace artifact is not verified")
	}
	if marker.Mode()&os.ModeSymlink != 0 || !marker.Mode().IsRegular() {
		return hccartifact.CanonicalArtifact{}, fmt.Errorf("marketplace artifact verification marker is not a regular file")
	}
	f, err := os.OpenFile(artifactPath, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return hccartifact.CanonicalArtifact{}, errors.New("marketplace artifact is unavailable")
	}
	defer func() { _ = f.Close() }()
	opened, err := f.Stat()
	if err != nil {
		return hccartifact.CanonicalArtifact{}, errors.New("marketplace artifact is unavailable")
	}
	if !opened.Mode().IsRegular() {
		return hccartifact.CanonicalArtifact{}, fmt.Errorf("marketplace artifact is not a regular file")
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxMarketplaceArtifactBytes+1))
	if err != nil {
		return hccartifact.CanonicalArtifact{}, errors.New("marketplace artifact is unavailable")
	}
	if len(raw) > maxMarketplaceArtifactBytes {
		return hccartifact.CanonicalArtifact{}, fmt.Errorf("marketplace artifact exceeds %d MiB", maxMarketplaceArtifactBytes>>20)
	}
	// This reader only renders a cache file that the verified marker already
	// covers, so content checks suffice; its archive digest is not re-derived here.
	canonical, err := hccartifact.DecodeContentArchive(raw)
	if err != nil {
		return hccartifact.CanonicalArtifact{}, errors.New("marketplace artifact is invalid")
	}
	return canonical, nil
}

func CompileSkillMarkdown(resource ir.Resource) ([]byte, error) {
	result, err := hcc.Compile(resource, hcc.TargetFactory)
	if err != nil {
		return nil, err
	}
	for _, compiled := range result.Artifacts {
		if filepath.Base(compiled.Path) == "SKILL.md" {
			return compiled.Bytes, nil
		}
	}
	return nil, fmt.Errorf("skill compilation produced no Markdown artifact")
}
