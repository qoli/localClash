package mihomotest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	cacheVersion         = 1
	defaultCacheEntryMax = 32
)

type ValidationOptions struct {
	CorePath   string
	ConfigPath string
	WorkDir    string
	CachePath  string
	Force      bool
	Timeout    time.Duration
	Now        func() time.Time
}

type ValidationResult struct {
	Enabled         bool   `json:"enabled"`
	Passed          bool   `json:"passed"`
	Cached          bool   `json:"cached"`
	CachePath       string `json:"cache_path,omitempty"`
	CacheHitMode    string `json:"cache_hit_mode,omitempty"`
	ValidatedAt     string `json:"validated_at,omitempty"`
	ConfigPath      string `json:"config_path"`
	ConfigSHA256    string `json:"config_sha256,omitempty"`
	ConfigSize      int64  `json:"config_size,omitempty"`
	ConfigModTime   string `json:"config_mod_time,omitempty"`
	CorePath        string `json:"core_path"`
	CoreType        string `json:"core_type,omitempty"`
	CoreVersion     string `json:"core_version,omitempty"`
	CoreSHA256      string `json:"core_sha256,omitempty"`
	CoreSize        int64  `json:"core_size,omitempty"`
	CoreModTime     string `json:"core_mod_time,omitempty"`
	Output          string `json:"output,omitempty"`
	Error           string `json:"error,omitempty"`
	CacheWriteError string `json:"cache_write_error,omitempty"`
	TimedOut        bool   `json:"timed_out,omitempty"`
	DurationMS      int64  `json:"duration_ms,omitempty"`
	ExitCode        int    `json:"exit_code,omitempty"`
	Isolated        bool   `json:"isolated,omitempty"`
	WorkDir         string `json:"work_dir,omitempty"`
	SourceWorkDir   string `json:"source_work_dir,omitempty"`
}

type CacheStatusResult struct {
	CachePath     string `json:"cache_path"`
	Present       bool   `json:"present"`
	Matched       bool   `json:"matched"`
	MatchMode     string `json:"match_mode,omitempty"`
	Passed        bool   `json:"passed,omitempty"`
	ValidatedAt   string `json:"validated_at,omitempty"`
	ConfigPath    string `json:"config_path"`
	ConfigSHA256  string `json:"config_sha256,omitempty"`
	ConfigSize    int64  `json:"config_size,omitempty"`
	ConfigModTime string `json:"config_mod_time,omitempty"`
	CorePath      string `json:"core_path"`
	CoreType      string `json:"core_type,omitempty"`
	CoreVersion   string `json:"core_version,omitempty"`
	CoreSHA256    string `json:"core_sha256,omitempty"`
	CoreSize      int64  `json:"core_size,omitempty"`
	CoreModTime   string `json:"core_mod_time,omitempty"`
	DurationMS    int64  `json:"duration_ms,omitempty"`
	Error         string `json:"error,omitempty"`
	Status        string `json:"status"`
}

type validationCacheFile struct {
	Version int                `json:"version"`
	Entries []ValidationResult `json:"entries"`
}

func DefaultCachePath(workDir string) string {
	workDir = strings.TrimSpace(workDir)
	if workDir == "" {
		workDir = filepath.Join(".runtime", "mihomo")
	}
	parent := filepath.Dir(workDir)
	if parent == "." || parent == "" {
		parent = ".runtime"
	}
	return filepath.Join(parent, "validation", "mihomo-test-cache.json")
}

func ValidateCached(ctx context.Context, opts ValidationOptions) (ValidationResult, error) {
	opts = normalizeValidationOptions(opts)
	started := opts.Now()
	metadata, err := buildInputMetadata(ctx, opts)
	if err != nil {
		return ValidationResult{Enabled: true, CachePath: opts.CachePath, ConfigPath: opts.ConfigPath, CorePath: opts.CorePath, Error: err.Error()}, err
	}
	if !opts.Force {
		if cached, ok := findCachedValidationByMetadata(opts.CachePath, metadata); ok && cached.Passed {
			cached.Enabled = true
			cached.Cached = true
			cached.CachePath = opts.CachePath
			cached.CacheHitMode = "metadata"
			return cached, nil
		}
	}
	fingerprint, err := buildFingerprintWithMetadata(opts, metadata)
	if err != nil {
		return ValidationResult{Enabled: true, CachePath: opts.CachePath, ConfigPath: opts.ConfigPath, CorePath: opts.CorePath, Error: err.Error()}, err
	}
	if !opts.Force {
		if cached, ok := findCachedValidation(opts.CachePath, fingerprint); ok && cached.Passed {
			cached.Enabled = true
			cached.Cached = true
			cached.CachePath = opts.CachePath
			cached.CacheHitMode = "sha256"
			return cached, nil
		}
	}

	result := ValidationResult{
		Enabled:       true,
		CachePath:     opts.CachePath,
		ValidatedAt:   started.UTC().Format(time.RFC3339),
		ConfigPath:    opts.ConfigPath,
		ConfigSHA256:  fingerprint.ConfigSHA256,
		ConfigSize:    metadata.ConfigSize,
		ConfigModTime: metadata.ConfigModTime,
		CorePath:      opts.CorePath,
		CoreType:      fingerprint.CoreType,
		CoreVersion:   fingerprint.CoreVersion,
		CoreSHA256:    fingerprint.CoreSHA256,
		CoreSize:      metadata.CoreSize,
		CoreModTime:   metadata.CoreModTime,
		Isolated:      true,
		SourceWorkDir: opts.WorkDir,
	}
	workDir, cleanup, err := SnapshotRuntimeDir(opts.WorkDir, "localclash-mihomo-test-*")
	if err != nil {
		result.Error = "cannot create isolated mihomo test runtime dir: " + err.Error()
		result.Output = result.Error
		result.DurationMS = opts.Now().Sub(started).Milliseconds()
		_ = writeValidationCache(opts.CachePath, result)
		return result, err
	}
	defer cleanup()
	result.WorkDir = workDir

	runCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, opts.CorePath, "-d", workDir, "-f", opts.ConfigPath, "-t")
	output, err := cmd.CombinedOutput()
	result.Passed = err == nil
	result.Output = compactOutput(output, err)
	result.DurationMS = opts.Now().Sub(started).Milliseconds()
	if err != nil {
		result.Error = err.Error()
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			result.TimedOut = true
			result.Error = fmt.Sprintf("mihomo config test timed out after %s: %v", opts.Timeout, err)
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
		}
	}
	if cacheErr := writeValidationCache(opts.CachePath, result); cacheErr != nil {
		result.CacheWriteError = cacheErr.Error()
	}
	return result, err
}

func CacheStatus(ctx context.Context, opts ValidationOptions) CacheStatusResult {
	opts = normalizeValidationOptions(opts)
	out := CacheStatusResult{
		CachePath:  opts.CachePath,
		ConfigPath: opts.ConfigPath,
		CorePath:   opts.CorePath,
		Status:     "unavailable",
	}
	if _, err := os.Stat(opts.CachePath); err == nil {
		out.Present = true
	} else if !os.IsNotExist(err) {
		out.Error = err.Error()
		return out
	}
	if out.Present {
		if _, err := readValidationCache(opts.CachePath); err != nil {
			out.Error = "read validation cache: " + err.Error()
			return out
		}
	}
	metadata, err := buildInputMetadata(ctx, opts)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	out.ConfigSize = metadata.ConfigSize
	out.ConfigModTime = metadata.ConfigModTime
	out.CoreType = metadata.CoreType
	out.CoreVersion = metadata.CoreVersion
	out.CoreSize = metadata.CoreSize
	out.CoreModTime = metadata.CoreModTime
	if !out.Present {
		out.Status = "missing"
		return out
	}
	if cached, ok := findCachedValidationByMetadata(opts.CachePath, metadata); ok {
		out.Matched = true
		out.MatchMode = "metadata"
		out.Passed = cached.Passed
		out.ValidatedAt = cached.ValidatedAt
		out.ConfigSHA256 = cached.ConfigSHA256
		out.CoreSHA256 = cached.CoreSHA256
		out.DurationMS = cached.DurationMS
		out.Status = "matched"
		if !cached.Passed {
			out.Status = "matched_failed"
			out.Error = cached.Error
		}
		return out
	}
	fingerprint, err := buildFingerprintWithMetadata(opts, metadata)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	out.ConfigSHA256 = fingerprint.ConfigSHA256
	out.CoreType = fingerprint.CoreType
	out.CoreVersion = fingerprint.CoreVersion
	out.CoreSHA256 = fingerprint.CoreSHA256
	if cached, ok := findCachedValidation(opts.CachePath, fingerprint); ok {
		out.Matched = true
		out.MatchMode = "sha256"
		out.Passed = cached.Passed
		out.ValidatedAt = cached.ValidatedAt
		out.DurationMS = cached.DurationMS
		out.Status = "matched"
		if !cached.Passed {
			out.Status = "matched_failed"
			out.Error = cached.Error
		}
		return out
	}
	if out.Present {
		out.Status = "present_but_stale_or_for_different_input"
	} else {
		out.Status = "missing"
	}
	return out
}

type validationFingerprint struct {
	ConfigSHA256  string
	CoreSHA256    string
	CoreVersion   string
	CoreType      string
	ConfigSize    int64
	ConfigModTime string
	CoreSize      int64
	CoreModTime   string
}

type validationInputMetadata struct {
	ConfigPath    string
	ConfigSize    int64
	ConfigModTime string
	CorePath      string
	CoreSize      int64
	CoreModTime   string
	CoreVersion   string
	CoreType      string
}

func normalizeValidationOptions(opts ValidationOptions) ValidationOptions {
	opts.CorePath = strings.TrimSpace(opts.CorePath)
	opts.ConfigPath = strings.TrimSpace(opts.ConfigPath)
	opts.WorkDir = strings.TrimSpace(opts.WorkDir)
	opts.CachePath = strings.TrimSpace(opts.CachePath)
	if opts.WorkDir == "" {
		opts.WorkDir = filepath.Join(".runtime", "mihomo")
	}
	if opts.CachePath == "" {
		opts.CachePath = DefaultCachePath(opts.WorkDir)
	}
	if opts.Timeout == 0 {
		opts.Timeout = 180 * time.Second
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return opts
}

func buildFingerprint(ctx context.Context, opts ValidationOptions) (validationFingerprint, error) {
	metadata, err := buildInputMetadata(ctx, opts)
	if err != nil {
		return validationFingerprint{}, err
	}
	return buildFingerprintWithMetadata(opts, metadata)
}

func buildFingerprintWithMetadata(opts ValidationOptions, metadata validationInputMetadata) (validationFingerprint, error) {
	configSHA, err := sha256File(opts.ConfigPath)
	if err != nil {
		return validationFingerprint{}, fmt.Errorf("hash generated config: %w", err)
	}
	coreSHA, err := sha256File(opts.CorePath)
	if err != nil {
		return validationFingerprint{}, fmt.Errorf("hash core: %w", err)
	}
	return validationFingerprint{
		ConfigSHA256:  configSHA,
		CoreSHA256:    coreSHA,
		CoreVersion:   metadata.CoreVersion,
		CoreType:      metadata.CoreType,
		ConfigSize:    metadata.ConfigSize,
		ConfigModTime: metadata.ConfigModTime,
		CoreSize:      metadata.CoreSize,
		CoreModTime:   metadata.CoreModTime,
	}, nil
}

func buildInputMetadata(ctx context.Context, opts ValidationOptions) (validationInputMetadata, error) {
	configInfo, err := os.Stat(opts.ConfigPath)
	if err != nil {
		return validationInputMetadata{}, fmt.Errorf("stat generated config: %w", err)
	}
	coreInfo, err := os.Stat(opts.CorePath)
	if err != nil {
		return validationInputMetadata{}, fmt.Errorf("stat core: %w", err)
	}
	version, err := coreVersion(ctx, opts.CorePath)
	if err != nil {
		return validationInputMetadata{}, err
	}
	return validationInputMetadata{
		ConfigPath:    opts.ConfigPath,
		ConfigSize:    configInfo.Size(),
		ConfigModTime: configInfo.ModTime().UTC().Format(time.RFC3339Nano),
		CorePath:      opts.CorePath,
		CoreSize:      coreInfo.Size(),
		CoreModTime:   coreInfo.ModTime().UTC().Format(time.RFC3339Nano),
		CoreVersion:   version,
		CoreType:      coreType(opts.CorePath),
	}, nil
}

func findCachedValidationByMetadata(path string, metadata validationInputMetadata) (ValidationResult, bool) {
	cache, err := readValidationCache(path)
	if err != nil {
		return ValidationResult{}, false
	}
	for _, entry := range cache.Entries {
		if entry.ConfigPath == metadata.ConfigPath &&
			entry.ConfigSize == metadata.ConfigSize &&
			entry.ConfigModTime == metadata.ConfigModTime &&
			entry.CorePath == metadata.CorePath &&
			entry.CoreSize == metadata.CoreSize &&
			entry.CoreModTime == metadata.CoreModTime &&
			entry.CoreVersion == metadata.CoreVersion &&
			entry.CoreType == metadata.CoreType {
			return entry, true
		}
	}
	return ValidationResult{}, false
}

func findCachedValidation(path string, fingerprint validationFingerprint) (ValidationResult, bool) {
	cache, err := readValidationCache(path)
	if err != nil {
		return ValidationResult{}, false
	}
	for _, entry := range cache.Entries {
		if entry.ConfigSHA256 == fingerprint.ConfigSHA256 &&
			entry.CoreSHA256 == fingerprint.CoreSHA256 &&
			entry.CoreVersion == fingerprint.CoreVersion &&
			entry.CoreType == fingerprint.CoreType {
			return entry, true
		}
	}
	return ValidationResult{}, false
}

func readValidationCache(path string) (validationCacheFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return validationCacheFile{}, err
	}
	var cache validationCacheFile
	if err := json.Unmarshal(data, &cache); err != nil {
		return validationCacheFile{}, err
	}
	if cache.Version != cacheVersion {
		return validationCacheFile{}, fmt.Errorf("validation cache schema version mismatch: got %d", cache.Version)
	}
	return cache, nil
}

func writeValidationCache(path string, result ValidationResult) error {
	cache, err := readValidationCache(path)
	if err != nil && !os.IsNotExist(err) {
		cache = validationCacheFile{}
	}
	cache.Version = cacheVersion
	var entries []ValidationResult
	entries = append(entries, result)
	for _, entry := range cache.Entries {
		if entry.ConfigSHA256 == result.ConfigSHA256 &&
			entry.CoreSHA256 == result.CoreSHA256 &&
			entry.CoreVersion == result.CoreVersion &&
			entry.CoreType == result.CoreType {
			continue
		}
		entries = append(entries, entry)
		if len(entries) >= defaultCacheEntryMax {
			break
		}
	}
	cache.Entries = entries
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	temp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	_, writeErr := temp.Write(data)
	syncErr := temp.Sync()
	closeErr := temp.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		_ = os.Remove(tempPath)
		if writeErr != nil {
			return writeErr
		}
		if syncErr != nil {
			return syncErr
		}
		return closeErr
	}
	return os.Rename(tempPath, path)
}

func sha256File(path string) (string, error) {
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

func coreVersion(ctx context.Context, corePath string) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(runCtx, corePath, "-v").CombinedOutput()
	if err != nil {
		if runCtx.Err() != nil {
			return "", fmt.Errorf("query core version: %w", runCtx.Err())
		}
		return "", fmt.Errorf("query core version: %w", err)
	}
	version := strings.TrimSpace(string(output))
	if version == "" {
		return "", errors.New("query core version: empty output")
	}
	return version, nil
}

func coreType(path string) string {
	base := strings.ToLower(filepath.Base(path))
	if strings.Contains(base, "smart") {
		return "smart"
	}
	return "meta"
}

func compactOutput(output []byte, err error) string {
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 || lines[0] == "" {
		if err != nil {
			return err.Error()
		}
		return ""
	}
	const maxLines = 8
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	if err != nil {
		lines = append(lines, "error: "+err.Error())
	}
	return strings.Join(lines, "\n")
}
