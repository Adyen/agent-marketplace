package config

import (
	"fmt"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/nameutil"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/urlutil"
	"gopkg.in/yaml.v3"
)

func ValidateMarketplaceInstalledName(name string) error {
	if name == "" {
		return nil
	}
	if err := nameutil.ValidateSafeFilename(name); err != nil {
		return fmt.Errorf("installed_name: %w", err)
	}
	return nil
}

func ValidateMarketplaceResourcePin(pin MarketplaceResourcePin) error {
	if err := validateMarketplaceSegment("registry_id", pin.RegistryID); err != nil {
		return err
	}
	if !api.IsMarketplaceKind(pin.Kind) {
		return fmt.Errorf("kind %q is not a valid Marketplace kind", pin.Kind)
	}
	if err := validateMarketplaceSegment("name", pin.Name); err != nil {
		return err
	}
	if err := validateMarketplaceSegment("revision", pin.Revision); err != nil {
		return err
	}
	if !api.IsCanonicalSHA256Digest(pin.Digest) {
		return fmt.Errorf("digest must be sha256:<64-hex>")
	}
	if pin.RegistrySlug != "" {
		if err := validateMarketplaceSegment("registry_slug", pin.RegistrySlug); err != nil {
			return err
		}
	}
	if pin.Acquisition != "" && pin.Acquisition != MarketplaceAcquisitionMirror && pin.Acquisition != MarketplaceAcquisitionIndexOnly {
		return fmt.Errorf("acquisition must be %q or %q", MarketplaceAcquisitionMirror, MarketplaceAcquisitionIndexOnly)
	}
	if err := validateMarketplaceOccurrenceFields(pin.Acquisition, pin.RegistryCommit, pin.RegistryManifestDigest); err != nil {
		return err
	}
	if err := validateMarketplaceSelections(pin.SelectedBy); err != nil {
		return err
	}
	return ValidateMarketplaceInstalledName(pin.InstalledName)
}

func containsMarketplaceSelection(selections []MarketplaceSelection, wanted MarketplaceSelection) bool {
	for _, selection := range selections {
		if selection == wanted {
			return true
		}
	}
	return false
}

func scalarNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

func boolNode(value bool) *yaml.Node {
	if value {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"}
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "false"}
}

func validateMarketplaceAlias(label, value string) error {
	if err := validateMarketplaceSegment(label, value); err != nil {
		return err
	}
	if value == "all" {
		return fmt.Errorf("%s %q is reserved for federated search", label, value)
	}
	return nil
}

func validateMarketplaceSegment(label, value string) error {
	if err := nameutil.ValidateSegment(value); err != nil {
		return fmt.Errorf("%s %s", label, err)
	}
	return nil
}

func NormalizeMarketplaceURL(raw string) (string, error) {
	return urlutil.NormalizeEndpointURL(raw)
}
