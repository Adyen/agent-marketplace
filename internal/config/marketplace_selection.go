package config

func EnsureMarketplaceDirectPin(pin MarketplaceResourcePin) MarketplaceResourcePin {
	pin.Direct = true
	return pin
}
