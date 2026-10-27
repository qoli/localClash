package coreconfig

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"localclash/internal/coredownload"
	"localclash/internal/runtimeprofile"
)

func TestConfigureAcquiresSelectedCoreBeforeChangingProfile(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, runtimeprofile.DefaultPath)
	old := []byte(`{"version":2,"mode":"router","core":"meta"}`)
	if err := os.WriteFile(profile, old, 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, runtimeprofile.SmartCorePath)
	status, err := Configure(context.Background(), Options{
		ProfilePath: profile, WorkspaceRoot: dir, Core: runtimeprofile.CoreSmart,
		Download: func(_ context.Context, opts coredownload.Options) ([]coredownload.Result, error) {
			current, _ := os.ReadFile(profile)
			if string(current) != string(old) || opts.Flavor != coredownload.FlavorSmart || opts.OutputPath != target {
				t.Fatalf("profile changed before acquisition or wrong target: %s %+v", current, opts)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return nil, err
			}
			return nil, os.WriteFile(target, []byte("selected"), 0o755)
		},
	}, nil)
	if err != nil || status.Core != runtimeprofile.CoreSmart {
		t.Fatalf("status=%+v err=%v", status, err)
	}
}

func TestConfigureFailurePreservesSelectionAndOldBinary(t *testing.T) {
	for _, failure := range []string{"download", "configuration"} {
		t.Run(failure, func(t *testing.T) {
			dir := t.TempDir()
			profile := filepath.Join(dir, runtimeprofile.DefaultPath)
			old := []byte(`{"version":2,"mode":"router","core":"meta"}`)
			if err := os.WriteFile(profile, old, 0o644); err != nil {
				t.Fatal(err)
			}
			oldCore := filepath.Join(dir, runtimeprofile.MetaCorePath)
			if err := os.MkdirAll(filepath.Dir(oldCore), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(oldCore, []byte("working"), 0o755); err != nil {
				t.Fatal(err)
			}
			wantErr := errors.New(failure + " failed")
			_, err := Configure(context.Background(), Options{
				ProfilePath: profile, WorkspaceRoot: dir, Core: runtimeprofile.CoreSmart,
				Download: func(_ context.Context, opts coredownload.Options) ([]coredownload.Result, error) {
					if failure == "download" {
						return nil, wantErr
					}
					if err := os.MkdirAll(filepath.Dir(opts.OutputPath), 0o755); err != nil {
						return nil, err
					}
					return nil, os.WriteFile(opts.OutputPath, []byte("candidate"), 0o755)
				},
			}, func(runtimeprofile.Status) error { return wantErr })
			if !errors.Is(err, wantErr) {
				t.Fatalf("error = %v", err)
			}
			if data, _ := os.ReadFile(profile); string(data) != string(old) {
				t.Fatalf("old selection lost: %s", data)
			}
			if data, err := os.ReadFile(oldCore); err != nil || string(data) != "working" {
				t.Fatalf("old core lost: %q %v", data, err)
			}
			if _, err := os.Stat(filepath.Join(dir, runtimeprofile.SmartCorePath)); !os.IsNotExist(err) {
				t.Fatalf("failed new selection kept a candidate: %v", err)
			}
		})
	}
}
