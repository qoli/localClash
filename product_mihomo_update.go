package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"

	"localclash/internal/appinit"
	"localclash/internal/coredownload"
	"localclash/internal/corerun"
	"localclash/internal/mihomotest"
)

type mihomoUpdateStatus struct {
	Downloads          []coredownload.Result        `json:"downloads"`
	Promoted           []string                     `json:"promoted"`
	ConfigValidation   *mihomotest.ValidationResult `json:"config_validation,omitempty"`
	ValidationRequired bool                         `json:"validation_required"`
}

type mihomoPromotion struct {
	target    string
	backup    string
	hadTarget bool
	backedUp  bool
	promoted  bool
}

func updateMihomoComponents(ctx context.Context, state appinit.RuntimeState) (mihomoUpdateStatus, error) {
	status := mihomoUpdateStatus{}
	candidateParent := filepath.Join(productWorkspaceRoot(state), ".runtime")
	if err := os.MkdirAll(candidateParent, 0o755); err != nil {
		return status, fmt.Errorf("create Mihomo update transaction root: %w", err)
	}
	candidateRoot, err := os.MkdirTemp(candidateParent, ".mihomo-update-")
	if err != nil {
		return status, fmt.Errorf("create Mihomo update transaction: %w", err)
	}
	defer os.RemoveAll(candidateRoot)

	downloads, err := downloadCore(ctx, coredownload.Options{
		Version:    "latest",
		Flavor:     coredownload.FlavorAll,
		Target:     coredownload.TargetRouter,
		TargetOS:   "linux",
		TargetArch: runtime.GOARCH,
		OutputDir:  candidateRoot,
		Repo:       "MetaCubeX/mihomo",
		Force:      true,
	})
	status.Downloads = downloads
	if err != nil {
		return status, err
	}

	candidates, err := checkedMihomoCandidates(downloads)
	if err != nil {
		return status, err
	}
	selectedCandidate, ok := candidates[filepath.Base(state.Paths.CorePath)]
	if !ok {
		return status, fmt.Errorf("Mihomo update candidates do not include selected core %q", filepath.Base(state.Paths.CorePath))
	}

	runtimeStatus := corerun.Status(runtimeStatusOptions(state))
	_, configErr := os.Stat(state.Paths.GeneratedConfig)
	switch {
	case configErr == nil:
		status.ValidationRequired = true
		validation, validateErr := mihomotest.ValidateCached(ctx, mihomotest.ValidationOptions{
			CorePath:   selectedCandidate,
			ConfigPath: state.Paths.GeneratedConfig,
			WorkDir:    state.Paths.MihomoRuntimeDir,
			CachePath:  filepath.Join(candidateRoot, "config-validation-cache.json"),
			Force:      true,
		})
		status.ConfigValidation = &validation
		if validateErr != nil {
			return status, fmt.Errorf("validate candidate Mihomo against current generated config: %w", validateErr)
		}
	case os.IsNotExist(configErr) && runtimeStatus.Running:
		return status, fmt.Errorf("current generated config %q is missing while Mihomo runtime is running", state.Paths.GeneratedConfig)
	case os.IsNotExist(configErr):
		// A first installation has no known-good generated config yet. The
		// candidate remains subject to the normal config-test before first start.
	default:
		return status, fmt.Errorf("inspect current generated config %q: %w", state.Paths.GeneratedConfig, configErr)
	}

	promoted, err := promoteMihomoCandidates(candidates, filepath.Dir(state.Paths.CorePath))
	status.Promoted = promoted
	if err != nil {
		return status, err
	}
	for index := range status.Downloads {
		status.Downloads[index].OutputPath = filepath.Join(filepath.Dir(state.Paths.CorePath), filepath.Base(status.Downloads[index].OutputPath))
	}
	return status, nil
}

func checkedMihomoCandidates(downloads []coredownload.Result) (map[string]string, error) {
	want := map[string]bool{
		"lc-mihomo-meta":  true,
		"lc-mihomo-smart": true,
	}
	candidates := make(map[string]string, len(want))
	for _, result := range downloads {
		name := filepath.Base(result.OutputPath)
		if !want[name] {
			return nil, fmt.Errorf("unexpected Mihomo update candidate %q", result.OutputPath)
		}
		if _, exists := candidates[name]; exists {
			return nil, fmt.Errorf("duplicate Mihomo update candidate %q", name)
		}
		info, err := os.Stat(result.OutputPath)
		if err != nil {
			return nil, fmt.Errorf("inspect Mihomo update candidate %q: %w", result.OutputPath, err)
		}
		if !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
			return nil, fmt.Errorf("Mihomo update candidate %q is not an executable regular file", result.OutputPath)
		}
		candidates[name] = result.OutputPath
	}
	for name := range want {
		if _, ok := candidates[name]; !ok {
			return nil, fmt.Errorf("Mihomo update candidate %q is missing", name)
		}
	}
	return candidates, nil
}

func promoteMihomoCandidates(candidates map[string]string, targetDir string) ([]string, error) {
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return nil, fmt.Errorf("create managed Mihomo directory: %w", err)
	}
	names := make([]string, 0, len(candidates))
	for name := range candidates {
		names = append(names, name)
	}
	sort.Strings(names)
	promotions := make([]mihomoPromotion, 0, len(names))
	rollback := func() error {
		var rollbackErr error
		for index := len(promotions) - 1; index >= 0; index-- {
			entry := promotions[index]
			if entry.promoted {
				if err := os.Remove(entry.target); err != nil && !os.IsNotExist(err) && rollbackErr == nil {
					rollbackErr = err
				}
			}
			if entry.backedUp {
				if err := os.Rename(entry.backup, entry.target); err != nil && rollbackErr == nil {
					rollbackErr = err
				}
			}
		}
		return rollbackErr
	}

	for _, name := range names {
		target := filepath.Join(targetDir, name)
		entry := mihomoPromotion{target: target, backup: target + ".previous"}
		if _, err := os.Lstat(entry.backup); err == nil {
			return nil, fmt.Errorf("stale Mihomo rollback artifact exists: %s", entry.backup)
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("inspect Mihomo rollback artifact %q: %w", entry.backup, err)
		}
		if _, err := os.Lstat(target); err == nil {
			entry.hadTarget = true
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("inspect managed Mihomo binary %q: %w", target, err)
		}
		promotions = append(promotions, entry)
	}
	for index := range promotions {
		entry := &promotions[index]
		if !entry.hadTarget {
			continue
		}
		if err := os.Rename(entry.target, entry.backup); err != nil {
			if rollbackErr := rollback(); rollbackErr != nil {
				return nil, fmt.Errorf("prepare Mihomo rollback for %q: %v; rollback failed: %w", entry.target, err, rollbackErr)
			}
			return nil, fmt.Errorf("prepare Mihomo rollback for %q: %w", entry.target, err)
		}
		entry.backedUp = true
	}

	for index, name := range names {
		if err := os.Rename(candidates[name], promotions[index].target); err != nil {
			if rollbackErr := rollback(); rollbackErr != nil {
				return nil, fmt.Errorf("promote Mihomo candidate %q: %v; rollback failed: %w", name, err, rollbackErr)
			}
			return nil, fmt.Errorf("promote Mihomo candidate %q: %w", name, err)
		}
		promotions[index].promoted = true
	}

	promoted := make([]string, 0, len(promotions))
	for _, entry := range promotions {
		promoted = append(promoted, entry.target)
		if entry.hadTarget {
			if err := os.Remove(entry.backup); err != nil {
				return promoted, fmt.Errorf("remove committed Mihomo rollback artifact %q: %w", entry.backup, err)
			}
		}
	}
	return promoted, nil
}
