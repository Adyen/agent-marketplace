package main

import (
	"context"
	"errors"

	"github.com/Adyen/agent-marketplace/internal/config"
)

type mergedConfigContext struct {
	OrgCfg    *config.Config
	GlobalCfg *config.Config
	RepoCfg   *config.Config
	LocalCfg  *config.LocalConfig
	MergedCfg *config.Config
	Managed   bool
}

func loadMergedConfigContext() (*mergedConfigContext, error) {
	ec := config.LoadEffective(context.Background(), config.LoadEffectiveOptions{})
	config.LogManagedDiagnostics(ec.Diagnostics)
	if len(ec.Errors) > 0 {
		return nil, errors.Join(ec.Errors...)
	}

	ctx := &mergedConfigContext{
		OrgCfg:    ec.Organization.Config,
		GlobalCfg: ec.User.Config,
		RepoCfg:   ec.Repo.Config,
		LocalCfg:  ec.Local,
		MergedCfg: ec.Merged,
		Managed:   ec.Managed,
	}

	return ctx, nil
}
