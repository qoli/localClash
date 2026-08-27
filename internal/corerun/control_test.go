package corerun

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStatusReportsRunningRuntime(t *testing.T) {
	dir := t.TempDir()
	workDir := filepath.Join(dir, "runtime")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	config := writeStartConfig(t, dir)
	table := stubProcessTable(t)
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer killProcess(cmd.Process.Pid)
	go func() { _ = cmd.Wait() }()
	table.add(cmd.Process.Pid, "lc-mihomo-smart", []string{"lc-mihomo-smart", "-d", ".runtime/mihomo", "-f", "generated/mihomo.yaml"})

	result := Status(StatusOptions{
		ConfigPath: config,
		WorkDir:    workDir,
		LogPath:    filepath.Join(workDir, "mihomo.log"),
	})
	if !result.Running || result.PID != cmd.Process.Pid {
		t.Fatalf("status = %+v, want running pid %d", result, cmd.Process.Pid)
	}
	if result.ExternalUIURL != "http://127.0.0.1:9090/ui" {
		t.Fatalf("external ui url = %q", result.ExternalUIURL)
	}
}

func TestStatusIgnoresUnmanagedProcessName(t *testing.T) {
	dir := t.TempDir()
	workDir := filepath.Join(dir, "runtime")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	config := writeStartConfig(t, dir)
	table := stubProcessTable(t)
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer killProcess(cmd.Process.Pid)
	go func() { _ = cmd.Wait() }()
	table.add(cmd.Process.Pid, "sleep", []string{"sleep", "30"})

	result := Status(StatusOptions{
		ConfigPath: config,
		WorkDir:    workDir,
		LogPath:    filepath.Join(workDir, "mihomo.log"),
	})
	if result.Running || result.PID != 0 {
		t.Fatalf("status = %+v, want unmanaged process ignored", result)
	}
}

func TestStatusIgnoresManagedNameWithDifferentRuntimeIdentity(t *testing.T) {
	dir := t.TempDir()
	core := filepath.Join(dir, "lc-mihomo-meta")
	writeStartExecutable(t, core, "#!/bin/sh\nexit 0\n")
	config := writeStartConfig(t, dir)
	workDir := filepath.Join(dir, "runtime")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	otherDir := t.TempDir()
	otherCore := filepath.Join(otherDir, "lc-mihomo-meta")
	writeStartExecutable(t, otherCore, "#!/bin/sh\nexit 0\n")
	otherConfig := writeStartConfig(t, otherDir)
	otherWorkDir := filepath.Join(otherDir, "runtime")
	if err := os.MkdirAll(otherWorkDir, 0o755); err != nil {
		t.Fatal(err)
	}

	table := stubProcessTable(t)
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer killProcess(cmd.Process.Pid)
	go func() { _ = cmd.Wait() }()
	table.add(cmd.Process.Pid, "lc-mihomo-meta", []string{otherCore, "-d", otherWorkDir, "-f", otherConfig})

	result := Status(StatusOptions{CorePath: core, ConfigPath: config, WorkDir: workDir})
	if result.Running || result.PID != 0 {
		t.Fatalf("status = %+v, want different runtime identity ignored", result)
	}
}

func TestStatusSkipsConfigTestProcess(t *testing.T) {
	dir := t.TempDir()
	workDir := filepath.Join(dir, "runtime")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	table := stubProcessTable(t)
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer killProcess(cmd.Process.Pid)
	go func() { _ = cmd.Wait() }()
	table.add(cmd.Process.Pid, "lc-mihomo-meta", []string{"lc-mihomo-meta", "-d", workDir, "-f", "generated/mihomo.yaml", "-t"})

	result := Status(StatusOptions{WorkDir: workDir})
	if result.Running || result.PID != 0 {
		t.Fatalf("status = %+v, want config-test process skipped", result)
	}
}

func TestStatusSkipsCoreVersionProcess(t *testing.T) {
	dir := t.TempDir()
	workDir := filepath.Join(dir, "runtime")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	table := stubProcessTable(t)
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer killProcess(cmd.Process.Pid)
	go func() { _ = cmd.Wait() }()
	table.add(cmd.Process.Pid, "lc-mihomo-meta", []string{"lc-mihomo-meta", "-v"})

	result := Status(StatusOptions{WorkDir: workDir})
	if result.Running || result.PID != 0 {
		t.Fatalf("status = %+v, want core version process skipped", result)
	}
}

func TestManagedRuntimeIdentityRecordsExplicitLauncherWrapper(t *testing.T) {
	dir := t.TempDir()
	core := filepath.Join(dir, "lc-mihomo-meta")
	writeStartExecutable(t, core, "#!/bin/sh\nexit 0\n")
	config := writeStartConfig(t, dir)
	workDir := filepath.Join(dir, "runtime")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	table := stubProcessTable(t)
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer killProcess(cmd.Process.Pid)
	go func() { _ = cmd.Wait() }()
	table.add(cmd.Process.Pid, "lc-mihomo-meta", []string{core, core, "-d", workDir, "-f", config})
	table.executables[cmd.Process.Pid] = "/opt/emulation/qemu-x86_64"

	launcher, err := InspectManagedRuntimeProcess(cmd.Process.Pid, core, config, workDir)
	if err != nil {
		t.Fatal(err)
	}
	if launcher != "/opt/emulation/qemu-x86_64" {
		t.Fatalf("launcher = %q", launcher)
	}
	if err := ValidateManagedRuntimeProcess(cmd.Process.Pid, core, config, workDir, launcher); err != nil {
		t.Fatal(err)
	}
}

func TestStopTerminatesRunningRuntime(t *testing.T) {
	dir := t.TempDir()
	workDir := filepath.Join(dir, "runtime")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	config := writeStartConfig(t, dir)
	table := stubProcessTable(t)
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	table.add(cmd.Process.Pid, "lc-mihomo-meta", []string{"lc-mihomo-meta", "-d", ".runtime/mihomo", "-f", "generated/mihomo.yaml"})

	result, err := Stop(StopOptions{CorePath: "mihomo", ConfigPath: config, WorkDir: workDir, Timeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Stopped || !result.WasRunning || result.PID != cmd.Process.Pid || result.ProcessNames[0] != "lc-mihomo-meta" {
		t.Fatalf("stop = %+v, want stopped pid %d", result, cmd.Process.Pid)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("process pid %d was not reaped", cmd.Process.Pid)
	}
}

func TestStopTerminatesAllManagedCores(t *testing.T) {
	dir := t.TempDir()
	workDir := filepath.Join(dir, "runtime")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	config := writeStartConfig(t, dir)
	table := stubProcessTable(t)
	meta := exec.Command("sleep", "30")
	if err := meta.Start(); err != nil {
		t.Fatal(err)
	}
	smart := exec.Command("sleep", "30")
	if err := smart.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan int, 2)
	go func() {
		_ = meta.Wait()
		done <- meta.Process.Pid
	}()
	go func() {
		_ = smart.Wait()
		done <- smart.Process.Pid
	}()
	table.add(meta.Process.Pid, "lc-mihomo-meta", []string{"lc-mihomo-meta", "-d", workDir, "-f", config})
	table.add(smart.Process.Pid, "lc-mihomo-smart", []string{"lc-mihomo-smart", "-d", workDir, "-f", config})

	result, err := Stop(StopOptions{
		CorePath:   "mihomo",
		ConfigPath: config,
		WorkDir:    workDir,
		Timeout:    2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Stopped || !result.WasRunning || len(result.StoppedPIDs) != 2 {
		t.Fatalf("stop = %+v, want both managed core pids stopped", result)
	}
	waitForStoppedPIDs(t, done, meta.Process.Pid, smart.Process.Pid)
}

func TestStopRuntimePIDsTreatsAlreadyExitedProcessAsStopped(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "exit 0")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}

	result, err := stopRuntimePIDs([]int{pid}, StopResult{}, 100*time.Millisecond, true)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Stopped || !result.WasRunning || len(result.StoppedPIDs) != 1 || result.StoppedPIDs[0] != pid {
		t.Fatalf("stop = %+v, want already-exited pid %d treated as stopped", result, pid)
	}
}

func TestRestartValidatesConfigBeforeRestartingRuntime(t *testing.T) {
	dir := t.TempDir()
	workDir := filepath.Join(dir, "runtime")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	table := stubProcessTable(t)
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer killProcess(cmd.Process.Pid)
	go func() { _ = cmd.Wait() }()
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
sleep 30
`)
	config := writeStartConfig(t, dir)
	table.add(cmd.Process.Pid, "lc-mihomo-meta", []string{"lc-mihomo-meta", "-d", workDir, "-f", config})
	stubAfterProcessStart(t, func(started *exec.Cmd) {
		table.add(started.Process.Pid, "lc-mihomo-meta", []string{core, "-d", workDir, "-f", config})
	})

	result, err := Restart(context.Background(), RestartOptions{
		CorePath:   core,
		ConfigPath: config,
		WorkDir:    workDir,
	})
	if err != nil {
		t.Fatalf("restart error = %v", err)
	}
	defer killProcess(result.Start.PID)
	if !result.ConfigValidation.Passed || result.ConfigValidation.Cached {
		t.Fatalf("validation = %+v, want uncached pass before restart", result.ConfigValidation)
	}
	status := Status(StatusOptions{ConfigPath: config, WorkDir: workDir})
	if !status.Running || status.PID == cmd.Process.Pid {
		t.Fatalf("status after restart = %+v, want new runtime", status)
	}
}

func TestRestartReusesCachedValidationForUnchangedConfigAndCore(t *testing.T) {
	dir := t.TempDir()
	core := filepath.Join(dir, "lc-mihomo-meta")
	counter := filepath.Join(dir, "count")
	writeStartExecutable(t, core, `#!/bin/sh
if [ "$1" = "-v" ]; then
  echo Mihomo Meta test
  exit 0
fi
for arg in "$@"; do
  if [ "$arg" = "-t" ]; then
    count=0
    [ -f "`+counter+`" ] && count=$(cat "`+counter+`")
    count=$((count + 1))
    echo "$count" > "`+counter+`"
    echo configuration test is successful
    exit 0
  fi
done
sleep 30
`)
	config := writeStartConfig(t, dir)
	workDir := filepath.Join(dir, "runtime")
	cache := filepath.Join(dir, "validation-cache.json")
	table := stubProcessTable(t)
	stubAfterProcessStart(t, func(started *exec.Cmd) {
		table.add(started.Process.Pid, "lc-mihomo-meta", []string{core, "-d", workDir, "-f", config})
	})

	first, err := Restart(context.Background(), RestartOptions{
		CorePath:            core,
		ConfigPath:          config,
		WorkDir:             workDir,
		ValidationCachePath: cache,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer killProcess(first.Start.PID)
	if !first.ConfigValidation.Passed || first.ConfigValidation.Cached {
		t.Fatalf("first validation = %+v, want uncached pass", first.ConfigValidation)
	}

	second, err := Restart(context.Background(), RestartOptions{
		CorePath:            core,
		ConfigPath:          config,
		WorkDir:             workDir,
		ValidationCachePath: cache,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer killProcess(second.Start.PID)
	if !second.ConfigValidation.Passed || !second.ConfigValidation.Cached {
		t.Fatalf("second validation = %+v, want cached pass", second.ConfigValidation)
	}
	if got := strings.TrimSpace(readControlTestFile(t, counter)); got != "1" {
		t.Fatalf("validation count = %q, want one actual mihomo -t run", got)
	}
}

func TestParseProcStatState(t *testing.T) {
	tests := []struct {
		name string
		stat string
		want byte
	}{
		{name: "simple", stat: "3866 (mihomo-meta) Z 1 2 3", want: 'Z'},
		{name: "space in command", stat: "3866 (mihomo meta) S 1 2 3", want: 'S'},
		{name: "paren in command", stat: "3866 (name with ) paren) R 1 2 3", want: 'R'},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseProcStatState(tt.stat)
			if !ok || got != tt.want {
				t.Fatalf("parseProcStatState(%q) = %q, %v; want %q, true", tt.stat, got, ok, tt.want)
			}
		})
	}
}

func stubProcessZombie(t *testing.T, pid int, zombie bool) {
	t.Helper()
	original := processZombie
	processZombie = func(candidate int) bool {
		if candidate == pid {
			return zombie
		}
		return original(candidate)
	}
	t.Cleanup(func() {
		processZombie = original
	})
}

type processTable struct {
	names       map[int]string
	args        map[int][]string
	executables map[int]string
	cwds        map[int]string
}

func stubProcessTable(t *testing.T) *processTable {
	t.Helper()
	stubSupervisionRuntime(t)
	table := &processTable{
		names:       map[int]string{},
		args:        map[int][]string{},
		executables: map[int]string{},
		cwds:        map[int]string{},
	}
	originalList := listProcessIDs
	originalComm := readProcessComm
	originalCommandLine := readProcessCommandLine
	originalExecutable := readProcessExecutable
	originalCWD := readProcessCWD
	listProcessIDs = func() []int {
		pids := make([]int, 0, len(table.names))
		for pid := range table.names {
			pids = append(pids, pid)
		}
		return pids
	}
	readProcessComm = func(candidate int) (string, bool, error) {
		if name, ok := table.names[candidate]; ok {
			return name, true, nil
		}
		return originalComm(candidate)
	}
	readProcessCommandLine = func(candidate int) ([]string, bool, error) {
		if args, ok := table.args[candidate]; ok {
			return append([]string(nil), args...), true, nil
		}
		return originalCommandLine(candidate)
	}
	readProcessExecutable = func(candidate int) (string, bool, error) {
		if executable, ok := table.executables[candidate]; ok {
			return executable, true, nil
		}
		return originalExecutable(candidate)
	}
	readProcessCWD = func(candidate int) (string, bool, error) {
		if cwd, ok := table.cwds[candidate]; ok {
			return cwd, true, nil
		}
		return originalCWD(candidate)
	}
	t.Cleanup(func() {
		listProcessIDs = originalList
		readProcessComm = originalComm
		readProcessCommandLine = originalCommandLine
		readProcessExecutable = originalExecutable
		readProcessCWD = originalCWD
	})
	return table
}

func stubSupervisionRuntime(t *testing.T) {
	t.Helper()
	originalBootID := currentBootID
	originalHealth := probeRuntimeHealth
	currentBootID = func() (string, error) { return "test-boot-id", nil }
	probeRuntimeHealth = func(context.Context, string, int, time.Duration) error { return nil }
	t.Cleanup(func() {
		currentBootID = originalBootID
		probeRuntimeHealth = originalHealth
	})
}

func (table *processTable) add(pid int, name string, args []string) {
	table.names[pid] = name
	table.args[pid] = append([]string(nil), args...)
	if len(args) > 0 {
		table.executables[pid] = args[0]
	}
	if cwd, err := os.Getwd(); err == nil {
		table.cwds[pid] = cwd
	}
}

func (table *processTable) remove(pid int) {
	delete(table.names, pid)
	delete(table.args, pid)
	delete(table.executables, pid)
	delete(table.cwds, pid)
}

func stubAfterProcessStart(t *testing.T, hook func(*exec.Cmd)) {
	t.Helper()
	original := afterProcessStart
	afterProcessStart = hook
	t.Cleanup(func() {
		afterProcessStart = original
	})
}

func waitForStoppedPIDs(t *testing.T, done <-chan int, pids ...int) {
	t.Helper()
	want := map[int]bool{}
	for _, pid := range pids {
		want[pid] = true
	}
	deadline := time.After(2 * time.Second)
	for len(want) > 0 {
		select {
		case pid := <-done:
			delete(want, pid)
		case <-deadline:
			t.Fatalf("processes were not reaped: %+v", want)
		}
	}
}

func readControlTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestRestartStopsExistingRuntimeAndStartsNewRuntime(t *testing.T) {
	dir := t.TempDir()
	workDir := filepath.Join(dir, "runtime")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	table := stubProcessTable(t)
	old := exec.Command("sleep", "30")
	if err := old.Start(); err != nil {
		t.Fatal(err)
	}
	oldDone := make(chan struct{})
	go func() {
		_ = old.Wait()
		close(oldDone)
	}()
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
sleep 30
`)
	config := writeStartConfig(t, dir)
	table.add(old.Process.Pid, "lc-mihomo-meta", []string{"lc-mihomo-meta", "-d", workDir, "-f", config})
	stubAfterProcessStart(t, func(started *exec.Cmd) {
		table.add(started.Process.Pid, "lc-mihomo-meta", []string{core, "-d", workDir, "-f", config})
	})
	var stages []RestartStageEvent

	result, err := Restart(context.Background(), RestartOptions{
		CorePath:    core,
		ConfigPath:  config,
		WorkDir:     workDir,
		StopTimeout: 2 * time.Second,
		OnStage: func(event RestartStageEvent) {
			stages = append(stages, event)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer killProcess(result.Start.PID)
	if !result.Restarted || !result.Stop.Stopped || !result.Start.Started || result.Start.PID == old.Process.Pid {
		t.Fatalf("restart = %+v, want stopped old pid %d and started new pid", result, old.Process.Pid)
	}
	if !result.Start.ConfigTestSkipped {
		t.Fatalf("restart start = %+v, want config test skipped", result.Start)
	}
	if !result.Status.Running || result.Status.PID != result.Start.PID {
		t.Fatalf("restart status = %+v, want new runtime pid %d running", result.Status, result.Start.PID)
	}
	if result.Timings.TotalMS == 0 {
		t.Fatalf("restart timings = %+v, want total duration", result.Timings)
	}
	for _, want := range []string{"config_test:done", "stop:done", "start:done", "status:done"} {
		if !restartStagesContain(stages, want) {
			t.Fatalf("restart stages = %+v, missing %s", stages, want)
		}
	}
	select {
	case <-oldDone:
	case <-time.After(2 * time.Second):
		t.Fatalf("old process pid %d was not reaped", old.Process.Pid)
	}
}

func restartStagesContain(stages []RestartStageEvent, want string) bool {
	for _, stage := range stages {
		if stage.Stage+":"+stage.Event == want {
			return true
		}
	}
	return false
}
