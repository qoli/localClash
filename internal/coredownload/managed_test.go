package coredownload

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureAcquiresOnlyMissingSelectedCore(t *testing.T) {
	for _, flavor := range []string{FlavorMeta, FlavorSmart} {
		t.Run(flavor, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "lc-mihomo-"+flavor)
			calls := 0
			download := func(_ context.Context, opts Options) ([]Result, error) {
				calls++
				if opts.Flavor != flavor || opts.OutputPath != path || opts.Force {
					t.Fatalf("download options = %+v", opts)
				}
				return nil, os.WriteFile(opts.OutputPath, []byte("selected"), 0o755)
			}
			created, err := Ensure(context.Background(), path, TargetRouter, download)
			if err != nil || !created || calls != 1 {
				t.Fatalf("first ensure created=%v calls=%d err=%v", created, calls, err)
			}
			created, err = Ensure(context.Background(), path, TargetRouter, download)
			if err != nil || created || calls != 1 {
				t.Fatalf("repeat ensure created=%v calls=%d err=%v", created, calls, err)
			}
		})
	}
}

func TestEnsureDownloadFailurePreservesExistingCore(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "lc-mihomo-meta")
	if err := os.WriteFile(old, []byte("working"), 0o755); err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("download unavailable")
	_, err := Ensure(context.Background(), filepath.Join(dir, "lc-mihomo-smart"), TargetRouter, func(context.Context, Options) ([]Result, error) {
		return nil, wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v", err)
	}
	if data, err := os.ReadFile(old); err != nil || string(data) != "working" {
		t.Fatalf("old core = %q, %v", data, err)
	}
}

func TestPruneUnusedKeepsSelectedAndUnrelatedFiles(t *testing.T) {
	for _, selectedName := range []string{"lc-mihomo-meta", "lc-mihomo-smart"} {
		t.Run(selectedName, func(t *testing.T) {
			dir := t.TempDir()
			files := []string{"lc-mihomo-meta", "lc-mihomo-smart", "lc-mihomo-smart.pre-whatsapp-20260920T0831Z", "lc-mihomo-smart.pre-active-monitor5-20260903-003420", "lc-mihomo-meta.previous", "lc-mihomo-smart.download", "other-program", "notes.txt"}
			for _, name := range files {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			removed, err := PruneUnused(filepath.Join(dir, selectedName))
			if err != nil || len(removed) != 3 {
				t.Fatalf("removed=%v err=%v", removed, err)
			}
			for _, name := range files {
				_, err := os.Stat(filepath.Join(dir, name))
				wantRemoved := name != selectedName && (name == "lc-mihomo-meta" || name == "lc-mihomo-smart" || name == files[2] || name == files[3])
				if os.IsNotExist(err) != wantRemoved || (err != nil && !os.IsNotExist(err)) {
					t.Errorf("%s: err=%v wantRemoved=%v", name, err, wantRemoved)
				}
			}
		})
	}
}

func TestPruneDoesNotRemoveAnythingWithoutSelectedBinary(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "lc-mihomo-meta")
	if err := os.WriteFile(old, []byte("working"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := PruneUnused(filepath.Join(dir, "lc-mihomo-smart")); err == nil {
		t.Fatal("missing selected core must fail cleanup")
	}
	if _, err := os.Stat(old); err != nil {
		t.Fatal(err)
	}
}
