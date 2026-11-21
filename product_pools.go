package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"localclash/internal/appinit"
	"localclash/internal/configrender"
	"localclash/internal/corerun"
	"localclash/internal/materialtxn"
	"localclash/internal/mihomotest"
	"localclash/internal/poolcontract"
	"localclash/internal/poollaunch"
	"localclash/internal/rules"
	"localclash/internal/runtimeprofile"
	"localclash/internal/runtimesupervision"
)

type poolConfigStatus struct {
	EnhancedFilter bool `json:"enhanced_filter"`
	Enabled        bool `json:"enabled"`
	Reloaded       bool `json:"reloaded"`
}

func runProductPools(args []string, state appinit.RuntimeState) error {
	flags := flag.NewFlagSet("config pools", flag.ContinueOnError)
	asJSON := flags.Bool("json", false, "print product JSON response")
	var enabled, enhanced *bool
	boolOption := func(target **bool) func(string) error {
		return func(value string) error {
			if value != "true" && value != "false" {
				return errors.New("pool option must be true or false")
			}
			on := value == "true"
			*target = &on
			return nil
		}
	}
	flags.Func("enabled", "enable or disable qualified proxy pools", boolOption(&enabled))
	flags.Func("enhanced-filter", "expose the enhanced YouTube throughput pool", boolOption(&enhanced))
	if err := flags.Parse(args); err != nil {
		return err
	}
	if !*asJSON || flags.NArg() != 0 {
		return errors.New("usage: localclash config pools [--enabled true|false] [--enhanced-filter true|false] --json")
	}
	if enabled == nil && enhanced == nil {
		profile, _, err := runtimeprofile.Load(state.Paths.RuntimeProfilePath)
		if err != nil {
			return err
		}
		return printProductOK(productEnvelope{OK: true, Summary: "Pool capability settings read.", Status: poolConfigStatus{Enabled: !profile.PoolsDisabled, EnhancedFilter: profile.EnhancedFilter}})
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	var status poolConfigStatus
	changed := false
	err := runtimesupervision.WithLock(state.Paths.RuntimeRoot, func() error {
		profile, _, err := runtimeprofile.Load(state.Paths.RuntimeProfilePath)
		// An older profile with no setting may still have pools in its rendered config.
		if err != nil {
			return err
		}
		status.Enabled = !profile.PoolsDisabled
		status.EnhancedFilter = profile.EnhancedFilter
		if enabled != nil {
			status.Enabled = *enabled
		}
		if enhanced != nil {
			status.EnhancedFilter = *enhanced
		}
		changePools := enabled != nil && (!profile.PoolsSettingExplicit || status.Enabled == profile.PoolsDisabled)
		if !changePools && status.EnhancedFilter == profile.EnhancedFilter {
			return nil
		}
		if !changePools && !status.Enabled {
			err := runtimeprofile.SetPoolOptions(state.Paths.RuntimeProfilePath, false, status.EnhancedFilter)
			changed = err == nil
			return err
		}
		reloaded, err := configureProductPools(ctx, state, status.Enabled, status.EnhancedFilter, changePools)
		changed = err == nil
		status.Reloaded = reloaded
		return err
	})
	if err != nil {
		return err
	}
	return printProductOK(productEnvelope{
		OK: true, Changed: changed, Summary: "Pool capability settings saved.",
		Status: status, Changes: changedIf(changed, "pool_capability_configured"),
	})
}

func configureProductPools(ctx context.Context, state appinit.RuntimeState, enabled, enhanced, changePools bool) (bool, error) {
	paths := state.Paths
	previousHash, err := mihomotest.ConfigSHA256(paths.GeneratedConfig)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	source, err := configrender.ReadSubscription(paths.SubscriptionPath)
	if errors.Is(err, os.ErrNotExist) && previousHash == "" && !corerun.Status(runtimeStatusOptions(state)).Running {
		return false, runtimeprofile.SetPoolOptions(paths.RuntimeProfilePath, enabled, enhanced)
	}
	if err != nil {
		return false, fmt.Errorf("read merged subscriptions for pool configuration: %w", err)
	}

	configDir := filepath.Dir(paths.GeneratedConfig)
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return false, err
	}
	dir, err := os.MkdirTemp(configDir, ".pool-config-")
	if err != nil {
		return false, err
	}
	defer os.RemoveAll(dir)
	opts := configRenderOptions(state)
	opts.OutputPath = filepath.Join(dir, "config.yaml")
	opts.Source = source
	opts.PoolsEnabled = &enabled
	opts.EnhancedFilter = &enhanced
	if enabled && opts.PacksSelectionPath != "" {
		selection, err := rules.LoadSelection(opts.PacksSelectionPath)
		if err != nil {
			return false, err
		}
		opts.Selection = &selection
		profiles := make([]string, 0, len(selection.ProxyGroups))
		for _, group := range selection.ProxyGroups {
			if (group.PoolCapability == poolcontract.BulkCapability && enhanced) || (group.PoolCapability != poolcontract.BulkCapability && changePools) {
				profiles = append(profiles, group.PoolCapability)
			}
		}
		if _, err := poollaunch.PrepareProfiles(ctx, poollaunch.Options{
			Workspace: productWorkspaceRoot(state),
			Core:      paths.CorePath,
			Runtime:   paths.MihomoRuntimeDir,
		}, profiles, source); err != nil {
			return false, err
		}
	}
	if _, err := configrender.Render(opts); err != nil {
		return false, err
	}

	if err := ctx.Err(); err != nil {
		return false, err
	}
	currentHash, err := mihomotest.ConfigSHA256(paths.GeneratedConfig)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if currentHash != previousHash {
		return false, errors.New("Mihomo configuration changed while preparing the pool setting")
	}

	running := corerun.Status(runtimeStatusOptions(state)).Running
	if running && previousHash == "" {
		return false, errors.New("the running Mihomo configuration is missing")
	}
	attestation := mihomotest.DefaultAttestationPath(paths.MihomoRuntimeDir)
	cache := mihomotest.DefaultCachePath(paths.MihomoRuntimeDir)
	validation := mihomotest.ValidationOptions{
		CorePath:   paths.CorePath,
		ConfigPath: paths.GeneratedConfig,
		WorkDir:    paths.MihomoRuntimeDir,
		CachePath:  cache,
	}
	reloadOpts := runtimeRestartOptions(state)
	reloadOpts.Strategy = corerun.RestartStrategyHotReload
	var attemptedReload bool
	err = materialtxn.Run([]string{paths.RuntimeProfilePath, paths.GeneratedConfig, attestation, cache}, func() error {
		if err := os.Rename(opts.OutputPath, paths.GeneratedConfig); err != nil {
			return err
		}
		test, err := mihomotest.Test(ctx, mihomotest.TestOptions{
			ValidationOptions: validation,
			Record:            true,
			AttestationPath:   attestation,
		})
		if err != nil {
			return err
		}
		if !test.Passed {
			return errors.New("pool configuration did not pass Mihomo validation")
		}
		if err := runtimeprofile.SetPoolOptions(paths.RuntimeProfilePath, enabled, enhanced); err != nil || !running {
			return err
		}
		reloadOpts.ConfigSHA256 = test.ConfigSHA256
		result, err := corerun.Restart(ctx, reloadOpts)
		attemptedReload = result.HotReload != nil
		if err == nil && !result.Reloaded {
			err = fmt.Errorf("pool configuration reload failed: %s", result.Error)
		} else if err == nil && result.Error != "" {
			err = errors.New(result.Error)
		}
		return err
	})
	if err != nil && attemptedReload {
		if _, hashErr := mihomotest.VerifyConfigHash(paths.GeneratedConfig, previousHash); hashErr != nil {
			return false, errors.Join(err, hashErr)
		}
		reloadOpts.ConfigSHA256 = previousHash
		rollbackCtx := context.WithoutCancel(ctx)
		if _, validationErr := mihomotest.ValidateCached(rollbackCtx, validation); validationErr != nil {
			return false, errors.Join(err, validationErr)
		}
		restored, rollbackErr := corerun.Restart(rollbackCtx, reloadOpts)
		if rollbackErr == nil && (!restored.Reloaded || restored.Error != "") {
			rollbackErr = fmt.Errorf("restore previous Mihomo configuration: %s", restored.Error)
		}
		err = errors.Join(err, rollbackErr)
	}
	return running && err == nil, err
}
