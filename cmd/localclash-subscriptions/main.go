package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"localclash/internal/poolcontract"
	"localclash/internal/poolprepare"
	"localclash/internal/runtimesupervision"
	"localclash/internal/subscriptions"
)

const usage = `Usage: localclash-subscriptions <command> --workspace DIR

  set     --input FILE|-
          Save sources from a JSON document with version 1 and a uris array.
          Use - to read from stdin.
  status  Show configured sources and merged artifact metadata.
  refresh Fetch and merge subscriptions into subscription.gob.
  qualify --mihomo PATH [--pool auto|chatgpt|both] [--concurrency N] [--force]
          Qualify the merged revision using an isolated Mihomo process.
          The default pool selection is both. Use --force to recheck a prepared revision.

Successful commands write JSON to stdout. Refresh stage timings go to stderr.
`

type output struct {
	OK         bool  `json:"ok"`
	DurationMS int64 `json:"duration_ms"`
	Result     any   `json:"result"`
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	start := time.Now()
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		_, err := fmt.Fprint(stdout, usage)
		return err
	}
	command := args[0]
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	workspace := flags.String("workspace", "", "subscription workspace")
	var inputPath, corePath, pool string
	var concurrency int
	var force bool
	switch command {
	case "set":
		flags.StringVar(&inputPath, "input", "", "JSON file or - for stdin")
	case "status", "refresh":
	case "qualify":
		flags.StringVar(&corePath, "mihomo", "", "Mihomo executable")
		flags.StringVar(&pool, "pool", "both", "auto, chatgpt, or both")
		flags.IntVar(&concurrency, "concurrency", poolprepare.DefaultConcurrency, "parallel probes per pool")
		flags.BoolVar(&force, "force", false, "recheck the prepared revision")
	default:
		return fmt.Errorf("unknown command %q; see --help", command)
	}
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_, err = fmt.Fprint(stdout, usage)
			return err
		}
		// Flag parser errors may include the supplied value.
		return fmt.Errorf("invalid %s options; see --help", command)
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments; see --help")
	}
	if *workspace == "" {
		return fmt.Errorf("--workspace is required")
	}
	root, err := filepath.Abs(*workspace)
	if err != nil {
		return err
	}
	runtime := filepath.Join(root, ".runtime")
	paths := subscriptions.StatusOptions{
		ConfigPath: filepath.Join(root, "localclash-subscriptions.json"),
		MergedPath: filepath.Join(root, "subscription.gob"),
		RuntimeDir: filepath.Join(runtime, "subscriptions"),
	}
	var result any
	switch command {
	case "set":
		input, err := readInput(inputPath, stdin)
		if err != nil {
			return err
		}
		err = runtimesupervision.WithLock(runtime, func() error {
			if err := ctx.Err(); err != nil {
				return err
			}
			configured, err := subscriptions.Configure(subscriptions.ConfigureOptions{
				ConfigPath: paths.ConfigPath, URIs: input.URIs, URLs: input.URLs,
			})
			configured.Message = "Subscription sources configured. Run refresh to update local artifacts."
			result = configured
			return err
		})
	case "status":
		result, err = subscriptions.Status(paths)
	case "refresh":
		progress := json.NewEncoder(stderr)
		err = runtimesupervision.WithLock(runtime, func() error {
			if err := ctx.Err(); err != nil {
				return err
			}
			refreshed, err := subscriptions.Refresh(ctx, subscriptions.RefreshOptions{
				ConfigPath: paths.ConfigPath, MergedPath: paths.MergedPath, RuntimeDir: paths.RuntimeDir,
				Force: true,
				OnStage: func(event subscriptions.StageEvent) {
					_ = progress.Encode(subscriptions.StageEvent{
						Stage: event.Stage, Event: event.Event, DurationMS: event.DurationMS,
					})
				},
			})
			result = refreshed
			return err
		})
	case "qualify":
		if concurrency < 1 {
			return fmt.Errorf("--concurrency must be positive")
		}
		if corePath == "" {
			return fmt.Errorf("--mihomo is required for qualify")
		}
		core, err := filepath.Abs(corePath)
		if err != nil {
			return err
		}
		qualified, err := qualify(ctx, paths.MergedPath, pool, poolprepare.Options{
			CorePath: core, RuntimeDir: filepath.Join(runtime, "mihomo"), Concurrency: concurrency, Force: force,
		})
		if err != nil {
			return err
		}
		result = qualified
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(output{OK: true, DurationMS: time.Since(start).Milliseconds(), Result: result})
}

type sourceInput struct {
	Version int      `json:"version"`
	URIs    []string `json:"uris"`
	URLs    []string `json:"urls"`
}

func readInput(path string, stdin io.Reader) (sourceInput, error) {
	var input sourceInput
	switch path {
	case "":
		return input, fmt.Errorf("--input is required for set")
	case "-":
	default:
		file, err := os.Open(path)
		if err != nil {
			return input, err
		}
		defer file.Close()
		stdin = file
	}
	decoder := json.NewDecoder(stdin)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return input, fmt.Errorf("invalid subscription input JSON")
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		return input, fmt.Errorf("subscription input must contain one JSON document")
	}
	if input.Version != 1 {
		return input, fmt.Errorf("subscription input version must be 1")
	}
	return input, nil
}

type qualification struct {
	Revision    string       `json:"revision"`
	Proxies     int          `json:"proxies"`
	Concurrency int          `json:"concurrency"`
	Pools       []poolResult `json:"pools"`
}

type poolResult struct {
	Profile                string `json:"profile"`
	Provider               string `json:"provider"`
	Reused                 bool   `json:"reused"`
	Candidates             int    `json:"candidates,omitempty"`
	Probed                 int    `json:"probed,omitempty"`
	UnprobedCount          int    `json:"unprobed_count,omitempty"`
	CanceledCount          int    `json:"canceled_count,omitempty"`
	ObservedQualifiedCount int    `json:"observed_qualified_count,omitempty"`
	QualifiedCount         int    `json:"qualified_count,omitempty"`
	DurationMS             int64  `json:"duration_ms,omitempty"`
}

func qualify(ctx context.Context, mergedPath, selected string, opts poolprepare.Options) (qualification, error) {
	var capabilities []string
	switch selected {
	case "auto":
		capabilities = []string{poolcontract.AutoCapability}
	case "chatgpt":
		capabilities = []string{poolcontract.ChatGPTCapability}
	case "both":
		capabilities = []string{poolcontract.AutoCapability, poolcontract.ChatGPTCapability}
	default:
		return qualification{}, fmt.Errorf("--pool must be auto, chatgpt, or both")
	}
	doc, err := subscriptions.ReadMerged(mergedPath)
	if errors.Is(err, os.ErrNotExist) {
		return qualification{}, fmt.Errorf("merged subscription is missing; run refresh first")
	}
	if err != nil {
		return qualification{}, err
	}
	input, err := poolcontract.FromSubscription(doc)
	if err != nil {
		return qualification{}, err
	}
	pools := make([]poolcontract.Pool, 0, len(capabilities))
	for _, profile := range capabilities {
		pool, _ := poolcontract.Lookup(profile)
		pools = append(pools, pool)
	}
	results, err := poolprepare.Prepare(ctx, input, pools, opts)
	if err != nil {
		return qualification{}, err
	}
	report := qualification{Revision: input.Revision, Proxies: len(input.Proxies), Concurrency: opts.Concurrency}
	for _, pool := range pools {
		entry := poolResult{
			Profile: pool.Capability, Provider: filepath.Join(opts.RuntimeDir, pool.Path(input.Revision)), Reused: true,
		}
		for _, result := range results {
			if result.Profile == pool.Capability {
				entry = poolResult{
					Profile: result.Profile, Provider: result.SnapshotPath, Candidates: result.Candidates,
					Probed: result.Probed, UnprobedCount: result.UnprobedCount, CanceledCount: result.CanceledCount,
					ObservedQualifiedCount: result.ObservedQualifiedCount,
					QualifiedCount:         result.QualifiedCount, DurationMS: result.DurationMS,
				}
				break
			}
		}
		report.Pools = append(report.Pools, entry)
	}
	return report, nil
}
