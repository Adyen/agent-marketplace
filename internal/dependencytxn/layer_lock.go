package dependencytxn

import (
	"errors"
	"log/slog"
	"os"

	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

type LayerLockEvidence struct {
	Document lockfile.Document
	Digest   string
	Overlay  bool
}

func ReadLayerLock(canonical, requested string, scope lockfile.Scope, overlays map[string][]byte) (LayerLockEvidence, bool, error) {
	data, overlaid := overlays[canonical]
	if !overlaid {
		data, overlaid = overlays[requested]
	}
	if !overlaid {
		var err error
		if ClassifyDestination(canonical) != AuthorityLegacy {
			var generation Generation
			data, generation, _, err = ReadSnapshot(canonical)
			if err == nil && !generation.Exists {
				return LayerLockEvidence{}, false, nil
			}
		} else {
			data, err = lockfile.ReadBytes(canonical)
		}
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return LayerLockEvidence{}, false, nil
			}
			return LayerLockEvidence{}, false, err
		}
	}
	doc, err := lockfile.Decode(data)
	if err == nil {
		err = lockfile.ValidateScope(doc, scope)
	}
	ev := LayerLockEvidence{Document: doc, Digest: Digest(data), Overlay: overlaid}
	if err != nil {
		return ev, true, err
	}
	if doc.APIVersion == lockfile.APIVersionV1 {
		slog.Warn("lock file uses the deprecated v1 schema; the next writable reconciliation upgrades it to v2",
			slog.String("code", "lock.version-deprecated"),
			slog.String("path", canonical),
			slog.String("scope", string(scope)),
		)
	}
	return ev, false, nil
}
