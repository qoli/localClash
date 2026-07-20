//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package runtimesupervision

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func WithLock(workDir string, fn func() error) error {
	if workDir == "" {
		return errors.New("runtime supervision lock workdir is required")
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return fmt.Errorf("create runtime supervision lock directory: %w", err)
	}
	path := filepath.Join(workDir, "runtime-supervision.lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open runtime supervision lock: %w", err)
	}
	defer file.Close()
	if err := file.Chmod(0o600); err != nil {
		return fmt.Errorf("set runtime supervision lock permissions: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("lock runtime supervision state: %w", err)
	}
	defer func() { _ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN) }()
	return fn()
}
