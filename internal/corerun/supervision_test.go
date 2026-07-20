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

	"localclash/internal/mihomotest"
	"localclash/internal/runtimesupervision"
)

func TestCheckSupervisionRecoversSameValidatedRuntime(t *testing.T) {
	fixture := newSupervisedFixture(t)
	fixture.crash(fixture.pid)
	now := time.Now().UTC().Add(time.Minute)
	result, err := CheckSupervision(context.Background(), SupervisionCheckOptions{WorkDir: fixture.workDir, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer fixture.crash(result.PID)
	if result.Action != "recovered" || result.Attempt != 1 || result.PID == 0 || result.PID == fixture.pid {
		t.Fatalf("check result = %+v, want first-attempt recovery with a new pid", result)
	}
	state := readSupervisionState(t, fixture.workDir)
	if state.State != runtimesupervision.StateRunning || state.PID != result.PID || state.Attempts != 1 {
		t.Fatalf("state = %+v, want recovered running state", state)
	}
	events := readSupervisionEvents(t, fixture.workDir)
	for _, want := range []string{`"event":"runtime_exit_observed"`, `"event":"runtime_restart_attempt"`, `"event":"runtime_restart_recovered"`} {
		if !strings.Contains(events, want) {
			t.Fatalf("watchdog events = %s, want %s", events, want)
		}
	}
}

func TestCheckSupervisionDoesNotInferMissingState(t *testing.T) {
	workDir := filepath.Join(t.TempDir(), "runtime")
	result, err := CheckSupervision(context.Background(), SupervisionCheckOptions{WorkDir: workDir})
	if err != nil {
		t.Fatal(err)
	}
	if result.Checked || result.Action != "" {
		t.Fatalf("result = %+v, want no inferred supervision", result)
	}
	if _, err := os.Stat(runtimesupervision.Path(workDir)); !os.IsNotExist(err) {
		t.Fatalf("supervision state stat error = %v, want missing state", err)
	}
}

func TestCheckSupervisionBackoffAndLatchAfterThreeFailedRecoveries(t *testing.T) {
	fixture := newSupervisedFixture(t)
	fixture.crash(fixture.pid)
	overrideRuntimeHealth(t, func(context.Context, string, int, time.Duration) error {
		return errors.New("controller unavailable")
	})
	base := time.Now().UTC().Add(time.Minute)

	first := checkSupervisionAt(t, fixture.workDir, base)
	if first.Action != "restart_failed" || first.Attempt != 1 {
		t.Fatalf("first result = %+v", first)
	}
	fixture.crash(first.PID)
	waiting := checkSupervisionAt(t, fixture.workDir, base.Add(5*time.Second))
	if waiting.Action != "waiting" || waiting.Reason != "restart_backoff" {
		t.Fatalf("waiting result = %+v", waiting)
	}
	second := checkSupervisionAt(t, fixture.workDir, base.Add(10*time.Second))
	if second.Action != "restart_failed" || second.Attempt != 2 {
		t.Fatalf("second result = %+v", second)
	}
	fixture.crash(second.PID)
	third := checkSupervisionAt(t, fixture.workDir, base.Add(40*time.Second))
	if third.Action != "restart_failed" || third.Attempt != 3 {
		t.Fatalf("third result = %+v", third)
	}
	fixture.crash(third.PID)
	latched := checkSupervisionAt(t, fixture.workDir, base.Add(41*time.Second))
	if latched.Action != "latched" || latched.Reason != "restart_budget_exhausted" {
		t.Fatalf("latched result = %+v", latched)
	}
	state := readSupervisionState(t, fixture.workDir)
	if state.State != runtimesupervision.StateLatchedFailed || state.Attempts != 3 {
		t.Fatalf("state = %+v, want latched after three attempts", state)
	}
	if !strings.Contains(readSupervisionEvents(t, fixture.workDir), `"event":"runtime_restart_latched"`) {
		t.Fatalf("watchdog events do not contain latch event")
	}
}

func TestCheckSupervisionFailsClosedOnBootConfigValidationAndProcessInvariants(t *testing.T) {
	t.Run("stale boot", func(t *testing.T) {
		fixture := newSupervisedFixture(t)
		fixture.crash(fixture.pid)
		currentBootID = func() (string, error) { return "different-boot", nil }
		result := checkSupervisionAt(t, fixture.workDir, time.Now().UTC())
		if result.Action != "blocked" || result.Reason != "stale_boot_id" {
			t.Fatalf("result = %+v", result)
		}
	})

	t.Run("boot identity unavailable", func(t *testing.T) {
		fixture := newSupervisedFixture(t)
		fixture.crash(fixture.pid)
		currentBootID = func() (string, error) { return "", errors.New("boot id unavailable") }
		result := checkSupervisionAt(t, fixture.workDir, time.Now().UTC())
		if result.Action != "blocked" || result.Reason != "boot_id_unavailable" {
			t.Fatalf("result = %+v", result)
		}
	})

	t.Run("config hash changed", func(t *testing.T) {
		fixture := newSupervisedFixture(t)
		fixture.crash(fixture.pid)
		info, err := os.Stat(fixture.config)
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(fixture.config)
		if err != nil {
			t.Fatal(err)
		}
		changed := strings.Replace(string(data), "9090", "9091", 1)
		if len(changed) != len(data) {
			t.Fatalf("changed config size = %d, want %d", len(changed), len(data))
		}
		if err := os.WriteFile(fixture.config, []byte(changed), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(fixture.config, info.ModTime(), info.ModTime()); err != nil {
			t.Fatal(err)
		}
		result := checkSupervisionAt(t, fixture.workDir, time.Now().UTC())
		if result.Action != "blocked" || result.Reason != "config_hash_mismatch" {
			t.Fatalf("result = %+v", result)
		}
		if state := readSupervisionState(t, fixture.workDir); state.Attempts != 0 {
			t.Fatalf("state = %+v, want no recovery attempt", state)
		}
	})

	t.Run("core hash changed", func(t *testing.T) {
		fixture := newSupervisedFixture(t)
		fixture.crash(fixture.pid)
		file, err := os.OpenFile(fixture.core, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.WriteString("\n# changed after validation\n"); err != nil {
			_ = file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		result := checkSupervisionAt(t, fixture.workDir, time.Now().UTC())
		if result.Action != "blocked" || result.Reason != "core_hash_mismatch" {
			t.Fatalf("result = %+v", result)
		}
	})

	t.Run("invalid managed core identity", func(t *testing.T) {
		fixture := newSupervisedFixture(t)
		fixture.crash(fixture.pid)
		state := readSupervisionState(t, fixture.workDir)
		state.CorePath = filepath.Join(filepath.Dir(fixture.core), "arbitrary-core")
		state.UpdatedAt = supervisionTimestamp(time.Now())
		if err := runtimesupervision.Write(fixture.workDir, state); err != nil {
			t.Fatal(err)
		}
		result := checkSupervisionAt(t, fixture.workDir, time.Now().UTC())
		if result.Action != "blocked" || result.Reason != "invalid_core_identity" {
			t.Fatalf("result = %+v", result)
		}
	})

	t.Run("validation cache missing", func(t *testing.T) {
		fixture := newSupervisedFixture(t)
		fixture.crash(fixture.pid)
		state := readSupervisionState(t, fixture.workDir)
		if err := os.Remove(state.ValidationCachePath); err != nil {
			t.Fatal(err)
		}
		result := checkSupervisionAt(t, fixture.workDir, time.Now().UTC())
		if result.Action != "blocked" || result.Reason != "validation_not_matched" {
			t.Fatalf("result = %+v", result)
		}
	})

	t.Run("multiple managed processes", func(t *testing.T) {
		fixture := newSupervisedFixture(t)
		second := exec.Command("sleep", "30")
		if err := second.Start(); err != nil {
			t.Fatal(err)
		}
		defer fixture.crash(second.Process.Pid)
		fixture.table.add(second.Process.Pid, "lc-mihomo-smart", []string{fixture.core, "-d", fixture.workDir, "-f", fixture.config})
		result := checkSupervisionAt(t, fixture.workDir, time.Now().UTC())
		if result.Action != "blocked" || result.Reason != "multiple_processes" {
			t.Fatalf("result = %+v", result)
		}
	})

	t.Run("process identity changed", func(t *testing.T) {
		fixture := newSupervisedFixture(t)
		otherConfig := filepath.Join(filepath.Dir(fixture.config), "other.yaml")
		if err := os.WriteFile(otherConfig, []byte("mode: direct\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		fixture.table.args[fixture.pid] = []string{fixture.core, "-d", fixture.workDir, "-f", otherConfig}
		result := checkSupervisionAt(t, fixture.workDir, time.Now().UTC())
		if result.Action != "blocked" || result.Reason != "process_identity_mismatch" {
			t.Fatalf("result = %+v", result)
		}
	})
}

func TestCheckSupervisionObservesUnhealthyProcessWithoutKillingIt(t *testing.T) {
	fixture := newSupervisedFixture(t)
	overrideRuntimeHealth(t, func(context.Context, string, int, time.Duration) error {
		return errors.New("controller timeout")
	})
	result := checkSupervisionAt(t, fixture.workDir, time.Now().UTC())
	if result.Action != "observed" || result.Reason != "runtime_unhealthy" || result.PID != fixture.pid {
		t.Fatalf("result = %+v", result)
	}
	if !processRunning(fixture.pid) {
		t.Fatalf("unhealthy runtime pid %d was killed", fixture.pid)
	}
	state := readSupervisionState(t, fixture.workDir)
	if state.State != runtimesupervision.StateRunning || state.Attempts != 0 {
		t.Fatalf("state = %+v, want running without a restart attempt", state)
	}
}

func TestCheckSupervisionStableHealthResetsAttemptBudget(t *testing.T) {
	fixture := newSupervisedFixture(t)
	now := time.Now().UTC().Add(time.Minute)
	state := readSupervisionState(t, fixture.workDir)
	state.Attempts = 2
	state.HealthySince = supervisionTimestamp(now.Add(-10 * time.Minute))
	state.LastHealthyAt = state.HealthySince
	state.NextAttemptAt = supervisionTimestamp(now.Add(time.Hour))
	state.UpdatedAt = supervisionTimestamp(now.Add(-10 * time.Minute))
	if err := runtimesupervision.Write(fixture.workDir, state); err != nil {
		t.Fatal(err)
	}
	result := checkSupervisionAt(t, fixture.workDir, now)
	if result.Action != "healthy" {
		t.Fatalf("result = %+v", result)
	}
	state = readSupervisionState(t, fixture.workDir)
	if state.Attempts != 0 || state.NextAttemptAt != "" {
		t.Fatalf("state = %+v, want reset attempt budget", state)
	}
}

func TestExplicitStopDisarmsAndExplicitStartClearsLatch(t *testing.T) {
	t.Run("stop disarms before watchdog check", func(t *testing.T) {
		fixture := newSupervisedFixture(t)
		result, err := Stop(StopOptions{CorePath: fixture.core, ConfigPath: fixture.config, WorkDir: fixture.workDir, Timeout: 2 * time.Second})
		if err != nil {
			t.Fatal(err)
		}
		if !result.Stopped {
			t.Fatalf("stop result = %+v", result)
		}
		state := readSupervisionState(t, fixture.workDir)
		if state.State != runtimesupervision.StateStopped {
			t.Fatalf("state = %+v, want stopped", state)
		}
		check := checkSupervisionAt(t, fixture.workDir, time.Now().UTC())
		if check.Action != "ignored" || check.Reason != runtimesupervision.StateStopped {
			t.Fatalf("check = %+v, want stopped state ignored", check)
		}
	})

	t.Run("explicit start clears latch", func(t *testing.T) {
		fixture := newSupervisedFixture(t)
		state := readSupervisionState(t, fixture.workDir)
		state.State = runtimesupervision.StateLatchedFailed
		state.Attempts = 3
		state.HealthySince = ""
		state.LastHealthyAt = ""
		state.UpdatedAt = supervisionTimestamp(time.Now())
		if err := runtimesupervision.Write(fixture.workDir, state); err != nil {
			t.Fatal(err)
		}
		result, err := Start(context.Background(), StartOptions{CorePath: fixture.core, ConfigPath: fixture.config, WorkDir: fixture.workDir})
		if err != nil {
			t.Fatal(err)
		}
		if !result.AlreadyRunning || result.PID != fixture.pid {
			t.Fatalf("start result = %+v", result)
		}
		state = readSupervisionState(t, fixture.workDir)
		if state.State != runtimesupervision.StateRunning || state.Attempts != 0 {
			t.Fatalf("state = %+v, want rearmed running state", state)
		}
	})
}

func TestStopWaitsForInFlightRecoveryThenDisarmsIt(t *testing.T) {
	fixture := newSupervisedFixture(t)
	fixture.crash(fixture.pid)
	probeStarted := make(chan struct{})
	releaseProbe := make(chan struct{})
	overrideRuntimeHealth(t, func(context.Context, string, int, time.Duration) error {
		select {
		case <-probeStarted:
		default:
			close(probeStarted)
		}
		<-releaseProbe
		return nil
	})
	checkDone := make(chan error, 1)
	go func() {
		_, err := CheckSupervision(context.Background(), SupervisionCheckOptions{WorkDir: fixture.workDir})
		checkDone <- err
	}()
	select {
	case <-probeStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("recovery did not reach controller health check")
	}
	stopDone := make(chan error, 1)
	go func() {
		_, err := Stop(StopOptions{CorePath: fixture.core, ConfigPath: fixture.config, WorkDir: fixture.workDir, Timeout: 2 * time.Second})
		stopDone <- err
	}()
	select {
	case err := <-stopDone:
		t.Fatalf("stop completed before recovery released its lifecycle lock: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	close(releaseProbe)
	if err := <-checkDone; err != nil {
		t.Fatal(err)
	}
	if err := <-stopDone; err != nil {
		t.Fatal(err)
	}
	state := readSupervisionState(t, fixture.workDir)
	if state.State != runtimesupervision.StateStopped {
		t.Fatalf("state = %+v, want explicit stop to win after recovery", state)
	}
}

func TestHotReloadSuccessUpdatesSupervisedConfigHash(t *testing.T) {
	fixture := newSupervisedFixture(t)
	before := readSupervisionState(t, fixture.workDir)
	if err := os.WriteFile(fixture.config, []byte("external-controller: 127.0.0.1:9090\nexternal-ui: ui/zashboard\nmode: global\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := updateSupervisionAfterHotReload(context.Background(), Options{CorePath: fixture.core, ConfigPath: fixture.config, WorkDir: fixture.workDir, LogPath: before.LogPath}, before.ValidationCachePath, fixture.pid, time.Now()); err == nil {
		t.Fatal("hot reload supervision update succeeded without a matched validation cache")
	}
	if unchanged := readSupervisionState(t, fixture.workDir); unchanged.ConfigSHA256 != before.ConfigSHA256 {
		t.Fatalf("state changed after invalid hot reload proof: before=%+v after=%+v", before, unchanged)
	}
	validation, err := mihomotest.ValidateCached(context.Background(), mihomotest.ValidationOptions{
		CorePath:   fixture.core,
		ConfigPath: fixture.config,
		WorkDir:    fixture.workDir,
		CachePath:  before.ValidationCachePath,
	})
	if err != nil || !validation.Passed {
		t.Fatalf("validation = %+v, err = %v", validation, err)
	}
	if err := updateSupervisionAfterHotReload(context.Background(), Options{CorePath: fixture.core, ConfigPath: fixture.config, WorkDir: fixture.workDir, LogPath: before.LogPath}, before.ValidationCachePath, fixture.pid, time.Now()); err != nil {
		t.Fatal(err)
	}
	after := readSupervisionState(t, fixture.workDir)
	if after.ConfigSHA256 == before.ConfigSHA256 || after.ConfigSHA256 != validation.ConfigSHA256 {
		t.Fatalf("before = %+v, after = %+v, validation = %+v", before, after, validation)
	}
}

type supervisedFixture struct {
	core    string
	config  string
	workDir string
	pid     int
	table   *processTable
}

func newSupervisedFixture(t *testing.T) supervisedFixture {
	t.Helper()
	dir := t.TempDir()
	core := filepath.Join(dir, "lc-mihomo-smart")
	writeStartExecutable(t, core, `#!/bin/sh
if [ "$1" = "-v" ]; then
  echo Mihomo Smart test
  exit 0
fi
for arg in "$@"; do
  if [ "$arg" = "-t" ]; then
    echo configuration test is successful
    exit 0
  fi
done
sleep 30
`)
	config := writeStartConfig(t, dir)
	workDir := filepath.Join(dir, "runtime")
	table := stubProcessTable(t)
	stubAfterProcessStart(t, func(started *exec.Cmd) {
		table.add(started.Process.Pid, "lc-mihomo-smart", []string{core, "-d", workDir, "-f", config})
	})
	result, err := Start(context.Background(), StartOptions{CorePath: core, ConfigPath: config, WorkDir: workDir})
	if err != nil {
		t.Fatal(err)
	}
	fixture := supervisedFixture{core: core, config: config, workDir: workDir, pid: result.PID, table: table}
	t.Cleanup(func() { fixture.crash(result.PID) })
	return fixture
}

func (fixture supervisedFixture) crash(pid int) {
	if pid <= 0 {
		return
	}
	fixture.table.remove(pid)
	killProcess(pid)
}

func checkSupervisionAt(t *testing.T, workDir string, now time.Time) SupervisionCheckResult {
	t.Helper()
	result, err := CheckSupervision(context.Background(), SupervisionCheckOptions{WorkDir: workDir, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func readSupervisionState(t *testing.T, workDir string) runtimesupervision.State {
	t.Helper()
	state, err := runtimesupervision.Read(workDir)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func readSupervisionEvents(t *testing.T, workDir string) string {
	t.Helper()
	data, err := os.ReadFile(runtimesupervision.EventLogPath(workDir))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func overrideRuntimeHealth(t *testing.T, probe func(context.Context, string, int, time.Duration) error) {
	t.Helper()
	original := probeRuntimeHealth
	probeRuntimeHealth = probe
	t.Cleanup(func() { probeRuntimeHealth = original })
}
