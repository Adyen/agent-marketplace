package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"pgregory.net/rapid"
)

func TestV4ProcessingFixturesPassFileAdmissionWithoutWrites(t *testing.T) {
	for _, count := range []int{1, 32, 128, 512} {
		_, doc := v4LockChainFixture(t, count)
		data, err := lockfile.Encode(doc)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := lockfile.Decode(data); err != nil {
			t.Fatalf("%d-resource fixture fails real decoder admission: %v", count, err)
		}
		if count != 512 {
			continue
		}
		dir := t.TempDir()
		configPath, lockPath := filepath.Join(dir, "config.yaml"), filepath.Join(dir, "lock.yaml")
		configData := []byte("apiVersion: agent-marketplace/v4\nsources: {m: {marketplace: https://market.example}}\nresources:\n  m:\n    include: [tools/collection/node-0511]\n")
		for path, content := range map[string][]byte{configPath: configData, lockPath: data} {
			if err := os.WriteFile(path, content, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		beforeConfig, err := os.Stat(configPath)
		if err != nil {
			t.Fatal(err)
		}
		beforeLock, err := os.Stat(lockPath)
		if err != nil {
			t.Fatal(err)
		}
		for range 2 {
			cfg, err := LoadConfigFromPath(configPath)
			if err != nil || len(cfg.Marketplaces) != 1 || len(cfg.Marketplaces[0].Resources) != count {
				t.Fatalf("file loader failed to hydrate %d pins: %v", count, err)
			}
		}
		for path, expected := range map[string]struct {
			data []byte
			info os.FileInfo
		}{configPath: {configData, beforeConfig}, lockPath: {data, beforeLock}} {
			got, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(got, expected.data) {
				t.Fatalf("loader changed bytes at %s: %v", path, err)
			}
			info, err := os.Stat(path)
			if err != nil || info.Mode() != expected.info.Mode() || !info.ModTime().Equal(expected.info.ModTime()) {
				t.Fatalf("loader changed metadata at %s: %v", path, err)
			}
		}
	}
}

func TestV4MarketplaceReachabilityCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		leaf, stale, orphan       bool
		projection, direct, cycle bool
		wantError                 bool
		wantEdges                 int
	}{
		{name: "reverse-chain"},
		{name: "reachable-cycle", cycle: true},
		{name: "shared-leaf", leaf: true, wantEdges: 2},
		{name: "direct-and-derived", leaf: true, direct: true, wantEdges: 2},
		{name: "unreachable-orphan", leaf: true, orphan: true, wantError: true},
		{name: "projection-orphan", leaf: true, orphan: true, projection: true},
		{name: "stale-parent", leaf: true, stale: true, orphan: true, wantError: true},
		{name: "projection-stale-parent", leaf: true, stale: true, orphan: true, projection: true},
		{name: "strict-mixed-edges", leaf: true, stale: true, wantEdges: 3},
		{name: "projection-mixed-edges", leaf: true, stale: true, projection: true, wantEdges: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, doc := v4LockChainFixture(t, 4)
			mp := doc.Marketplaces[0]
			if tc.cycle {
				mp.Resources[3].SelectedBy = []lockfile.MarketplaceSelectionLock{{RegistryID: "tools-id", Name: mp.Resources[0].Name, Revision: mp.Resources[0].ContentDigest}}
			}
			group, src := cfg.Groups["m"], cfg.Sources["m"]
			if tc.leaf {
				leaf := v4LockResource("tools", "tools-id", "skill", "shared", v4sha("a"), v4sha("a"))
				leaf.ArchiveDigest = lockfile.Digest(v4sha("b"))
				if !tc.orphan {
					for _, parent := range []int{0, 2} {
						leaf.SelectedBy = append(leaf.SelectedBy, lockfile.MarketplaceSelectionLock{RegistryID: "tools-id", Name: mp.Resources[parent].Name, Revision: mp.Resources[parent].ContentDigest})
					}
				}
				if tc.stale {
					leaf.SelectedBy = append(leaf.SelectedBy, lockfile.MarketplaceSelectionLock{RegistryID: "tools-id", Name: mp.Resources[3].Name, Revision: lockfile.Digest(v4sha("f"))})
				}
				mp.Resources = append(mp.Resources, leaf)
				if tc.direct {
					request := &V4ResourceRequest{Locator: "tools/skill/shared", Registry: "tools", Kind: "skill", Name: "shared", Options: V4ResourceOptions{As: "local-shared", HasAs: true}}
					group.Decisions = append(group.Decisions, V4SelectionDecision{Action: V4SelectionExplicit, Locator: request.Locator, Request: request})
				}
			}
			before, _ := json.Marshal([]any{mp, group})
			entry, err := cfg.marketplaceEntryForLock("m", src, group, mp, repoLayer, tc.projection)
			if tc.wantError {
				var validation *V4ValidationError
				if !errors.As(err, &validation) || validation.Code != v4MarketplaceLockMismatchCode || validation.Path != "resources.m" || validation.Line != src.Line || !strings.Contains(validation.Msg, "tools-id/skill/shared") || entry != nil {
					t.Fatalf("entry/error = %#v/%v, want typed resource mismatch", entry, err)
				}
			} else {
				wantCount := 4
				if tc.leaf && !tc.orphan {
					wantCount++
				}
				if err != nil || len(entry.Resources) != wantCount {
					t.Fatalf("entry/error = %#v/%v, want %d pins", entry, err, wantCount)
				}
				for i, pin := range entry.Resources {
					if pin.Name != mp.Resources[i].Name || pin.Digest != string(mp.Resources[i].ContentDigest) || pin.Direct != (i == 3 || tc.direct && i == 4) {
						t.Fatalf("pin[%d] lost order, identity or direct status: %#v", i, pin)
					}
				}
				if wantCount == 5 {
					pin := entry.Resources[4]
					if len(pin.SelectedBy.List()) != tc.wantEdges || pin.LockArchiveDigest != v4sha("b") || tc.direct && pin.InstalledName != "local-shared" {
						t.Fatalf("shared pin = %#v, want %d edges and preserved metadata", pin, tc.wantEdges)
					}
				}
			}
			after, _ := json.Marshal([]any{mp, group})
			if !bytes.Equal(before, after) {
				t.Fatal("hydration mutated lock or decisions")
			}
		})
	}
}

func TestV4MarketplaceReachabilityMatchesScan(t *testing.T) {
	cfg, doc := v4LockChainFixture(t, 1)
	rapid.Check(t, func(t *rapid.T) {
		count := rapid.IntRange(1, 33).Draw(t, "resources")
		mp := doc.Marketplaces[0]
		mp.Resources = make([]lockfile.MarketplaceResourceLock, count)
		for i := range mp.Resources {
			mp.Resources[i] = doc.Marketplaces[0].Resources[0]
			mp.Resources[i].Name = fmt.Sprintf("node-%04d", i)
		}
		for i := 1; i < count; i++ {
			parent := rapid.IntRange(0, count-1).Draw(t, fmt.Sprintf("parent-%d", i))
			mp.Resources[i].SelectedBy = []lockfile.MarketplaceSelectionLock{{RegistryID: "tools-id", Name: mp.Resources[parent].Name, Revision: mp.Resources[parent].ContentDigest}}
			if rapid.Bool().Draw(t, fmt.Sprintf("stale-%d", i)) {
				mp.Resources[i].SelectedBy = append(mp.Resources[i].SelectedBy, lockfile.MarketplaceSelectionLock{RegistryID: "tools-id", Name: mp.Resources[parent].Name, Revision: lockfile.Digest(v4sha("f"))})
			}
		}
		before, _ := json.Marshal(mp)
		want := scanV4Reachability(mp, 0)
		entry, err := cfg.marketplaceEntryForLock("m", cfg.Sources["m"], cfg.Groups["m"], mp, repoLayer, true)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for i, reachable := range want {
			if !reachable {
				continue
			}
			names = append(names, mp.Resources[i].Name)
		}
		var got []string
		for _, pin := range entry.Resources {
			got = append(got, pin.Name)
		}
		if !reflect.DeepEqual(got, names) {
			t.Fatalf("graph=%#v: pins=%v, want %v", mp.Resources, got, names)
		}
		after, _ := json.Marshal(mp)
		if !bytes.Equal(before, after) {
			t.Fatal("hydration mutated graph")
		}
		_, err = cfg.marketplaceEntryForLock("m", cfg.Sources["m"], cfg.Groups["m"], mp, repoLayer, false)
		for i, reachable := range want {
			if !reachable {
				var validation *V4ValidationError
				if !errors.As(err, &validation) || validation.Code != v4MarketplaceLockMismatchCode || validation.Path != "resources.m" || validation.Line != cfg.Sources["m"].Line || !strings.Contains(validation.Msg, "tools-id/collection/"+mp.Resources[i].Name) {
					t.Fatalf("strict error = %v, want typed mismatch for first unreachable resource", err)
				}
				return
			}
		}
		if err != nil {
			t.Fatal(err)
		}
	})
}

// scanV4Reachability pins the original fixed-point walk as an oracle.
func scanV4Reachability(mp lockfile.MarketplaceLock, seed int) []bool {
	collections := make(map[string]int)
	for i := range mp.Resources {
		if mp.Resources[i].Kind == "collection" {
			r := &mp.Resources[i]
			collections[r.RegistryID+"\x00"+r.Name] = i
		}
	}
	reachable := make([]bool, len(mp.Resources))
	reachable[seed] = true
	for changed := true; changed; {
		changed = false
		for i := range mp.Resources {
			if reachable[i] {
				continue
			}
			for _, edge := range mp.Resources[i].SelectedBy {
				parent, ok := collections[edge.RegistryID+"\x00"+edge.Name]
				if ok && reachable[parent] && mp.Resources[parent].ContentDigest == edge.Revision {
					reachable[i] = true
					changed = true
					break
				}
			}
		}
	}
	return reachable
}
