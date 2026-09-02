package corerun

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"localclash/internal/mihomoapi"
	"localclash/internal/mihomotest"
	"localclash/internal/runtimesupervision"
)

const (
	defaultRuntimeHealthTimeout = 2 * time.Minute
	defaultRuntimeProbeTimeout  = time.Second
	defaultStableHealthWindow   = 10 * time.Minute
	maxSupervisionAttempts      = 3
	supervisionNoticeInterval   = time.Minute
)

var (
	currentBootID      = runtimesupervision.CurrentBootID
	probeRuntimeHealth = defaultProbeRuntimeHealth
)

type SupervisionCheckOptions struct {
	WorkDir       string
	ProbeTimeout  time.Duration
	HealthTimeout time.Duration
	StableWindow  time.Duration
	Now           func() time.Time
}

type SupervisionCheckResult struct {
	Checked bool   `json:"checked"`
	Action  string `json:"action,omitempty"`
	Reason  string `json:"reason,omitempty"`
	PID     int    `json:"pid,omitempty"`
	Attempt int    `json:"attempt,omitempty"`
}

func prepareStartingSupervisionLocked(runOpts Options, cachePath string, validation mihomotest.ValidationResult, now time.Time) (*runtimesupervision.State, string, error) {
	bootID, err := currentBootID()
	if err != nil {
		appendRuntimeSupervisionEvent(runOpts.WorkDir, now, map[string]any{
			"event":  "runtime_supervision_blocked",
			"reason": "boot_id_unavailable",
			"error":  err.Error(),
		})
		return nil, "Runtime supervision is unavailable because the current boot identity cannot be verified.", nil
	}
	if !validation.Passed {
		return nil, "", errors.New("runtime supervision requires a passing Mihomo config validation")
	}
	if strings.TrimSpace(validation.CacheWriteError) != "" {
		return nil, "", fmt.Errorf("runtime supervision requires a durable validation cache: %s", validation.CacheWriteError)
	}
	if strings.TrimSpace(validation.CoreSHA256) == "" || strings.TrimSpace(validation.ConfigSHA256) == "" {
		return nil, "", errors.New("runtime supervision requires verified core and config SHA-256 values")
	}
	identity, err := canonicalSupervisionIdentity(runOpts, cachePath)
	if err != nil {
		return nil, "", err
	}
	state := runtimesupervision.State{
		Version:             runtimesupervision.Version,
		State:               runtimesupervision.StateStarting,
		BootID:              bootID,
		CorePath:            identity.CorePath,
		CoreSHA256:          validation.CoreSHA256,
		ConfigPath:          identity.ConfigPath,
		ConfigSHA256:        validation.ConfigSHA256,
		WorkDir:             identity.WorkDir,
		LogPath:             identity.LogPath,
		ValidationCachePath: identity.ValidationCachePath,
		UpdatedAt:           supervisionTimestamp(now),
	}
	if err := runtimesupervision.Write(runOpts.WorkDir, state); err != nil {
		return nil, "", fmt.Errorf("write starting runtime supervision state: %w", err)
	}
	return &state, "", nil
}

func completeRunningSupervisionLocked(workDir string, state *runtimesupervision.State, pid int, now time.Time) error {
	if state == nil {
		return nil
	}
	launcherPath, err := InspectManagedRuntimeProcess(pid, state.CorePath, state.ConfigPath, state.WorkDir)
	if err != nil {
		return fmt.Errorf("inspect runtime process before arming supervision: %w", err)
	}
	state.State = runtimesupervision.StateRunning
	state.PID = pid
	state.LauncherPath = launcherPath
	state.Attempts = 0
	state.HealthySince = supervisionTimestamp(now)
	state.LastHealthyAt = supervisionTimestamp(now)
	state.NextAttemptAt = ""
	state.LastNotice = ""
	state.LastNoticeAt = ""
	state.UpdatedAt = supervisionTimestamp(now)
	if err := runtimesupervision.Write(workDir, *state); err != nil {
		return fmt.Errorf("arm runtime supervision: %w", err)
	}
	appendRuntimeSupervisionEvent(workDir, now, map[string]any{
		"event":         "runtime_supervision_armed",
		"pid":           pid,
		"boot_id":       state.BootID,
		"core_sha256":   state.CoreSHA256,
		"config_sha256": state.ConfigSHA256,
	})
	return nil
}

func markRuntimeStartFailure(workDir, reason string, err error, now time.Time) {
	fields := map[string]any{
		"event":  "runtime_supervision_blocked",
		"reason": reason,
	}
	if err != nil {
		fields["error"] = err.Error()
	}
	appendRuntimeSupervisionEvent(workDir, now, fields)
}

func disarmSupervisionLocked(runOpts Options, hadProcesses bool, now time.Time) error {
	state, err := runtimesupervision.Read(runOpts.WorkDir)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if !hadProcesses {
			return nil
		}
		workDir, pathErr := absoluteCleanPath(runOpts.WorkDir)
		if pathErr != nil {
			return pathErr
		}
		state = runtimesupervision.State{
			Version:   runtimesupervision.Version,
			State:     runtimesupervision.StateStopped,
			WorkDir:   workDir,
			UpdatedAt: supervisionTimestamp(now),
		}
	} else {
		state.State = runtimesupervision.StateStopped
		state.PID = 0
		state.Attempts = 0
		state.HealthySince = ""
		state.LastHealthyAt = ""
		state.NextAttemptAt = ""
		state.LastNotice = ""
		state.LastNoticeAt = ""
		state.UpdatedAt = supervisionTimestamp(now)
	}
	if err := runtimesupervision.Write(runOpts.WorkDir, state); err != nil {
		return fmt.Errorf("disarm runtime supervision: %w", err)
	}
	appendRuntimeSupervisionEvent(runOpts.WorkDir, now, map[string]any{
		"event": "runtime_supervision_disarmed",
	})
	return nil
}

func markSupervisionRestartingLocked(runOpts Options, now time.Time) error {
	state, err := runtimesupervision.Read(runOpts.WorkDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if state.State != runtimesupervision.StateRunning && state.State != runtimesupervision.StateLatchedFailed {
		return nil
	}
	bootID, err := currentBootID()
	if err != nil || bootID != state.BootID {
		return nil
	}
	state.State = runtimesupervision.StateRestarting
	state.UpdatedAt = supervisionTimestamp(now)
	return runtimesupervision.Write(runOpts.WorkDir, state)
}

// prepareHotReloadSupervisionLocked verifies recovery proof before the controller
// receives the new config. The caller holds the lifecycle lock through commit.
func prepareHotReloadSupervisionLocked(ctx context.Context, runOpts Options, cachePath string, pid int, configSHA string) (*runtimesupervision.State, error) {
	state, err := runtimesupervision.Read(runOpts.WorkDir)
	if errors.Is(err, os.ErrNotExist) {
		// Runtimes started without supervision remain usable; do not invent proof.
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if state.State != runtimesupervision.StateRunning {
		return nil, fmt.Errorf("runtime supervision state is %q, want %q", state.State, runtimesupervision.StateRunning)
	}
	if state.PID != pid {
		return nil, fmt.Errorf("runtime supervision pid mismatch: state has %d, runtime has %d", state.PID, pid)
	}
	identity, err := canonicalSupervisionIdentity(runOpts, cachePath)
	if err != nil {
		return nil, err
	}
	if state.CorePath != identity.CorePath || state.ConfigPath != identity.ConfigPath || state.WorkDir != identity.WorkDir {
		return nil, errors.New("runtime supervision paths do not match hot reload inputs")
	}
	state.ConfigSHA256 = configSHA
	state.ValidationCachePath = identity.ValidationCachePath
	cache := mihomotest.CacheStatus(ctx, mihomotest.ValidationOptions{
		CorePath: state.CorePath, ConfigPath: state.ConfigPath,
		WorkDir: state.WorkDir, CachePath: state.ValidationCachePath,
	})
	if !cache.Present || !cache.Matched || !cache.Passed {
		return nil, fmt.Errorf("runtime supervision validation cache is not a matched pass: %s: %s", cache.Status, cache.Error)
	}
	if cache.CoreSHA256 != state.CoreSHA256 || cache.ConfigSHA256 != state.ConfigSHA256 {
		return nil, errors.New("runtime supervision validation cache hashes do not match hot reload inputs")
	}
	// Check the actual bytes after the version probe, including metadata hits.
	if err := verifyHotReloadSupervisionInputs(&state); err != nil {
		return nil, err
	}
	return &state, nil
}

func verifyHotReloadSupervisionInputs(state *runtimesupervision.State) error {
	bootID, err := currentBootID()
	if err != nil {
		return err
	}
	if state.BootID != bootID {
		return errors.New("runtime supervision boot_id mismatch")
	}
	if _, err := InspectManagedRuntimeProcess(state.PID, state.CorePath, state.ConfigPath, state.WorkDir); err != nil {
		return fmt.Errorf("inspect supervised process during hot reload: %w", err)
	}
	coreSHA, err := runtimesupervision.HashFile(state.CorePath)
	if err != nil {
		return fmt.Errorf("hash supervised core during hot reload: %w", err)
	}
	if coreSHA != state.CoreSHA256 {
		return errors.New("runtime supervision core hash changed during hot reload")
	}
	configSHA, err := runtimesupervision.HashFile(state.ConfigPath)
	if err != nil {
		return fmt.Errorf("hash supervised config during hot reload: %w", err)
	}
	if configSHA != state.ConfigSHA256 {
		return errors.New("runtime supervision config hash changed during hot reload")
	}
	return nil
}

func commitHotReloadSupervisionLocked(workDir string, state *runtimesupervision.State, now time.Time) error {
	if state == nil {
		return nil
	}
	// Reuse the preflight validation proof; do not run another fallible version
	// probe after the controller has already accepted the material change.
	if err := verifyHotReloadSupervisionInputs(state); err != nil {
		return err
	}
	state.UpdatedAt = supervisionTimestamp(now)
	if err := runtimesupervision.Write(workDir, *state); err != nil {
		return err
	}
	appendRuntimeSupervisionEvent(workDir, now, map[string]any{
		"event": "runtime_supervision_armed", "action": "hot_reload",
		"pid": state.PID, "core_sha256": state.CoreSHA256, "config_sha256": state.ConfigSHA256,
	})
	return nil
}

func CheckSupervision(ctx context.Context, opts SupervisionCheckOptions) (SupervisionCheckResult, error) {
	opts = normalizeSupervisionCheckOptions(opts)
	result := SupervisionCheckResult{}
	err := runtimesupervision.WithLock(opts.WorkDir, func() error {
		now := opts.Now().UTC()
		state, err := runtimesupervision.Read(opts.WorkDir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		result.Checked = true
		if state.State != runtimesupervision.StateRunning {
			result.Action = "ignored"
			result.Reason = state.State
			return nil
		}
		if err := validateManagedCorePath(state.CorePath); err != nil {
			result.Action = "blocked"
			result.Reason = "invalid_core_identity"
			return recordSupervisionNoticeLocked(opts.WorkDir, &state, now, result.Reason, map[string]any{"error": err.Error()})
		}
		bootID, err := currentBootID()
		if err != nil {
			result.Action = "blocked"
			result.Reason = "boot_id_unavailable"
			return recordSupervisionNoticeLocked(opts.WorkDir, &state, now, result.Reason, map[string]any{"error": err.Error()})
		}
		if bootID != state.BootID {
			result.Action = "blocked"
			result.Reason = "stale_boot_id"
			return recordSupervisionNoticeLocked(opts.WorkDir, &state, now, result.Reason, map[string]any{"state_boot_id": state.BootID, "current_boot_id": bootID})
		}
		processes := ManagedRuntimeProcesses()
		if len(processes) > 1 {
			result.Action = "blocked"
			result.Reason = "multiple_processes"
			return recordSupervisionNoticeLocked(opts.WorkDir, &state, now, result.Reason, map[string]any{"process_count": len(processes)})
		}
		if len(processes) == 1 {
			process := processes[0]
			result.PID = process.PID
			if process.PID != state.PID {
				result.Action = "blocked"
				result.Reason = "pid_mismatch"
				return recordSupervisionNoticeLocked(opts.WorkDir, &state, now, result.Reason, map[string]any{"state_pid": state.PID, "observed_pid": process.PID})
			}
			if err := ValidateManagedRuntimeProcess(process.PID, state.CorePath, state.ConfigPath, state.WorkDir, state.LauncherPath); err != nil {
				result.Action = "blocked"
				result.Reason = "process_identity_mismatch"
				return recordSupervisionNoticeLocked(opts.WorkDir, &state, now, result.Reason, map[string]any{"pid": process.PID, "error": err.Error()})
			}
			if err := probeRuntimeHealth(ctx, state.ConfigPath, process.PID, opts.ProbeTimeout); err != nil {
				result.Action = "observed"
				result.Reason = "runtime_unhealthy"
				return recordSupervisionNoticeLocked(opts.WorkDir, &state, now, result.Reason, map[string]any{"pid": process.PID, "error": err.Error()})
			}
			result.Action = "healthy"
			return recordHealthySupervisionLocked(opts.WorkDir, &state, now, opts.StableWindow)
		}
		return recoverMissingRuntimeLocked(ctx, opts, &state, now, &result)
	})
	return result, err
}

func recoverMissingRuntimeLocked(ctx context.Context, opts SupervisionCheckOptions, state *runtimesupervision.State, now time.Time, result *SupervisionCheckResult) error {
	if state.Attempts >= maxSupervisionAttempts {
		state.State = runtimesupervision.StateLatchedFailed
		state.UpdatedAt = supervisionTimestamp(now)
		if err := runtimesupervision.Write(opts.WorkDir, *state); err != nil {
			return err
		}
		result.Action = "latched"
		result.Reason = "restart_budget_exhausted"
		appendRuntimeSupervisionEvent(opts.WorkDir, now, map[string]any{
			"event":    "runtime_restart_latched",
			"attempts": state.Attempts,
			"reason":   result.Reason,
		})
		return nil
	}
	if state.NextAttemptAt != "" {
		next, _ := time.Parse(time.RFC3339Nano, state.NextAttemptAt)
		if now.Before(next) {
			result.Action = "waiting"
			result.Reason = "restart_backoff"
			return nil
		}
	}
	runOpts := normalizeOptions(Options{CorePath: state.CorePath, ConfigPath: state.ConfigPath, WorkDir: state.WorkDir, LogPath: state.LogPath})
	if err := runOpts.validate(); err != nil {
		result.Action = "blocked"
		result.Reason = "runtime_inputs_invalid"
		return recordSupervisionNoticeLocked(opts.WorkDir, state, now, result.Reason, map[string]any{"error": err.Error()})
	}
	coreSHA, err := runtimesupervision.HashFile(state.CorePath)
	if err != nil {
		result.Action = "blocked"
		result.Reason = "core_hash_unavailable"
		return recordSupervisionNoticeLocked(opts.WorkDir, state, now, result.Reason, map[string]any{"error": err.Error()})
	}
	if coreSHA != state.CoreSHA256 {
		result.Action = "blocked"
		result.Reason = "core_hash_mismatch"
		return recordSupervisionNoticeLocked(opts.WorkDir, state, now, result.Reason, map[string]any{"expected": state.CoreSHA256, "actual": coreSHA})
	}
	configSHA, err := runtimesupervision.HashFile(state.ConfigPath)
	if err != nil {
		result.Action = "blocked"
		result.Reason = "config_hash_unavailable"
		return recordSupervisionNoticeLocked(opts.WorkDir, state, now, result.Reason, map[string]any{"error": err.Error()})
	}
	if configSHA != state.ConfigSHA256 {
		result.Action = "blocked"
		result.Reason = "config_hash_mismatch"
		return recordSupervisionNoticeLocked(opts.WorkDir, state, now, result.Reason, map[string]any{"expected": state.ConfigSHA256, "actual": configSHA})
	}
	cache := mihomotest.CacheStatus(ctx, mihomotest.ValidationOptions{
		CorePath:   state.CorePath,
		ConfigPath: state.ConfigPath,
		WorkDir:    state.WorkDir,
		CachePath:  state.ValidationCachePath,
	})
	if !cache.Present || !cache.Matched || !cache.Passed {
		result.Action = "blocked"
		result.Reason = "validation_not_matched"
		return recordSupervisionNoticeLocked(opts.WorkDir, state, now, result.Reason, map[string]any{"validation_status": cache.Status, "error": cache.Error})
	}
	if cache.CoreSHA256 != coreSHA || cache.ConfigSHA256 != configSHA {
		result.Action = "blocked"
		result.Reason = "validation_hash_mismatch"
		return recordSupervisionNoticeLocked(opts.WorkDir, state, now, result.Reason, map[string]any{
			"validated_core_sha256":   cache.CoreSHA256,
			"validated_config_sha256": cache.ConfigSHA256,
			"actual_core_sha256":      coreSHA,
			"actual_config_sha256":    configSHA,
		})
	}
	attempt := state.Attempts + 1
	result.Action = "restart_attempt"
	result.Attempt = attempt
	state.Attempts = attempt
	state.HealthySince = ""
	state.LastHealthyAt = ""
	state.LastNotice = ""
	state.LastNoticeAt = ""
	state.UpdatedAt = supervisionTimestamp(now)
	switch attempt {
	case 1:
		state.NextAttemptAt = supervisionTimestamp(now.Add(10 * time.Second))
	case 2:
		state.NextAttemptAt = supervisionTimestamp(now.Add(30 * time.Second))
	default:
		state.NextAttemptAt = ""
	}
	if err := runtimesupervision.Write(opts.WorkDir, *state); err != nil {
		return err
	}
	appendRuntimeSupervisionEvent(opts.WorkDir, now, map[string]any{
		"event":        "runtime_exit_observed",
		"previous_pid": state.PID,
		"attempt":      attempt,
	})
	appendRuntimeSupervisionEvent(opts.WorkDir, now, map[string]any{
		"event":         "runtime_restart_attempt",
		"attempt":       attempt,
		"core_sha256":   state.CoreSHA256,
		"config_sha256": state.ConfigSHA256,
	})
	cmd, err := spawnBackgroundRuntime(runOpts)
	if err != nil {
		pid := 0
		if cmd != nil && cmd.Process != nil {
			pid = cmd.Process.Pid
			state.PID = pid
			if writeErr := runtimesupervision.Write(opts.WorkDir, *state); writeErr != nil {
				err = errors.Join(err, fmt.Errorf("record started recovery pid: %w", writeErr))
			}
		}
		result.Action = "restart_failed"
		result.Reason = "process_start_failed"
		if attempt == maxSupervisionAttempts {
			result.Action = "latched"
			result.Reason = "restart_budget_exhausted"
		}
		return recordRecoveryFailureLocked(opts.WorkDir, state, now, attempt, pid, err)
	}
	result.PID = cmd.Process.Pid
	if err := probeRuntimeHealth(ctx, state.ConfigPath, cmd.Process.Pid, opts.HealthTimeout); err != nil {
		launcherPath, identityErr := InspectManagedRuntimeProcess(cmd.Process.Pid, state.CorePath, state.ConfigPath, state.WorkDir)
		if identityErr != nil {
			result.Action = "restart_failed"
			result.Reason = "process_identity_mismatch"
			return recordRecoveryFailureLocked(opts.WorkDir, state, now, attempt, cmd.Process.Pid, errors.Join(err, identityErr))
		}
		state.PID = cmd.Process.Pid
		state.LauncherPath = launcherPath
		if writeErr := runtimesupervision.Write(opts.WorkDir, *state); writeErr != nil {
			return writeErr
		}
		if attempt == maxSupervisionAttempts && (!processRunning(cmd.Process.Pid) || processZombie(cmd.Process.Pid)) {
			result.Action = "latched"
			result.Reason = "restart_budget_exhausted"
			return recordRecoveryFailureLocked(opts.WorkDir, state, now, attempt, cmd.Process.Pid, err)
		}
		appendRuntimeSupervisionEvent(opts.WorkDir, now, map[string]any{
			"event":   "runtime_restart_failed",
			"attempt": attempt,
			"pid":     cmd.Process.Pid,
			"error":   err.Error(),
		})
		result.Action = "restart_failed"
		result.Reason = "health_check_failed"
		return nil
	}
	launcherPath, err := InspectManagedRuntimeProcess(cmd.Process.Pid, state.CorePath, state.ConfigPath, state.WorkDir)
	if err != nil {
		result.Action = "restart_failed"
		result.Reason = "process_identity_mismatch"
		return recordRecoveryFailureLocked(opts.WorkDir, state, now, attempt, cmd.Process.Pid, err)
	}
	state.PID = cmd.Process.Pid
	state.LauncherPath = launcherPath
	state.HealthySince = supervisionTimestamp(now)
	state.LastHealthyAt = supervisionTimestamp(now)
	state.UpdatedAt = supervisionTimestamp(now)
	if err := runtimesupervision.Write(opts.WorkDir, *state); err != nil {
		return err
	}
	appendRuntimeSupervisionEvent(opts.WorkDir, now, map[string]any{
		"event":   "runtime_restart_recovered",
		"attempt": attempt,
		"pid":     cmd.Process.Pid,
	})
	result.Action = "recovered"
	return nil
}

func recordRecoveryFailureLocked(workDir string, state *runtimesupervision.State, now time.Time, attempt, pid int, failure error) error {
	appendRuntimeSupervisionEvent(workDir, now, map[string]any{
		"event":   "runtime_restart_failed",
		"attempt": attempt,
		"pid":     pid,
		"error":   failure.Error(),
	})
	if attempt < maxSupervisionAttempts {
		return nil
	}
	state.State = runtimesupervision.StateLatchedFailed
	state.UpdatedAt = supervisionTimestamp(now)
	if err := runtimesupervision.Write(workDir, *state); err != nil {
		return err
	}
	appendRuntimeSupervisionEvent(workDir, now, map[string]any{
		"event":    "runtime_restart_latched",
		"attempts": state.Attempts,
		"reason":   "restart_budget_exhausted",
	})
	return nil
}

func recordHealthySupervisionLocked(workDir string, state *runtimesupervision.State, now time.Time, stableWindow time.Duration) error {
	changed := false
	resetAttempts := false
	if state.HealthySince == "" {
		state.HealthySince = supervisionTimestamp(now)
		state.LastHealthyAt = supervisionTimestamp(now)
		changed = true
	}
	if state.LastNotice != "" || state.LastNoticeAt != "" {
		state.LastNotice = ""
		state.LastNoticeAt = ""
		changed = true
	}
	healthySince, _ := time.Parse(time.RFC3339Nano, state.HealthySince)
	if state.Attempts > 0 && !now.Before(healthySince.Add(stableWindow)) {
		state.Attempts = 0
		state.NextAttemptAt = ""
		state.LastHealthyAt = supervisionTimestamp(now)
		changed = true
		resetAttempts = true
	}
	if !changed {
		return nil
	}
	state.UpdatedAt = supervisionTimestamp(now)
	if err := runtimesupervision.Write(workDir, *state); err != nil {
		return err
	}
	if resetAttempts {
		appendRuntimeSupervisionEvent(workDir, now, map[string]any{
			"event": "runtime_supervision_stable",
			"pid":   state.PID,
		})
	}
	return nil
}

func recordSupervisionNoticeLocked(workDir string, state *runtimesupervision.State, now time.Time, reason string, fields map[string]any) error {
	if state.LastNotice == reason && state.LastNoticeAt != "" {
		last, _ := time.Parse(time.RFC3339Nano, state.LastNoticeAt)
		if now.Before(last.Add(supervisionNoticeInterval)) {
			return nil
		}
	}
	state.LastNotice = reason
	state.LastNoticeAt = supervisionTimestamp(now)
	state.UpdatedAt = supervisionTimestamp(now)
	if err := runtimesupervision.Write(workDir, *state); err != nil {
		return err
	}
	event := "runtime_supervision_blocked"
	if reason == "runtime_unhealthy" {
		event = "runtime_unhealthy"
	}
	record := map[string]any{"event": event, "reason": reason}
	for key, value := range fields {
		if value != "" && value != nil {
			record[key] = value
		}
	}
	appendRuntimeSupervisionEvent(workDir, now, record)
	return nil
}

func normalizeSupervisionCheckOptions(opts SupervisionCheckOptions) SupervisionCheckOptions {
	opts.WorkDir = strings.TrimSpace(opts.WorkDir)
	if opts.ProbeTimeout <= 0 {
		opts.ProbeTimeout = defaultRuntimeProbeTimeout
	}
	if opts.HealthTimeout <= 0 {
		opts.HealthTimeout = defaultRuntimeHealthTimeout
	}
	if opts.StableWindow <= 0 {
		opts.StableWindow = defaultStableHealthWindow
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return opts
}

type supervisionIdentity struct {
	CorePath            string
	ConfigPath          string
	WorkDir             string
	LogPath             string
	ValidationCachePath string
}

func canonicalSupervisionIdentity(runOpts Options, cachePath string) (supervisionIdentity, error) {
	corePath, err := absoluteCleanPath(runOpts.CorePath)
	if err != nil {
		return supervisionIdentity{}, fmt.Errorf("resolve supervised core path: %w", err)
	}
	configPath, err := absoluteCleanPath(runOpts.ConfigPath)
	if err != nil {
		return supervisionIdentity{}, fmt.Errorf("resolve supervised config path: %w", err)
	}
	workDir, err := absoluteCleanPath(runOpts.WorkDir)
	if err != nil {
		return supervisionIdentity{}, fmt.Errorf("resolve supervised runtime dir: %w", err)
	}
	logPath, err := absoluteCleanPath(runOpts.LogPath)
	if err != nil {
		return supervisionIdentity{}, fmt.Errorf("resolve supervised log path: %w", err)
	}
	cachePath, err = absoluteCleanPath(cachePath)
	if err != nil {
		return supervisionIdentity{}, fmt.Errorf("resolve supervised validation cache path: %w", err)
	}
	return supervisionIdentity{CorePath: corePath, ConfigPath: configPath, WorkDir: workDir, LogPath: logPath, ValidationCachePath: cachePath}, nil
}

func absoluteCleanPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("path is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(absolute), nil
}

func defaultProbeRuntimeHealth(ctx context.Context, configPath string, pid int, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = defaultRuntimeHealthTimeout
	}
	client, err := mihomoapi.NewFromConfig(configPath)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		if !processRunning(pid) || processZombie(pid) {
			return fmt.Errorf("runtime pid %d exited before controller became healthy", pid)
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			if lastErr == nil {
				lastErr = errors.New("controller did not become healthy")
			}
			return fmt.Errorf("mihomo controller /version did not become healthy within %s: %w", timeout, lastErr)
		}
		requestTimeout := time.Second
		if remaining < requestTimeout {
			requestTimeout = remaining
		}
		_, lastErr = client.Request(ctx, mihomoapi.RequestOptions{Method: "GET", Path: "/version", Timeout: requestTimeout, MaxBytes: 64 * 1024})
		if lastErr == nil {
			return nil
		}
		timer := time.NewTimer(200 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func appendRuntimeSupervisionEvent(workDir string, now time.Time, fields map[string]any) {
	fields["ts"] = supervisionTimestamp(now)
	_ = runtimesupervision.AppendEvent(workDir, fields)
}

func supervisionTimestamp(now time.Time) string {
	return now.UTC().Format(time.RFC3339Nano)
}
