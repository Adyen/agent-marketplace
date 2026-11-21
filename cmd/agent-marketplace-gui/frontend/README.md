# Frontend build output

`dist/` is the Vite build output from `make gui-build-frontend` (`gui/vite.config.ts` `outDir`). `native.go` embeds it, and it is not committed. Build the frontend before compiling the native host.

This file is tracked so the `//go:embed all:frontend` pattern stays non-empty when `dist/` is absent.
