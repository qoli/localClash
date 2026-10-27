// Package coreconfig prepares a selected binary before committing its profile.
package coreconfig

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"localclash/internal/coredownload"
	"localclash/internal/materialtxn"
	"localclash/internal/runtimeprofile"
)

type Options struct {
	ProfilePath   string
	WorkspaceRoot string
	Mode          string
	Core          string
	Download      coredownload.DownloadFunc
}

// Configure leaves the old selection intact if acquisition or configuration
// fails. A missing target is acquired only for an explicit core selection.
// The previous flavor remains available until the new runtime starts.
func Configure(ctx context.Context, opts Options, apply func(runtimeprofile.Status) error) (runtimeprofile.Status, error) {
	file, err := runtimeprofile.PreviewConfigure(opts.ProfilePath, opts.Mode, opts.Core)
	if err != nil {
		return runtimeprofile.Status{}, err
	}
	corePath := runtimeprofile.ActiveCorePathFromFile(file)
	if !filepath.IsAbs(corePath) {
		corePath = filepath.Join(opts.WorkspaceRoot, corePath)
	}
	created := false
	if strings.TrimSpace(opts.Core) != "" {
		created, err = coredownload.Ensure(ctx, corePath, coredownload.TargetForMode(file.Mode), opts.Download)
		if err != nil {
			if created {
				_ = os.Remove(corePath)
			}
			return runtimeprofile.Status{}, err
		}
	}
	var status runtimeprofile.Status
	err = materialtxn.Run([]string{opts.ProfilePath}, func() error {
		status, err = runtimeprofile.Configure(opts.ProfilePath, opts.Mode, opts.Core)
		if err != nil {
			return err
		}
		if apply != nil {
			return apply(status)
		}
		return nil
	})
	if err != nil && created {
		if cleanupErr := os.Remove(corePath); cleanupErr != nil && !os.IsNotExist(cleanupErr) {
			return status, fmt.Errorf("%w; remove unused candidate: %v", err, cleanupErr)
		}
	}
	return status, err
}
