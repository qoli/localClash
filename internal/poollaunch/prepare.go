// Package poollaunch starts qualification in the independent pool worker.
package poollaunch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"localclash/internal/capability"
	"localclash/internal/localconfig"
	"localclash/internal/poolcontract"

	"gopkg.in/yaml.v3"
)

type Options struct {
	Workspace string
	Core      string
	Runtime   string
}

func Prepare(ctx context.Context, opts Options, cfg localconfig.Config, doc map[string]any) ([]capability.Result, error) {
	profiles := make([]string, 0, len(cfg.ProxyGroups))
	for _, group := range cfg.ProxyGroups {
		profiles = append(profiles, group.Capability)
	}
	return PrepareProfiles(ctx, opts, profiles, doc)
}

func PrepareProfiles(ctx context.Context, opts Options, profiles []string, doc map[string]any) ([]capability.Result, error) {
	profiles = capability.Profiles(profiles)
	if len(profiles) == 0 {
		return nil, nil
	}
	for _, profile := range profiles {
		if _, ok := poolcontract.Lookup(profile); !ok {
			return nil, fmt.Errorf("unsupported proxy-group capability: %s", profile)
		}
	}
	input, err := poolcontract.FromSubscription(doc)
	if err != nil {
		return nil, err
	}
	data, err := yaml.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("encode pool worker input: %w", err)
	}
	binary, err := filepath.Abs(poolcontract.WorkerPath(opts.Workspace))
	if err != nil {
		return nil, err
	}
	if opts.Runtime == "" {
		return nil, fmt.Errorf("pool worker runtime directory is required")
	}
	runtimeDir, err := filepath.Abs(opts.Runtime)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, binary, "prepare", "--core", opts.Core, "--runtime", runtimeDir, "--pools", strings.Join(profiles, ","))
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 20 * time.Second
	cmd.Stdin = bytes.NewReader(data)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if message := strings.TrimSpace(stderr.String()); message != "" {
			return nil, fmt.Errorf("prepare qualified pools: %w: %s", err, message)
		}
		return nil, fmt.Errorf("start pool worker from %s (install matching localClash base assets): %w", binary, err)
	}
	var results []capability.Result
	if err := json.Unmarshal(output, &results); err != nil {
		return nil, fmt.Errorf("read pool worker result: %w", err)
	}
	return results, nil
}
