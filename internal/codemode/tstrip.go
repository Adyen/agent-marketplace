package codemode

import (
	"strings"

	"github.com/evanw/esbuild/pkg/api"
)

func StripTypes(code string) (string, error) {
	out := api.Transform(code, api.TransformOptions{
		Loader:            api.LoaderTS,
		Format:            api.FormatESModule,
		Target:            api.ES2022,
		Sourcemap:         api.SourceMapNone,
		LogLevel:          api.LogLevelSilent,
		MinifyWhitespace:  false,
		MinifyIdentifiers: false,
		MinifySyntax:      false,
	})
	if len(out.Errors) > 0 {
		return "", CodeError(ErrCodeScriptError, "typescript strip failed: %s", out.Errors[0].Text)
	}
	out2 := strings.TrimSpace(string(out.Code))
	out2 = strings.TrimPrefix(out2, `"use strict";`)
	out2 = strings.TrimSpace(out2)
	out2 = strings.TrimSuffix(out2, ";")
	return strings.TrimSpace(out2), nil
}
