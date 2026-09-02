package corerun

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"localclash/internal/mihomotest"
	"localclash/internal/runtimeprofile"
	"localclash/internal/runtimesupervision"
)

type StartOptions struct {
	CorePath             string
	ConfigPath           string
	WorkDir              string
	LogPath              string
	Foreground           bool
	SkipConfigTest       bool
	ValidationCachePath  string
	ForceConfigTest      bool
	RuntimeHealthTimeout time.Duration
	OnStage              func(StartStageEvent) `json:"-"`
}

type StartStageEvent struct {
	Stage      string         `json:"stage"`
	Event      string         `json:"event"`
	DurationMS int64          `json:"duration_ms,omitempty"`
	PID        int            `json:"pid,omitempty"`
	Error      string         `json:"error,omitempty"`
	Fields     map[string]any `json:"fields,omitempty"`
}

type StartResult struct {
	Started            bool                        `json:"started"`
	AlreadyRunning     bool                        `json:"already_running"`
	PID                int                         `json:"pid,omitempty"`
	Config             string                      `json:"config"`
	RuntimeDir         string                      `json:"runtime_dir"`
	LogFile            string                      `json:"log_file"`
	ExternalController string                      `json:"external_controller,omitempty"`
	ExternalUIURL      string                      `json:"external_ui_url,omitempty"`
	ConfigTestSkipped  bool                        `json:"config_test_skipped,omitempty"`
	ConfigValidation   mihomotest.ValidationResult `json:"config_validation"`
	Warnings           []string                    `json:"warnings"`
	NextActions        []string                    `json:"next_actions,omitempty"`
}

var NetworkInterruptionWarnings = []string{
	"Starting or restarting the proxy runtime may temporarily interrupt network connectivity.",
	"The Agent itself may depend on the current network/proxy path and could be disconnected after this operation.",
}

var afterProcessStart = func(*exec.Cmd) {}

func Start(ctx context.Context, opts StartOptions) (StartResult, error) {
	opts = normalizeStartOptions(opts)
	stage := startStageEmitter(opts.OnStage)
	if opts.Foreground {
		return StartResult{}, fmt.Errorf("foreground=true is not supported by MCP run_runtime; use the CLI run command for foreground execution")
	}
	finish := stage("prepare", nil)
	runOpts := normalizeOptions(Options{
		CorePath:   opts.CorePath,
		ConfigPath: opts.ConfigPath,
		WorkDir:    opts.WorkDir,
		LogPath:    opts.LogPath,
	})
	if err := validateManagedCorePath(runOpts.CorePath); err != nil {
		finish(err, 0)
		return StartResult{}, err
	}
	if err := runOpts.validate(); err != nil {
		finish(err, 0)
		return StartResult{}, err
	}
	if err := os.MkdirAll(runOpts.WorkDir, 0o755); err != nil {
		finish(err, 0)
		return StartResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(runOpts.LogPath), 0o755); err != nil {
		finish(err, 0)
		return StartResult{}, err
	}
	finish(nil, 0)
	baseResult := newStartResult(runOpts)

	cachePath := validationCachePath(opts.ValidationCachePath, runOpts.WorkDir)
	var validation mihomotest.ValidationResult
	if opts.SkipConfigTest {
		baseResult.ConfigTestSkipped = true
		finish := stage("config_cache_check", map[string]any{"cache": cachePath})
		cache := mihomotest.CacheStatus(ctx, mihomotest.ValidationOptions{
			CorePath:   runOpts.CorePath,
			ConfigPath: runOpts.ConfigPath,
			WorkDir:    runOpts.WorkDir,
			CachePath:  cachePath,
		})
		if !cache.Present || !cache.Matched || !cache.Passed {
			err := fmt.Errorf("skip_config_test requires an existing matched passing validation cache: %s", cache.Status)
			finish(err, 0)
			return baseResult, err
		}
		validation = validationResultFromCacheStatus(cache)
		finish(nil, 0)
	} else {
		finish := stage("config_test", map[string]any{"cache": cachePath})
		var err error
		validation, err = validateConfig(ctx, runOpts, opts.ValidationCachePath, opts.ForceConfigTest)
		if err != nil {
			finish(err, 0)
			baseResult.ConfigValidation = validation
			return baseResult, err
		}
		finish(nil, 0)
	}
	baseResult.ConfigValidation = validation
	var result StartResult
	err := runtimesupervision.WithLock(runOpts.WorkDir, func() error {
		var startErr error
		result, startErr = startValidatedLocked(ctx, opts, runOpts, cachePath, baseResult, validation, stage)
		return startErr
	})
	if result.Config == "" {
		result = baseResult
	}
	return result, err
}

func newStartResult(runOpts Options) StartResult {
	result := StartResult{
		Config:     runOpts.ConfigPath,
		RuntimeDir: runOpts.WorkDir,
		LogFile:    runOpts.LogPath,
		Warnings:   append([]string(nil), NetworkInterruptionWarnings...),
	}
	endpoints := readRuntimeConfigEndpoints(runOpts.ConfigPath)
	result.ExternalController = endpoints.ExternalController
	result.ExternalUIURL = externalUIURL(result.ExternalController, endpoints.ExternalUI)

	return result
}

func startValidatedLocked(ctx context.Context, opts StartOptions, runOpts Options, cachePath string, result StartResult, validation mihomotest.ValidationResult, stage func(string, map[string]any) func(error, int)) (StartResult, error) {
	finish := stage("status_check", nil)
	processes := findManagedRuntimeProcesses()
	if len(processes) > 1 {
		err := fmt.Errorf("multiple managed Mihomo runtimes are already running: %d processes", len(processes))
		finish(err, 0)
		markRuntimeStartFailure(runOpts.WorkDir, "multiple_processes", err, time.Now())
		return result, err
	}
	if len(processes) == 1 {
		process := processes[0]
		if _, err := InspectManagedRuntimeProcess(process.PID, runOpts.CorePath, runOpts.ConfigPath, runOpts.WorkDir); err != nil {
			finish(err, process.PID)
			markRuntimeStartFailure(runOpts.WorkDir, "process_identity_mismatch", err, time.Now())
			return result, err
		}
		finish(nil, process.PID)
		finish = stage("controller_health", map[string]any{"pid": process.PID, "path": "/version"})
		if err := probeRuntimeHealth(ctx, runOpts.ConfigPath, process.PID, opts.RuntimeHealthTimeout); err != nil {
			finish(err, process.PID)
			markRuntimeStartFailure(runOpts.WorkDir, "controller_health_failed", err, time.Now())
			return result, err
		}
		finish(nil, process.PID)
		state, warning, err := prepareStartingSupervisionLocked(runOpts, cachePath, validation, time.Now())
		if err != nil {
			markRuntimeStartFailure(runOpts.WorkDir, "state_write_failed", err, time.Now())
			return result, err
		}
		if warning != "" {
			result.Warnings = append(result.Warnings, warning)
		}
		if err := completeRunningSupervisionLocked(runOpts.WorkDir, state, process.PID, time.Now()); err != nil {
			markRuntimeStartFailure(runOpts.WorkDir, "state_write_failed", err, time.Now())
			return result, err
		}
		result.AlreadyRunning = true
		result.PID = process.PID
		result.Warnings = append(result.Warnings, "Runtime is already running; run_runtime did not start a second process.")
		return result, nil
	}
	finish(nil, 0)

	state, warning, err := prepareStartingSupervisionLocked(runOpts, cachePath, validation, time.Now())
	if err != nil {
		markRuntimeStartFailure(runOpts.WorkDir, "state_write_failed", err, time.Now())
		return result, err
	}
	if warning != "" {
		result.Warnings = append(result.Warnings, warning)
	}

	finish = stage("open_log", map[string]any{"log_file": runOpts.LogPath})
	logFile, err := openBackgroundRuntimeLog(runOpts.LogPath)
	if err != nil {
		finish(err, 0)
		markRuntimeStartFailure(runOpts.WorkDir, "log_open_failed", err, time.Now())
		return result, err
	}
	finish(nil, 0)

	finish = stage("start_process", map[string]any{"core": runOpts.CorePath, "config": runOpts.ConfigPath, "work_dir": runOpts.WorkDir})
	cmd, err := startBackgroundRuntimeProcess(runOpts, logFile)
	closeErr := logFile.Close()
	if err != nil {
		finish(err, 0)
		markRuntimeStartFailure(runOpts.WorkDir, "process_start_failed", err, time.Now())
		return result, err
	}
	if closeErr != nil {
		result.Warnings = append(result.Warnings, "Runtime started, but the parent Mihomo log handle did not close cleanly: "+closeErr.Error())
		appendRuntimeSupervisionEvent(runOpts.WorkDir, time.Now(), map[string]any{"event": "runtime_log_close_error", "error": closeErr.Error()})
	}
	finish(nil, cmd.Process.Pid)
	result.Started = true
	result.PID = cmd.Process.Pid

	finish = stage("controller_health", map[string]any{"pid": cmd.Process.Pid, "path": "/version"})
	if healthErr := probeRuntimeHealth(ctx, runOpts.ConfigPath, cmd.Process.Pid, opts.RuntimeHealthTimeout); healthErr != nil {
		failureErr := healthErr
		if cleanupErr := cleanupFailedStartLocked(runOpts.WorkDir, cmd.Process.Pid, time.Now()); cleanupErr != nil {
			failureErr = fmt.Errorf("%w; cleanup newly started runtime: %v", healthErr, cleanupErr)
		}
		finish(failureErr, cmd.Process.Pid)
		markRuntimeStartFailure(runOpts.WorkDir, "controller_health_failed", failureErr, time.Now())
		return result, failureErr
	}
	finish(nil, cmd.Process.Pid)
	if err := completeRunningSupervisionLocked(runOpts.WorkDir, state, cmd.Process.Pid, time.Now()); err != nil {
		markRuntimeStartFailure(runOpts.WorkDir, "state_write_failed", err, time.Now())
		return result, err
	}
	return result, nil
}

func cleanupFailedStartLocked(workDir string, pid int, now time.Time) error {
	fields := map[string]any{
		"event": "runtime_start_cleanup",
		"pid":   pid,
	}
	if !processRunning(pid) || processZombie(pid) {
		fields["outcome"] = "already_exited"
		appendRuntimeSupervisionEvent(workDir, now, fields)
		return markFailedStartStoppedLocked(workDir, now)
	}
	result, err := stopRuntimePIDs([]int{pid}, StopResult{RuntimeDir: workDir, PID: pid}, 2*time.Second, true)
	if err != nil && (!processRunning(pid) || processZombie(pid)) {
		err = nil
	}
	if err == nil && result.Error != "" {
		err = errors.New(result.Error)
	}
	fields["forced"] = result.Forced
	fields["signal"] = result.Signal
	if err != nil {
		fields["outcome"] = "failed"
		fields["error"] = err.Error()
		appendRuntimeSupervisionEvent(workDir, now, fields)
		return err
	}
	fields["outcome"] = "stopped"
	appendRuntimeSupervisionEvent(workDir, now, fields)
	return markFailedStartStoppedLocked(workDir, now)
}

func markFailedStartStoppedLocked(workDir string, now time.Time) error {
	state, err := runtimesupervision.Read(workDir)
	if err != nil {
		return fmt.Errorf("read supervision after failed start: %w", err)
	}
	if state.State != runtimesupervision.StateStarting {
		return fmt.Errorf("supervision state after failed start is %q, want %q", state.State, runtimesupervision.StateStarting)
	}
	state.State = runtimesupervision.StateStopped
	state.PID = 0
	state.HealthySince = ""
	state.LastHealthyAt = ""
	state.NextAttemptAt = ""
	state.UpdatedAt = supervisionTimestamp(now)
	if err := runtimesupervision.Write(workDir, state); err != nil {
		return fmt.Errorf("write stopped supervision after failed start: %w", err)
	}
	return nil
}

func openBackgroundRuntimeLog(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
}

func startBackgroundRuntimeProcess(runOpts Options, logFile *os.File) (*exec.Cmd, error) {
	cmd := exec.Command(runOpts.CorePath, "-d", runOpts.WorkDir, "-f", runOpts.ConfigPath)
	cmd.Stdin = nil
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	afterProcessStart(cmd)
	go func() {
		_ = cmd.Wait()
	}()
	return cmd, nil
}

func spawnBackgroundRuntime(runOpts Options) (*exec.Cmd, error) {
	logFile, err := openBackgroundRuntimeLog(runOpts.LogPath)
	if err != nil {
		return nil, err
	}
	cmd, startErr := startBackgroundRuntimeProcess(runOpts, logFile)
	closeErr := logFile.Close()
	if startErr != nil {
		return nil, startErr
	}
	if closeErr != nil {
		appendRuntimeSupervisionEvent(runOpts.WorkDir, time.Now(), map[string]any{"event": "runtime_log_close_error", "error": closeErr.Error()})
	}
	return cmd, nil
}

func validationResultFromCacheStatus(status mihomotest.CacheStatusResult) mihomotest.ValidationResult {
	return mihomotest.ValidationResult{
		Enabled:       true,
		Passed:        status.Passed,
		Cached:        true,
		CachePath:     status.CachePath,
		CacheHitMode:  status.MatchMode,
		ValidatedAt:   status.ValidatedAt,
		ConfigPath:    status.ConfigPath,
		ConfigSHA256:  status.ConfigSHA256,
		ConfigSize:    status.ConfigSize,
		ConfigModTime: status.ConfigModTime,
		CorePath:      status.CorePath,
		CoreType:      status.CoreType,
		CoreVersion:   status.CoreVersion,
		CoreSHA256:    status.CoreSHA256,
		CoreSize:      status.CoreSize,
		CoreModTime:   status.CoreModTime,
		DurationMS:    status.DurationMS,
	}
}

func startStageEmitter(callback func(StartStageEvent)) func(string, map[string]any) func(error, int) {
	return func(stage string, fields map[string]any) func(error, int) {
		if callback == nil {
			return func(error, int) {}
		}
		started := time.Now()
		callback(StartStageEvent{Stage: stage, Event: "started", Fields: fields})
		return func(err error, pid int) {
			event := StartStageEvent{
				Stage:      stage,
				Event:      "done",
				DurationMS: time.Since(started).Milliseconds(),
				PID:        pid,
			}
			if err != nil {
				event.Event = "error"
				event.Error = err.Error()
			}
			callback(event)
		}
	}
}

func normalizeStartOptions(opts StartOptions) StartOptions {
	opts.CorePath = strings.TrimSpace(opts.CorePath)
	opts.ConfigPath = strings.TrimSpace(opts.ConfigPath)
	opts.WorkDir = strings.TrimSpace(opts.WorkDir)
	opts.LogPath = strings.TrimSpace(opts.LogPath)
	if opts.CorePath == "" {
		opts.CorePath = runtimeprofile.MetaCorePath
	}
	if opts.ConfigPath == "" {
		opts.ConfigPath = filepath.Join(".runtime", "mihomo", "config.yaml")
	}
	if opts.WorkDir == "" {
		opts.WorkDir = ".runtime/mihomo"
	}
	if opts.LogPath == "" {
		opts.LogPath = filepath.Join(opts.WorkDir, "mihomo.log")
	}
	if opts.RuntimeHealthTimeout <= 0 {
		opts.RuntimeHealthTimeout = defaultRuntimeHealthTimeout
	}
	return opts
}

func processRunning(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}

func validateConfig(ctx context.Context, opts Options, cachePath string, force bool) (mihomotest.ValidationResult, error) {
	result, err := mihomotest.ValidateCached(ctx, mihomotest.ValidationOptions{
		CorePath:   opts.CorePath,
		ConfigPath: opts.ConfigPath,
		WorkDir:    opts.WorkDir,
		CachePath:  validationCachePath(cachePath, opts.WorkDir),
		Force:      force,
	})
	if err != nil {
		if result.Output != "" {
			return result, fmt.Errorf("mihomo config test failed: %s", compactStartOutput([]byte(result.Output), err))
		}
		return result, fmt.Errorf("mihomo config test failed: %w", err)
	}
	if !result.Passed {
		return result, fmt.Errorf("mihomo config test failed: %s", result.Output)
	}
	return result, nil
}

func validationCachePath(path, workDir string) string {
	if strings.TrimSpace(path) != "" {
		return path
	}
	return mihomotest.DefaultCachePath(workDir)
}

func compactStartOutput(output []byte, err error) string {
	text := strings.TrimSpace(string(output))
	if text == "" {
		if err != nil {
			return err.Error()
		}
		return ""
	}
	lines := strings.Split(text, "\n")
	const maxLines = 8
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return strings.Join(lines, "\n")
}

func readExternalController(path string) string {
	return readRuntimeConfigEndpoints(path).ExternalController
}

func externalUIURL(controller, ui string) string {
	if controller == "" || strings.TrimSpace(ui) == "" {
		return ""
	}
	return "http://" + controller + "/ui"
}

type runtimeConfigEndpoints struct {
	ExternalController string
	ExternalUI         string
}

func readRuntimeConfigEndpoints(path string) runtimeConfigEndpoints {
	file, err := os.Open(path)
	if err != nil {
		return runtimeConfigEndpoints{}
	}
	defer file.Close()

	var endpoints runtimeConfigEndpoints
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || line[0] == ' ' || line[0] == '\t' {
			continue
		}
		key, value, ok := splitTopLevelYAMLScalar(line)
		if !ok {
			continue
		}
		switch key {
		case "external-controller":
			endpoints.ExternalController = value
		case "external-ui":
			endpoints.ExternalUI = value
		}
		if endpoints.ExternalController != "" && endpoints.ExternalUI != "" {
			break
		}
	}
	return endpoints
}

func splitTopLevelYAMLScalar(line string) (string, string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", "", false
	}
	idx := strings.Index(trimmed, ":")
	if idx <= 0 {
		return "", "", false
	}
	key := strings.TrimSpace(trimmed[:idx])
	value := strings.TrimSpace(stripInlineYAMLComment(trimmed[idx+1:]))
	value = strings.Trim(value, `"'`)
	return key, value, true
}

func stripInlineYAMLComment(value string) string {
	inSingle := false
	inDouble := false
	for i := 0; i < len(value); i++ {
		switch value[i] {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble && (i == 0 || value[i-1] == ' ' || value[i-1] == '\t') {
				return value[:i]
			}
		}
	}
	return value
}
