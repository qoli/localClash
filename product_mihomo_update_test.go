package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"localclash/internal/appinit"
	"localclash/internal/coredownload"
)

func TestUpdateMihomoDownloadsAndKeepsOnlySelectedFlavor(t *testing.T) {
	for _, flavor := range []string{coredownload.FlavorMeta, coredownload.FlavorSmart} {
		t.Run(flavor, func(t *testing.T) {
			state := updateTestState(t, flavor)
			dir := filepath.Dir(state.Paths.CorePath)
			other := "lc-mihomo-meta"
			if flavor == coredownload.FlavorMeta {
				other = "lc-mihomo-smart"
			}
			writeUpdateTestFile(t, state.Paths.CorePath, updateTestCore("old", false))
			writeUpdateTestFile(t, filepath.Join(dir, other), "unused")
			writeUpdateTestFile(t, state.Paths.CorePath+".pre-old-diagnostic", "old backup")
			writeUpdateTestFile(t, filepath.Join(dir, "unrelated"), "preserve")
			calls := 0
			oldDownload := downloadCore
			downloadCore = func(_ context.Context, opts coredownload.Options) ([]coredownload.Result, error) {
				calls++
				if opts.Flavor != flavor {
					t.Fatalf("download flavor=%q want %q", opts.Flavor, flavor)
				}
				path := filepath.Join(opts.OutputDir, "linux-test", "lc-mihomo-"+flavor)
				writeUpdateTestFile(t, path, updateTestCore("candidate", false))
				return []coredownload.Result{{OutputPath: path, Flavor: flavor}}, nil
			}
			t.Cleanup(func() { downloadCore = oldDownload })
			result, err := updateMihomoComponents(context.Background(), state)
			if err != nil || !result.Changed || !result.ValidationRequired || calls != 1 || len(result.Downloads) != 1 || len(result.Promoted) != 1 || len(result.Removed) != 2 {
				t.Fatalf("result=%+v calls=%d err=%v", result, calls, err)
			}
			if _, err := os.Stat(filepath.Join(dir, other)); !os.IsNotExist(err) {
				t.Fatalf("inactive core remains: %v", err)
			}
			for _, path := range []string{state.Paths.CorePath, filepath.Join(dir, "unrelated")} {
				if _, err := os.Stat(path); err != nil {
					t.Fatal(err)
				}
			}
			result, err = updateMihomoComponents(context.Background(), state)
			if err != nil || result.Changed || len(result.Promoted) != 0 || len(result.Removed) != 0 {
				t.Fatalf("repeat update should be unchanged: %+v err=%v", result, err)
			}
			entries, err := os.ReadDir(state.Paths.RuntimeRoot)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), ".mihomo-update-") {
					t.Fatalf("update transaction remains: %s", entry.Name())
				}
			}
		})
	}
}

func TestUpdateMihomoFailureKeepsBothExistingCoresAndBackups(t *testing.T) {
	for _, failure := range []string{"download", "config-test"} {
		t.Run(failure, func(t *testing.T) {
			state := updateTestState(t, coredownload.FlavorMeta)
			old := map[string]string{
				state.Paths.CorePath: updateTestCore("working", false),
				filepath.Join(filepath.Dir(state.Paths.CorePath), "lc-mihomo-smart"): "other",
				state.Paths.CorePath + ".pre-diagnostic":                             "backup",
			}
			for path, content := range old {
				writeUpdateTestFile(t, path, content)
			}
			oldDownload := downloadCore
			downloadCore = func(_ context.Context, opts coredownload.Options) ([]coredownload.Result, error) {
				if failure == "download" {
					return nil, errors.New("download unavailable")
				}
				path := filepath.Join(opts.OutputDir, "lc-mihomo-meta")
				writeUpdateTestFile(t, path, updateTestCore("invalid", true))
				return []coredownload.Result{{OutputPath: path, Flavor: coredownload.FlavorMeta}}, nil
			}
			t.Cleanup(func() { downloadCore = oldDownload })
			if _, err := updateMihomoComponents(context.Background(), state); err == nil {
				t.Fatal("failed acquisition or validation must report failure")
			}
			for path, want := range old {
				if data, err := os.ReadFile(path); err != nil || string(data) != want {
					t.Errorf("%s changed on failed update: %q, %v", path, data, err)
				}
			}
		})
	}
}

func updateTestState(t *testing.T, flavor string) appinit.RuntimeState {
	t.Helper()
	dir := t.TempDir()
	root := filepath.Join(dir, ".runtime")
	config := filepath.Join(root, "mihomo", "config.yaml")
	writeUpdateTestFile(t, config, "rules:\n  - MATCH,DIRECT\n")
	return appinit.RuntimeState{Paths: appinit.RuntimePaths{
		WorkspaceRoot: dir, RuntimeRoot: root, GeneratedConfig: config,
		MihomoRuntimeDir: filepath.Dir(config), CorePath: filepath.Join(dir, "bin", "linux-test", "lc-mihomo-"+flavor),
	}}
}

func updateTestCore(version string, rejectConfig bool) string {
	exit := "0"
	if rejectConfig {
		exit = "9"
	}
	return "#!/bin/sh\nif [ \"$1\" = -v ]; then echo 'Mihomo " + version + "'; exit 0; fi\nexit " + exit + "\n"
}

func TestApplyAcquiresRequestedFlavorBeforeSavingSelection(t *testing.T) {
	state := updateTestState(t, coredownload.FlavorMeta)
	state.Paths.RuntimeProfilePath = filepath.Join(state.Paths.WorkspaceRoot, "localclash-runtime.json")
	old := `{"version":2,"mode":"router","core":"meta"}`
	writeUpdateTestFile(t, state.Paths.RuntimeProfilePath, old)
	writeUpdateTestFile(t, state.Paths.CorePath, updateTestCore("working", false))
	wantErr := errors.New("target download failed")
	oldDownload := downloadCore
	downloadCore = func(_ context.Context, opts coredownload.Options) ([]coredownload.Result, error) {
		if opts.Flavor != coredownload.FlavorSmart {
			t.Fatalf("apply downloaded current core %q instead of requested smart", opts.Flavor)
		}
		if data, _ := os.ReadFile(state.Paths.RuntimeProfilePath); string(data) != old {
			t.Fatalf("selection changed before download: %s", data)
		}
		return nil, wantErr
	}
	t.Cleanup(func() { downloadCore = oldDownload })
	_, _, err := executeDesiredState(desiredStateInput{
		Config:     &desiredConfig{Core: "smart", RuntimeProfile: "router"},
		Components: &desiredComponents{Mihomo: "installed_or_latest"},
	}, state)
	if !errors.Is(err, wantErr) {
		t.Fatalf("error=%v", err)
	}
	if data, _ := os.ReadFile(state.Paths.RuntimeProfilePath); string(data) != old {
		t.Fatalf("failed download changed profile: %s", data)
	}
}

func TestApplyFailedRenderRestoresCoreSelectionAndGeneratedConfig(t *testing.T) {
	state := updateTestState(t, coredownload.FlavorMeta)
	state.Paths.RuntimeProfilePath = filepath.Join(state.Paths.WorkspaceRoot, "localclash-runtime.json")
	state.Paths.SubscriptionPath = filepath.Join(state.Paths.WorkspaceRoot, "missing-subscription.gob")
	old := `{"version":2,"mode":"router","core":"meta"}`
	writeUpdateTestFile(t, state.Paths.RuntimeProfilePath, old)
	writeUpdateTestFile(t, state.Paths.CorePath, updateTestCore("working", false))
	oldGenerated, err := os.ReadFile(state.Paths.GeneratedConfig)
	if err != nil {
		t.Fatal(err)
	}
	oldDownload := downloadCore
	downloadCore = func(_ context.Context, opts coredownload.Options) ([]coredownload.Result, error) {
		writeUpdateTestFile(t, opts.OutputPath, updateTestCore("smart", false))
		return []coredownload.Result{{OutputPath: opts.OutputPath, Flavor: opts.Flavor}}, nil
	}
	t.Cleanup(func() { downloadCore = oldDownload })
	_, _, err = executeDesiredState(desiredStateInput{Config: &desiredConfig{Core: "smart", RuntimeProfile: "router"}}, state)
	if err == nil {
		t.Fatal("missing subscription must fail the render")
	}
	if data, _ := os.ReadFile(state.Paths.RuntimeProfilePath); string(data) != old {
		t.Fatalf("failed render kept new selection: %s", data)
	}
	if data, _ := os.ReadFile(state.Paths.GeneratedConfig); string(data) != string(oldGenerated) {
		t.Fatalf("failed render lost previous generated config: %s", data)
	}
	if _, err := os.Stat(state.Paths.CorePath); err != nil {
		t.Fatal("previous working core was removed", err)
	}
}

func TestMihomoCandidatesChangedUsesContentNotDownloadOccurrence(t *testing.T) {
	dir := t.TempDir()
	targetDir := filepath.Join(dir, "managed")
	candidateDir := filepath.Join(dir, "candidate")
	candidates := map[string]string{}
	for _, name := range []string{"lc-mihomo-meta", "lc-mihomo-smart"} {
		contents := "same-" + name
		writeUpdateTestFile(t, filepath.Join(targetDir, name), contents)
		candidate := filepath.Join(candidateDir, name)
		writeUpdateTestFile(t, candidate, contents)
		candidates[name] = candidate
	}

	changed, err := mihomoCandidatesChanged(candidates, targetDir)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("byte-identical Mihomo pair was reported as changed")
	}

	writeUpdateTestFile(t, candidates["lc-mihomo-smart"], "new-smart")
	changed, err = mihomoCandidatesChanged(candidates, targetDir)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("changed Smart core was reported as unchanged")
	}
}

func TestMihomoCandidatesChangedTreatsMissingTargetAsChange(t *testing.T) {
	dir := t.TempDir()
	candidate := filepath.Join(dir, "candidate", "lc-mihomo-meta")
	writeUpdateTestFile(t, candidate, "meta")

	changed, err := mihomoCandidatesChanged(map[string]string{"lc-mihomo-meta": candidate}, filepath.Join(dir, "managed"))
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("missing managed core was reported as unchanged")
	}
}

func TestPromoteMihomoCandidatesReplacesPair(t *testing.T) {
	dir := t.TempDir()
	targetDir := filepath.Join(dir, "managed")
	candidateDir := filepath.Join(dir, "candidate")
	if err := os.MkdirAll(candidateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"lc-mihomo-meta", "lc-mihomo-smart"} {
		writeUpdateTestFile(t, filepath.Join(targetDir, name), "old-"+name)
		writeUpdateTestFile(t, filepath.Join(candidateDir, name), "new-"+name)
	}

	promoted, err := promoteMihomoCandidates(map[string]string{
		"lc-mihomo-meta":  filepath.Join(candidateDir, "lc-mihomo-meta"),
		"lc-mihomo-smart": filepath.Join(candidateDir, "lc-mihomo-smart"),
	}, targetDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(promoted) != 2 {
		t.Fatalf("promoted = %+v, want both Mihomo flavors", promoted)
	}
	for _, name := range []string{"lc-mihomo-meta", "lc-mihomo-smart"} {
		data, err := os.ReadFile(filepath.Join(targetDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "new-"+name {
			t.Fatalf("%s = %q, want promoted candidate", name, data)
		}
		if _, err := os.Stat(filepath.Join(targetDir, name) + ".previous"); !os.IsNotExist(err) {
			t.Fatalf("rollback artifact for %s remains, err=%v", name, err)
		}
	}
}

func TestPromoteMihomoCandidatesRejectsStaleRollbackArtifactWithoutMutation(t *testing.T) {
	dir := t.TempDir()
	targetDir := filepath.Join(dir, "managed")
	candidateDir := filepath.Join(dir, "candidate")
	writeUpdateTestFile(t, filepath.Join(targetDir, "lc-mihomo-meta"), "old-meta")
	writeUpdateTestFile(t, filepath.Join(targetDir, "lc-mihomo-smart"), "old-smart")
	writeUpdateTestFile(t, filepath.Join(targetDir, "lc-mihomo-smart.previous"), "stale")
	writeUpdateTestFile(t, filepath.Join(candidateDir, "lc-mihomo-meta"), "new-meta")
	writeUpdateTestFile(t, filepath.Join(candidateDir, "lc-mihomo-smart"), "new-smart")

	_, err := promoteMihomoCandidates(map[string]string{
		"lc-mihomo-meta":  filepath.Join(candidateDir, "lc-mihomo-meta"),
		"lc-mihomo-smart": filepath.Join(candidateDir, "lc-mihomo-smart"),
	}, targetDir)
	if err == nil {
		t.Fatal("stale rollback artifact did not fail")
	}
	for name, want := range map[string]string{"lc-mihomo-meta": "old-meta", "lc-mihomo-smart": "old-smart"} {
		data, readErr := os.ReadFile(filepath.Join(targetDir, name))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if string(data) != want {
			t.Fatalf("%s = %q, want unchanged %q", name, data, want)
		}
	}
}

func TestPromoteMihomoCandidatesRollsBackPairWhenPromotionFails(t *testing.T) {
	dir := t.TempDir()
	targetDir := filepath.Join(dir, "managed")
	candidateDir := filepath.Join(dir, "candidate")
	writeUpdateTestFile(t, filepath.Join(targetDir, "lc-mihomo-meta"), "old-meta")
	writeUpdateTestFile(t, filepath.Join(targetDir, "lc-mihomo-smart"), "old-smart")
	writeUpdateTestFile(t, filepath.Join(candidateDir, "lc-mihomo-meta"), "new-meta")

	_, err := promoteMihomoCandidates(map[string]string{
		"lc-mihomo-meta":  filepath.Join(candidateDir, "lc-mihomo-meta"),
		"lc-mihomo-smart": filepath.Join(candidateDir, "missing-smart"),
	}, targetDir)
	if err == nil {
		t.Fatal("missing second candidate did not fail promotion")
	}
	for name, want := range map[string]string{"lc-mihomo-meta": "old-meta", "lc-mihomo-smart": "old-smart"} {
		data, readErr := os.ReadFile(filepath.Join(targetDir, name))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if string(data) != want {
			t.Fatalf("%s = %q after rollback, want %q", name, data, want)
		}
	}
}

func writeUpdateTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		t.Fatal(err)
	}
}
