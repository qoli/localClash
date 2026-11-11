// Package poolprepare qualifies and writes providers for a subscription revision.
package poolprepare

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"localclash/internal/capability"
	"localclash/internal/poolcontract"
	"localclash/internal/runtimesupervision"
)

const DefaultConcurrency = 16

type Options struct {
	CorePath    string
	RuntimeDir  string
	Concurrency int
	Force       bool
}

// Prepare publishes each successfully qualified pool and returns its result.
// An existing provider for the revision is reused unless Force is set.
func Prepare(ctx context.Context, input poolcontract.Input, pools []poolcontract.Pool, opts Options) ([]capability.Result, error) {
	parent := filepath.Join(opts.RuntimeDir, poolcontract.Directory)
	var results []capability.Result
	err := runtimesupervision.WithLock(parent, func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		pending := make([]poolPreparation, 0, len(pools))
		for _, pool := range pools {
			path := filepath.Join(opts.RuntimeDir, pool.Path(input.Revision))
			if !opts.Force {
				ready, err := poolcontract.PreparedProvider(pool, path)
				if ready {
					continue
				}
				if err != nil && !errors.Is(err, os.ErrNotExist) {
					return err
				}
			}
			pending = append(pending, poolPreparation{pool: pool, path: path})
		}
		if len(pending) == 0 {
			return nil
		}
		stage, err := os.MkdirTemp(filepath.Dir(opts.RuntimeDir), ".pool-probe-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(stage)
		probeOpts := opts
		probeOpts.RuntimeDir = stage
		qualifyPreparations(pending, func(pool poolcontract.Pool) (capability.Result, error) {
			return Qualify(ctx, pool, input, probeOpts)
		})
		var failures error
		for _, item := range pending {
			result, err := item.result, item.err
			if err == nil {
				err = writeProvider(item, input)
			}
			if err != nil {
				failures = errors.Join(failures, fmt.Errorf("%s: %w", item.pool.Provider, err))
				continue
			}
			result.SnapshotPath = item.path
			results = append(results, result)
		}
		return failures
	})
	return results, err
}

func writeProvider(item poolPreparation, input poolcontract.Input) error {
	data, _, err := poolcontract.ProviderData(item.pool, input, item.result.Qualified)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(item.path), 0o700); err != nil {
		return err
	}
	return poolcontract.WriteFile(item.path, data)
}

type poolPreparation struct {
	pool   poolcontract.Pool
	path   string
	result capability.Result
	err    error
}

func qualifyPreparations(pending []poolPreparation, probe func(poolcontract.Pool) (capability.Result, error)) {
	var jobs sync.WaitGroup
	for index := range pending {
		item := &pending[index]
		jobs.Add(1)
		go func() {
			defer jobs.Done()
			item.result, item.err = probe(item.pool)
		}()
	}
	jobs.Wait()
}
