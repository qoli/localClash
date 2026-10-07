//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package customsitesapply

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"localclash/internal/customsites"
)

func TestTransactionLockSerializesIndependentFileDescriptors(t *testing.T) {
	paths := customsites.DefaultPaths(t.TempDir())
	releaseFirst, err := acquireTransactionLock(context.Background(), paths)
	if err != nil {
		t.Fatal(err)
	}

	acquired := make(chan func(), 1)
	errs := make(chan error, 1)
	go func() {
		release, err := acquireTransactionLock(context.Background(), paths)
		if err != nil {
			errs <- err
			return
		}
		acquired <- release
	}()

	select {
	case release := <-acquired:
		release()
		t.Fatal("second lock acquired before first was released")
	case err := <-errs:
		t.Fatal(err)
	case <-time.After(150 * time.Millisecond):
	}

	releaseFirst()
	select {
	case release := <-acquired:
		release()
	case err := <-errs:
		t.Fatal(err)
	case <-time.After(2 * time.Second):
		t.Fatal("second lock did not acquire after first was released")
	}
}

func TestTransactionLockWaitHonorsContextCancellation(t *testing.T) {
	root := t.TempDir()
	paths := customsites.Paths{
		Proxy:  filepath.Join(root, "custom-sites", customsites.ProxyFilename),
		Direct: filepath.Join(root, "custom-sites", customsites.DirectFilename),
	}
	release, err := acquireTransactionLock(context.Background(), paths)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	secondRelease, err := acquireTransactionLock(ctx, paths)
	if secondRelease != nil {
		secondRelease()
		t.Fatal("cancelled lock unexpectedly acquired")
	}
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context deadline exceeded", err)
	}
}
