package ingest

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/inspection"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// spec: marketplace-analysis/ingest-persists-rejection-beside-unavailable-inspection-content
func TestBuildRegistryRevisionRejectsNonUTF8Support(t *testing.T) {
	for _, mode := range []store.AcquisitionMode{store.AcquisitionMirror, store.AcquisitionIndexOnly} {
		t.Run(string(mode), func(t *testing.T) {
			repo := createIngestRepo(t)
			data := []byte("# \x1b[31m reviewed \u202e\n# \xff\necho ok\n")
			filename := filepath.Join(repo, ".agents", "skills", "review", "run")
			if err := os.WriteFile(filename, data, 0o644); err != nil {
				t.Fatal(err)
			}
			commitAllIngestRepo(t, repo, "add invalid UTF-8 support")
			source := store.Registry{
				ID: "source", Slug: "source", Ref: "main", Origins: []string{"file://" + repo},
				Acquisition: mode, License: "MIT",
				Roots: []store.RegistryRoot{{Path: ".agents", SourceHarness: "factory"}},
			}
			cfg := testConfig(t, repo)
			cfg.Policy.HiddenContent = "warn"
			revision, err := BuildRegistryRevision(t.Context(), cfg, "marketplace", source, repo, mustRepoCommit(t, repo))
			if err != nil {
				t.Fatal(err)
			}
			assessment := resourceAssessmentFor(t, revision.Assessments, "skill")
			found := false
			for _, finding := range assessment.Findings {
				if finding.Code == "marketplace.find.invalid-utf8" && finding.Severity == "reject" &&
					finding.Policy == "reject" && finding.Location != nil && finding.Location.Path == "support/run" {
					found = true
				}
			}
			if assessment.Decision != store.AssessmentDecisionReject || !found {
				t.Errorf("unscanned support was not rejected: %+v", assessment)
			}
			resource := resourceRevisionFor(t, revision.Resources, "skill")
			if resource.ContentDigest == "" || assessment.ContentDigest != resource.ContentDigest {
				t.Fatal("rejection lost intrinsic resource identity")
			}
			var manifest inspection.Manifest
			for _, blob := range revision.InspectionBlobs {
				if string(blob.Digest) == string(assessment.InspectionManifestDigest) {
					if err := json.Unmarshal(blob.Data, &manifest); err != nil {
						t.Fatal(err)
					}
				}
			}
			listed := false
			for _, file := range manifest.Files {
				if file.Path != "support/run" {
					continue
				}
				listed = true
				if file.InspectionClass != inspection.ClassMetadata || file.ContentDigest != inspection.Digest(data) {
					t.Fatalf("unsafe inspection policy or changed support identity: %+v", file)
				}
				for _, blob := range revision.InspectionBlobs {
					if string(blob.Digest) == file.ContentDigest {
						t.Fatal("invalid UTF-8 inspection body was exposed")
					}
				}
			}
			if !listed {
				t.Fatal("inspection omitted the support file")
			}
			original, err := os.ReadFile(filename)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(original, data) {
				t.Fatal("ingest rewrote original support bytes")
			}
			if mode == store.AcquisitionMirror {
				verified := false
				for _, blob := range revision.ExtraBlobs {
					if blob.Digest != resource.ArchiveDigest {
						continue
					}
					artifact, err := hccartifact.VerifyArchiveBytes(blob.Data, string(blob.Digest))
					if err != nil {
						t.Fatal(err)
					}
					verified = true
					if !bytes.Equal(artifact.Support["run"].Data, data) {
						t.Fatal("archive changed original support bytes")
					}
				}
				if !verified {
					t.Fatal("rejection lost intrinsic archive")
				}
			} else if resource.ArchiveDigest != "" {
				t.Fatal("index-only ingestion published an archive")
			}
		})
	}
}
