package runtimesupervision

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteReadStateAtomicallyWithPrivatePermissions(t *testing.T) {
	dir := t.TempDir()
	state := validTestState(dir)
	if err := Write(dir, state); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("state mode = %o, want 600", got)
	}
	read, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if read != state {
		t.Fatalf("read state = %+v, want %+v", read, state)
	}
}

func TestReadRejectsMalformedAndUnknownState(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(Path(dir), []byte(`{"version":1,"state":"invented","runtime_dir":"/tmp/runtime","updated_at":"2026-07-20T12:00:00Z"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(dir); err == nil || !strings.Contains(err.Error(), "unknown state") {
		t.Fatalf("Read error = %v, want unknown state rejection", err)
	}
	if err := os.WriteFile(Path(dir), []byte(`{"version":1,"state":"stopped","runtime_dir":"/tmp/runtime","updated_at":"2026-07-20T12:00:00Z","unexpected":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(dir); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("Read error = %v, want unknown field rejection", err)
	}
}

func TestWriteRejectsStateForDifferentRuntimeDirectory(t *testing.T) {
	dir := t.TempDir()
	state := validTestState(filepath.Join(dir, "other"))
	if err := Write(filepath.Join(dir, "runtime"), state); err == nil || !strings.Contains(err.Error(), "does not match state file directory") {
		t.Fatalf("Write error = %v, want runtime directory mismatch", err)
	}
}

func TestAppendEventUsesExistingWatchdogLogLocation(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".runtime", "mihomo")
	if err := AppendEvent(dir, map[string]any{"event": "runtime_restart_attempt", "attempt": 1}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(dir), "logs", "watchdog.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, `"event":"runtime_restart_attempt"`) || !strings.Contains(text, `"attempt":1`) || !strings.Contains(text, `"ts":`) {
		t.Fatalf("event log = %s", text)
	}
}

func validTestState(dir string) State {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	return State{
		Version:             Version,
		State:               StateRunning,
		BootID:              "boot-test",
		CorePath:            filepath.Join(dir, "lc-mihomo-smart"),
		CoreSHA256:          strings.Repeat("a", 64),
		LauncherPath:        filepath.Join(dir, "lc-mihomo-smart"),
		ConfigPath:          filepath.Join(dir, "config.yaml"),
		ConfigSHA256:        strings.Repeat("b", 64),
		WorkDir:             dir,
		LogPath:             filepath.Join(dir, "mihomo.log"),
		ValidationCachePath: filepath.Join(dir, "validation.json"),
		PID:                 42,
		HealthySince:        now,
		LastHealthyAt:       now,
		UpdatedAt:           now,
	}
}
