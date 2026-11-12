//go:build cgo && !e2e && (darwin || linux)

package main

func startE2EControl(*App) (func(), error) {
	return func() {}, nil
}
