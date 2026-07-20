package corerun

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"localclash/internal/runtimesupervision"
)

func TestStartMissingConfigReturnsError(t *testing.T) {
	dir := t.TempDir()
	core := filepath.Join(dir, "lc-mihomo-meta")
	writeStartExecutable(t, core, "#!/bin/sh\nexit 0\n")

	_, err := Start(context.Background(), StartOptions{
		CorePath:   core,
		ConfigPath: filepath.Join(dir, "missing.yaml"),
		WorkDir:    filepath.Join(dir, "runtime"),
	})
	if err == nil || !strings.Contains(err.Error(), "config") {
		t.Fatalf("error = %v, want missing config error", err)
	}
}

func TestStartRejectsUnmanagedCoreName(t *testing.T) {
	dir := t.TempDir()
	core := filepath.Join(dir, "mihomo")
	writeStartExecutable(t, core, "#!/bin/sh\nexit 0\n")
	config := writeStartConfig(t, dir)

	_, err := Start(context.Background(), StartOptions{
		CorePath:   core,
		ConfigPath: config,
		WorkDir:    filepath.Join(dir, "runtime"),
	})
	if err == nil || !strings.Contains(err.Error(), "not a localClash managed core name") {
		t.Fatalf("error = %v, want managed core name rejection", err)
	}
}

func TestStartRejectsConfigTestFailure(t *testing.T) {
	dir := t.TempDir()
	core := filepath.Join(dir, "lc-mihomo-meta")
	writeStartExecutable(t, core, `#!/bin/sh
if [ "$1" = "-v" ]; then
  echo Mihomo Meta test
  exit 0
fi
for arg in "$@"; do
  if [ "$arg" = "-t" ]; then
    echo bad config >&2
    exit 9
  fi
done
sleep 30
`)
	config := writeStartConfig(t, dir)

	_, err := Start(context.Background(), StartOptions{
		CorePath:   core,
		ConfigPath: config,
		WorkDir:    filepath.Join(dir, "runtime"),
	})
	if err == nil || !strings.Contains(err.Error(), "mihomo config test failed") {
		t.Fatalf("error = %v, want config test failure", err)
	}
}

func TestStartAlreadyRunningDoesNotStartSecondRuntime(t *testing.T) {
	dir := t.TempDir()
	core := filepath.Join(dir, "lc-mihomo-meta")
	writeStartExecutable(t, core, `#!/bin/sh
if [ "$1" = "-v" ]; then
  echo Mihomo Meta test
  exit 0
fi
for arg in "$@"; do
  if [ "$arg" = "-t" ]; then
    echo configuration test is successful
    exit 0
  fi
done
exit 44
`)
	config := writeStartConfig(t, dir)
	workDir := filepath.Join(dir, "runtime")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	table := stubProcessTable(t)
	current := exec.Command("sleep", "30")
	if err := current.Start(); err != nil {
		t.Fatal(err)
	}
	defer killProcess(current.Process.Pid)
	go func() { _ = current.Wait() }()
	table.add(current.Process.Pid, "lc-mihomo-meta", []string{core, "-d", workDir, "-f", config})

	result, err := Start(context.Background(), StartOptions{
		CorePath:   core,
		ConfigPath: config,
		WorkDir:    workDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Started || !result.AlreadyRunning || result.PID != current.Process.Pid {
		t.Fatalf("result = %+v, want already running pid %d", result, current.Process.Pid)
	}
}

func TestStartLaunchesBackgroundRuntime(t *testing.T) {
	dir := t.TempDir()
	core := filepath.Join(dir, "lc-mihomo-meta")
	writeStartExecutable(t, core, `#!/bin/sh
if [ "$1" = "-v" ]; then
  echo Mihomo Meta test
  exit 0
fi
for arg in "$@"; do
  if [ "$arg" = "-t" ]; then
    echo configuration test is successful
    exit 0
  fi
done
echo runtime started
sleep 30
`)
	config := writeStartConfig(t, dir)
	workDir := filepath.Join(dir, "runtime")
	logPath := filepath.Join(workDir, "mihomo.log")
	table := stubProcessTable(t)
	stubAfterProcessStart(t, func(started *exec.Cmd) {
		table.add(started.Process.Pid, "lc-mihomo-meta", []string{core, "-d", workDir, "-f", config})
	})

	result, err := Start(context.Background(), StartOptions{
		CorePath:   core,
		ConfigPath: config,
		WorkDir:    workDir,
		LogPath:    logPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer killProcess(result.PID)
	if !result.Started || result.AlreadyRunning || result.PID == 0 {
		t.Fatalf("result = %+v, want started pid", result)
	}
	if result.LogFile != logPath || result.Config != config || result.ExternalUIURL != "http://127.0.0.1:9090/ui" {
		t.Fatalf("result = %+v, want paths and ui url", result)
	}
	if len(result.Warnings) < 2 || !strings.Contains(result.Warnings[0], "network connectivity") {
		t.Fatalf("warnings = %+v, want network warning", result.Warnings)
	}
	state, err := runtimesupervision.Read(workDir)
	if err != nil {
		t.Fatal(err)
	}
	if state.State != runtimesupervision.StateRunning || state.PID != result.PID || state.Attempts != 0 {
		t.Fatalf("supervision state = %+v, want running pid %d", state, result.PID)
	}
	info, err := os.Stat(runtimesupervision.Path(workDir))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("supervision state mode = %o, want 600", info.Mode().Perm())
	}
}

func TestStartCleansUpNewProcessWhenControllerHealthFails(t *testing.T) {
	dir := t.TempDir()
	core := filepath.Join(dir, "lc-mihomo-meta")
	writeStartExecutable(t, core, `#!/bin/sh
if [ "$1" = "-v" ]; then
  echo Mihomo Meta test
  exit 0
fi
for arg in "$@"; do
  if [ "$arg" = "-t" ]; then
    echo configuration test is successful
    exit 0
  fi
done
exec sleep 30
`)
	config := writeStartConfig(t, dir)
	workDir := filepath.Join(dir, "runtime")
	table := stubProcessTable(t)
	originalHealth := probeRuntimeHealth
	probeRuntimeHealth = func(context.Context, string, int, time.Duration) error {
		return errors.New("controller unavailable")
	}
	t.Cleanup(func() { probeRuntimeHealth = originalHealth })
	stubAfterProcessStart(t, func(started *exec.Cmd) {
		table.add(started.Process.Pid, "lc-mihomo-meta", []string{core, "-d", workDir, "-f", config})
	})

	result, err := Start(context.Background(), StartOptions{
		CorePath:   core,
		ConfigPath: config,
		WorkDir:    workDir,
	})
	if err == nil || !strings.Contains(err.Error(), "controller unavailable") {
		t.Fatalf("error = %v, want controller health failure", err)
	}
	if !result.Started || result.PID == 0 {
		t.Fatalf("result = %+v, want attempted process pid", result)
	}
	if !waitForExit(result.PID, 2*time.Second) {
		t.Fatalf("newly started pid %d remained running after health failure", result.PID)
	}
	state, err := runtimesupervision.Read(workDir)
	if err != nil {
		t.Fatal(err)
	}
	if state.State != runtimesupervision.StateStopped || state.PID != 0 {
		t.Fatalf("supervision state = %+v, want stopped without pid", state)
	}
	events, err := os.ReadFile(runtimesupervision.EventLogPath(workDir))
	if err != nil {
		t.Fatal(err)
	}
	if text := string(events); !strings.Contains(text, `"event":"runtime_start_cleanup"`) || !strings.Contains(text, `"outcome":"stopped"`) {
		t.Fatalf("watchdog events = %s, want successful runtime_start_cleanup", text)
	}
}

func TestReadRuntimeConfigEndpointsScansOnlyTopLevelFields(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "mihomo.yaml")
	if err := os.WriteFile(config, []byte(`
proxies:
  - name: external-controller: nested
    server: example.com
external-controller: "127.0.0.1:19090" # local dashboard controller
external-ui: 'ui/zashboard'
proxy-groups:
  - name: external-ui: nested
`), 0o644); err != nil {
		t.Fatal(err)
	}

	endpoints := readRuntimeConfigEndpoints(config)
	if endpoints.ExternalController != "127.0.0.1:19090" {
		t.Fatalf("external controller = %q", endpoints.ExternalController)
	}
	if endpoints.ExternalUI != "ui/zashboard" {
		t.Fatalf("external ui = %q", endpoints.ExternalUI)
	}
	if got := externalUIURL(endpoints.ExternalController, endpoints.ExternalUI); got != "http://127.0.0.1:19090/ui" {
		t.Fatalf("external ui url = %q", got)
	}
}

func writeStartConfig(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "mihomo.yaml")
	if err := os.WriteFile(path, []byte("external-controller: 127.0.0.1:9090\nexternal-ui: ui/zashboard\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeStartExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func killProcess(pid int) {
	process, err := os.FindProcess(pid)
	if err == nil {
		_ = process.Kill()
	}
}
