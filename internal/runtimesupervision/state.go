package runtimesupervision

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	Version = 1

	StateStarting      = "starting"
	StateRunning       = "running"
	StateRestarting    = "restarting"
	StateStopped       = "stopped"
	StateLatchedFailed = "latched_failed"

	stateFileName = "runtime-supervision.json"
	eventLogName  = "watchdog.jsonl"
)

type State struct {
	Version             int    `json:"version"`
	State               string `json:"state"`
	BootID              string `json:"boot_id,omitempty"`
	CorePath            string `json:"core_path,omitempty"`
	CoreSHA256          string `json:"core_sha256,omitempty"`
	LauncherPath        string `json:"launcher_path,omitempty"`
	ConfigPath          string `json:"config_path,omitempty"`
	ConfigSHA256        string `json:"config_sha256,omitempty"`
	WorkDir             string `json:"runtime_dir"`
	LogPath             string `json:"log_path,omitempty"`
	ValidationCachePath string `json:"validation_cache_path,omitempty"`
	PID                 int    `json:"pid,omitempty"`
	Attempts            int    `json:"attempts,omitempty"`
	HealthySince        string `json:"healthy_since,omitempty"`
	LastHealthyAt       string `json:"last_healthy_at,omitempty"`
	NextAttemptAt       string `json:"next_attempt_at,omitempty"`
	LastNotice          string `json:"last_notice,omitempty"`
	LastNoticeAt        string `json:"last_notice_at,omitempty"`
	UpdatedAt           string `json:"updated_at"`
}

func Path(workDir string) string {
	return filepath.Join(strings.TrimSpace(workDir), stateFileName)
}

func EventLogPath(workDir string) string {
	workDir = strings.TrimSpace(workDir)
	parent := filepath.Dir(workDir)
	if parent == "." || parent == "" {
		return filepath.Join(".runtime", "logs", eventLogName)
	}
	return filepath.Join(parent, "logs", eventLogName)
}

func CurrentBootID() (string, error) {
	data, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", fmt.Errorf("read current boot_id: %w", err)
	}
	bootID := strings.TrimSpace(string(data))
	if bootID == "" {
		return "", errors.New("read current boot_id: value is empty")
	}
	return bootID, nil
}

func Read(workDir string) (State, error) {
	path := Path(workDir)
	data, err := os.ReadFile(path)
	if err != nil {
		return State{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var state State
	if err := decoder.Decode(&state); err != nil {
		return State{}, fmt.Errorf("decode runtime supervision state %q: %w", path, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("unexpected trailing JSON value")
		}
		return State{}, fmt.Errorf("decode runtime supervision state %q: %w", path, err)
	}
	if err := Validate(state); err != nil {
		return State{}, fmt.Errorf("invalid runtime supervision state %q: %w", path, err)
	}
	absoluteWorkDir, err := filepath.Abs(strings.TrimSpace(workDir))
	if err != nil {
		return State{}, fmt.Errorf("resolve runtime supervision state directory %q: %w", workDir, err)
	}
	if filepath.Clean(absoluteWorkDir) != filepath.Clean(state.WorkDir) {
		return State{}, fmt.Errorf("invalid runtime supervision state %q: runtime_dir %q does not match state file directory %q", path, state.WorkDir, filepath.Clean(absoluteWorkDir))
	}
	return state, nil
}

func Write(workDir string, state State) error {
	workDir = strings.TrimSpace(workDir)
	if workDir == "" {
		return errors.New("runtime supervision workdir is required")
	}
	if state.Version == 0 {
		state.Version = Version
	}
	absoluteWorkDir, err := filepath.Abs(workDir)
	if err != nil {
		return fmt.Errorf("resolve runtime supervision workdir: %w", err)
	}
	if filepath.Clean(absoluteWorkDir) != filepath.Clean(state.WorkDir) {
		return fmt.Errorf("runtime supervision state runtime_dir %q does not match state file directory %q", state.WorkDir, filepath.Clean(absoluteWorkDir))
	}
	if err := Validate(state); err != nil {
		return err
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return fmt.Errorf("create runtime supervision directory: %w", err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode runtime supervision state: %w", err)
	}
	data = append(data, '\n')
	path := Path(workDir)
	temp, err := os.CreateTemp(workDir, "."+stateFileName+".tmp-*")
	if err != nil {
		return fmt.Errorf("create runtime supervision temporary file: %w", err)
	}
	tempPath := temp.Name()
	cleanup := true
	defer func() {
		_ = temp.Close()
		if cleanup {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		return fmt.Errorf("set runtime supervision temporary file permissions: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		return fmt.Errorf("write runtime supervision state: %w", err)
	}
	if err := temp.Sync(); err != nil {
		return fmt.Errorf("sync runtime supervision state: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close runtime supervision state: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace runtime supervision state: %w", err)
	}
	cleanup = false
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("set runtime supervision state permissions: %w", err)
	}
	if dir, err := os.Open(workDir); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}

func Validate(state State) error {
	if state.Version != Version {
		return fmt.Errorf("schema version mismatch: expected %d, got %d", Version, state.Version)
	}
	switch state.State {
	case StateStarting, StateRunning, StateRestarting, StateStopped, StateLatchedFailed:
	default:
		return fmt.Errorf("unknown state %q", state.State)
	}
	if strings.TrimSpace(state.WorkDir) == "" || !filepath.IsAbs(state.WorkDir) {
		return errors.New("runtime_dir must be an absolute path")
	}
	if err := validateTimestamp("updated_at", state.UpdatedAt, true); err != nil {
		return err
	}
	for name, value := range map[string]string{
		"healthy_since":   state.HealthySince,
		"last_healthy_at": state.LastHealthyAt,
		"next_attempt_at": state.NextAttemptAt,
		"last_notice_at":  state.LastNoticeAt,
	} {
		if err := validateTimestamp(name, value, false); err != nil {
			return err
		}
	}
	if state.Attempts < 0 || state.Attempts > 3 {
		return fmt.Errorf("attempts must be between 0 and 3, got %d", state.Attempts)
	}
	if (state.LastNotice == "") != (state.LastNoticeAt == "") {
		return errors.New("last_notice and last_notice_at must be set together")
	}
	if state.State == StateStopped {
		return nil
	}
	if strings.TrimSpace(state.BootID) == "" {
		return errors.New("boot_id is required")
	}
	for name, value := range map[string]string{
		"core_path":             state.CorePath,
		"config_path":           state.ConfigPath,
		"log_path":              state.LogPath,
		"validation_cache_path": state.ValidationCachePath,
	} {
		if strings.TrimSpace(value) == "" || !filepath.IsAbs(value) {
			return fmt.Errorf("%s must be an absolute path", name)
		}
	}
	if !validSHA256(state.CoreSHA256) {
		return errors.New("core_sha256 must be a lowercase SHA-256 digest")
	}
	if !validSHA256(state.ConfigSHA256) {
		return errors.New("config_sha256 must be a lowercase SHA-256 digest")
	}
	if state.State != StateStarting && (strings.TrimSpace(state.LauncherPath) == "" || !filepath.IsAbs(state.LauncherPath)) {
		return errors.New("launcher_path must be an absolute path")
	}
	if state.PID <= 0 && state.State == StateRunning {
		return errors.New("pid must be positive while state is running")
	}
	return nil
}

func HashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func AppendEvent(workDir string, fields map[string]any) error {
	if fields == nil {
		return nil
	}
	record := make(map[string]any, len(fields)+1)
	for key, value := range fields {
		record[key] = value
	}
	if _, ok := record["ts"]; !ok {
		record["ts"] = time.Now().UTC().Format(time.RFC3339Nano)
	}
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode watchdog event: %w", err)
	}
	data = append(data, '\n')
	path := EventLogPath(workDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	rotateEventLog(path, eventLogMaxBytes())
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(data)
	return err
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validateTimestamp(name, value string, required bool) error {
	value = strings.TrimSpace(value)
	if value == "" {
		if required {
			return fmt.Errorf("%s is required", name)
		}
		return nil
	}
	if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
		return fmt.Errorf("%s must be RFC3339: %w", name, err)
	}
	return nil
}

func eventLogMaxBytes() int64 {
	const fallback = int64(512 * 1024)
	raw := strings.TrimSpace(os.Getenv("LOCALCLASH_SERVICE_LOG_MAX_BYTES"))
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || parsed < 0 {
		return fallback
	}
	return parsed
}

func rotateEventLog(path string, maxBytes int64) {
	if maxBytes <= 0 {
		return
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() < maxBytes {
		return
	}
	rotated := path + ".1"
	_ = os.Remove(rotated)
	_ = os.Rename(path, rotated)
}
