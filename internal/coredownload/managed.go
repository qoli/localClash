package coredownload

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"localclash/internal/runtimeprofile"
)

type DownloadFunc func(context.Context, Options) ([]Result, error)

func FlavorForPath(path string) (string, error) {
	switch strings.TrimSuffix(filepath.Base(path), ".exe") {
	case runtimeprofile.ManagedMetaCoreName:
		return FlavorMeta, nil
	case runtimeprofile.ManagedSmartCoreName:
		return FlavorSmart, nil
	default:
		return "", fmt.Errorf("unrecognized managed Mihomo core %q", path)
	}
}

// Ensure downloads only a missing selected core. It never removes the old core;
// cleanup belongs to successful configuration validation or runtime activation.
func Ensure(ctx context.Context, path, target string, download DownloadFunc) (bool, error) {
	if info, err := os.Stat(path); err == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			return false, fmt.Errorf("selected Mihomo core %q is not an executable file", path)
		}
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, err
	}
	flavor, err := FlavorForPath(path)
	if err != nil {
		return false, err
	}
	if download == nil {
		download = Download
	}
	_, err = download(ctx, Options{
		Version: "latest", Flavor: flavor, Target: target,
		TargetOS: runtime.GOOS, TargetArch: runtime.GOARCH,
		OutputPath: path, Repo: "MetaCubeX/mihomo",
	})
	if err != nil {
		return false, fmt.Errorf("download selected %s core: %w", flavor, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return false, fmt.Errorf("downloaded selected Mihomo core %q is unavailable: %w", path, err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return true, fmt.Errorf("downloaded selected Mihomo core %q is not executable", path)
	}
	return true, nil
}

// TargetForMode keeps normal Linux hosts on host release selection. Only an
// explicitly selected router profile on Linux uses router-specific assets.
func TargetForMode(mode string) string {
	if mode == runtimeprofile.ModeRouter && runtime.GOOS == "linux" {
		return TargetRouter
	}
	return TargetHost
}

// PruneUnused removes the other managed flavor and legacy pre-* backups in the
// selected platform directory. Call only after the selected core is validated.
// In-flight .previous and .download files are owned by their update transaction.
func PruneUnused(selected string) ([]string, error) {
	if _, err := FlavorForPath(selected); err != nil {
		return nil, err
	}
	if _, err := os.Stat(selected); err != nil {
		return nil, fmt.Errorf("cannot prune cores without selected binary: %w", err)
	}
	entries, err := os.ReadDir(filepath.Dir(selected))
	if err != nil {
		return nil, err
	}
	var removed []string
	for _, entry := range entries {
		name := entry.Name()
		if name == filepath.Base(selected) {
			continue
		}
		managed := false
		for _, base := range []string{runtimeprofile.ManagedMetaCoreName, runtimeprofile.ManagedSmartCoreName} {
			if name == base || name == base+".exe" || strings.HasPrefix(name, base+".pre-") || strings.HasPrefix(name, base+".exe.pre-") {
				managed = true
				break
			}
		}
		if !managed || entry.IsDir() {
			continue
		}
		path := filepath.Join(filepath.Dir(selected), name)
		if err := os.Remove(path); err != nil {
			return removed, fmt.Errorf("remove unused Mihomo core %q: %w", path, err)
		}
		removed = append(removed, path)
	}
	return removed, nil
}
