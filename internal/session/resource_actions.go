package session

import (
	"errors"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// toggleDenial identifies why the guarded row toggle cannot proceed, in the
// order the keyboard Space dispatch reports it. toggleEligible means the
// toggle modal may open. This is the single authoritative toggle policy:
// the keyboard path maps each denial to its toast/no-op outcome and the
// palette maps any denial to hiding the toggle action.
type RowToggleDenial int

const (
	RowToggleEligible RowToggleDenial = iota
	RowToggleBusy
	RowToggleConfigInvalid
	RowToggleMalformed
	RowTogglePendingVerification
	RowToggleMarketplaceBlocked
	RowToggleRequired
	RowToggleCollectionOnly
)

// rowToggleEligibility evaluates the shared toggle guards against the
// current model and row without dispatching.
func RowToggleEligibility(r Row, busy, configInvalid bool) RowToggleDenial {
	if busy {
		return RowToggleBusy
	}
	if configInvalid {
		return RowToggleConfigInvalid
	}
	switch {
	case r.IsMalformed:
		return RowToggleMalformed
	case r.PendingVerification:
		return RowTogglePendingVerification
	case r.MarketplaceDiscovery != nil && !MarketplaceDiscoveryInstallable(r):
		return RowToggleMarketplaceBlocked
	case r.Artifact.Required:
		return RowToggleRequired
	case r.MarketplaceCollectionOnly && !r.CanOverrideMarketplace():
		return RowToggleCollectionOnly
	}
	return RowToggleEligible
}

const (
	UserMarketplaceActionNone    = "none"
	UserMarketplaceActionInstall = "install"
	UserMarketplaceActionRemove  = "remove"
	UserMarketplaceActionExclude = "exclude"
	UserMarketplaceActionRestore = "restore"
)

func UserMarketplaceAction(r Row) (string, error) {
	if !r.Artifact.IsMarketplace || RowToggleEligibility(r, false, false) != RowToggleEligible {
		return UserMarketplaceActionNone, nil
	}
	if r.MarketplaceDiscovery != nil {
		if MarketplaceDiscoveryInstallable(r) {
			return UserMarketplaceActionInstall, nil
		}
		return UserMarketplaceActionNone, nil
	}
	if r.CanOverrideMarketplace() {
		if r.Artifact.Kind == ir.KindCollection {
			return UserMarketplaceActionNone, nil
		}
		selection, ok := r.MarketplaceSelections[config.Organization]
		if !ok || selection.Pin.RegistrySlug == "" {
			return UserMarketplaceActionNone, nil
		}
		if r.ExcludedByUser {
			return UserMarketplaceActionRestore, nil
		}
		return UserMarketplaceActionExclude, nil
	}
	selection, ok := r.MarketplaceSelections[config.UserPersonal]
	if !ok || !r.EnabledGlobal || (!selection.Pin.Direct && len(selection.Pin.SelectedBy.List()) > 0) {
		return UserMarketplaceActionNone, nil
	}
	return UserMarketplaceActionRemove, nil
}

func UserDiscoveryInstallOptions(homeDir string, r Row) (pipeline.InstallOptions, error) {
	if !config.IsHomeDir(homeDir) {
		return pipeline.InstallOptions{}, errors.New("user Marketplace install requires the home directory")
	}
	options, err := DiscoveryInstallOptions(homeDir, r, pipeline.ScopeUser)
	if err != nil {
		return pipeline.InstallOptions{}, err
	}
	options.LocalConfigDir = homeDir
	return options, nil
}
