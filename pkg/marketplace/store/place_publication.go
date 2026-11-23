package store

import (
	"fmt"

	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/nameutil"
)

func validatePlaceArchiveBindings(req RegistryRevisionPublication, uploads []BlobUpload, seen map[ArchiveDigest]int, placeholders map[ArchiveDigest]hccartifact.CanonicalArtifact) error {
	for _, rev := range req.Resources {
		_, _, scoped := nameutil.SplitPlaceKind(rev.Coordinate.Kind)
		if !scoped {
			continue
		}
		if rev.ArchiveDigest == "" {
			return fmt.Errorf("place resource %s has no archive digest", rev.Coordinate)
		}
		i, ok := seen[rev.ArchiveDigest]
		if !ok || i < 0 || i >= len(uploads) {
			return fmt.Errorf("place resource %s final archive is missing from uploads", rev.Coordinate)
		}

		var manifest hccartifact.CanonicalArtifact
		var err error
		if placeholder, ok := placeholders[rev.ArchiveDigest]; ok {
			manifest, err = hccartifact.VerifyFinalAgainstPlaceholder(uploads[i].Data, placeholder)
		} else {
			if contentDigest(uploads[i].Data) == string(rev.ArchiveDigest) {
				return fmt.Errorf("place resource %s final archive is a placeholder", rev.Coordinate)
			}
			manifest, err = hccartifact.VerifyArchiveBytes(uploads[i].Data, string(rev.ArchiveDigest))
		}
		if err != nil {
			return fmt.Errorf("verify place archive for %s: %w", rev.Coordinate, err)
		}
		if manifest.Manifest.Kind != rev.Coordinate.Kind ||
			manifest.Manifest.Name != rev.Coordinate.Name ||
			manifest.Manifest.ContentDigest != string(rev.ContentDigest) {
			return fmt.Errorf("place archive does not match resource revision %s", rev.Coordinate)
		}
		if manifest.Manifest.RegistryID != "" && manifest.Manifest.RegistryID != string(rev.Coordinate.Registry) {
			return fmt.Errorf("place archive registry does not match resource revision %s", rev.Coordinate)
		}

		var match *ResourceAssessment
		for i := range req.Assessments {
			assessment := &req.Assessments[i]
			if assessment.Coordinate != rev.Coordinate || assessment.ContentDigest != rev.ContentDigest {
				continue
			}
			if match != nil {
				return fmt.Errorf("place resource %s has multiple matching assessments", rev.Coordinate)
			}
			match = assessment
		}
		if match == nil {
			return fmt.Errorf("place resource %s has no matching assessment", rev.Coordinate)
		}
		if match.HostPackageIdentity == nil || manifest.Manifest.HostPackageIdentity == nil {
			return fmt.Errorf("place resource %s is missing a host package identity", rev.Coordinate)
		}
		if *match.HostPackageIdentity != *manifest.Manifest.HostPackageIdentity {
			return fmt.Errorf("place resource %s assessment binding does not match archive", rev.Coordinate)
		}
	}
	return nil
}
