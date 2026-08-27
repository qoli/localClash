package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"localclash/internal/appinit"
	"localclash/internal/configrender"
	"localclash/internal/coredownload"
	"localclash/internal/corerun"
	"localclash/internal/dashboard"
	"localclash/internal/doctor"
	"localclash/internal/mcp"
	"localclash/internal/mihomotest"
	"localclash/internal/reset"
	"localclash/internal/rules"
	"localclash/internal/subdownload"
)

const usage = `localclash

Usage:
  localclash status --json
  localclash subscription status --json
  localclash subscription get --json
  localclash subscription set --input subscriptions.json --json
  localclash subscription refresh --json
  localclash component status --json
  localclash component update assets --json
  localclash component update mihomo --json
  localclash component update dashboard --json
  localclash config status --json
  localclash config apply-template --input config-request.json --json
  localclash config render --json
  localclash mihomo config-test --json
  localclash mihomo config-promote --json
  localclash runtime status --json
  localclash runtime facts --json
  localclash runtime start --json
  localclash runtime restart --json
  localclash runtime stop --json
  localclash apply --input desired-state.json --json
  localclash reset [--full] [--dry-run] [--workspace <path>] --json
  localclash mcp serve [flags]

Advanced/internal commands:
  localclash core download [flags]
  localclash subscription download --url <url> [flags]
  localclash dashboard download [flags]
  localclash rules adapt [flags]
  localclash rules index-dump [flags]
  localclash rules render [flags]
  localclash run [flags]
  localclash doctor [flags]
  localclash mcp [flags]
  localclash reset [flags]

Flags for core download:
  --version string   GitHub release tag, or "latest" (default "latest")
  --flavor string    core flavor: all, meta, or smart (default "all")
  --target string    download target: host or router (default "host")
  --os string        target OS (default current OS for host, linux for router)
  --arch string      target arch (default current arch)
  --output string    output binary path for a single flavor
  --output-dir string
                     output directory for default flavor paths (default "bin")
  --repo string      Meta core GitHub repo owner/name (default "MetaCubeX/mihomo")
  --smart-branch string
                     OpenClash core branch for smart core downloads (default "master")
  --force           overwrite output if it exists
  --dry-run         print selected release asset without downloading

Flags for subscription download:
  --url string          subscription URL
  --output string       output file path (default subscription.gob)
  --user-agent string   subscription User-Agent (default "clash-verge/v1.5.1")
  --force              overwrite output if it exists

Flags for dashboard download:
  --version string   zashboard GitHub release tag, or "latest" (default "latest")
  --asset string     zashboard release asset name (default "dist.zip")
  --output string    output directory (default ".runtime/mihomo/ui/zashboard")
  --repo string      GitHub repo owner/name (default "Zephyruso/zashboard")
  --force           replace output directory if it exists

Flags for config render:
  --source string   downloaded subscription gob (default "subscription.gob")
  --output string   generated Mihomo config path (default ".runtime/mihomo/config.yaml")
  --packs-selection string
                 packs selection gob; optional
  --rules-cache string
                 runtime pack cache directory (default ".runtime/rules/packs")
  --runtime-profile string
                 runtime profile JSON (default "localclash-runtime.json")
  --force          overwrite output if it exists

Flags for rules adapt:
  --sources string  rule source JSON directory (default "rule-sources")
  --cache string    runtime pack cache directory (default ".runtime/rules/packs")

Flags for rules index-dump:
  --cache string    runtime pack cache directory (default ".runtime/rules/packs")
  --format string   dump format: json or yaml (default "json")
  --output string   output path, or "-" for stdout (default "-")

Flags for rules render:
  --selection string  packs selection gob (default "localclash-packs.gob")
  --subscription string
                    subscription gob for node label classification (default "subscription.gob")
  --cache string      runtime pack cache directory (default ".runtime/rules/packs")
  --output string     output rules fragment path, or "-" for stdout (default "-")

Flags for run:
  --core string     Mihomo core binary path (default from active runtime profile)
  --config string   Mihomo runtime config path (default ".runtime/mihomo/config.yaml")
  --workdir string  Mihomo runtime data directory (default ".runtime/mihomo")
  --log string      Mihomo log file path (default "<workdir>/logs/mihomo-YYYY-MM-DD.log")
  --log-retention int
                 days of dated Mihomo logs to keep (default 7)

Flags for status:
  --config string   Mihomo runtime config path (default ".runtime/mihomo/config.yaml")
  --workdir string  Mihomo runtime data directory (default ".runtime/mihomo")
  --log string      Mihomo log file path (default "<workdir>/mihomo.log")
  --json            print machine-readable JSON status

Flags for stop:
  --workdir string       Mihomo runtime data directory (default ".runtime/mihomo")
  --timeout duration     stop timeout before reporting failure (default 5s)
  --force                send SIGKILL if SIGTERM does not stop before timeout
  --json                 print machine-readable JSON result

Flags for restart:
  --core string          Mihomo core binary path (default from active runtime profile)
  --config string        Mihomo runtime config path (default ".runtime/mihomo/config.yaml")
  --workdir string       Mihomo runtime data directory (default ".runtime/mihomo")
  --log string           Mihomo log file path (default "<workdir>/mihomo.log")
  --strategy string      restart strategy: process_restart or hot_reload (default process_restart)
  --config-sha256 string expected config sha256 for hot_reload
  --attestation string   passing mihomo config-test attestation for hot_reload
  --timeout duration     stop timeout before reporting failure (default 5s)
  --reload-timeout duration
                         hot reload API timeout (default 10s)
  --force                send SIGKILL if SIGTERM does not stop before timeout
  --json                 print machine-readable JSON result

Flags for doctor:
  --core string          Mihomo core binary path (default from active runtime profile)
  --subscription string  downloaded subscription gob (default "subscription.gob")
  --config string        generated Mihomo config path (default ".runtime/mihomo/config.yaml")
  --dashboard string     zashboard directory (default ".runtime/mihomo/ui/zashboard")
  --workdir string       Mihomo runtime data directory for config test (default ".runtime/mihomo")
  --json                print machine-readable JSON report

Flags for mcp:
  --addr string   HTTP listen address (default "127.0.0.1:8765")
  --path string   MCP HTTP JSON-RPC path (default "/mcp")

Flags for reset:
  --full      delete the entire localClash workspace directory
  --workspace string
              explicit localClash workspace path for destructive reset
  --dry-run   print the factory reset plan without deleting files
  --yes       skip interactive confirmation
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return nil
	}
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help" || args[0] == "help") {
		fmt.Print(usage)
		return nil
	}
	if len(args) >= 1 && args[0] == "reset" && !hasFlag(args[1:], "json") {
		return runReset(args[1:])
	}
	bootstrapCtx, bootstrapCancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer bootstrapCancel()
	state := appinit.Bootstrap(bootstrapCtx, bootstrapOptionsForArgs(args))
	if handled, err := runProductCommand(args, state); handled {
		return err
	}
	if len(args) >= 2 && args[0] == "core" && args[1] == "download" {
		return runCoreDownload(args[2:])
	}
	if len(args) >= 2 && (args[0] == "subscription" || args[0] == "sub") && args[1] == "download" {
		return runSubscriptionDownload(args[2:])
	}
	if len(args) >= 2 && args[0] == "dashboard" && args[1] == "download" {
		return runDashboardDownload(args[2:])
	}
	if len(args) >= 2 && args[0] == "config" && args[1] == "render" {
		return runConfigRender(args[2:], state)
	}
	if len(args) >= 2 && args[0] == "mihomo" && args[1] == "config-test" {
		return runMihomoConfigTest(args[2:], state)
	}
	if len(args) >= 2 && args[0] == "mihomo" && args[1] == "config-promote" {
		return runMihomoConfigPromote(args[2:], state)
	}
	if len(args) >= 1 && args[0] == "rules" {
		return runRules(args[1:], state)
	}
	if len(args) >= 1 && args[0] == "run" {
		return runCore(args[1:], state)
	}
	if len(args) >= 1 && args[0] == "status" {
		return runStatus(args[1:], state)
	}
	if len(args) >= 1 && args[0] == "stop" {
		return runStop(args[1:], state)
	}
	if len(args) >= 1 && args[0] == "restart" {
		return runRestart(args[1:], state)
	}
	if len(args) >= 1 && args[0] == "doctor" {
		return runDoctor(args[1:], state)
	}
	if len(args) >= 1 && args[0] == "mcp" {
		return runMCP(args[1:], state)
	}
	return fmt.Errorf("unknown command %q\n\n%s", args[0], usage)
}

func bootstrapOptionsForArgs(args []string) appinit.Options {
	return appinit.Options{}
}

func runReset(args []string) error {
	fs := flag.NewFlagSet("reset", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	opts := reset.Options{}
	fs.BoolVar(&opts.DryRun, "dry-run", false, "print the factory reset plan without deleting files")
	fs.BoolVar(&opts.Yes, "yes", false, "skip interactive confirmation")
	fs.BoolVar(&opts.Full, "full", false, "delete the entire localClash workspace directory")
	fs.StringVar(&opts.Workspace, "workspace", "", "explicit localClash workspace path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", fs.Args())
	}
	if opts.Workspace != "" {
		opts.WorkspaceSource = "flag:--workspace"
	}
	_, err := reset.Run(opts)
	return err
}

func runCoreDownload(args []string) error {
	fs := flag.NewFlagSet("core download", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	opts := coredownload.Options{}
	fs.StringVar(&opts.Version, "version", "latest", "GitHub release tag, or latest")
	fs.StringVar(&opts.Flavor, "flavor", coredownload.FlavorAll, "core flavor: all, meta, or smart")
	fs.StringVar(&opts.Target, "target", coredownload.TargetHost, "download target: host or router")
	fs.StringVar(&opts.TargetOS, "os", "", "target OS")
	fs.StringVar(&opts.TargetArch, "arch", runtime.GOARCH, "target arch")
	fs.StringVar(&opts.OutputPath, "output", "", "output binary path")
	fs.StringVar(&opts.OutputDir, "output-dir", "bin", "output directory for default flavor paths")
	fs.StringVar(&opts.Repo, "repo", "MetaCubeX/mihomo", "GitHub repo owner/name")
	fs.StringVar(&opts.SmartBranch, "smart-branch", "master", "OpenClash core branch for smart core downloads")
	fs.BoolVar(&opts.Force, "force", false, "overwrite output if it exists")
	fs.BoolVar(&opts.DryRun, "dry-run", false, "print selected release asset without downloading")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", fs.Args())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	results, err := coredownload.Download(ctx, opts)
	if err != nil {
		return err
	}

	if opts.DryRun {
		for _, result := range results {
			fmt.Printf("target: %s\nflavor: %s\nrelease: %s\nasset: %s\nurl: %s\noutput: %s\n", result.Target, result.Flavor, result.Version, result.AssetName, result.DownloadURL, result.OutputPath)
		}
		return nil
	}

	for _, result := range results {
		fmt.Printf("downloaded %s core %s (%s) to %s\n", result.Flavor, result.AssetName, result.Version, result.OutputPath)
	}
	return nil
}

func runSubscriptionDownload(args []string) error {
	fs := flag.NewFlagSet("subscription download", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	opts := subdownload.Options{}
	fs.StringVar(&opts.URL, "url", "", "subscription URL")
	fs.StringVar(&opts.OutputPath, "output", "subscription.gob", "output file path")
	fs.StringVar(&opts.UserAgent, "user-agent", "clash-verge/v1.5.1", "subscription User-Agent")
	fs.BoolVar(&opts.Force, "force", false, "overwrite output if it exists")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", fs.Args())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	result, err := subdownload.Download(ctx, opts)
	if err != nil {
		return err
	}

	fmt.Printf("downloaded subscription to %s (%d bytes)\n", result.OutputPath, result.BytesWritten)
	return nil
}

func runDashboardDownload(args []string) error {
	fs := flag.NewFlagSet("dashboard download", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	opts := dashboard.Options{}
	fs.StringVar(&opts.Version, "version", "latest", "zashboard GitHub release tag, or latest")
	fs.StringVar(&opts.AssetName, "asset", "dist.zip", "zashboard release asset name")
	fs.StringVar(&opts.OutputDir, "output", ".runtime/mihomo/ui/zashboard", "output directory")
	fs.StringVar(&opts.Repo, "repo", "Zephyruso/zashboard", "GitHub repo owner/name")
	fs.BoolVar(&opts.Force, "force", false, "replace output directory if it exists")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", fs.Args())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	result, err := dashboard.Download(ctx, opts)
	if err != nil {
		return err
	}

	fmt.Printf("downloaded %s (%s) to %s\n", result.AssetName, result.Version, result.OutputDir)
	return nil
}

func runConfigRender(args []string, state appinit.RuntimeState) error {
	fs := flag.NewFlagSet("config render", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	opts := configrender.Options{}
	fs.StringVar(&opts.SourcePath, "source", state.Paths.SubscriptionPath, "downloaded subscription gob")
	fs.StringVar(&opts.OutputPath, "output", state.Paths.GeneratedConfig, "generated Mihomo config path")
	fs.StringVar(&opts.PacksSelectionPath, "packs-selection", "", "packs selection gob; optional")
	fs.StringVar(&opts.RulesCacheDir, "rules-cache", state.Paths.RulesCacheDir, "runtime pack cache directory")
	fs.StringVar(&opts.RuntimeProfilePath, "runtime-profile", state.Paths.RuntimeProfilePath, "runtime profile JSON")
	fs.BoolVar(&opts.Force, "force", false, "overwrite output if it exists")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", fs.Args())
	}
	if opts.OutputPath == state.Paths.GeneratedConfig && state.Config.Rendered {
		opts.Force = true
	}

	result, err := configrender.Render(opts)
	if err != nil {
		return err
	}

	fmt.Printf("rendered runtime/%s core/%s config to %s (%d proxies, %d rules)\n", result.RuntimeMode, result.Core, result.OutputPath, result.ProxyCount, result.RuleCount)
	return nil
}

func runMihomoConfigTest(args []string, state appinit.RuntimeState) error {
	fs := flag.NewFlagSet("mihomo config-test", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var asJSON bool
	var record bool
	var timeout time.Duration
	opts := mihomotest.TestOptions{}
	fs.StringVar(&opts.CorePath, "core", state.Paths.CorePath, "Mihomo core binary path")
	fs.StringVar(&opts.ConfigPath, "config", state.Paths.GeneratedConfig, "Mihomo config path")
	fs.StringVar(&opts.WorkDir, "workdir", state.Paths.MihomoRuntimeDir, "Mihomo runtime data directory")
	fs.StringVar(&opts.AttestationPath, "attestation", "", "passing config test attestation path")
	fs.StringVar(&opts.PromotedConfigPath, "promoted-config", "", "promoted config path to record in the attestation")
	fs.BoolVar(&record, "record", true, "write passing config test attestation")
	fs.DurationVar(&timeout, "timeout", 90*time.Second, "config test timeout")
	fs.BoolVar(&asJSON, "json", false, "print machine-readable JSON result")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", fs.Args())
	}
	opts.Record = record
	opts.Timeout = timeout
	opts.CachePath = mihomotest.DefaultCachePath(opts.WorkDir)
	ctx, cancel := context.WithTimeout(context.Background(), timeout+5*time.Second)
	defer cancel()
	result, err := mihomotest.Test(ctx, opts)
	if asJSON {
		if jsonErr := printJSON(result); jsonErr != nil {
			return jsonErr
		}
	} else if result.Passed {
		fmt.Printf("mihomo config test passed for %s sha256=%s\n", result.ConfigPath, result.ConfigSHA256)
	} else {
		fmt.Printf("mihomo config test failed for %s\n%s\n", result.ConfigPath, result.Output)
	}
	return err
}

func runMihomoConfigPromote(args []string, state appinit.RuntimeState) error {
	fs := flag.NewFlagSet("mihomo config-promote", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var candidate string
	var promoted string
	var attestation string
	var asJSON bool
	fs.StringVar(&candidate, "candidate", filepath.Join(state.Paths.MihomoRuntimeDir, "config.candidate.yaml"), "candidate config path")
	fs.StringVar(&promoted, "config", state.Paths.GeneratedConfig, "promoted config path")
	fs.StringVar(&attestation, "attestation", mihomotest.DefaultAttestationPath(state.Paths.MihomoRuntimeDir), "passing config test attestation path")
	fs.BoolVar(&asJSON, "json", false, "print machine-readable JSON result")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", fs.Args())
	}
	result, err := mihomotest.PromoteConfig(candidate, promoted, attestation)
	if asJSON {
		if jsonErr := printJSON(result); jsonErr != nil {
			return jsonErr
		}
	} else if err == nil {
		fmt.Printf("promoted mihomo config to %s sha256=%s\n", result.Promoted, result.ConfigSHA256)
	}
	return err
}

func runRules(args []string, state appinit.RuntimeState) error {
	if len(args) == 0 {
		return fmt.Errorf("rules subcommand is required: adapt, index-dump, or render")
	}
	switch args[0] {
	case "adapt":
		return runRulesAdapt(args[1:], state)
	case "index-dump":
		return runRulesIndexDump(args[1:], state)
	case "render":
		return runRulesRender(args[1:], state)
	default:
		return fmt.Errorf("unknown rules subcommand %q", args[0])
	}
}

func runRulesAdapt(args []string, state appinit.RuntimeState) error {
	fs := flag.NewFlagSet("rules adapt", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	opts := rules.Options{}
	fs.StringVar(&opts.SourcesDir, "sources", state.Paths.RuleSourcesDir, "rule source JSON directory")
	fs.StringVar(&opts.CacheDir, "cache", state.Paths.RulesCacheDir, "runtime pack cache directory")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", fs.Args())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	caches, err := rules.Adapt(ctx, opts)
	if err != nil {
		return err
	}
	for _, cache := range caches {
		fmt.Printf("adapted %s (%s): %d packs\n", cache.Source, cache.Adapter, len(cache.Packs))
	}
	return nil
}

func runRulesIndexDump(args []string, state appinit.RuntimeState) error {
	fs := flag.NewFlagSet("rules index-dump", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	cacheDir := state.Paths.RulesCacheDir
	format := "json"
	output := "-"
	fs.StringVar(&cacheDir, "cache", cacheDir, "runtime pack cache directory")
	fs.StringVar(&format, "format", format, "dump format: json or yaml")
	fs.StringVar(&output, "output", output, "output path, or - for stdout")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", fs.Args())
	}

	data, err := rules.DumpPackIndex(rules.PackIndexPath(cacheDir), format)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if output == "" || output == "-" {
		_, err = os.Stdout.Write(data)
		return err
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return err
	}
	return os.WriteFile(output, data, 0o644)
}

func runRulesRender(args []string, state appinit.RuntimeState) error {
	fs := flag.NewFlagSet("rules render", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	opts := rules.Options{}
	defaultSelection := state.Paths.PacksSelectionPath
	if defaultSelection == "" {
		defaultSelection = productWorkspacePath(state, "localclash-packs.gob")
	}
	fs.StringVar(&opts.SelectionPath, "selection", defaultSelection, "packs selection gob")
	fs.StringVar(&opts.Subscription, "subscription", state.Paths.SubscriptionPath, "subscription gob for node label classification")
	fs.StringVar(&opts.CacheDir, "cache", state.Paths.RulesCacheDir, "runtime pack cache directory")
	fs.StringVar(&opts.OutputPath, "output", "-", "output rules fragment path, or - for stdout")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", fs.Args())
	}

	fragment, err := rules.Render(opts)
	if err != nil {
		return err
	}
	return rules.WriteFragment(opts.OutputPath, fragment)
}

func runCore(args []string, state appinit.RuntimeState) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	opts := corerun.Options{}
	fs.StringVar(&opts.CorePath, "core", state.Paths.CorePath, "Mihomo core binary path")
	fs.StringVar(&opts.ConfigPath, "config", state.Paths.GeneratedConfig, "Mihomo runtime config path")
	fs.StringVar(&opts.WorkDir, "workdir", state.Paths.MihomoRuntimeDir, "Mihomo runtime data directory")
	fs.StringVar(&opts.LogPath, "log", "", "Mihomo log file path")
	fs.IntVar(&opts.LogRetentionDays, "log-retention", 7, "days of dated Mihomo logs to keep")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", fs.Args())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	report, err := doctor.Run(ctx, doctor.Options{
		CorePath:         opts.CorePath,
		SubscriptionPath: state.Paths.SubscriptionPath,
		ConfigPath:       opts.ConfigPath,
		DashboardDir:     filepath.Join(state.Paths.MihomoRuntimeDir, "ui", "zashboard"),
		WorkDir:          opts.WorkDir,
	})
	if err != nil {
		return err
	}
	if !doctorReportOK(report) {
		return fmt.Errorf("doctor checks failed; run localclash doctor for details")
	}
	return corerun.Run(opts)
}

func runStatus(args []string, state appinit.RuntimeState) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var opts corerun.StatusOptions
	var asJSON bool
	fs.StringVar(&opts.CorePath, "core", state.Paths.CorePath, "Mihomo core binary path")
	fs.StringVar(&opts.ConfigPath, "config", state.Paths.GeneratedConfig, "Mihomo runtime config path")
	fs.StringVar(&opts.WorkDir, "workdir", state.Paths.MihomoRuntimeDir, "Mihomo runtime data directory")
	fs.StringVar(&opts.LogPath, "log", "", "Mihomo log file path")
	fs.BoolVar(&asJSON, "json", false, "print machine-readable JSON status")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", fs.Args())
	}

	result := corerun.Status(opts)
	if asJSON {
		return printJSON(result)
	}
	printRuntimeStatus(result)
	return nil
}

func runStop(args []string, state appinit.RuntimeState) error {
	fs := flag.NewFlagSet("stop", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	opts := corerun.StopOptions{}
	var asJSON bool
	fs.StringVar(&opts.WorkDir, "workdir", state.Paths.MihomoRuntimeDir, "Mihomo runtime data directory")
	fs.DurationVar(&opts.Timeout, "timeout", 5*time.Second, "stop timeout before reporting failure")
	fs.BoolVar(&opts.ForceKill, "force", false, "send SIGKILL if SIGTERM does not stop before timeout")
	fs.BoolVar(&asJSON, "json", false, "print machine-readable JSON result")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", fs.Args())
	}

	result, err := corerun.Stop(opts)
	if err != nil {
		return err
	}
	if asJSON {
		if err := printJSON(result); err != nil {
			return err
		}
	} else {
		printRuntimeStop(result)
	}
	if result.Error != "" {
		return errors.New(result.Error)
	}
	return nil
}

func runRestart(args []string, state appinit.RuntimeState) error {
	fs := flag.NewFlagSet("restart", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	opts := corerun.RestartOptions{}
	var asJSON bool
	fs.StringVar(&opts.CorePath, "core", state.Paths.CorePath, "Mihomo core binary path")
	fs.StringVar(&opts.ConfigPath, "config", state.Paths.GeneratedConfig, "Mihomo runtime config path")
	fs.StringVar(&opts.WorkDir, "workdir", state.Paths.MihomoRuntimeDir, "Mihomo runtime data directory")
	fs.StringVar(&opts.LogPath, "log", "", "Mihomo log file path")
	fs.StringVar(&opts.Strategy, "strategy", "", "restart strategy: process_restart or hot_reload")
	fs.StringVar(&opts.ConfigSHA256, "config-sha256", "", "expected config sha256 for hot_reload")
	fs.StringVar(&opts.AttestationPath, "attestation", "", "mihomo config test attestation for hot_reload")
	fs.DurationVar(&opts.StopTimeout, "timeout", 5*time.Second, "stop timeout before reporting failure")
	fs.DurationVar(&opts.ReloadTimeout, "reload-timeout", 10*time.Second, "hot reload API timeout")
	fs.BoolVar(&opts.ForceKill, "force", false, "send SIGKILL if the runtime does not exit before timeout")
	fs.BoolVar(&asJSON, "json", false, "print machine-readable JSON result")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", fs.Args())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	result, err := corerun.Restart(ctx, opts)
	if err != nil {
		return err
	}
	if asJSON {
		if err := printJSON(result); err != nil {
			return err
		}
	} else {
		printRuntimeRestart(result)
	}
	if result.Error != "" {
		return errors.New(result.Error)
	}
	return nil
}

func runDoctor(args []string, state appinit.RuntimeState) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	opts := doctor.Options{}
	fs.StringVar(&opts.CorePath, "core", state.Paths.CorePath, "Mihomo core binary path")
	fs.StringVar(&opts.SubscriptionPath, "subscription", state.Paths.SubscriptionPath, "downloaded subscription gob")
	fs.StringVar(&opts.ConfigPath, "config", state.Paths.GeneratedConfig, "generated Mihomo config path")
	fs.StringVar(&opts.DashboardDir, "dashboard", filepath.Join(state.Paths.MihomoRuntimeDir, "ui", "zashboard"), "zashboard directory")
	fs.StringVar(&opts.WorkDir, "workdir", state.Paths.MihomoRuntimeDir, "Mihomo runtime data directory for config test")
	fs.BoolVar(&opts.JSON, "json", false, "print machine-readable JSON report")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", fs.Args())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	report, err := doctor.Run(ctx, opts)
	if err != nil {
		return err
	}
	if opts.JSON {
		return doctor.PrintJSON(report)
	}
	doctor.PrintText(report)
	return nil
}

func runMCP(args []string, state appinit.RuntimeState) error {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	opts := mcp.HTTPOptions{}
	fs.StringVar(&opts.Addr, "addr", "127.0.0.1:8765", "HTTP listen address")
	fs.StringVar(&opts.Path, "path", "/mcp", "MCP HTTP JSON-RPC path")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", fs.Args())
	}
	opts = mcp.NormalizeHTTPOptions(opts)
	fmt.Fprintf(os.Stderr, "localClash MCP HTTP listening on %s\n", mcp.HTTPURL(opts))
	fmt.Fprintln(os.Stderr, "health check: http://"+opts.Addr+"/health")
	return mcp.ListenAndServeHTTPWithState(context.Background(), state, opts)
}

func doctorReportOK(report doctor.Report) bool {
	return report.Status != "fail"
}

func printJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func printRuntimeStatus(result corerun.StatusResult) {
	if result.Running {
		fmt.Printf("mihomo runtime running (pid %d)\n", result.PID)
	} else {
		fmt.Println("mihomo runtime not running")
	}
	fmt.Printf("runtime dir: %s\n", result.RuntimeDir)
	if len(result.PIDs) > 0 {
		fmt.Printf("managed pids: %v\n", result.PIDs)
	}
	if len(result.ProcessNames) > 0 {
		fmt.Printf("process names: %v\n", result.ProcessNames)
	}
	fmt.Printf("config: %s\n", result.Config)
	fmt.Printf("log: %s\n", result.LogFile)
	if result.ExternalController != "" {
		fmt.Printf("external controller: %s\n", result.ExternalController)
	}
	if result.ExternalUIURL != "" {
		fmt.Printf("external ui: %s\n", result.ExternalUIURL)
	}
}

func printRuntimeStop(result corerun.StopResult) {
	switch {
	case result.Stopped:
		fmt.Printf("stopped mihomo runtime pid %d with %s\n", result.PID, result.Signal)
	case result.WasRunning:
		fmt.Printf("mihomo runtime pid %d did not stop\n", result.PID)
	default:
		fmt.Println("mihomo runtime was not running")
	}
	fmt.Printf("runtime dir: %s\n", result.RuntimeDir)
	if len(result.PIDs) > 0 {
		fmt.Printf("managed pids: %v\n", result.PIDs)
	}
	if len(result.ProcessNames) > 0 {
		fmt.Printf("process names: %v\n", result.ProcessNames)
	}
	if result.Forced {
		fmt.Println("forced: true")
	}
	if result.Error != "" {
		fmt.Printf("error: %s\n", result.Error)
	}
}

func printRuntimeRestart(result corerun.RestartResult) {
	printRuntimeStop(result.Stop)
	if result.Start.Started {
		fmt.Printf("started mihomo runtime pid %d\n", result.Start.PID)
	} else if result.Start.AlreadyRunning {
		fmt.Printf("mihomo runtime already running pid %d\n", result.Start.PID)
	} else {
		fmt.Println("mihomo runtime was not started")
	}
	if result.Start.RuntimeDir != "" {
		fmt.Printf("runtime dir: %s\n", result.Start.RuntimeDir)
	}
	if result.Start.Config != "" {
		fmt.Printf("config: %s\n", result.Start.Config)
	}
	if result.Start.LogFile != "" {
		fmt.Printf("log: %s\n", result.Start.LogFile)
	}
	if result.Error != "" {
		fmt.Printf("error: %s\n", result.Error)
	}
}
