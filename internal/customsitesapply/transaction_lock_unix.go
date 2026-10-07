//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package customsitesapply

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"localclash/internal/customsites"
)

func acquireTransactionLock(ctx context.Context, paths customsites.Paths) (func(), error) {
	dir := filepath.Dir(paths.Proxy)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create custom site transaction lock directory: %w", err)
	}
	path := filepath.Join(dir, ".transaction.lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open custom site transaction lock: %w", err)
	}
	closeWithError := func(err error) (func(), error) {
		_ = file.Close()
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		return closeWithError(fmt.Errorf("inspect custom site transaction lock: %w", err))
	}
	if !info.Mode().IsRegular() {
		return closeWithError(errors.New("custom site transaction lock must be a regular file"))
	}
	if err := file.Chmod(0o600); err != nil {
		return closeWithError(fmt.Errorf("set custom site transaction lock permissions: %w", err))
	}

	for {
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() {
				_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
				_ = file.Close()
			}, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			return closeWithError(fmt.Errorf("lock custom site transaction: %w", err))
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return closeWithError(fmt.Errorf("wait for custom site transaction lock: %w", ctx.Err()))
		case <-timer.C:
		}
	}
}
