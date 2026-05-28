package reset

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestRunDryRunDoesNotDeleteFactoryResetTargets(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeResetFile(t, filepath.Join(".runtime", "logs", "mcp.log"), "log")
	writeResetFile(t, filepath.Join("generated", "mihomo.yaml"), "config")
	writeResetFile(t, "localclash.json", "version: 1\n")
	writeResetFile(t, "localclash-packs.gob", "version: 1\n")
	writeResetFile(t, "localclash-subscriptions.json", "sources: []\n")
	writeResetFile(t, "localclash-runtime.json", "active: router\n")
	writeResetFile(t, filepath.Join("profiles", "router.yaml"), "mihomo: {}\n")
	writeResetFile(t, "subscription.gob", "proxies: []\n")
	writeResetFile(t, "subscription-backup.gob", "proxies: []\n")

	var out bytes.Buffer
	result, err := Run(Options{DryRun: true, Out: &out})
	if err != nil {
		t.Fatal(err)
	}
	if !result.DryRun || len(result.Deleted) != 9 {
		t.Fatalf("result = %+v, want dry-run with nine delete targets", result)
	}
	for _, path := range []string{".runtime", "generated", "localclash.json", "localclash-subscriptions.json", "localclash-runtime.json", "profiles", "subscription.gob", "subscription-backup.gob"} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("%s should remain after dry-run: %v", path, err)
		}
	}
	if !strings.Contains(out.String(), "localClash factory reset dry run") || !strings.Contains(out.String(), "subscription-backup.gob") {
		t.Fatalf("output = %q, want dry-run plan", out.String())
	}
}

func TestRunDeletesFactoryResetTargetsWithYes(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeResetFile(t, filepath.Join(".runtime", "mihomo", "logs", "mihomo.log"), "log")
	writeResetFile(t, filepath.Join("generated", "mihomo.yaml"), "config")
	writeResetFile(t, filepath.Join("bin", "mihomo"), "binary")
	writeResetFile(t, filepath.Join("policy-templates", "minimal.json"), "template")
	writeResetFile(t, filepath.Join("rule-sources", "source.json"), "source")
	writeResetFile(t, "localclash.json", "version: 1\n")
	writeResetFile(t, "localclash-packs.gob", "version: 1\n")
	writeResetFile(t, "localclash-subscriptions.json", "sources: []\n")
	writeResetFile(t, "localclash-runtime.json", "active: router\n")
	writeResetFile(t, filepath.Join("profiles", "normal.yaml"), "mihomo: {}\n")
	writeResetFile(t, "subscription.gob", "proxies: []\n")

	var out bytes.Buffer
	if _, err := Run(Options{Yes: true, Out: &out}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{".runtime", "generated", "localclash.json", "localclash-packs.gob", "localclash-subscriptions.json", "localclash-runtime.json", "profiles", "subscription.gob"} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s should be deleted, err=%v", path, err)
		}
	}
	for _, path := range []string{filepath.Join("bin", "mihomo"), filepath.Join("policy-templates", "minimal.json"), filepath.Join("rule-sources", "source.json")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("%s should be kept: %v", path, err)
		}
	}
	if !strings.Contains(out.String(), "Reset complete.") {
		t.Fatalf("output = %q, want completion", out.String())
	}
}

func TestRunFullDryRunPlansWorkspaceWithoutDeleting(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "localclash")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	writeResetFile(t, filepath.Join("profiles", "router.json"), "{}\n")
	writeResetFile(t, filepath.Join("bin", "mihomo"), "binary")
	writeResetFile(t, workspaceMarkerPath(dir), "version=1\nowner=localclash\n")
	expected, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	result, err := Run(Options{Full: true, DryRun: true, Workspace: dir, Out: &out})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Full || !result.DryRun || len(result.Deleted) != 1 || result.Deleted[0].Path != expected {
		t.Fatalf("result = %+v, want full dry-run plan for %s", result, expected)
	}
	if _, err := os.Stat(filepath.Join(dir, "profiles", "router.json")); err != nil {
		t.Fatalf("workspace should remain after full dry-run: %v", err)
	}
	if !strings.Contains(out.String(), "full workspace reset dry run") || !strings.Contains(out.String(), dir) {
		t.Fatalf("output = %q, want full dry-run plan", out.String())
	}
}

func TestRunFullDeletesWorkspaceWithYes(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "localclash")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	writeResetFile(t, filepath.Join("profiles", "router.json"), "{}\n")
	writeResetFile(t, filepath.Join("bin", "mihomo"), "binary")
	writeResetFile(t, workspaceMarkerPath(dir), "version=1\nowner=localclash\n")
	expected, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	result, err := Run(Options{Full: true, Yes: true, Workspace: dir, Out: &out})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Full || len(result.Deleted) != 1 || result.Deleted[0].Path != expected {
		t.Fatalf("result = %+v, want full reset deletion for %s", result, expected)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("workspace should be deleted, err=%v", err)
	}
	if _, err := os.Stat(parent); err != nil {
		t.Fatalf("parent should remain: %v", err)
	}
	if !strings.Contains(out.String(), "full workspace reset") || !strings.Contains(out.String(), "Reset complete.") {
		t.Fatalf("output = %q, want full reset completion", out.String())
	}
}

func TestRunFullRequiresExplicitWorkspace(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeResetFile(t, "localclash.json", "version: 1\n")

	_, err := Run(Options{Full: true, Yes: true, Out: &bytes.Buffer{}})
	if err == nil || !strings.Contains(err.Error(), "requires an explicit workspace") {
		t.Fatalf("error = %v, want explicit workspace refusal", err)
	}
	if _, err := os.Stat("localclash.json"); err != nil {
		t.Fatalf("workspace content should remain after refused full reset: %v", err)
	}
}

func TestRunFullRejectsMissingWorkspaceMarker(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "localclash")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeResetFile(t, filepath.Join(dir, "localclash.json"), "version: 1\n")

	_, err := Run(Options{Full: true, Yes: true, Workspace: dir, Out: &bytes.Buffer{}})
	if err == nil || !strings.Contains(err.Error(), "missing .localclash-workspace marker") {
		t.Fatalf("error = %v, want missing marker refusal", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "localclash.json")); err != nil {
		t.Fatalf("workspace content should remain after refused full reset: %v", err)
	}
}

func TestRunFullRejectsSourceCheckout(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "localClash")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	writeResetFile(t, "go.mod", "module localclash\n")
	writeResetFile(t, workspaceMarkerPath(dir), "version=1\nowner=localclash\n")

	_, err := Run(Options{Full: true, Yes: true, Workspace: dir, Out: &bytes.Buffer{}})
	if err == nil || !strings.Contains(err.Error(), "source checkout") {
		t.Fatalf("error = %v, want source checkout refusal", err)
	}
	if _, err := os.Stat("go.mod"); err != nil {
		t.Fatalf("source checkout content should remain after refused full reset: %v", err)
	}
}

func TestRunRequiresConfirmation(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeResetFile(t, "localclash.json", "version: 1\n")

	_, err := Run(Options{In: strings.NewReader("no\n"), Out: &bytes.Buffer{}})
	if err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("error = %v, want cancelled", err)
	}
	if _, err := os.Stat("localclash.json"); err != nil {
		t.Fatalf("localclash.json should remain after cancelled reset: %v", err)
	}

	if _, err := Run(Options{In: strings.NewReader(ConfirmationPhrase + "\n"), Out: &bytes.Buffer{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat("localclash.json"); !os.IsNotExist(err) {
		t.Fatalf("localclash.json should be deleted after confirmed reset, err=%v", err)
	}
}

func TestRunRefusesWhileRuntimeIsRunning(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	cmd := startResetFakeRuntime(t, dir)
	writeResetFile(t, filepath.Join(".runtime", "mihomo", "mihomo.pid"), strconv.Itoa(cmd.Process.Pid)+"\n")

	_, err := Run(Options{Yes: true, Out: &bytes.Buffer{}})
	if err == nil || !strings.Contains(err.Error(), "runtime is running") {
		t.Fatalf("error = %v, want running runtime refusal", err)
	}
	if _, err := os.Stat(filepath.Join(".runtime", "mihomo", "mihomo.pid")); err != nil {
		t.Fatalf("pid file should remain after refused reset: %v", err)
	}
}

func TestRunDryRunAllowsRunningRuntime(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	cmd := startResetFakeRuntime(t, dir)
	writeResetFile(t, filepath.Join(".runtime", "mihomo", "mihomo.pid"), strconv.Itoa(cmd.Process.Pid)+"\n")

	result, err := Run(Options{DryRun: true, Out: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.DryRun || len(result.Deleted) != 2 {
		t.Fatalf("result = %+v, want dry-run plan for runtime dir", result)
	}
	if _, err := os.Stat(filepath.Join(".runtime", "mihomo", "mihomo.pid")); err != nil {
		t.Fatalf("pid file should remain after dry-run: %v", err)
	}
}

func writeResetFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func workspaceMarkerPath(dir string) string {
	return filepath.Join(dir, ".localclash-workspace")
}

func startResetFakeRuntime(t *testing.T, dir string) *exec.Cmd {
	t.Helper()
	workDir := filepath.Join(".runtime", "mihomo")
	config := filepath.Join("generated", "mihomo.yaml")
	core := filepath.Join("bin", "linux-"+runtime.GOARCH, "mihomo-meta")
	writeResetFile(t, config, "external-controller: 127.0.0.1:9090\n")
	writeResetFile(t, core, "#!/bin/sh\ntrap 'exit 0' TERM INT\nwhile :; do sleep 1; done\n")
	if err := os.Chmod(core, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(core, "-d", workDir, "-f", config)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	return cmd
}
