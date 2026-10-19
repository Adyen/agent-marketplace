package artifact

import (
	"archive/tar"
	"bytes"
	"fmt"
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
			verify := map[string]func() error{
				"VerifyArchiveBytes": func() error {
					_, err := VerifyArchiveBytes(raw, ArchiveDigest(raw))
					return err
				},
				"DecodeContentArchive": func() error {
					_, err := DecodeContentArchive(raw)
					return err
				},
			}
			for name, run := range verify {
				err := run()
				if err == nil || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "link") {
					t.Errorf("%s: error = %v, want %q before the tripwire entry is decoded", name, err, tc.want)
				}
			}
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
	if _, err := VerifyArchiveBytes(placeholder, digest); err != nil {
		t.Fatalf("placeholder archive within the limits: %v", err)
	}
	if _, err := DecodeContentArchive(final); err != nil {
		t.Fatalf("final archive within the limits: %v", err)
	}
}
