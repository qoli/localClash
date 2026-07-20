package mcp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"localclash/internal/corerun"
	"localclash/internal/runtimesupervision"
)

const (
	defaultWatchdogInterval        = time.Hour
	defaultRuntimeWatchdogInterval = 5 * time.Second
	defaultMihomoLogMaxBytes       = int64(10 * 1024 * 1024)
	watchdogMihomoLogDefaultName   = "mihomo.log"
)

type watchdogOptions struct {
	Interval             time.Duration
	RuntimeInterval      time.Duration
	RuntimeProbeTimeout  time.Duration
	RuntimeHealthTimeout time.Duration
	MihomoLogMaxBytes    int64
}

func defaultWatchdogOptions() watchdogOptions {
	return watchdogOptions{
		Interval:             taskMonitorDurationEnv("LOCALCLASH_WATCHDOG_INTERVAL_MS", defaultWatchdogInterval),
		RuntimeInterval:      taskMonitorDurationEnv("LOCALCLASH_RUNTIME_WATCHDOG_INTERVAL_MS", defaultRuntimeWatchdogInterval),
		RuntimeProbeTimeout:  time.Second,
		RuntimeHealthTimeout: 20 * time.Second,
		MihomoLogMaxBytes:    int64(envInt("LOCALCLASH_MIHOMO_LOG_MAX_BYTES", int(defaultMihomoLogMaxBytes))),
	}
}

func (s *Server) startWatchdog() {
	if s == nil || s.state == nil {
		return
	}
	opts := defaultWatchdogOptions()
	if opts.Interval <= 0 && opts.RuntimeInterval <= 0 {
		return
	}
	s.taskWG.Add(1)
	go func() {
		defer s.taskWG.Done()
		s.watchdogLoop(s.taskBaseContext(), opts)
	}()
}

func (s *Server) watchdogLoop(ctx context.Context, opts watchdogOptions) {
	if opts.Interval <= 0 && opts.RuntimeInterval <= 0 {
		return
	}
	select {
	case <-ctx.Done():
		return
	default:
	}
	var logTicker *time.Ticker
	var logTicks <-chan time.Time
	if opts.Interval > 0 {
		s.runWatchdogChecks(opts)
		logTicker = time.NewTicker(opts.Interval)
		logTicks = logTicker.C
		defer logTicker.Stop()
	}
	var runtimeTicker *time.Ticker
	var runtimeTicks <-chan time.Time
	if opts.RuntimeInterval > 0 {
		s.runRuntimeWatchdogCheck(ctx, opts)
		runtimeTicker = time.NewTicker(opts.RuntimeInterval)
		runtimeTicks = runtimeTicker.C
		defer runtimeTicker.Stop()
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-logTicks:
			s.runWatchdogChecks(opts)
		case <-runtimeTicks:
			s.runRuntimeWatchdogCheck(ctx, opts)
		}
	}
}

func (s *Server) runWatchdogChecks(opts watchdogOptions) {
	s.checkMihomoLogSize(opts.MihomoLogMaxBytes)
}

func (s *Server) runRuntimeWatchdogCheck(ctx context.Context, opts watchdogOptions) {
	if s == nil || s.state == nil {
		return
	}
	runtimeDir := strings.TrimSpace(s.state.Paths.MihomoRuntimeDir)
	if runtimeDir == "" {
		return
	}
	_, err := corerun.CheckSupervision(ctx, corerun.SupervisionCheckOptions{
		WorkDir:       runtimeDir,
		ProbeTimeout:  opts.RuntimeProbeTimeout,
		HealthTimeout: opts.RuntimeHealthTimeout,
	})
	if err == nil {
		return
	}
	s.appendWatchdogEventThrottled("supervision_check_error:"+err.Error(), time.Minute, map[string]any{
		"event":  "runtime_supervision_blocked",
		"reason": "supervision_check_error",
		"error":  err.Error(),
	})
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
	if s == nil || s.state == nil || fields == nil {
		return
	}
	_ = runtimesupervision.AppendEvent(s.state.Paths.MihomoRuntimeDir, fields)
}

func (s *Server) appendWatchdogEventThrottled(key string, interval time.Duration, fields map[string]any) {
	now := time.Now()
	s.watchdogNoticeMu.Lock()
	if s.watchdogNotices == nil {
		s.watchdogNotices = map[string]time.Time{}
	}
	last := s.watchdogNotices[key]
	if !last.IsZero() && now.Before(last.Add(interval)) {
		s.watchdogNoticeMu.Unlock()
		return
	}
	s.watchdogNotices[key] = now
	s.watchdogNoticeMu.Unlock()
	s.appendWatchdogEvent(fields)
}
