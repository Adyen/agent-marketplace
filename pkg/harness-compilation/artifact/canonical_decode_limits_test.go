package artifact

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func writeLimitTripwire(t *testing.T, tw *tar.Writer) {
	t.Helper()
	if err := tw.WriteHeader(&tar.Header{Name: "tripwire", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd", Mode: 0o777}); err != nil {
		t.Fatal(err)
	}
}

func writeLimitMember(t *testing.T, tw *tar.Writer, name string, size int) {
	t.Helper()
	writeLimitMemberData(t, tw, name, make([]byte, size))
}

func writeLimitMemberData(t *testing.T, tw *tar.Writer, name string, data []byte) {
	t.Helper()
	if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(data))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(data); err != nil {
		t.Fatal(err)
	}
}

// requireArchiveLimitRefusals checks every verification path refuses raw with
// the named limit. A refusal identified as ErrArchiveLimitExceeded proves the
// tripwire entry after the limit was never decoded.
func requireArchiveLimitRefusals(t *testing.T, raw []byte, want string) {
	t.Helper()
	verify := map[string]func() error{
		"VerifyArchiveBytes/placeholder": func() error {
			_, err := VerifyArchiveBytes(raw, ArchiveDigest(raw))
			return err
		},
		"VerifyArchiveBytes/fallback": func() error {
			_, err := VerifyArchiveBytes(raw, archiveDigestPlaceholder)
			return err
		},
		"VerifyFinalAgainstPlaceholder": func() error {
			_, err := VerifyFinalAgainstPlaceholder(raw, CanonicalArtifact{})
			return err
		},
		"DecodeContentArchive": func() error {
			_, err := DecodeContentArchive(raw)
			return err
		},
	}
	for name, run := range verify {
		if err := run(); !errors.Is(err, ErrArchiveLimitExceeded) || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: error = %v, want %v naming %q", name, err, ErrArchiveLimitExceeded, want)
		}
	}
}

// spec: marketplace-api/verified-archive-decoding-stops-at-the-archive-limits
func TestVerifiedArchiveDecodingStopsAtTheArchiveLimits(t *testing.T) {
	limits := DefaultArchiveLimits()
	for _, tc := range []struct {
		name    string
		members func(t *testing.T, tw *tar.Writer)
		want    string
	}{
		{
			name: "total decoded bytes",
			members: func(t *testing.T, tw *tar.Writer) {
				for i := range int(limits.MaxTotalBytes/limits.MaxAssetBytes) + 8 {
					writeLimitMember(t, tw, fmt.Sprintf("%s/a%03d", CanonicalAssetsDir, i), int(limits.MaxAssetBytes))
				}
			},
			want: "total archive size",
		},
		{
			name: "asset file count",
			members: func(t *testing.T, tw *tar.Writer) {
				for i := range int(limits.MaxAssetFiles) + 8 {
					writeLimitMember(t, tw, fmt.Sprintf("%s/a%03d", CanonicalAssetsDir, i), 1)
				}
			},
			want: "too many asset files",
		},
		{
			name: "support file count",
			members: func(t *testing.T, tw *tar.Writer) {
				for i := range int(limits.MaxAssetFiles) + 8 {
					writeLimitMember(t, tw, fmt.Sprintf("%s/s%03d", CanonicalSupportDir, i), 1)
				}
			},
			want: "too many support files",
		},
		{
			name: "total file count",
			members: func(t *testing.T, tw *tar.Writer) {
				for i := range int(limits.MaxTotalFiles) + 1 {
					writeLimitMember(t, tw, fmt.Sprintf("%s/l%04d", CanonicalLicensesDir, i), 1)
				}
			},
			want: "total files exceed",
		},
		{
			name: "manifest bytes toward the total",
			members: func(t *testing.T, tw *tar.Writer) {
				writeLimitMemberData(t, tw, CanonicalManifestPath, append([]byte("{}"), bytes.Repeat([]byte{' '}, 4094)...))
				count := int(limits.MaxTotalBytes / limits.MaxAssetBytes)
				for i := range count - 1 {
					writeLimitMember(t, tw, fmt.Sprintf("unknown/m%03d", i), int(limits.MaxAssetBytes))
				}
				writeLimitMember(t, tw, "unknown/last", int(limits.MaxAssetBytes)-2048)
			},
			want: "total archive size",
		},
		{
			name: "repeated manifest",
			members: func(t *testing.T, tw *tar.Writer) {
				writeLimitMemberData(t, tw, CanonicalManifestPath, []byte("{}"))
				writeLimitMemberData(t, tw, CanonicalManifestPath, []byte("{}"))
			},
			want: "duplicate manifest member",
		},
		{
			name: "entry count",
			members: func(t *testing.T, tw *tar.Writer) {
				for i := range int(limits.MaxTotalFiles) + 64 {
					if err := tw.WriteHeader(&tar.Header{Name: fmt.Sprintf("d%05d/", i), Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
						t.Fatal(err)
					}
				}
			},
			want: "too many archive entries",
		},
		{
			name: "member size",
			members: func(t *testing.T, tw *tar.Writer) {
				writeLimitMember(t, tw, CanonicalAssetsDir+"/large", int(limits.MaxAssetBytes)+1)
			},
			want: "exceeds max bytes",
		},
		{
			name: "ignored member bytes",
			members: func(t *testing.T, tw *tar.Writer) {
				for i := range int(limits.MaxTotalBytes/limits.MaxAssetBytes) + 8 {
					writeLimitMember(t, tw, fmt.Sprintf("unknown/m%03d", i), int(limits.MaxAssetBytes))
				}
			},
			want: "total archive size",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := writeTarGz(t, func(tw *tar.Writer) {
				tc.members(t, tw)
				writeLimitTripwire(t, tw)
			})
			requireArchiveLimitRefusals(t, raw, tc.want)
		})
	}
}

// spec: marketplace-api/verified-archive-decoding-stops-at-the-archive-limits
func TestVerifiedArchiveDecodingAcceptsArchivesWithinTheLimits(t *testing.T) {
	limits := DefaultArchiveLimits()
	a, err := FromIR(ir.NewSkill("limits-control", "control", "body", nil, nil, ir.SourceRef{}))
	if err != nil {
		t.Fatal(err)
	}
	a.Assets = map[string][]byte{}
	for i := range int(limits.MaxAssetFiles) {
		a.Assets[fmt.Sprintf("a%03d", i)] = bytes.Repeat([]byte{'x'}, 1024)
	}
	a.Assets["largest"] = bytes.Repeat([]byte{'y'}, int(limits.MaxAssetBytes))
	delete(a.Assets, "a000")
	final, placeholder, digest, err := EncodeArchives(a)
	if err != nil {
		t.Fatal(err)
	}
	verifiedPlaceholder, err := VerifyArchiveBytes(placeholder, digest)
	if err != nil {
		t.Fatalf("placeholder archive within the limits: %v", err)
	}
	if _, err := VerifyArchiveBytes(final, digest); err != nil {
		t.Fatalf("final archive within the limits through the fallback: %v", err)
	}
	if _, err := VerifyFinalAgainstPlaceholder(final, verifiedPlaceholder); err != nil {
		t.Fatalf("final archive within the limits against its placeholder: %v", err)
	}
	if _, err := DecodeContentArchive(final); err != nil {
		t.Fatalf("final archive within the limits: %v", err)
	}
}

// spec: marketplace-api/verified-archive-decoding-stops-at-the-archive-limits
func TestVerifiedArchiveDecodingRefusesADeclaredSizeThatWouldOverflowTheTotal(t *testing.T) {
	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	writeLimitMember(t, tw, "unknown/first", 1024)
	if err := tw.WriteHeader(&tar.Header{Name: "unknown/huge", Typeflag: tar.TypeReg, Mode: 0o644, Size: math.MaxInt64 - 512}); err != nil {
		t.Fatal(err)
	}
	var raw bytes.Buffer
	gw := gzip.NewWriter(&raw)
	if _, err := gw.Write(archive.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	requireArchiveLimitRefusals(t, raw.Bytes(), "total archive size")
}

func TestDecodeBudgetRefusesNegativeMemberSizes(t *testing.T) {
	budget := &decodeBudget{limits: DefaultArchiveLimits()}
	if err := budget.admit("", -1); !errors.Is(err, ErrArchiveLimitExceeded) {
		t.Fatalf("negative member size error = %v, want %v", err, ErrArchiveLimitExceeded)
	}
}
