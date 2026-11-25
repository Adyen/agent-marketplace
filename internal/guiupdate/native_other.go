//go:build !darwin && !linux

package guiupdate

func NewNative(format string) *Service {
	return New(Config{Format: format})
}
