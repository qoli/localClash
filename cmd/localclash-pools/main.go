package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata"

	"localclash/internal/capability"
	"localclash/internal/poolcontract"
	"localclash/internal/poolprepare"
	"localclash/internal/runtimesupervision"

	"gopkg.in/yaml.v3"
)

const defaultPeriod = "15m"

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:]); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: localclash-pools prepare|refresh|schedule|features")
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	switch args[0] {
	case "features":
		if len(args) != 1 {
			return errors.New("usage: localclash-pools features")
		}
		return json.NewEncoder(os.Stdout).Encode(struct {
			EnhancedFilter bool `json:"enhanced_filter"`
		}{EnhancedFilter: true})
	case "prepare":
		core := flags.String("core", "", "Mihomo executable")
		workDir := flags.String("runtime", "", "Mihomo runtime directory")
		poolIDs := flags.String("pools", "", "comma-separated capability IDs")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if *core == "" || !filepath.IsAbs(*workDir) || *poolIDs == "" || flags.NArg() != 0 {
			return errors.New("prepare requires --core, --runtime and --pools")
		}
		var input poolcontract.Input
		if err := yaml.NewDecoder(os.Stdin).Decode(&input); err != nil {
			return fmt.Errorf("read pool input: %w", err)
		}
		canonical, err := poolcontract.NewInput(input.Proxies)
		if err != nil {
			return err
		}
		if input.Version != canonical.Version || input.Revision != canonical.Revision {
			return errors.New("pool input version or revision mismatch")
		}
		var pools []poolcontract.Pool
		for _, id := range strings.Split(*poolIDs, ",") {
			pool, ok := poolcontract.Lookup(id)
			if !ok {
				return fmt.Errorf("unsupported pool %q", id)
			}
			pools = append(pools, pool)
		}
		results, err := poolprepare.Prepare(ctx, input, pools, poolprepare.Options{
			CorePath: *core, RuntimeDir: *workDir,
		})
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(results)
	case "refresh", "schedule":
		workDir := flags.String("runtime", "", "Mihomo runtime directory")
		period := flags.String("period", defaultPeriod, "5m, 15m, 30m, 1h or daily-08")
		enhancedFilter := flags.Bool("enhanced-filter", false, "require SMTP, Ubuntu, TMDB and Microsoft connectivity for the automatic pool")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if !filepath.IsAbs(*workDir) || flags.NArg() != 0 {
			return errors.New("an absolute --runtime directory is required")
		}
		if args[0] == "schedule" {
			return schedule(ctx, *workDir, *period, *enhancedFilter)
		}
		results, err := refresh(ctx, *workDir, *enhancedFilter)
		return errors.Join(json.NewEncoder(os.Stdout).Encode(results), err)
	default:
		return fmt.Errorf("unknown localclash-pools command %q", args[0])
	}
}

func rebuild(ctx context.Context, input poolcontract.Input, pools []poolcontract.Pool, core, workDir string, live *activeRuntime, enhancedFilter bool) ([]capability.Result, error) {
	parent := filepath.Join(workDir, poolcontract.Directory)
	var results []capability.Result
	err := runtimesupervision.WithLock(parent, func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		stage, err := os.MkdirTemp(filepath.Dir(workDir), ".pool-probe-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(stage)
		opts := poolprepare.Options{CorePath: core, RuntimeDir: workDir, ProbeDir: stage, EnhancedFilter: enhancedFilter}
		var failures error
		for _, pool := range pools {
			path := filepath.Join(workDir, pool.Path(input.Revision))
			result, err := poolprepare.Qualify(ctx, pool, input, opts)
			if err == nil {
				err = publish(ctx, live, pool, path, input, result.Qualified)
			}
			if err != nil {
				failures = errors.Join(failures, fmt.Errorf("%s: %w", pool.Provider, err))
				continue
			}
			result.SnapshotPath = path
			results = append(results, result)
		}
		return failures
	})
	return results, err
}

func nextRun(now time.Time, period string) (time.Time, error) {
	switch period {
	case "5m", "15m", "30m", "1h":
		interval, _ := time.ParseDuration(period)
		return now.Add(interval), nil
	case "daily-08":
		today := time.Date(now.Year(), now.Month(), now.Day(), 8, 0, 0, 0, now.Location())
		if !today.After(now) {
			today = time.Date(now.Year(), now.Month(), now.Day()+1, 8, 0, 0, 0, now.Location())
		}
		return today, nil
	default:
		return time.Time{}, fmt.Errorf("invalid pool update period %q", period)
	}
}

type scheduleStatus struct {
	EnhancedFilter bool                    `json:"enhanced_filter"`
	AutoQuality    *capability.AutoQuality `json:"auto_quality,omitempty"`
	DurationMS     int64                   `json:"duration_ms,omitempty"`
	NextRunAt      *time.Time              `json:"next_run_at,omitempty"`
	LastRunAt      *time.Time              `json:"last_run_at,omitempty"`
	Qualified      map[string]int          `json:"qualified,omitempty"`
	Error          string                  `json:"error,omitempty"`
}

func schedule(ctx context.Context, workDir, period string, enhancedFilter bool) error {
	due, err := nextRun(time.Now(), period)
	if err != nil {
		return err
	}
	dir := filepath.Join(workDir, poolcontract.Directory)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	statusPath := filepath.Join(dir, "schedule.json")
	var status scheduleStatus
	if data, err := os.ReadFile(statusPath); err == nil {
		if err := json.Unmarshal(data, &status); err != nil {
			fmt.Fprintln(os.Stderr, "decode previous pool schedule status:", err)
			status = scheduleStatus{}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for {
		status.EnhancedFilter = enhancedFilter
		status.NextRunAt = &due
		if err := writeScheduleStatus(statusPath, status); err != nil {
			return err
		}
		timer := time.NewTimer(time.Until(due))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		status.NextRunAt = nil
		if err := writeScheduleStatus(statusPath, status); err != nil {
			return err
		}
		started := time.Now()
		results, err := refresh(ctx, workDir, enhancedFilter)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		finished := time.Now()
		status.DurationMS = finished.Sub(started).Milliseconds()
		status.AutoQuality = nil
		status.LastRunAt = &finished
		status.Error = ""
		if err != nil {
			status.Error = err.Error()
			fmt.Fprintln(os.Stderr, "refresh qualified pools:", err)
		}
		status.Qualified = make(map[string]int, len(results))
		for _, result := range results {
			status.Qualified[result.Profile] = result.QualifiedCount
			if result.Profile == poolcontract.AutoCapability {
				status.AutoQuality = result.AutoQuality
			}
		}
		due, _ = nextRun(due, period)
		for !due.After(finished) {
			due, _ = nextRun(due, period)
		}
	}
}

func writeScheduleStatus(path string, status scheduleStatus) error {
	data, err := json.Marshal(status)
	if err != nil {
		return err
	}
	return poolcontract.WriteFile(path, data)
}
