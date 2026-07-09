package mcp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultWatchdogInterval      = time.Hour
	defaultMihomoLogMaxBytes     = int64(10 * 1024 * 1024)
	watchdogLogName              = "watchdog.jsonl"
	watchdogMihomoLogDefaultName = "mihomo.log"
)

type watchdogOptions struct {
	Interval          time.Duration
	MihomoLogMaxBytes int64
}

func defaultWatchdogOptions() watchdogOptions {
	return watchdogOptions{
		Interval:          taskMonitorDurationEnv("LOCALCLASH_WATCHDOG_INTERVAL_MS", defaultWatchdogInterval),
		MihomoLogMaxBytes: int64(envInt("LOCALCLASH_MIHOMO_LOG_MAX_BYTES", int(defaultMihomoLogMaxBytes))),
	}
}

func (s *Server) startWatchdog() {
	if s == nil || s.state == nil {
		return
	}
	opts := defaultWatchdogOptions()
	if opts.Interval <= 0 {
		return
	}
	s.taskWG.Add(1)
	go func() {
		defer s.taskWG.Done()
		s.watchdogLoop(s.taskBaseContext(), opts)
	}()
}

func (s *Server) watchdogLoop(ctx context.Context, opts watchdogOptions) {
	if opts.Interval <= 0 {
		return
	}
	select {
	case <-ctx.Done():
		return
	default:
	}
	s.runWatchdogChecks(opts)
	ticker := time.NewTicker(opts.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runWatchdogChecks(opts)
		}
	}
}

func (s *Server) runWatchdogChecks(opts watchdogOptions) {
	s.checkMihomoLogSize(opts.MihomoLogMaxBytes)
}

func (s *Server) checkMihomoLogSize(maxBytes int64) {
	if s == nil || s.state == nil || maxBytes <= 0 {
		return
	}
	runtimeDir := strings.TrimSpace(s.state.Paths.MihomoRuntimeDir)
	if runtimeDir == "" {
		return
	}
	logPath := filepath.Join(runtimeDir, watchdogMihomoLogDefaultName)
	info, err := os.Lstat(logPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			s.appendWatchdogEvent(map[string]any{
				"event":  "watchdog_mihomo_log",
				"check":  "mihomo_log_size",
				"action": "stat_error",
				"path":   logPath,
				"error":  err.Error(),
			})
		}
		return
	}
	if !info.Mode().IsRegular() {
		s.appendWatchdogEvent(map[string]any{
			"event": "watchdog_mihomo_log",
			"check": "mihomo_log_size",
			"skip":  "non_regular_file",
			"path":  logPath,
			"mode":  info.Mode().String(),
		})
		return
	}
	size := info.Size()
	if size <= maxBytes {
		return
	}
	event := map[string]any{
		"event":     "watchdog_mihomo_log",
		"check":     "mihomo_log_size",
		"action":    "truncate",
		"path":      logPath,
		"old_size":  size,
		"max_bytes": maxBytes,
	}
	if err := os.Truncate(logPath, 0); err != nil {
		event["result"] = "error"
		event["error"] = err.Error()
	} else {
		event["result"] = "ok"
	}
	s.appendWatchdogEvent(event)
}

func (s *Server) appendWatchdogEvent(fields map[string]any) {
	if fields == nil {
		return
	}
	fields["ts"] = time.Now().UTC().Format(time.RFC3339Nano)
	appendBoundedJSONLog(s.serviceLogPath(watchdogLogName), fields, serviceLogMaxBytes())
}
