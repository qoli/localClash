package corerun

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"localclash/internal/mihomoapi"
	"localclash/internal/mihomotest"
	"localclash/internal/runtimeprofile"
	"localclash/internal/runtimesupervision"
)

const (
	RestartStrategyProcess   = "process_restart"
	RestartStrategyHotReload = "hot_reload"
)

type StatusOptions struct {
	CorePath   string
	ConfigPath string
	WorkDir    string
	LogPath    string
}

type StatusResult struct {
	Running            bool     `json:"running"`
	PID                int      `json:"pid,omitempty"`
	PIDs               []int    `json:"pids,omitempty"`
	ProcessNames       []string `json:"process_names,omitempty"`
	RuntimeDir         string   `json:"runtime_dir"`
	Config             string   `json:"config"`
	LogFile            string   `json:"log_file"`
	ExternalController string   `json:"external_controller,omitempty"`
	ExternalUIURL      string   `json:"external_ui_url,omitempty"`
}

type StopOptions struct {
	CorePath                 string
	ConfigPath               string
	WorkDir                  string
	Timeout                  time.Duration
	ForceKill                bool
	PreserveSupervisionState bool `json:"-"`
}

type RestartOptions struct {
	CorePath            string
	ConfigPath          string
	WorkDir             string
	LogPath             string
	Strategy            string
	ConfigSHA256        string
	AttestationPath     string
	ReloadTimeout       time.Duration
	ValidationCachePath string
	ForceConfigTest     bool
	StopTimeout         time.Duration
	ForceKill           bool
	OnStage             func(RestartStageEvent) `json:"-"`
}

type RestartStageEvent struct {
	Stage      string `json:"stage"`
	Event      string `json:"event"`
	DurationMS int64  `json:"duration_ms,omitempty"`
	PID        int    `json:"pid,omitempty"`
	Error      string `json:"error,omitempty"`
}

type RestartTimings struct {
	ValidateMS int64 `json:"validate_ms,omitempty"`
	StopMS     int64 `json:"stop_ms"`
	StartMS    int64 `json:"start_ms"`
	StatusMS   int64 `json:"status_ms"`
	TotalMS    int64 `json:"total_ms"`
}

type StopResult struct {
	Stopped      bool     `json:"stopped"`
	WasRunning   bool     `json:"was_running"`
	Refused      bool     `json:"refused,omitempty"`
	PID          int      `json:"pid,omitempty"`
	PIDs         []int    `json:"pids,omitempty"`
	ProcessNames []string `json:"process_names,omitempty"`
	Signal       string   `json:"signal,omitempty"`
	Forced       bool     `json:"forced,omitempty"`
	StoppedPIDs  []int    `json:"stopped_pids,omitempty"`
	RuntimeDir   string   `json:"runtime_dir"`
	Error        string   `json:"error,omitempty"`
	Warnings     []string `json:"warnings,omitempty"`
	NextActions  []string `json:"next_actions,omitempty"`
}

type RestartResult struct {
	Restarted        bool                        `json:"restarted"`
	Reloaded         bool                        `json:"reloaded,omitempty"`
	AppliedStrategy  string                      `json:"applied_strategy"`
	ConfigSHA256     string                      `json:"config_sha256,omitempty"`
	ConfigValidation mihomotest.ValidationResult `json:"config_validation"`
	Stop             StopResult                  `json:"stop"`
	Start            StartResult                 `json:"start"`
	Status           StatusResult                `json:"status"`
	HotReload        *HotReloadResult            `json:"hot_reload,omitempty"`
	Timings          RestartTimings              `json:"timings"`
	Error            string                      `json:"error,omitempty"`
	Warnings         []string                    `json:"warnings,omitempty"`
	NextActions      []string                    `json:"next_actions,omitempty"`
}

type HotReloadResult struct {
	Config     string `json:"config"`
	StatusCode int    `json:"status_code"`
}

func Status(opts StatusOptions) StatusResult {
	identityScoped := strings.TrimSpace(opts.CorePath) != "" &&
		strings.TrimSpace(opts.ConfigPath) != "" &&
		strings.TrimSpace(opts.WorkDir) != ""
	normalized := normalizeStartOptions(StartOptions{
		CorePath:   opts.CorePath,
		ConfigPath: opts.ConfigPath,
		WorkDir:    opts.WorkDir,
		LogPath:    opts.LogPath,
	})
	result := StatusResult{
		RuntimeDir: normalized.WorkDir,
		Config:     normalized.ConfigPath,
		LogFile:    normalized.LogPath,
	}
	endpoints := readRuntimeConfigEndpoints(normalized.ConfigPath)
	result.ExternalController = endpoints.ExternalController
	result.ExternalUIURL = externalUIURL(result.ExternalController, endpoints.ExternalUI)

	processes := findManagedRuntimeProcesses()
	if identityScoped {
		matched := make([]runtimeProcess, 0, len(processes))
		for _, process := range processes {
			if _, err := InspectManagedRuntimeProcess(process.PID, normalized.CorePath, normalized.ConfigPath, normalized.WorkDir); err == nil {
				matched = append(matched, process)
			}
		}
		processes = matched
	}
	if len(processes) == 0 {
		return result
	}
	result.Running = true
	for _, process := range processes {
		result.PIDs = appendUniquePIDs(result.PIDs, process.PID)
		result.ProcessNames = appendUniqueStrings(result.ProcessNames, process.Name)
	}
	result.PID = processes[0].PID
	return result
}

func Restart(ctx context.Context, opts RestartOptions) (RestartResult, error) {
	totalStarted := time.Now()
	opts = normalizeRestartOptions(opts)
	stage := func(event RestartStageEvent) {
		if opts.OnStage != nil {
			opts.OnStage(event)
		}
	}
	startOpts := normalizeStartOptions(StartOptions{
		CorePath:       opts.CorePath,
		ConfigPath:     opts.ConfigPath,
		WorkDir:        opts.WorkDir,
		LogPath:        opts.LogPath,
		SkipConfigTest: true,
	})
	startOpts.ValidationCachePath = opts.ValidationCachePath
	startOpts.ForceConfigTest = opts.ForceConfigTest
	runOpts := normalizeOptions(Options{
		CorePath:   startOpts.CorePath,
		ConfigPath: startOpts.ConfigPath,
		WorkDir:    startOpts.WorkDir,
		LogPath:    startOpts.LogPath,
	})
	result := RestartResult{
		AppliedStrategy: opts.Strategy,
		Warnings:        append([]string(nil), NetworkInterruptionWarnings...),
		NextActions: []string{
			"call runtime_status to verify the restarted Mihomo process",
		},
	}
	if opts.Strategy == RestartStrategyHotReload {
		return hotReload(ctx, opts, runOpts, result, stage, totalStarted)
	}
	if err := validateManagedCorePath(runOpts.CorePath); err != nil {
		return result, err
	}
	if err := runOpts.validate(); err != nil {
		return result, err
	}
	if err := os.MkdirAll(runOpts.WorkDir, 0o755); err != nil {
		return result, err
	}
	if err := os.MkdirAll(filepath.Dir(runOpts.LogPath), 0o755); err != nil {
		return result, err
	}
	validateStarted := time.Now()
	stage(RestartStageEvent{Stage: "config_test", Event: "started"})
	validation, err := validateConfig(ctx, runOpts, opts.ValidationCachePath, opts.ForceConfigTest)
	result.ConfigValidation = validation
	result.Timings.ValidateMS = elapsedMS(validateStarted)
	if err != nil {
		result.Error = err.Error()
		result.Timings.TotalMS = elapsedMS(totalStarted)
		result.NextActions = []string{
			"inspect config_validation and fix generated config before restarting runtime",
			"call config_render after durable localClash intent changes",
			"call doctor --json for a full validation report",
		}
		stage(RestartStageEvent{Stage: "config_test", Event: "error", DurationMS: result.Timings.ValidateMS, Error: err.Error()})
		return result, nil
	}
	stage(RestartStageEvent{Stage: "config_test", Event: "done", DurationMS: result.Timings.ValidateMS})
	err = runtimesupervision.WithLock(runOpts.WorkDir, func() error {
		var restartErr error
		result, restartErr = restartValidatedLocked(ctx, opts, startOpts, runOpts, result, validation, stage, totalStarted)
		return restartErr
	})
	return result, err
}

func restartValidatedLocked(ctx context.Context, opts RestartOptions, startOpts StartOptions, runOpts Options, result RestartResult, validation mihomotest.ValidationResult, stage func(RestartStageEvent), totalStarted time.Time) (RestartResult, error) {
	if err := verifyRestartValidationProof(runOpts, validation); err != nil {
		result.Error = "cannot restart with the validated inputs: " + err.Error()
		result.Timings.TotalMS = elapsedMS(totalStarted)
		stage(RestartStageEvent{Stage: "config_test", Event: "error", Error: result.Error})
		return result, nil
	}
	if err := markSupervisionRestartingLocked(runOpts, time.Now()); err != nil {
		result.Error = "cannot mark runtime supervision as restarting: " + err.Error()
		result.Timings.TotalMS = elapsedMS(totalStarted)
		return result, nil
	}

	stopStarted := time.Now()
	stage(RestartStageEvent{Stage: "stop", Event: "started"})
	stop, err := stopLocked(runOpts, StopOptions{
		Timeout: opts.StopTimeout, ForceKill: opts.ForceKill, PreserveSupervisionState: true,
	})
	result.Stop = stop
	result.Timings.StopMS = elapsedMS(stopStarted)
	if err != nil {
		result.Timings.TotalMS = elapsedMS(totalStarted)
		stage(RestartStageEvent{Stage: "stop", Event: "error", DurationMS: result.Timings.StopMS, PID: stop.PID, Error: err.Error()})
		return result, err
	}
	if stop.Error != "" {
		result.Error = stop.Error
		result.Timings.TotalMS = elapsedMS(totalStarted)
		stage(RestartStageEvent{Stage: "stop", Event: "error", DurationMS: result.Timings.StopMS, PID: stop.PID, Error: stop.Error})
		return result, nil
	}
	stage(RestartStageEvent{Stage: "stop", Event: "done", DurationMS: result.Timings.StopMS, PID: stop.PID})

	startStarted := time.Now()
	stage(RestartStageEvent{Stage: "start", Event: "started"})
	if err := verifyRestartValidationProof(runOpts, validation); err != nil {
		result.Error = "validation proof no longer valid after stopping runtime: " + err.Error()
		if stateErr := disarmSupervisionLocked(runOpts, stop.WasRunning, time.Now()); stateErr != nil {
			result.Error += "; cannot record stopped supervision: " + stateErr.Error()
		}
		markRuntimeStartFailure(runOpts.WorkDir, "restart_validation_changed", err, time.Now())
		result.Timings.StartMS = elapsedMS(startStarted)
		result.Timings.TotalMS = elapsedMS(totalStarted)
		stage(RestartStageEvent{Stage: "start", Event: "error", DurationMS: result.Timings.StartMS, Error: result.Error})
		return result, nil
	}
	baseStart := newStartResult(runOpts)
	baseStart.ConfigTestSkipped = true
	baseStart.ConfigValidation = validation
	start, err := startValidatedLocked(ctx, startOpts, runOpts, validationCachePath(opts.ValidationCachePath, runOpts.WorkDir), baseStart, validation, startStageEmitter(startOpts.OnStage))
	result.Start = start
	result.Timings.StartMS = elapsedMS(startStarted)
	if err != nil {
		result.Error = err.Error()
		result.Timings.TotalMS = elapsedMS(totalStarted)
		stage(RestartStageEvent{Stage: "start", Event: "error", DurationMS: result.Timings.StartMS, PID: start.PID, Error: err.Error()})
		return result, nil
	}
	start.ConfigValidation = validation
	result.Start = start
	stage(RestartStageEvent{Stage: "start", Event: "done", DurationMS: result.Timings.StartMS, PID: start.PID})

	statusStarted := time.Now()
	stage(RestartStageEvent{Stage: "status", Event: "started"})
	status := Status(StatusOptions{
		CorePath:   runOpts.CorePath,
		ConfigPath: runOpts.ConfigPath,
		WorkDir:    runOpts.WorkDir,
		LogPath:    runOpts.LogPath,
	})
	result.Status = status
	result.Timings.StatusMS = elapsedMS(statusStarted)
	stage(RestartStageEvent{Stage: "status", Event: "done", DurationMS: result.Timings.StatusMS, PID: status.PID})
	result.Restarted = start.Started
	result.Timings.TotalMS = elapsedMS(totalStarted)
	return result, nil
}

// verifyRestartValidationProof binds the already completed validation to the
// bytes used by this restart without executing another core version probe.
func verifyRestartValidationProof(runOpts Options, validation mihomotest.ValidationResult) error {
	if err := mihomotest.VerifyCachedValidation(validation); err != nil {
		return fmt.Errorf("restart requires a durable validation cache: %w", err)
	}
	coreSHA, err := runtimesupervision.HashFile(runOpts.CorePath)
	if err != nil {
		return fmt.Errorf("hash validated restart core: %w", err)
	}
	if coreSHA != validation.CoreSHA256 {
		return errors.New("core SHA-256 no longer matches the restart validation proof")
	}
	configSHA, err := runtimesupervision.HashFile(runOpts.ConfigPath)
	if err != nil {
		return fmt.Errorf("hash validated restart config: %w", err)
	}
	if configSHA != validation.ConfigSHA256 {
		return errors.New("config SHA-256 no longer matches the restart validation proof")
	}
	return nil
}

func normalizeRestartOptions(opts RestartOptions) RestartOptions {
	opts.Strategy = strings.TrimSpace(opts.Strategy)
	if opts.Strategy == "" {
		opts.Strategy = RestartStrategyProcess
	}
	if opts.ReloadTimeout <= 0 {
		opts.ReloadTimeout = 10 * time.Second
	}
	return opts
}

func hotReload(ctx context.Context, opts RestartOptions, runOpts Options, result RestartResult, stage func(RestartStageEvent), totalStarted time.Time) (RestartResult, error) {
	err := runtimesupervision.WithLock(runOpts.WorkDir, func() error {
		var reloadErr error
		result, reloadErr = hotReloadLocked(ctx, opts, runOpts, result, stage, totalStarted)
		return reloadErr
	})
	return result, err
}

func hotReloadLocked(ctx context.Context, opts RestartOptions, runOpts Options, result RestartResult, stage func(RestartStageEvent), totalStarted time.Time) (RestartResult, error) {
	if opts.ForceConfigTest {
		result.Error = "force_config_test is not supported for hot_reload; call mihomo_config_test first"
		result.Timings.TotalMS = elapsedMS(totalStarted)
		return result, nil
	}
	if _, err := os.Stat(runOpts.ConfigPath); err != nil {
		result.Error = fmt.Sprintf("config %q is not available: %v", runOpts.ConfigPath, err)
		result.Timings.TotalMS = elapsedMS(totalStarted)
		return result, nil
	}
	hashStarted := time.Now()
	stage(RestartStageEvent{Stage: "hash_check", Event: "started"})
	expected := strings.TrimSpace(opts.ConfigSHA256)
	if expected == "" {
		attestationPath := opts.AttestationPath
		if strings.TrimSpace(attestationPath) == "" {
			attestationPath = mihomotest.DefaultAttestationPath(runOpts.WorkDir)
		}
		attestation, err := mihomotest.ReadAttestation(attestationPath)
		if err != nil {
			result.Error = "cannot read mihomo_config_test attestation: " + err.Error()
			result.Timings.TotalMS = elapsedMS(totalStarted)
			stage(RestartStageEvent{Stage: "hash_check", Event: "error", DurationMS: elapsedMS(hashStarted), Error: result.Error})
			return result, nil
		}
		expected = attestation.ConfigSHA256
	}
	actual, err := mihomotest.VerifyConfigHash(runOpts.ConfigPath, expected)
	if err != nil {
		result.ConfigSHA256 = actual
		result.Error = err.Error()
		result.Timings.TotalMS = elapsedMS(totalStarted)
		stage(RestartStageEvent{Stage: "hash_check", Event: "error", DurationMS: elapsedMS(hashStarted), Error: err.Error()})
		return result, nil
	}
	result.ConfigSHA256 = actual
	stage(RestartStageEvent{Stage: "hash_check", Event: "done", DurationMS: elapsedMS(hashStarted)})

	statusStarted := time.Now()
	stage(RestartStageEvent{Stage: "status", Event: "started"})
	status := Status(StatusOptions{
		CorePath:   runOpts.CorePath,
		ConfigPath: runOpts.ConfigPath,
		WorkDir:    runOpts.WorkDir,
		LogPath:    runOpts.LogPath,
	})
	result.Status = status
	result.Timings.StatusMS = elapsedMS(statusStarted)
	if !status.Running {
		result.Error = "runtime is not running; hot_reload requires an active Mihomo controller"
		result.Timings.TotalMS = elapsedMS(totalStarted)
		stage(RestartStageEvent{Stage: "status", Event: "error", DurationMS: result.Timings.StatusMS, Error: result.Error})
		return result, nil
	}
	stage(RestartStageEvent{Stage: "status", Event: "done", DurationMS: result.Timings.StatusMS, PID: status.PID})

	reloadStarted := time.Now()
	stage(RestartStageEvent{Stage: "hot_reload", Event: "started"})
	client, err := mihomoapi.NewFromConfig(runOpts.ConfigPath)
	if err != nil {
		result.Error = err.Error()
		result.Timings.TotalMS = elapsedMS(totalStarted)
		stage(RestartStageEvent{Stage: "hot_reload", Event: "error", DurationMS: elapsedMS(reloadStarted), Error: err.Error()})
		return result, nil
	}
	configPath, err := filepath.Abs(runOpts.ConfigPath)
	if err != nil {
		result.Error = err.Error()
		result.Timings.TotalMS = elapsedMS(totalStarted)
		stage(RestartStageEvent{Stage: "hot_reload", Event: "error", DurationMS: elapsedMS(reloadStarted), Error: err.Error()})
		return result, nil
	}
	supervision, err := prepareHotReloadSupervisionLocked(ctx, runOpts, validationCachePath(opts.ValidationCachePath, runOpts.WorkDir), status.PID, actual)
	if err != nil {
		result.Error = "prepare runtime supervision for hot reload: " + err.Error()
		result.Timings.TotalMS = elapsedMS(totalStarted)
		stage(RestartStageEvent{Stage: "hot_reload", Event: "error", DurationMS: elapsedMS(reloadStarted), Error: result.Error})
		return result, nil
	}
	if supervision == nil {
		result.Warnings = append(result.Warnings, "Runtime supervision is unavailable for this running process.")
	}
	response, err := client.Request(ctx, mihomoapi.RequestOptions{
		Method:  "PUT",
		Path:    "/configs",
		Query:   map[string]any{"force": true},
		Body:    map[string]any{"path": configPath},
		Timeout: opts.ReloadTimeout,
	})
	result.HotReload = &HotReloadResult{Config: configPath, StatusCode: response.StatusCode}
	if err != nil {
		result.Error = err.Error()
		result.Timings.TotalMS = elapsedMS(totalStarted)
		stage(RestartStageEvent{Stage: "hot_reload", Event: "error", DurationMS: elapsedMS(reloadStarted), Error: err.Error()})
		return result, nil
	}
	result.Reloaded = true
	if err := commitHotReloadSupervisionLocked(runOpts.WorkDir, supervision, time.Now()); err != nil {
		result.Error = "Hot reload succeeded, but runtime supervision identity was not updated: " + err.Error()
		markRuntimeStartFailure(runOpts.WorkDir, "hot_reload_state_update_failed", err, time.Now())
		result.Timings.TotalMS = elapsedMS(totalStarted)
		stage(RestartStageEvent{Stage: "hot_reload", Event: "error", DurationMS: elapsedMS(reloadStarted), PID: status.PID, Error: result.Error})
		return result, nil
	}
	result.Timings.TotalMS = elapsedMS(totalStarted)
	stage(RestartStageEvent{Stage: "hot_reload", Event: "done", DurationMS: elapsedMS(reloadStarted), PID: status.PID})
	return result, nil
}

func elapsedMS(started time.Time) int64 {
	return time.Since(started).Milliseconds()
}

func Stop(opts StopOptions) (StopResult, error) {
	normalized := normalizeStartOptions(StartOptions{
		CorePath:   opts.CorePath,
		ConfigPath: opts.ConfigPath,
		WorkDir:    opts.WorkDir,
	})
	result := StopResult{
		RuntimeDir: normalized.WorkDir,
	}
	if len(findManagedRuntimeProcesses()) == 0 {
		if _, err := os.Stat(runtimesupervision.Path(normalized.WorkDir)); errors.Is(err, os.ErrNotExist) {
			return result, nil
		}
	}
	runOpts := normalizeOptions(Options{CorePath: normalized.CorePath, ConfigPath: normalized.ConfigPath, WorkDir: normalized.WorkDir, LogPath: normalized.LogPath})
	var stopResult StopResult
	err := runtimesupervision.WithLock(runOpts.WorkDir, func() error {
		var stopErr error
		stopResult, stopErr = stopLocked(runOpts, opts)
		return stopErr
	})
	if stopResult.RuntimeDir == "" {
		stopResult = result
	}
	return stopResult, err
}

// stopLocked is shared by standalone Stop and the complete restart transaction.
func stopLocked(runOpts Options, opts StopOptions) (StopResult, error) {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	result := StopResult{RuntimeDir: runOpts.WorkDir}
	processes := findManagedRuntimeProcesses()
	if !opts.PreserveSupervisionState {
		if err := disarmSupervisionLocked(runOpts, len(processes) > 0, time.Now()); err != nil {
			return result, err
		}
	}
	if len(processes) == 0 {
		return result, nil
	}
	result.PID = processes[0].PID
	for _, process := range processes {
		result.PIDs = appendUniquePIDs(result.PIDs, process.PID)
		result.ProcessNames = appendUniqueStrings(result.ProcessNames, process.Name)
	}
	return stopRuntimePIDs(result.PIDs, result, timeout, opts.ForceKill)
}

func stopRuntimePIDs(pids []int, result StopResult, timeout time.Duration, forceKill bool) (StopResult, error) {
	result.PIDs = appendUniquePIDs(result.PIDs, pids...)
	if len(pids) == 0 {
		return result, nil
	}
	result.WasRunning = true
	result.Signal = "SIGTERM"
	for _, pid := range pids {
		process, err := os.FindProcess(pid)
		if err != nil {
			return result, err
		}
		if err := process.Signal(syscall.SIGTERM); err != nil {
			if errors.Is(err, os.ErrProcessDone) || !processRunning(pid) || processZombie(pid) {
				continue
			}
			return result, fmt.Errorf("send SIGTERM to pid %d: %w", pid, err)
		}
	}
	if waitForAllExit(pids, timeout) {
		result.Stopped = true
		result.StoppedPIDs = appendUniquePIDs(result.StoppedPIDs, pids...)
		return result, nil
	}
	if !forceKill {
		result.Error = "runtime did not stop before timeout"
		return result, nil
	}
	result.Forced = true
	result.Signal = "SIGKILL"
	for _, pid := range pids {
		if !processRunning(pid) || processZombie(pid) {
			continue
		}
		process, err := os.FindProcess(pid)
		if err != nil {
			return result, err
		}
		if err := process.Kill(); err != nil {
			if errors.Is(err, os.ErrProcessDone) || !processRunning(pid) || processZombie(pid) {
				continue
			}
			return result, fmt.Errorf("send SIGKILL to pid %d: %w", pid, err)
		}
	}
	if waitForAllExit(pids, timeout) {
		result.Stopped = true
		result.StoppedPIDs = appendUniquePIDs(result.StoppedPIDs, pids...)
		return result, nil
	}
	result.Error = "runtime did not stop after SIGKILL"
	return result, nil
}

func validateManagedCorePath(corePath string) error {
	if managedProcessNames[filepath.Base(corePath)] {
		return nil
	}
	return fmt.Errorf("background runtime core %q is not a localClash managed core name; use %s or %s", corePath, runtimeprofile.ManagedMetaCoreName, runtimeprofile.ManagedSmartCoreName)
}

func waitForExit(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if !processRunning(pid) || processZombie(pid) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func waitForAllExit(pids []int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		allExited := true
		for _, pid := range pids {
			if processRunning(pid) && !processZombie(pid) {
				allExited = false
				break
			}
		}
		if allExited {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}

type runtimeProcess struct {
	PID  int
	Name string
}

type ManagedRuntimeProcess struct {
	PID  int
	Name string
}

var managedProcessNames = map[string]bool{
	runtimeprofile.ManagedMetaCoreName:  true,
	runtimeprofile.ManagedSmartCoreName: true,
}

func findManagedRuntimeProcesses() []runtimeProcess {
	return findRuntimeProcessesByName(managedProcessNames)
}

func ManagedRuntimeProcesses() []ManagedRuntimeProcess {
	processes := findManagedRuntimeProcesses()
	result := make([]ManagedRuntimeProcess, 0, len(processes))
	for _, process := range processes {
		result = append(result, ManagedRuntimeProcess{PID: process.PID, Name: process.Name})
	}
	return result
}

func ValidateManagedRuntimeProcess(pid int, corePath, configPath, workDir, launcherPath string) error {
	observedLauncher, err := InspectManagedRuntimeProcess(pid, corePath, configPath, workDir)
	if err != nil {
		return err
	}
	launcherPath = filepath.Clean(strings.TrimSpace(launcherPath))
	if launcherPath == "." || !filepath.IsAbs(launcherPath) {
		return fmt.Errorf("managed runtime launcher identity must be an absolute path")
	}
	if observedLauncher != launcherPath {
		return fmt.Errorf("managed runtime pid %d launcher mismatch: got %q, want %q", pid, observedLauncher, launcherPath)
	}
	return nil
}

func InspectManagedRuntimeProcess(pid int, corePath, configPath, workDir string) (string, error) {
	if pid <= 0 {
		return "", fmt.Errorf("managed runtime pid must be positive, got %d", pid)
	}
	expectedName := filepath.Base(strings.TrimSpace(corePath))
	if !managedProcessNames[expectedName] {
		return "", fmt.Errorf("core %q is not a localClash managed core", corePath)
	}
	name, ok, err := readProcessComm(pid)
	if err != nil {
		return "", fmt.Errorf("read managed runtime pid %d name: %w", pid, err)
	}
	if !ok {
		return "", fmt.Errorf("managed runtime pid %d no longer exists", pid)
	}
	if name != expectedName {
		return "", fmt.Errorf("managed runtime pid %d name mismatch: got %q, want %q", pid, name, expectedName)
	}
	executable, ok, err := readProcessExecutable(pid)
	if err != nil {
		return "", fmt.Errorf("read managed runtime pid %d executable: %w", pid, err)
	}
	if !ok {
		return "", fmt.Errorf("managed runtime pid %d executable is unavailable", pid)
	}
	launcherPath := filepath.Clean(strings.TrimSpace(executable))
	if launcherPath == "." || !filepath.IsAbs(launcherPath) {
		return "", fmt.Errorf("managed runtime pid %d launcher is not an absolute path: %q", pid, executable)
	}
	args, ok, err := readProcessCommandLine(pid)
	if err != nil {
		return "", fmt.Errorf("read managed runtime pid %d command line: %w", pid, err)
	}
	if !ok {
		return "", fmt.Errorf("managed runtime pid %d command line is unavailable", pid)
	}
	cwd, ok, err := readProcessCWD(pid)
	if err != nil {
		return "", fmt.Errorf("read managed runtime pid %d cwd: %w", pid, err)
	}
	if !ok {
		return "", fmt.Errorf("managed runtime pid %d cwd is unavailable", pid)
	}
	containsCore, err := commandContainsExactPath(args, corePath, cwd)
	if err != nil {
		return "", fmt.Errorf("managed runtime pid %d core command identity: %w", pid, err)
	}
	if !containsCore {
		return "", fmt.Errorf("managed runtime pid %d command line does not contain exact core path %q", pid, corePath)
	}
	actualWorkDir, err := exactFlagPath(args, "-d", cwd)
	if err != nil {
		return "", fmt.Errorf("managed runtime pid %d workdir identity: %w", pid, err)
	}
	expectedWorkDir, err := canonicalProcessPath(workDir, "")
	if err != nil {
		return "", fmt.Errorf("resolve expected managed runtime workdir: %w", err)
	}
	if actualWorkDir != expectedWorkDir {
		return "", fmt.Errorf("managed runtime pid %d workdir mismatch: got %q, want %q", pid, actualWorkDir, expectedWorkDir)
	}
	actualConfig, err := exactFlagPath(args, "-f", cwd)
	if err != nil {
		return "", fmt.Errorf("managed runtime pid %d config identity: %w", pid, err)
	}
	expectedConfig, err := canonicalProcessPath(configPath, "")
	if err != nil {
		return "", fmt.Errorf("resolve expected managed runtime config: %w", err)
	}
	if actualConfig != expectedConfig {
		return "", fmt.Errorf("managed runtime pid %d config mismatch: got %q, want %q", pid, actualConfig, expectedConfig)
	}
	return launcherPath, nil
}

func commandContainsExactPath(args []string, expected, cwd string) (bool, error) {
	expectedPath, err := canonicalProcessPath(expected, "")
	if err != nil {
		return false, err
	}
	for _, arg := range args {
		if strings.TrimSpace(arg) == "" {
			continue
		}
		candidate, err := canonicalProcessPath(arg, cwd)
		if err != nil {
			continue
		}
		if candidate == expectedPath {
			return true, nil
		}
	}
	return false, nil
}

func exactFlagPath(args []string, flag, cwd string) (string, error) {
	var value string
	for index := 0; index < len(args); index++ {
		if args[index] != flag {
			continue
		}
		if value != "" {
			return "", fmt.Errorf("command line contains duplicate %s flags", flag)
		}
		if index+1 >= len(args) || strings.TrimSpace(args[index+1]) == "" {
			return "", fmt.Errorf("command line %s value is missing", flag)
		}
		value = args[index+1]
	}
	if value == "" {
		return "", fmt.Errorf("command line is missing %s", flag)
	}
	return canonicalProcessPath(value, cwd)
}

func canonicalProcessPath(path, cwd string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("path is empty")
	}
	if !filepath.IsAbs(path) {
		if strings.TrimSpace(cwd) != "" {
			path = filepath.Join(cwd, path)
		} else {
			absolute, err := filepath.Abs(path)
			if err != nil {
				return "", err
			}
			path = absolute
		}
	}
	resolved, err := filepath.EvalSymlinks(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	return resolved, nil
}

func findRuntimeProcessesByName(names map[string]bool) []runtimeProcess {
	var processes []runtimeProcess
	for _, pid := range listProcessIDs() {
		if pid <= 0 || pid == os.Getpid() {
			continue
		}
		if !processRunning(pid) || processZombie(pid) {
			continue
		}
		name, ok, err := readProcessComm(pid)
		if err != nil || !ok || !names[name] {
			continue
		}
		if processCommandHasExactArg(pid, "-t") || processCommandHasExactArg(pid, "-v") {
			continue
		}
		processes = append(processes, runtimeProcess{PID: pid, Name: name})
	}
	sort.Slice(processes, func(i, j int) bool {
		if processes[i].PID == processes[j].PID {
			return processes[i].Name < processes[j].Name
		}
		return processes[i].PID < processes[j].PID
	})
	return processes
}

func processCommandHasExactArg(pid int, arg string) bool {
	args, ok, err := readProcessCommandLine(pid)
	if err != nil || !ok {
		return false
	}
	for _, value := range args {
		if value == arg {
			return true
		}
	}
	return false
}

var readProcessComm = func(pid int) (string, bool, error) {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "comm"))
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	name := strings.TrimSpace(string(data))
	if name == "" {
		return "", true, nil
	}
	return name, true, nil
}

var processZombie = defaultProcessZombie

func defaultProcessZombie(pid int) bool {
	state, ok := readProcStatState(pid)
	return ok && state == 'Z'
}

func readProcStatState(pid int) (byte, bool) {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0, false
	}
	return parseProcStatState(string(data))
}

func parseProcStatState(stat string) (byte, bool) {
	closeParen := strings.LastIndex(stat, ")")
	if closeParen < 0 || closeParen+1 >= len(stat) {
		return 0, false
	}
	fields := strings.Fields(stat[closeParen+1:])
	if len(fields) == 0 || len(fields[0]) != 1 {
		return 0, false
	}
	return fields[0][0], true
}

var listProcessIDs = defaultListProcessIDs

func defaultListProcessIDs() []int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var pids []int
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err == nil && pid > 0 {
			pids = append(pids, pid)
		}
	}
	return pids
}

func appendUniquePIDs(existing []int, pids ...int) []int {
	seen := map[int]bool{}
	for _, pid := range existing {
		if pid > 0 {
			seen[pid] = true
		}
	}
	for _, pid := range pids {
		if pid <= 0 || seen[pid] {
			continue
		}
		existing = append(existing, pid)
		seen[pid] = true
	}
	return existing
}

func appendUniqueStrings(existing []string, values ...string) []string {
	seen := map[string]bool{}
	for _, value := range existing {
		if value != "" {
			seen[value] = true
		}
	}
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		existing = append(existing, value)
		seen[value] = true
	}
	return existing
}

var readProcessCommandLine = func(pid int) ([]string, bool, error) {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	fields := strings.Split(strings.TrimRight(string(data), "\x00"), "\x00")
	if len(fields) == 1 && fields[0] == "" {
		return nil, true, nil
	}
	return fields, true, nil
}

var readProcessExecutable = func(pid int) (string, bool, error) {
	path, err := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "exe"))
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	return path, true, nil
}

var readProcessCWD = func(pid int) (string, bool, error) {
	path, err := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "cwd"))
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	return path, true, nil
}
