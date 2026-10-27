package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"localclash/internal/appinit"
	"localclash/internal/coredownload"
	"localclash/internal/runtimeprofile"
)

func TestConfigConfigureDownloadFailurePreservesProfileAndServerCore(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, runtimeprofile.DefaultPath)
	old := `{"version":2,"mode":"router","core":"meta"}`
	writeMCPFile(t, profile, old)
	before, err := os.ReadFile(profile)
	if err != nil {
		t.Fatal(err)
	}
	oldCore := filepath.Join(dir, runtimeprofile.MetaCorePath)
	writeMCPFile(t, oldCore, "working")
	server := NewServerWithState(appinit.RuntimeState{Paths: appinit.RuntimePaths{
		WorkspaceRoot: dir, RuntimeProfilePath: profile, CorePath: oldCore,
	}})
	wantErr := errors.New("selected download failed")
	server.downloadCore = func(_ context.Context, opts coredownload.Options) ([]coredownload.Result, error) {
		if opts.Flavor != coredownload.FlavorSmart {
			t.Fatalf("requested %q instead of smart", opts.Flavor)
		}
		return nil, wantErr
	}
	_, err = server.callConfigConfigure(context.Background(), json.RawMessage(`{"core":"smart"}`))
	if !errors.Is(err, wantErr) {
		t.Fatalf("error=%v", err)
	}
	if got, _ := os.ReadFile(profile); string(got) != string(before) || server.state.Paths.CorePath != oldCore {
		t.Fatalf("selection changed on acquisition failure: profile=%s state=%s", got, server.state.Paths.CorePath)
	}
	if got, _ := os.ReadFile(oldCore); string(got) != "working" {
		t.Fatalf("old binary changed: %q", got)
	}
}
