package codemode

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/marketplace"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"golang.org/x/sys/unix"
)

const maxSkillFileBytes = 4 << 20

type FileSkillLoader struct {
	Cache *marketplace.Cache

	Online bool
}

func NewFileSkillLoader(online bool) *FileSkillLoader {
	return &FileSkillLoader{Cache: marketplace.DefaultCache(), Online: online}
}

func (l *FileSkillLoader) Load(ctx context.Context, res *Resource) (SkillContent, error) {
	if res.MarketplaceOrigin() != nil {
		return l.loadMarketplace(ctx, res)
	}
	o := res.Origin()
	if o == nil || o.ContentDir == "" {
		return SkillContent{}, CodeError(ErrCodeNotFound, "ref %q has no loadable content", res.Ref)
	}
	file := o.File
	if file == "" {
		file = "SKILL.md"
	}
	raw, err := readRooted(o.ContentDir, file)
	if err != nil {
		return SkillContent{}, CodeError(ErrCodeNotFound, "read %s: %v", file, err)
	}
	files, err := listSupportFiles(o.ContentDir, file)
	if err != nil {
		return SkillContent{}, err
	}
	sum := sha256.Sum256(raw)
	return SkillContent{
		Ref:      res.Ref,
		Revision: res.Revision,
		Digest:   "sha256:" + hex.EncodeToString(sum[:]),
		Content:  string(raw),
		Files:    files,
	}, nil
}

func (l *FileSkillLoader) Gate(ctx context.Context, res *Resource) error {
	if res.MarketplaceOrigin() == nil {
		return nil
	}
	_, _, err := l.verifiedArtifact(ctx, res.MarketplaceOrigin())
	return err
}

func matchRevisionIdentity(mo *MarketplaceOrigin, rev store.AssessedResourceRevision) error {
	c := rev.Coordinate
	if mo.Entry.InstanceID != "" && string(c.Marketplace) != mo.Entry.InstanceID {
		return CodeError(ErrCodeNotFound, "revision %q: marketplace %q does not match entry", mo.Revision, c.Marketplace)
	}
	if string(c.Registry) != mo.RegistryID || c.Kind != mo.Kind || c.Name != mo.Name || string(rev.ContentDigest) != mo.Revision {
		return CodeError(ErrCodeNotFound, "revision %q: cached identity does not match request", mo.Revision)
	}
	if err := rev.Assessment.Validate(); err != nil {
		return CodeError(ErrCodeNotFound, "revision %q: invalid assessment: %v", mo.Revision, err)
	}
	if rev.Assessment.Coordinate != rev.Coordinate || rev.Assessment.ContentDigest != rev.ContentDigest {
		return CodeError(ErrCodeNotFound, "revision %q: assessment does not match revision identity", mo.Revision)
	}
	return nil
}

func cleanRel(path string) (string, error) {
	if path == "" {
		return "", CodeError(ErrCodeNotFound, "empty path")
	}
	for _, seg := range strings.Split(filepath.ToSlash(path), "/") {
		if seg == ".." {
			return "", CodeError(ErrCodeNotFound, "path %q escapes the resource root", path)
		}
	}
	return filepath.Clean("/" + path)[1:], nil
}

func (l *FileSkillLoader) ReadFile(ctx context.Context, res *Resource, path string) (string, error) {
	clean, err := cleanRel(path)
	if err != nil {
		return "", err
	}
	if res.MarketplaceOrigin() != nil {
		return l.readMarketplace(ctx, res, clean)
	}
	o := res.Origin()
	if o == nil || o.ContentDir == "" {
		return "", CodeError(ErrCodeNotFound, "ref %q has no loadable content", res.Ref)
	}
	raw, err := readRooted(o.ContentDir, clean)
	if err != nil {
		return "", CodeError(ErrCodeNotFound, "read %s: %v", path, err)
	}
	return string(raw), nil
}

func (l *FileSkillLoader) loadMarketplace(ctx context.Context, res *Resource) (SkillContent, error) {
	mo := res.MarketplaceOrigin()
	ca, rev, err := l.verifiedArtifact(ctx, mo)
	if err != nil {
		return SkillContent{}, err
	}
	resource, err := hccartifact.DecodeResource(mo.Kind, ca.Payload)
	if err != nil {
		return SkillContent{}, CodeError(ErrCodeScriptError, "decode artifact: %v", err)
	}
	skill, ok := resource.(*ir.Skill)
	if !ok {
		return SkillContent{}, CodeError(ErrCodeUnsupportedKind, "artifact decoded to %T, not a skill", resource)
	}
	fileMap, err := marketplaceFiles(ca)
	if err != nil {
		return SkillContent{}, err
	}
	files := make([]string, 0, len(fileMap))
	for name := range fileMap {
		files = append(files, name)
	}
	sort.Strings(files)
	return SkillContent{
		Ref:      res.Ref,
		Revision: mo.Revision,
		Digest:   string(rev.ContentDigest),
		Content:  skill.Body,
		Files:    files,
		Findings: findingsOf(ca),
	}, nil
}

func (l *FileSkillLoader) readMarketplace(ctx context.Context, res *Resource, clean string) (string, error) {
	mo := res.MarketplaceOrigin()
	ca, _, err := l.verifiedArtifact(ctx, mo)
	if err != nil {
		return "", err
	}
	files, err := marketplaceFiles(ca)
	if err != nil {
		return "", err
	}
	raw, ok := files[filepath.ToSlash(clean)]
	if !ok {
		return "", CodeError(ErrCodeNotFound, "artifact has no file %q", clean)
	}
	return string(raw), nil
}

func marketplaceFiles(ca hccartifact.CanonicalArtifact) (map[string][]byte, error) {
	files := make(map[string][]byte, len(ca.Assets)+len(ca.Support))
	for name, data := range ca.Assets {
		files[name] = data
	}
	for name, support := range ca.Support {
		if _, dup := files[name]; dup {
			return nil, CodeError(ErrCodeNotFound, "artifact has ambiguous file %q", name)
		}
		files[name] = support.Data
	}
	return files, nil
}

func (l *FileSkillLoader) verifiedArtifact(ctx context.Context, mo *MarketplaceOrigin) (hccartifact.CanonicalArtifact, store.AssessedResourceRevision, error) {
	var rev store.AssessedResourceRevision
	if l.Online {
		client, cerr := marketplace.NewClient(mo.Entry.URL)
		if cerr != nil {
			return hccartifact.CanonicalArtifact{}, rev, CodeError(ErrCodeNotFound, "marketplace client: %v", cerr)
		}
		var err error
		rev, err = client.GetResourceRevisionForRegistry(ctx, mo.RegistryID, mo.RegistryID, mo.Kind, mo.Name, mo.Revision)
		if err != nil {
			return hccartifact.CanonicalArtifact{}, rev, CodeError(ErrCodeNotFound, "revision %q: %v", mo.Revision, err)
		}
	} else {
		cached, ok, err := l.Cache.ReadResourceRevision(mo.Entry.URL, mo.RegistryID, mo.Kind, mo.Name, mo.Revision)
		if !ok || err != nil {
			return hccartifact.CanonicalArtifact{}, cached, CodeError(ErrCodeNotFound, "revision %q not in cache (offline)", mo.Revision)
		}
		rev = cached
	}
	if err := matchRevisionIdentity(mo, rev); err != nil {
		return hccartifact.CanonicalArtifact{}, rev, err
	}
	if l.Online {
		if _, werr := l.Cache.WriteResourceRevision(mo.Entry.URL, mo.RegistryID, mo.Kind, mo.Name, mo.Revision, rev); werr != nil {
			return hccartifact.CanonicalArtifact{}, rev, CodeError(ErrCodeNotFound, "cache revision: %v", werr)
		}
	}
	switch rev.EffectiveAvailability {
	case "", "available":
	case "revoked":
		return hccartifact.CanonicalArtifact{}, rev, CodeError(ErrCodeRevoked, "%s is revoked", mo.Name)
	case "blocked", "policy-blocked", "operator-exposed":
		return hccartifact.CanonicalArtifact{}, rev, CodeError(ErrCodeRejected, "%s is rejected", mo.Name)
	default:
		return hccartifact.CanonicalArtifact{}, rev, CodeError(ErrCodeNotFound, "%s is unavailable (%s)", mo.Name, rev.EffectiveAvailability)
	}
	if rev.Revocation != nil {
		return hccartifact.CanonicalArtifact{}, rev, CodeError(ErrCodeRevoked, "%s is revoked", mo.Name)
	}
	if rev.Assessment.Decision == store.AssessmentDecisionReject {
		return hccartifact.CanonicalArtifact{}, rev, CodeError(ErrCodeRejected, "%s is rejected by assessment", mo.Name)
	}
	digest := string(rev.ArchiveDigest)
	raw, verified, err := l.Cache.ReadArtifact(digest)
	if err != nil || raw == nil || !verified {
		if !l.Online {
			return hccartifact.CanonicalArtifact{}, rev, CodeError(ErrCodeNotFound, "artifact %q not verified in cache (offline)", digest)
		}
		client, cerr := marketplace.NewClient(mo.Entry.URL)
		if cerr != nil {
			return hccartifact.CanonicalArtifact{}, rev, CodeError(ErrCodeNotFound, "marketplace client: %v", cerr)
		}
		raw, err = client.GetArtifact(ctx, digest)
		if err != nil {
			return hccartifact.CanonicalArtifact{}, rev, CodeError(ErrCodeNotFound, "fetch artifact: %v", err)
		}
		if _, werr := l.Cache.WriteArtifact(digest, raw); werr != nil {
			return hccartifact.CanonicalArtifact{}, rev, CodeError(ErrCodeNotFound, "cache artifact: %v", werr)
		}
	}
	ca, caRaw, err := hccartifact.Decode(strings.NewReader(string(raw)))
	if err != nil {
		return hccartifact.CanonicalArtifact{}, rev, CodeError(ErrCodeScriptError, "decode artifact: %v", err)
	}
	if err := ca.ValidateArchive(nil); err != nil {
		l.Cache.InvalidateArtifactVerified(digest)
		return hccartifact.CanonicalArtifact{}, rev, CodeError(ErrCodeScriptError, "validate artifact: %v", err)
	}
	if err := ca.VerifyArchiveDigest(caRaw); err != nil {
		l.Cache.InvalidateArtifactVerified(digest)
		return hccartifact.CanonicalArtifact{}, rev, CodeError(ErrCodeScriptError, "verify artifact digest: %v", err)
	}
	if err := ca.VerifyContentDigest(); err != nil {
		l.Cache.InvalidateArtifactVerified(digest)
		return hccartifact.CanonicalArtifact{}, rev, CodeError(ErrCodeScriptError, "verify content digest: %v", err)
	}
	if ca.Manifest.ArchiveDigest != digest {
		l.Cache.InvalidateArtifactVerified(digest)
		return hccartifact.CanonicalArtifact{}, rev, CodeError(ErrCodeScriptError, "archive digest %q does not match expected %q", ca.Manifest.ArchiveDigest, digest)
	}
	if ca.Manifest.ContentDigest != string(rev.ContentDigest) {
		l.Cache.InvalidateArtifactVerified(digest)
		return hccartifact.CanonicalArtifact{}, rev, CodeError(ErrCodeScriptError, "content digest %q does not match expected %q", ca.Manifest.ContentDigest, rev.ContentDigest)
	}
	_ = l.Cache.MarkArtifactVerified(digest)
	return ca, rev, nil
}

func (l *FileSkillLoader) ResolveServerDef(ctx context.Context, res *Resource) (*ServerDef, error) {
	mo := res.MarketplaceOrigin()
	if mo == nil {
		return nil, CodeError(ErrCodeStartFailed, "ref %q has no server definition", res.Ref)
	}
	ca, rev, err := l.verifiedArtifact(ctx, mo)
	if err != nil {
		return nil, err
	}
	resource, err := hccartifact.DecodeResource(mo.Kind, ca.Payload)
	if err != nil {
		return nil, CodeError(ErrCodeStartFailed, "decode artifact: %v", err)
	}
	srv, ok := resource.(*ir.McpServer)
	if !ok {
		return nil, CodeError(ErrCodeStartFailed, "artifact decoded to %T, not an MCP server", resource)
	}
	return &ServerDef{
		Command: srv.Command, Args: srv.Args, Env: srv.Env,
		URL: srv.URL, Headers: srv.Headers,
		Digest:        string(rev.ArchiveDigest),
		ContentDigest: string(rev.ContentDigest),
	}, nil
}

func findingsOf(ca hccartifact.CanonicalArtifact) []Finding {
	if len(ca.Manifest.Findings) == 0 {
		return nil
	}
	out := make([]Finding, 0, len(ca.Manifest.Findings))
	for _, f := range ca.Manifest.Findings {
		out = append(out, Finding{Rule: f.Code, Severity: f.Severity, Message: f.Message})
	}
	return out
}

func readRooted(root, rel string) ([]byte, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if resolved, rerr := filepath.EvalSymlinks(rootAbs); rerr == nil {
		rootAbs = resolved
	}
	fd, err := unix.Open(rootAbs, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	f, err := openRootedFile(fd, rel)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxSkillFileBytes {
		return nil, fmt.Errorf("file too large or not regular")
	}
	buf := make([]byte, info.Size())
	if _, err := f.ReadAt(buf, 0); err != nil {
		return nil, err
	}
	return buf, nil
}

func openRootedFile(rootFD int, rel string) (*os.File, error) {
	defer func() { _ = unix.Close(rootFD) }()
	segs := strings.Split(filepath.ToSlash(rel), "/")
	last := -1
	for i, seg := range segs {
		if seg != "" && seg != "." {
			last = i
		}
	}
	if last < 0 {
		return nil, fmt.Errorf("empty path")
	}
	fd := rootFD
	for i, seg := range segs {
		if seg == "" || seg == "." {
			continue
		}
		flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW
		if i != last {
			flags |= unix.O_DIRECTORY
		}
		next, err := unix.Openat(fd, seg, flags, 0)
		if err != nil {
			if fd != rootFD {
				_ = unix.Close(fd)
			}
			return nil, err
		}
		if fd != rootFD {
			_ = unix.Close(fd)
		}
		fd = next
	}
	return os.NewFile(uintptr(fd), segs[last]), nil
}

func listSupportFiles(dir, primary string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		if rel != primary && len(files) < 512 {
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		return nil, CodeError(ErrCodeNotFound, "list files: %v", err)
	}
	sort.Strings(files)
	return files, nil
}
