package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"localclash/internal/appinit"
	"localclash/internal/baseassets"
	"localclash/internal/chatgptavailable"
	"localclash/internal/configinspect"
	"localclash/internal/configpatch"
	"localclash/internal/configrender"
	"localclash/internal/coredownload"
	"localclash/internal/corerun"
	"localclash/internal/dashboard"
	"localclash/internal/localconfig"
	"localclash/internal/policytemplate"
	"localclash/internal/reset"
	"localclash/internal/routertakeover"
	"localclash/internal/runtimeprofile"
	"localclash/internal/subscriptions"
	"localclash/internal/workspace"
)

type productEnvelope struct {
	OK          bool     `json:"ok"`
	Changed     bool     `json:"changed"`
	Summary     string   `json:"summary"`
	Status      any      `json:"status,omitempty"`
	Changes     []string `json:"changes"`
	Warnings    []string `json:"warnings"`
	NextActions []string `json:"next_actions"`
}

type productErrorEnvelope struct {
	OK          bool     `json:"ok"`
	Code        string   `json:"code"`
	Message     string   `json:"message"`
	Details     any      `json:"details,omitempty"`
	NextActions []string `json:"next_actions"`
}

type codedProductError struct {
	code        string
	message     string
	details     any
	nextActions []string
}

var downloadCore = coredownload.Download
var rebuildProductChatGPT = chatgptavailable.RebuildWithMihomo

func (err codedProductError) Error() string {
	return err.message
}

func runProductCommand(args []string, state appinit.RuntimeState) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	var err error
	switch args[0] {
	case "status":
		if hasFlag(args[1:], "json") {
			err = runProductStatus(args[1:], state)
		}
	case "subscription":
		if len(args) >= 2 && args[1] != "download" {
			err = runProductSubscription(args[1:], state)
		}
	case "component":
		err = runProductComponent(args[1:], state)
	case "config":
		if len(args) >= 2 && (args[1] == "status" || args[1] == "apply-template" || (args[1] == "render" && hasFlag(args[2:], "json"))) {
			err = runProductConfig(args[1:], state)
		}
	case "runtime":
		err = runProductRuntime(args[1:], state)
	case "takeover":
		err = runProductTakeover(args[1:], state)
	case "apply":
		err = runProductApply(args[1:], state)
	case "reset":
		if hasFlag(args[1:], "json") {
			err = runProductReset(args[1:], state)
		}
	case "mcp":
		if len(args) >= 2 && args[1] == "serve" {
			err = runMCP(args[2:], state)
		}
	}
	if err == nil {
		return productCommandWasHandled(args), nil
	}
	if productCommandWasHandled(args) {
		_ = printProductError(err)
		return true, err
	}
	return false, nil
}

func productCommandWasHandled(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "component", "runtime", "takeover", "apply":
		return true
	case "status":
		return hasFlag(args[1:], "json")
	case "subscription":
		return len(args) >= 2 && args[1] != "download"
	case "config":
		return len(args) >= 2 && (args[1] == "status" || args[1] == "apply-template" || (args[1] == "render" && hasFlag(args[2:], "json")))
	case "reset":
		return hasFlag(args[1:], "json")
	case "mcp":
		return len(args) >= 2 && args[1] == "serve"
	default:
		return false
	}
}

func runProductStatus(args []string, state appinit.RuntimeState) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var asJSON bool
	fs.BoolVar(&asJSON, "json", false, "print product JSON status")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !asJSON || fs.NArg() != 0 {
		return fmt.Errorf("usage: localclash status --json")
	}
	status, warnings := productStatus(state)
	return printProductOK(productEnvelope{
		OK:       true,
		Changed:  false,
		Summary:  "localClash product status read.",
		Status:   status,
		Warnings: warnings,
		Changes:  []string{},
	})
}

func runProductSubscription(args []string, state appinit.RuntimeState) error {
	if len(args) == 0 {
		return fmt.Errorf("subscription subcommand is required: status, get, set, or refresh")
	}
	switch args[0] {
	case "status":
		if err := parseJSONOnly("subscription status", args[1:]); err != nil {
			return err
		}
		result, err := subscriptions.Status(subscriptionStatusOptions(state))
		if err != nil {
			return err
		}
		return printProductOK(productEnvelope{OK: true, Summary: "Subscription status read.", Status: result, Changes: []string{}, Warnings: []string{}})
	case "get":
		if err := parseJSONOnly("subscription get", args[1:]); err != nil {
			return err
		}
		result, err := subscriptions.Get(subscriptionStatusOptions(state))
		if err != nil {
			return err
		}
		return printProductOK(productEnvelope{OK: true, Summary: "Subscription sources read.", Status: result, Changes: []string{}, Warnings: []string{}})
	case "set":
		input, err := parseSubscriptionInput(args[1:])
		if err != nil {
			return err
		}
		uris := subscriptionInputURIs(input)
		if _, err := sourcesFromURIs(uris); err != nil {
			return err
		}
		replace := true
		result, err := subscriptions.Configure(subscriptions.ConfigureOptions{
			ConfigPath: state.Paths.SubscriptionConfig,
			URIs:       uris,
			Replace:    &replace,
		})
		if err != nil {
			return err
		}
		return printProductOK(productEnvelope{OK: true, Changed: true, Summary: "Subscription sources configured.", Status: result, Changes: []string{"subscription_sources_replaced"}, Warnings: []string{}})
	case "refresh":
		if err := parseJSONOnly("subscription refresh", args[1:]); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		result, err := subscriptions.Refresh(ctx, subscriptions.RefreshOptions{
			ConfigPath: state.Paths.SubscriptionConfig,
			RuntimeDir: state.Paths.SubscriptionRuntime,
			MergedPath: state.Paths.SubscriptionPath,
			Force:      true,
			UserAgent:  subscriptions.DefaultUserAgent,
			OnStage:    productSubscriptionStageLogger(os.Stderr),
		})
		if err != nil {
			return err
		}
		capabilities, err := refreshProductCapabilities(ctx, state, result.MergedDoc, os.Stderr)
		if err != nil {
			return err
		}
		status := productSubscriptionRefreshStatus{RefreshResult: result, Capabilities: capabilities}
		return printProductOK(productEnvelope{OK: true, Changed: true, Summary: "Subscription artifacts and configured capabilities refreshed.", Status: status, Changes: []string{"subscriptions_refreshed"}, Warnings: result.Warnings})
	default:
		return fmt.Errorf("unknown subscription subcommand %q", args[0])
	}
}

type productSubscriptionRefreshStatus struct {
	subscriptions.RefreshResult
	Capabilities []chatgptavailable.Result `json:"capabilities,omitempty"`
}

func refreshProductCapabilities(ctx context.Context, state appinit.RuntimeState, subscriptionDoc map[string]any, logOutput io.Writer) ([]chatgptavailable.Result, error) {
	configPath := productWorkspacePath(state, "localclash-intent.json")
	config, err := localconfig.Load(configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []chatgptavailable.Result{}, nil
		}
		return nil, err
	}
	profiles := configuredProductCapabilityProfiles(config)
	if len(profiles) == 0 {
		return []chatgptavailable.Result{}, nil
	}
	if len(profiles) != 1 || profiles[0] != chatgptavailable.ProfileID {
		if len(profiles) == 1 && profiles[0] == chatgptavailable.LegacyProfileID {
			return nil, fmt.Errorf("legacy ChatGPT capability %q is no longer supported; refresh the localclash-default policy-template patches before subscription refresh", chatgptavailable.LegacyProfileID)
		}
		return nil, fmt.Errorf("unsupported proxy-group capabilities: %s", strings.Join(profiles, ", "))
	}
	proxies, err := productSubscriptionProxyMaps(subscriptionDoc)
	if err != nil {
		return nil, err
	}
	capabilityRoot := productCapabilityRoot(state)
	snapshotPath := filepath.Join(capabilityRoot, "chatgpt-available.json")
	started := time.Now()
	writeProductCapabilityStage(logOutput, "started", started, nil, map[string]any{
		"profile":     chatgptavailable.ProfileID,
		"proxy_count": len(proxies),
	})
	result, err := rebuildProductChatGPT(ctx, proxies, normalizeCorePathForState(state, state.Paths.CorePath), capabilityRoot, snapshotPath)
	fields := map[string]any{"profile": chatgptavailable.ProfileID, "proxy_count": len(proxies)}
	if err == nil {
		fields["candidates"] = result.Candidates
		fields["probed"] = result.Probed
		fields["qualified"] = result.QualifiedCount
		fields["observed_qualified"] = result.ObservedQualifiedCount
		fields["retained"] = result.RetainedCount
		fields["unavailable"] = result.UnavailableCount
	}
	writeProductCapabilityStage(logOutput, "done", started, err, fields)
	if err != nil {
		return nil, fmt.Errorf("refresh ChatGPT capability: %w", err)
	}
	return []chatgptavailable.Result{result}, nil
}

func writeProductCapabilityStage(w io.Writer, event string, started time.Time, stageErr error, fields map[string]any) {
	if w == nil {
		return
	}
	record := map[string]any{
		"ts":          time.Now().UTC().Format(time.RFC3339Nano),
		"component":   "capability_refresh",
		"stage":       "qualify_chatgpt",
		"event":       event,
		"duration_ms": time.Since(started).Milliseconds(),
	}
	if stageErr != nil {
		record["event"] = "error"
		record["error"] = stageErr.Error()
	}
	for key, value := range fields {
		record[key] = value
	}
	data, err := json.Marshal(record)
	if err == nil {
		_, _ = fmt.Fprintln(w, string(data))
	}
}

func configuredProductCapabilityProfiles(config localconfig.Config) []string {
	profiles := make([]string, 0, len(config.ProxyGroups))
	for _, group := range config.ProxyGroups {
		profiles = append(profiles, group.Capability)
	}
	return chatgptavailable.Profiles(profiles)
}

func productSubscriptionProxyMaps(doc map[string]any) ([]map[string]any, error) {
	if doc == nil {
		return nil, errors.New("merged subscription document is required for capability qualification")
	}
	rawProxies, ok := doc["proxies"].([]any)
	if !ok || len(rawProxies) == 0 {
		return nil, errors.New("merged subscription document has no proxies for capability qualification")
	}
	proxies := make([]map[string]any, 0, len(rawProxies))
	for index, raw := range rawProxies {
		proxy, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("merged subscription proxy %d is invalid for capability qualification", index)
		}
		proxies = append(proxies, proxy)
	}
	return proxies, nil
}

func productCapabilityRoot(state appinit.RuntimeState) string {
	if root := strings.TrimSpace(state.Paths.RuntimeRoot); root != "" {
		return filepath.Join(root, "capabilities")
	}
	return productWorkspacePath(state, filepath.Join(".runtime", "capabilities"))
}

func productSubscriptionStageLogger(w io.Writer) func(subscriptions.StageEvent) {
	var mu sync.Mutex
	return func(event subscriptions.StageEvent) {
		record := map[string]any{
			"ts":          time.Now().UTC().Format(time.RFC3339Nano),
			"component":   "subscription_refresh",
			"stage":       event.Stage,
			"event":       event.Event,
			"duration_ms": event.DurationMS,
		}
		if event.Error != "" {
			record["error"] = event.Error
		}
		for key, value := range event.Fields {
			record[key] = value
		}
		data, err := json.Marshal(record)
		if err != nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		_, _ = fmt.Fprintln(w, string(data))
	}
}

func runProductComponent(args []string, state appinit.RuntimeState) error {
	if len(args) == 0 {
		return fmt.Errorf("component subcommand is required: status or update")
	}
	switch args[0] {
	case "status":
		if err := parseJSONOnly("component status", args[1:]); err != nil {
			return err
		}
		return printProductOK(productEnvelope{OK: true, Summary: "Component status read.", Status: componentStatus(state), Changes: []string{}, Warnings: []string{}})
	case "update":
		return runProductComponentUpdate(args[1:], state)
	default:
		return fmt.Errorf("unknown component subcommand %q", args[0])
	}
}

func runProductComponentUpdate(args []string, state appinit.RuntimeState) error {
	if len(args) == 0 {
		return fmt.Errorf("component update requires component name")
	}
	component := args[0]
	if err := parseJSONOnly("component update "+component, args[1:]); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	switch component {
	case "localclash":
		return codedProductError{
			code:        "localclash_component_update_helper_owned",
			message:     "localClash core install/update is owned by the LuCI helper when the core is missing; Go self-update is not implemented yet.",
			nextActions: []string{"Use the LuCI helper bootstrap_core method to install or update /usr/local/bin/localclash."},
		}
	case "assets", "base-assets":
		result, err := baseassets.Install(ctx, baseassets.Options{
			OutputDir: productWorkspaceRoot(state),
			Force:     true,
		})
		if err != nil {
			return err
		}
		return printProductOK(productEnvelope{OK: true, Changed: true, Summary: "Base assets updated.", Status: result, Changes: []string{"base_assets_updated"}, Warnings: []string{}})
	case "mihomo":
		result, err := downloadCore(ctx, coredownload.Options{
			Version:    "latest",
			Flavor:     coredownload.FlavorAll,
			Target:     coredownload.TargetRouter,
			TargetOS:   "linux",
			TargetArch: runtime.GOARCH,
			OutputDir:  productWorkspacePath(state, "bin"),
			Repo:       "MetaCubeX/mihomo",
			Force:      true,
		})
		if err != nil {
			return err
		}
		warnings := refreshCoreVersionCacheWarnings(ctx, state, "")
		return printProductOK(productEnvelope{OK: true, Changed: true, Summary: "Mihomo components updated.", Status: result, Changes: []string{"mihomo_updated"}, Warnings: warnings})
	case "dashboard":
		result, err := dashboard.Download(ctx, dashboard.Options{
			Version:   "latest",
			AssetName: "dist.zip",
			OutputDir: filepath.Join(state.Paths.MihomoRuntimeDir, "ui", "zashboard"),
			Repo:      "Zephyruso/zashboard",
			Force:     true,
		})
		if err != nil {
			return err
		}
		return printProductOK(productEnvelope{OK: true, Changed: true, Summary: "Dashboard assets updated.", Status: result, Changes: []string{"dashboard_updated"}, Warnings: []string{}})
	default:
		return fmt.Errorf("unknown component %q", component)
	}
}

func runProductConfig(args []string, state appinit.RuntimeState) error {
	if len(args) == 0 {
		return fmt.Errorf("config subcommand is required: status, apply-template, or render")
	}
	switch args[0] {
	case "status":
		if err := parseJSONOnly("config status", args[1:]); err != nil {
			return err
		}
		if _, err := runtimeprofile.StatusFor(state.Paths.RuntimeProfilePath); err != nil {
			return err
		}
		status, warnings := configStatus(state)
		return printProductOK(productEnvelope{OK: true, Summary: "Config status read.", Status: status, Changes: []string{}, Warnings: warnings})
	case "apply-template":
		input, err := parseConfigInput(args[1:])
		if err != nil {
			return err
		}
		result, warnings, err := applyTemplateInput(context.Background(), input, state)
		if err != nil {
			return err
		}
		return printProductOK(productEnvelope{OK: true, Changed: true, Summary: "Config template applied.", Status: result, Changes: []string{"config_template_applied"}, Warnings: warnings})
	case "render":
		if err := parseJSONOnly("config render", args[1:]); err != nil {
			return err
		}
		result, warnings, err := renderProductConfig(state)
		if err != nil {
			return err
		}
		return printProductOK(productEnvelope{OK: true, Changed: true, Summary: "Generated Mihomo config rendered.", Status: result, Changes: []string{"config_rendered"}, Warnings: warnings})
	default:
		return fmt.Errorf("unknown config subcommand %q", args[0])
	}
}

func runProductRuntime(args []string, state appinit.RuntimeState) error {
	if len(args) == 0 {
		return fmt.Errorf("runtime subcommand is required: status, start, restart, or stop")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	switch args[0] {
	case "status":
		if err := parseJSONOnly("runtime status", args[1:]); err != nil {
			return err
		}
		result := corerun.Status(runtimeStatusOptions(state))
		return printProductOK(productEnvelope{OK: true, Summary: "Runtime status read.", Status: result, Changes: []string{}, Warnings: []string{}})
	case "start":
		if err := parseJSONOnly("runtime start", args[1:]); err != nil {
			return err
		}
		result, err := corerun.Start(ctx, runtimeStartOptions(state))
		if err != nil {
			return err
		}
		warnings := append([]string{}, result.Warnings...)
		warnings = append(warnings, refreshCoreVersionCacheWarnings(ctx, state, "")...)
		return printProductOK(productEnvelope{OK: true, Changed: result.Started, Summary: "Runtime start completed.", Status: result, Changes: changedIf(result.Started, "runtime_started"), Warnings: warnings, NextActions: result.NextActions})
	case "restart":
		input, err := parseRuntimeRestartInput(args[1:])
		if err != nil {
			return err
		}
		opts := runtimeRestartOptions(state)
		opts.Strategy = input.Strategy
		opts.ConfigSHA256 = input.ConfigSHA256
		opts.AttestationPath = input.AttestationPath
		result, err := corerun.Restart(ctx, opts)
		if err != nil {
			return err
		}
		warnings := append([]string{}, result.Warnings...)
		warnings = append(warnings, refreshCoreVersionCacheWarnings(ctx, state, "")...)
		return printProductOK(productEnvelope{OK: true, Changed: result.Restarted, Summary: "Runtime restart completed.", Status: result, Changes: changedIf(result.Restarted, "runtime_restarted"), Warnings: warnings, NextActions: result.NextActions})
	case "stop":
		if err := parseJSONOnly("runtime stop", args[1:]); err != nil {
			return err
		}
		result, err := corerun.Stop(corerun.StopOptions{WorkDir: state.Paths.MihomoRuntimeDir, Timeout: 5 * time.Second})
		if err != nil {
			return err
		}
		return printProductOK(productEnvelope{OK: true, Changed: result.Stopped, Summary: "Runtime stop completed.", Status: result, Changes: changedIf(result.Stopped, "runtime_stopped"), Warnings: result.Warnings, NextActions: result.NextActions})
	default:
		return fmt.Errorf("unknown runtime subcommand %q", args[0])
	}
}

type runtimeRestartInput struct {
	Strategy        string
	ConfigSHA256    string
	AttestationPath string
}

func parseRuntimeRestartInput(args []string) (runtimeRestartInput, error) {
	fs := flag.NewFlagSet("runtime restart", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var input runtimeRestartInput
	asJSON := fs.Bool("json", false, "print product JSON response")
	fs.StringVar(&input.Strategy, "strategy", "", "restart strategy: process_restart or hot_reload")
	fs.StringVar(&input.ConfigSHA256, "config-sha256", "", "expected config sha256 for hot_reload")
	fs.StringVar(&input.AttestationPath, "attestation", "", "mihomo config test attestation for hot_reload")
	if err := fs.Parse(args); err != nil {
		return input, err
	}
	if !*asJSON || fs.NArg() != 0 {
		return input, fmt.Errorf("usage: localclash runtime restart [--strategy process_restart|hot_reload] [--config-sha256 <sha256>] [--attestation <file>] --json")
	}
	switch strings.TrimSpace(input.Strategy) {
	case "", corerun.RestartStrategyProcess, corerun.RestartStrategyHotReload:
		input.Strategy = strings.TrimSpace(input.Strategy)
	default:
		return input, fmt.Errorf("runtime restart: invalid strategy %q", input.Strategy)
	}
	return input, nil
}

func runProductTakeover(args []string, state appinit.RuntimeState) error {
	if len(args) == 0 {
		return fmt.Errorf("takeover subcommand is required: status, apply, or stop")
	}
	if err := parseJSONOnly("takeover "+args[0], args[1:]); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	opts := routerTakeoverOptions(state)
	switch args[0] {
	case "status":
		result, err := routertakeover.Status(ctx, opts)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return codedProductError{
					code:    "router_takeover_status_timeout",
					message: "router takeover status observation timed out; actual takeover state is unknown",
					details: map[string]any{"timeout_seconds": int(routertakeover.StatusObservationTimeout / time.Second)},
					nextActions: []string{
						"retry router takeover status after router load decreases",
						"inspect router logs and nft command latency if the timeout repeats",
					},
				}
			}
			return err
		}
		return printProductOK(productEnvelope{OK: true, Summary: "Router takeover status read.", Status: result, Changes: []string{}, Warnings: result.Warnings, NextActions: result.NextActions})
	case "apply":
		result, err := routertakeover.Apply(ctx, opts)
		if err != nil {
			return err
		}
		if result.Error != "" || !result.Applied {
			message := strings.TrimSpace(result.Error)
			if message == "" {
				message = "router takeover apply did not complete"
			}
			return codedProductError{code: "router_takeover_apply_failed", message: message, details: result, nextActions: result.NextActions}
		}
		return printProductOK(productEnvelope{OK: true, Changed: result.Applied, Summary: "Router takeover apply completed.", Status: result, Changes: changedIf(result.Applied, "takeover_applied"), Warnings: result.Warnings, NextActions: result.NextActions})
	case "stop":
		result, err := routertakeover.Stop(ctx, opts)
		if err != nil {
			return err
		}
		if result.Error != "" || !result.Stopped {
			message := strings.TrimSpace(result.Error)
			if message == "" {
				message = "router takeover stop did not complete"
			}
			return codedProductError{code: "router_takeover_stop_failed", message: message, details: result, nextActions: result.NextActions}
		}
		return printProductOK(productEnvelope{OK: true, Changed: result.Stopped, Summary: "Router takeover stop completed.", Status: result, Changes: changedIf(result.Stopped, "takeover_stopped"), Warnings: result.Warnings, NextActions: result.NextActions})
	default:
		return fmt.Errorf("unknown takeover subcommand %q", args[0])
	}
}

type desiredStateInput struct {
	Version       int                   `json:"version"`
	Mode          string                `json:"mode"`
	Subscriptions *desiredSubscriptions `json:"subscriptions,omitempty"`
	Components    *desiredComponents    `json:"components,omitempty"`
	Config        *desiredConfig        `json:"config,omitempty"`
	Runtime       *desiredRuntime       `json:"runtime,omitempty"`
}

type desiredSubscriptions struct {
	URIs    []string `json:"uris,omitempty"`
	URLs    []string `json:"urls,omitempty"`
	Refresh *bool    `json:"refresh,omitempty"`
}

type desiredComponents struct {
	LocalClash string `json:"localclash,omitempty"`
	Mihomo     string `json:"mihomo,omitempty"`
	Dashboard  string `json:"dashboard,omitempty"`
}

type desiredConfig struct {
	Template               string `json:"template,omitempty"`
	RuntimeProfile         string `json:"runtime_profile,omitempty"`
	Core                   string `json:"core,omitempty"`
	AllowOverwriteModified *bool  `json:"allow_overwrite_modified,omitempty"`
}

type desiredRuntime struct {
	Service        string `json:"service,omitempty"`
	RouterTakeover string `json:"router_takeover,omitempty"`
}

func runProductApply(args []string, state appinit.RuntimeState) error {
	var input desiredStateInput
	if err := parseInputJSON("apply", args, &input); err != nil {
		return err
	}
	if err := validateDesiredState(input); err != nil {
		return err
	}
	if input.Mode == "preview" {
		status, warnings := productStatus(state)
		return printProductOK(productEnvelope{
			OK:          true,
			Changed:     false,
			Summary:     "Apply preview generated. No runtime state was changed.",
			Status:      map[string]any{"desired": input, "current": status},
			Changes:     desiredChanges(input),
			Warnings:    warnings,
			NextActions: []string{"Run apply with mode=execute after user confirmation."},
		})
	}
	changes, warnings, err := executeDesiredState(input, state)
	if err != nil {
		return err
	}
	status, statusWarnings := productStatus(state)
	warnings = append(warnings, statusWarnings...)
	return printProductOK(productEnvelope{
		OK:       true,
		Changed:  len(changes) > 0,
		Summary:  "Desired state applied.",
		Status:   status,
		Changes:  changes,
		Warnings: warnings,
	})
}

func validateDesiredState(input desiredStateInput) error {
	if input.Version != 1 {
		return fmt.Errorf("version must be 1")
	}
	if input.Mode != "preview" && input.Mode != "execute" {
		return fmt.Errorf("mode must be preview or execute")
	}
	if input.Components != nil {
		if err := validateLeaveOrInstalled("components.localclash", input.Components.LocalClash); err != nil {
			return err
		}
		if err := validateLeaveOrInstalled("components.mihomo", input.Components.Mihomo); err != nil {
			return err
		}
		if err := validateLeaveOrInstalled("components.dashboard", input.Components.Dashboard); err != nil {
			return err
		}
	}
	if input.Config != nil {
		if err := validateOneOf("config.template", input.Config.Template, "leave", policytemplate.TemplateMinimal, policytemplate.TemplateLocalClashDefault); err != nil {
			return err
		}
		if err := validateOneOf("config.runtime_profile", input.Config.RuntimeProfile, "leave", runtimeprofile.ModeNormal, runtimeprofile.ModeRouter); err != nil {
			return err
		}
		if err := validateOneOf("config.core", input.Config.Core, "leave", runtimeprofile.CoreMeta, runtimeprofile.CoreSmart); err != nil {
			return err
		}
	}
	if input.Runtime != nil {
		if err := validateOneOf("runtime.service", input.Runtime.Service, "leave", "start", "restart", "restart_if_needed", "stop"); err != nil {
			return err
		}
		if err := validateOneOf("runtime.router_takeover", input.Runtime.RouterTakeover, "leave", "enabled", "disabled"); err != nil {
			return err
		}
	}
	if input.Subscriptions != nil && len(subscriptionURIs(input.Subscriptions)) > 0 {
		if _, err := sourcesFromURIs(subscriptionURIs(input.Subscriptions)); err != nil {
			return err
		}
	}
	return nil
}

func validateLeaveOrInstalled(field, value string) error {
	return validateOneOf(field, value, "leave", "installed_or_latest")
}

func validateOneOf(field, value string, allowed ...string) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	for _, candidate := range allowed {
		if value == candidate {
			return nil
		}
	}
	return fmt.Errorf("%s must be one of: %s", field, strings.Join(allowed, ", "))
}

func executeDesiredState(input desiredStateInput, state appinit.RuntimeState) ([]string, []string, error) {
	changes := []string{}
	warnings := []string{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if input.Subscriptions != nil && len(subscriptionURIs(input.Subscriptions)) > 0 {
		uris := subscriptionURIs(input.Subscriptions)
		if _, err := sourcesFromURIs(uris); err != nil {
			return changes, warnings, err
		}
		replace := true
		if _, err := subscriptions.Configure(subscriptions.ConfigureOptions{ConfigPath: state.Paths.SubscriptionConfig, URIs: uris, Replace: &replace}); err != nil {
			return changes, warnings, err
		}
		changes = append(changes, "subscription_sources_replaced")
	}
	if input.Subscriptions != nil && input.Subscriptions.Refresh != nil && *input.Subscriptions.Refresh {
		result, err := subscriptions.Refresh(ctx, subscriptions.RefreshOptions{
			ConfigPath: state.Paths.SubscriptionConfig,
			RuntimeDir: state.Paths.SubscriptionRuntime,
			MergedPath: state.Paths.SubscriptionPath,
			Force:      true,
			UserAgent:  subscriptions.DefaultUserAgent,
		})
		if err != nil {
			return changes, warnings, err
		}
		warnings = append(warnings, result.Warnings...)
		changes = append(changes, "subscriptions_refreshed")
	}
	if input.Components != nil {
		if input.Components.LocalClash == "installed_or_latest" {
			warnings = append(warnings, "localClash core update is owned by the LuCI helper/bootstrap layer in V1.")
		}
		if input.Components.Mihomo == "installed_or_latest" {
			if _, err := downloadCore(ctx, coredownload.Options{Version: "latest", Flavor: coredownload.FlavorAll, Target: coredownload.TargetRouter, TargetOS: "linux", TargetArch: runtime.GOARCH, OutputDir: productWorkspacePath(state, "bin"), Repo: "MetaCubeX/mihomo", Force: true}); err != nil {
				return changes, warnings, err
			}
			warnings = append(warnings, refreshCoreVersionCacheWarnings(ctx, state, "")...)
			changes = append(changes, "mihomo_updated")
		}
		if input.Components.Dashboard == "installed_or_latest" {
			if _, err := dashboard.Download(ctx, dashboard.Options{Version: "latest", AssetName: "dist.zip", OutputDir: filepath.Join(state.Paths.MihomoRuntimeDir, "ui", "zashboard"), Repo: "Zephyruso/zashboard", Force: true}); err != nil {
				return changes, warnings, err
			}
			changes = append(changes, "dashboard_updated")
		}
	}
	configChanged, configWarnings, err := executeDesiredConfig(ctx, input.Config, state)
	if err != nil {
		return changes, warnings, err
	}
	warnings = append(warnings, configWarnings...)
	if configChanged {
		changes = append(changes, "config_updated")
	}
	if configChanged || contains(changes, "subscriptions_refreshed") {
		_, renderWarnings, err := renderProductConfig(state)
		if err != nil {
			return changes, warnings, err
		}
		warnings = append(warnings, renderWarnings...)
		changes = append(changes, "config_rendered")
	}
	runtimeChanges, runtimeWarnings, err := executeDesiredRuntime(input.Runtime, state)
	if err != nil {
		return changes, warnings, err
	}
	changes = append(changes, runtimeChanges...)
	warnings = append(warnings, runtimeWarnings...)
	return changes, warnings, nil
}

func executeDesiredConfig(ctx context.Context, input *desiredConfig, state appinit.RuntimeState) (bool, []string, error) {
	if input == nil {
		return false, nil, nil
	}
	template := emptyAsLeave(input.Template)
	mode := emptyAsLeave(input.RuntimeProfile)
	core := emptyAsLeave(input.Core)
	if template == "leave" && mode == "leave" && core == "leave" {
		return false, nil, nil
	}
	if mode == "leave" || core == "leave" {
		profile, err := runtimeprofile.StatusFor(state.Paths.RuntimeProfilePath)
		if err != nil {
			return false, nil, err
		}
		if mode == "leave" {
			mode = profile.Mode
		}
		if core == "leave" {
			core = profile.Core
		}
	}
	if template != "leave" {
		allow := false
		if input.AllowOverwriteModified != nil {
			allow = *input.AllowOverwriteModified
		}
		_, warnings, err := applyTemplateInput(ctx, configInput{Version: 1, Template: template, RuntimeProfile: mode, Core: core, AllowOverwriteModified: allow}, state)
		return true, warnings, err
	}
	profile, err := runtimeprofile.Configure(state.Paths.RuntimeProfilePath, mode, core)
	if err != nil {
		return false, nil, err
	}
	warnings := refreshCoreVersionCacheWarnings(ctx, state, profile.CorePath)
	return true, warnings, nil
}

func executeDesiredRuntime(input *desiredRuntime, state appinit.RuntimeState) ([]string, []string, error) {
	if input == nil {
		return nil, nil, nil
	}
	changes := []string{}
	warnings := []string{}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	service := emptyAsLeave(input.Service)
	switch service {
	case "start":
		result, err := corerun.Start(ctx, runtimeStartOptions(state))
		if err != nil {
			return changes, warnings, err
		}
		warnings = append(warnings, result.Warnings...)
		warnings = append(warnings, refreshCoreVersionCacheWarnings(ctx, state, "")...)
		if result.Started {
			changes = append(changes, "runtime_started")
		}
	case "restart":
		result, err := corerun.Restart(ctx, runtimeRestartOptions(state))
		if err != nil {
			return changes, warnings, err
		}
		warnings = append(warnings, result.Warnings...)
		warnings = append(warnings, refreshCoreVersionCacheWarnings(ctx, state, "")...)
		if result.Restarted {
			changes = append(changes, "runtime_restarted")
		}
	case "restart_if_needed":
		status := corerun.Status(runtimeStatusOptions(state))
		if status.Running {
			result, err := corerun.Restart(ctx, runtimeRestartOptions(state))
			if err != nil {
				return changes, warnings, err
			}
			warnings = append(warnings, result.Warnings...)
			warnings = append(warnings, refreshCoreVersionCacheWarnings(ctx, state, "")...)
			if result.Restarted {
				changes = append(changes, "runtime_restarted")
			}
		} else {
			result, err := corerun.Start(ctx, runtimeStartOptions(state))
			if err != nil {
				return changes, warnings, err
			}
			warnings = append(warnings, result.Warnings...)
			warnings = append(warnings, refreshCoreVersionCacheWarnings(ctx, state, "")...)
			if result.Started {
				changes = append(changes, "runtime_started")
			}
		}
	case "stop":
		result, err := corerun.Stop(corerun.StopOptions{WorkDir: state.Paths.MihomoRuntimeDir, Timeout: 5 * time.Second})
		if err != nil {
			return changes, warnings, err
		}
		warnings = append(warnings, result.Warnings...)
		if result.Stopped {
			changes = append(changes, "runtime_stopped")
		}
	}
	takeover := emptyAsLeave(input.RouterTakeover)
	switch takeover {
	case "enabled":
		result, err := routertakeover.Apply(ctx, routerTakeoverOptions(state))
		if err != nil {
			return changes, warnings, err
		}
		warnings = append(warnings, result.Warnings...)
		if result.Applied {
			changes = append(changes, "takeover_applied")
		}
	case "disabled":
		result, err := routertakeover.Stop(ctx, routerTakeoverOptions(state))
		if err != nil {
			return changes, warnings, err
		}
		warnings = append(warnings, result.Warnings...)
		if result.Stopped {
			changes = append(changes, "takeover_stopped")
		}
	}
	return changes, warnings, nil
}

func desiredChanges(input desiredStateInput) []string {
	changes := []string{}
	if input.Subscriptions != nil {
		if len(subscriptionURIs(input.Subscriptions)) > 0 {
			changes = append(changes, "subscription_sources_replace")
		}
		if input.Subscriptions.Refresh != nil && *input.Subscriptions.Refresh {
			changes = append(changes, "subscriptions_refresh")
		}
	}
	if input.Components != nil {
		if input.Components.LocalClash == "installed_or_latest" {
			changes = append(changes, "localclash_install_or_update")
		}
		if input.Components.Mihomo == "installed_or_latest" {
			changes = append(changes, "mihomo_install_or_update")
		}
		if input.Components.Dashboard == "installed_or_latest" {
			changes = append(changes, "dashboard_install_or_update")
		}
	}
	if input.Config != nil && (emptyAsLeave(input.Config.Template) != "leave" || emptyAsLeave(input.Config.RuntimeProfile) != "leave" || emptyAsLeave(input.Config.Core) != "leave") {
		changes = append(changes, "config_update")
	}
	if input.Runtime != nil {
		if emptyAsLeave(input.Runtime.Service) != "leave" {
			changes = append(changes, "runtime_"+input.Runtime.Service)
		}
		if emptyAsLeave(input.Runtime.RouterTakeover) != "leave" {
			changes = append(changes, "takeover_"+input.Runtime.RouterTakeover)
		}
	}
	return changes
}

func runProductReset(args []string, state appinit.RuntimeState) error {
	opts, err := parseResetInput(args)
	if err != nil {
		return err
	}
	workspacePath, workspaceSource := resetWorkspaceForProduct(opts, state)
	result, err := reset.Run(reset.Options{
		Yes:                      true,
		DryRun:                   opts.DryRun,
		Full:                     opts.Full,
		Workspace:                workspacePath,
		WorkspaceSource:          workspaceSource,
		RequireExplicitWorkspace: opts.Full,
		Out:                      io.Discard,
	})
	if err != nil {
		return err
	}
	changed := !result.DryRun && len(result.Deleted) > 0
	summary := "Reset completed."
	change := "reset_completed"
	if result.Full {
		summary = "Full workspace reset completed."
		change = "full_reset_completed"
	}
	if result.DryRun {
		summary = "Reset dry run completed."
		change = ""
	}
	return printProductOK(productEnvelope{OK: true, Changed: changed, Summary: summary, Status: result, Changes: changedIf(changed, change), Warnings: []string{}})
}

type resetInput struct {
	Full      bool
	DryRun    bool
	Workspace string
}

type subscriptionInput struct {
	Version int      `json:"version"`
	URIs    []string `json:"uris"`
	URLs    []string `json:"urls"`
}

type configInput struct {
	Version                      int    `json:"version"`
	Template                     string `json:"template"`
	RuntimeProfile               string `json:"runtime_profile"`
	Core                         string `json:"core"`
	AllowOverwriteModified       bool   `json:"allow_overwrite_modified"`
	ResetPatches                 bool   `json:"reset_patches"`
	RefreshPolicyTemplatePatches bool   `json:"refresh_policy_template_patches"`
}

func parseSubscriptionInput(args []string) (subscriptionInput, error) {
	var input subscriptionInput
	if err := parseInputJSON("subscription set", args, &input); err != nil {
		return input, err
	}
	if input.Version != 1 {
		return input, fmt.Errorf("version must be 1")
	}
	if len(subscriptionInputURIs(input)) == 0 {
		return input, fmt.Errorf("uris must contain at least one URI")
	}
	return input, nil
}

func parseConfigInput(args []string) (configInput, error) {
	var input configInput
	if err := parseInputJSON("config apply-template", args, &input); err != nil {
		return input, err
	}
	if input.Version != 1 {
		return input, fmt.Errorf("version must be 1")
	}
	if input.Template != policytemplate.TemplateMinimal && input.Template != policytemplate.TemplateLocalClashDefault {
		return input, fmt.Errorf("template must be minimal or localclash-default")
	}
	if input.RuntimeProfile != runtimeprofile.ModeNormal && input.RuntimeProfile != runtimeprofile.ModeRouter {
		return input, fmt.Errorf("runtime_profile must be normal or router")
	}
	if input.Core != runtimeprofile.CoreMeta && input.Core != runtimeprofile.CoreSmart {
		return input, fmt.Errorf("core must be meta or smart")
	}
	if input.ResetPatches && input.RefreshPolicyTemplatePatches {
		return input, fmt.Errorf("reset_patches and refresh_policy_template_patches are mutually exclusive")
	}
	return input, nil
}

func parseInputJSON(name string, args []string, dest any) error {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	inputPath := fs.String("input", "", "input JSON path")
	asJSON := fs.Bool("json", false, "print product JSON response")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !*asJSON || fs.NArg() != 0 || strings.TrimSpace(*inputPath) == "" {
		return fmt.Errorf("usage: localclash %s --input <file> --json", name)
	}
	data, err := os.ReadFile(*inputPath)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dest); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return fmt.Errorf("input JSON must contain exactly one object")
	}
	return nil
}

func parseJSONOnly(name string, args []string) error {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	asJSON := fs.Bool("json", false, "print product JSON response")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !*asJSON || fs.NArg() != 0 {
		return fmt.Errorf("usage: localclash %s --json", name)
	}
	return nil
}

func parseResetInput(args []string) (resetInput, error) {
	fs := flag.NewFlagSet("reset", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var input resetInput
	fs.BoolVar(&input.Full, "full", false, "delete the entire localClash workspace directory")
	fs.BoolVar(&input.DryRun, "dry-run", false, "print the reset plan without deleting files")
	fs.StringVar(&input.Workspace, "workspace", "", "explicit localClash workspace path")
	asJSON := fs.Bool("json", false, "print product JSON response")
	if err := fs.Parse(args); err != nil {
		return input, err
	}
	if !*asJSON || fs.NArg() != 0 {
		return input, fmt.Errorf("usage: localclash reset [--full] [--dry-run] [--workspace <path>] --json")
	}
	return input, nil
}

func resetWorkspaceForProduct(input resetInput, state appinit.RuntimeState) (string, string) {
	if path := strings.TrimSpace(input.Workspace); path != "" {
		return path, "flag:--workspace"
	}
	if path := strings.TrimSpace(os.Getenv(workspace.EnvVar)); path != "" {
		return path, "env:" + workspace.EnvVar
	}
	if input.Full {
		return "", ""
	}
	if path := productWorkspaceRoot(state); path != "" {
		return path, "runtime_state"
	}
	return "", ""
}

func productWorkspaceRoot(state appinit.RuntimeState) string {
	if root := strings.TrimSpace(state.Paths.WorkspaceRoot); root != "" {
		return root
	}
	if root := workspace.FromRuntimeRoot(state.Paths.RuntimeRoot); root != "" {
		return root
	}
	return "."
}

func productWorkspacePath(state appinit.RuntimeState, name string) string {
	if strings.TrimSpace(name) == "" || filepath.IsAbs(name) {
		return name
	}
	root := productWorkspaceRoot(state)
	if root == "" || root == "." {
		return name
	}
	return filepath.Join(root, name)
}

func sourcesFromURLs(rawURLs []string) ([]subscriptions.Source, error) {
	return sourcesFromURIs(rawURLs)
}

func sourcesFromURIs(rawURIs []string) ([]subscriptions.Source, error) {
	return subscriptions.SourcesFromURIs(rawURIs)
}

func subscriptionInputURIs(input subscriptionInput) []string {
	if len(input.URIs) > 0 {
		return input.URIs
	}
	return input.URLs
}

func subscriptionURIs(input *desiredSubscriptions) []string {
	if input == nil {
		return nil
	}
	if len(input.URIs) > 0 {
		return input.URIs
	}
	return input.URLs
}

func productStatus(state appinit.RuntimeState) (map[string]any, []string) {
	warnings := append([]string{}, diagnosticsToWarnings(state.Diagnostics)...)
	subStatus, err := subscriptions.Status(subscriptionStatusOptions(state))
	if err != nil {
		warnings = append(warnings, err.Error())
	}
	cfgStatus, cfgWarnings := configStatus(state)
	warnings = append(warnings, cfgWarnings...)
	runtimeStatus := corerun.Status(runtimeStatusOptions(state))
	return map[string]any{
		"bootstrap":    state,
		"subscription": subStatus,
		"components":   componentStatus(state),
		"config":       cfgStatus,
		"runtime":      runtimeStatus,
	}, warnings
}

func componentStatus(state appinit.RuntimeState) map[string]any {
	exe, _ := os.Executable()
	dashboardPath := filepath.Join(state.Paths.MihomoRuntimeDir, "ui", "zashboard")
	return map[string]any{
		"base_assets": baseassets.Status(productWorkspaceRoot(state)),
		"localclash": map[string]any{
			"path":      exe,
			"installed": exe != "",
		},
		"mihomo": map[string]any{
			"path":      state.Core.Path,
			"installed": state.Core.Exists,
			"missing":   state.Core.Missing,
			"version":   state.Core.Version,
		},
		"dashboard": map[string]any{
			"path":      dashboardPath,
			"installed": pathExistsAny(dashboardPath),
		},
	}
}

func configStatus(state appinit.RuntimeState) (map[string]any, []string) {
	warnings := []string{}
	configPath := productWorkspacePath(state, "localclash-intent.json")
	intent, err := configinspect.InspectIntent(configinspect.IntentOptions{
		ConfigPath:          configPath,
		Subscription:        state.Paths.SubscriptionPath,
		SubscriptionConfig:  state.Paths.SubscriptionConfig,
		SubscriptionRuntime: state.Paths.SubscriptionRuntime,
		RulesCache:          state.Paths.RulesCacheDir,
		Limit:               8,
		SkipResolve:         true,
	})
	if err != nil {
		warnings = append(warnings, err.Error())
	}
	patchInventory := configpatch.InventoryFor(productWorkspacePath(state, configpatch.RegistryDirName), intent.PolicyTemplate, configPath, productWorkspacePath(state, "localclash-packs.gob"), state.Paths.GeneratedConfig, 8)
	profile, err := runtimeprofile.StatusFor(state.Paths.RuntimeProfilePath)
	if err != nil {
		warnings = append(warnings, err.Error())
	}
	return map[string]any{
		"patch_registry":  patchInventory,
		"compiled_intent": intent,
		"runtime_profile": profile,
		"generated": map[string]any{
			"path":   state.Paths.GeneratedConfig,
			"exists": pathExists(state.Paths.GeneratedConfig),
		},
	}, warnings
}

func applyTemplateInput(ctx context.Context, input configInput, state appinit.RuntimeState) (map[string]any, []string, error) {
	configPath := productWorkspacePath(state, "localclash-intent.json")
	if !input.AllowOverwriteModified {
		current, err := localconfig.Load(configPath)
		if err == nil && current.PolicyTemplate != "" && current.PolicyTemplate != input.Template {
			return nil, nil, codedProductError{
				code:        "modified_config_requires_confirmation",
				message:     "Current localclash-intent.json does not match the requested template; refusing to overwrite without allow_overwrite_modified.",
				nextActions: []string{"Set allow_overwrite_modified to true after user confirmation."},
				details: map[string]string{
					"current_policy_template": current.PolicyTemplate,
					"requested_template":      input.Template,
				},
			}
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, nil, err
		}
	}
	profile, err := runtimeprofile.Configure(state.Paths.RuntimeProfilePath, input.RuntimeProfile, input.Core)
	if err != nil {
		return nil, nil, err
	}
	patchResult, err := configpatch.ImportPolicyTemplate(ctx, configpatch.ImportTemplateOptions{
		RegistryDir:         productWorkspacePath(state, configpatch.RegistryDirName),
		PolicyTemplatesDir:  productWorkspacePath(state, policytemplate.DefaultDir),
		PolicyTemplate:      input.Template,
		ResetPatches:        input.ResetPatches,
		ConfigPath:          configPath,
		SelectionPath:       productWorkspacePath(state, "localclash-packs.gob"),
		OutputPath:          state.Paths.GeneratedConfig,
		Subscription:        state.Paths.SubscriptionPath,
		SubscriptionConfig:  state.Paths.SubscriptionConfig,
		SubscriptionRuntime: state.Paths.SubscriptionRuntime,
		RulesCache:          state.Paths.RulesCacheDir,
		RuntimeProfilePath:  state.Paths.RuntimeProfilePath,
		CorePath:            normalizeCorePathForState(state, profile.CorePath),
		WorkDir:             state.Paths.MihomoRuntimeDir,
		RefreshTemplateOnly: input.RefreshPolicyTemplatePatches,
	})
	if err != nil {
		return nil, nil, err
	}
	warnings := refreshCoreVersionCacheWarnings(ctx, state, profile.CorePath)
	warnings = append(warnings, patchResult.Warnings...)
	return map[string]any{"template": patchResult.Template, "patch_registry": patchResult, "runtime_profile": profile}, warnings, nil
}

func subscriptionStatusOptions(state appinit.RuntimeState) subscriptions.StatusOptions {
	return subscriptions.StatusOptions{
		ConfigPath: state.Paths.SubscriptionConfig,
		MergedPath: state.Paths.SubscriptionPath,
		RuntimeDir: state.Paths.SubscriptionRuntime,
	}
}

func configRenderOptions(state appinit.RuntimeState) configrender.Options {
	return configrender.Options{
		SourcePath:         state.Paths.SubscriptionPath,
		OutputPath:         state.Paths.GeneratedConfig,
		PacksSelectionPath: state.Paths.PacksSelectionPath,
		RulesCacheDir:      state.Paths.RulesCacheDir,
		RuntimeProfilePath: state.Paths.RuntimeProfilePath,
		Force:              true,
	}
}

func renderProductConfig(state appinit.RuntimeState) (map[string]any, []string, error) {
	opts := configRenderOptions(state)
	configPath := productWorkspacePath(state, "localclash-intent.json")
	selectionPath := ""
	source := "base"
	warnings := []string{}

	registryDir := productWorkspacePath(state, configpatch.RegistryDirName)
	if productRegistryHasPatches(registryDir) {
		policyTemplate := ""
		if current, err := localconfig.Load(configPath); err == nil {
			policyTemplate = current.PolicyTemplate
		}
		config, _, err := configpatch.Compile(registryDir, policyTemplate, time.Now())
		if err != nil {
			return nil, nil, err
		}
		if err := localconfig.Write(configPath, config); err != nil {
			return nil, nil, err
		}
		source = "patch_registry"
	}
	if pathExists(configPath) {
		config, err := localconfig.Load(configPath)
		if err != nil {
			return nil, nil, err
		}
		capabilityNodes, err := loadProductCapabilityNodes(config, state)
		if err != nil {
			return nil, nil, err
		}
		resolved, err := localconfig.Resolve(localconfig.ResolveOptions{
			Config:              config,
			SubscriptionPath:    state.Paths.SubscriptionPath,
			SubscriptionConfig:  state.Paths.SubscriptionConfig,
			SubscriptionRuntime: state.Paths.SubscriptionRuntime,
			CapabilityNodes:     capabilityNodes,
			RulesCache:          state.Paths.RulesCacheDir,
		})
		if err != nil {
			return nil, nil, err
		}
		selectionPath = state.Paths.PacksSelectionPath
		if strings.TrimSpace(selectionPath) == "" {
			selectionPath = productWorkspacePath(state, "localclash-packs.gob")
		}
		if err := localconfig.WriteSelection(selectionPath, resolved.Selection); err != nil {
			return nil, nil, err
		}
		opts.PacksSelectionPath = selectionPath
		if source != "patch_registry" {
			source = "compiled_intent"
		}
		warnings = append(warnings, resolved.Warnings...)
	}

	result, err := configrender.Render(opts)
	if err != nil {
		return nil, nil, err
	}
	return map[string]any{
		"render":          result,
		"source":          source,
		"source_of_truth": registryDir,
		"compiled_intent": configPath,
		"selection":       selectionPath,
		"output":          state.Paths.GeneratedConfig,
	}, warnings, nil
}

func loadProductCapabilityNodes(config localconfig.Config, state appinit.RuntimeState) (map[string][]string, error) {
	profiles := configuredProductCapabilityProfiles(config)
	if len(profiles) == 0 {
		return map[string][]string{}, nil
	}
	if len(profiles) != 1 || profiles[0] != chatgptavailable.ProfileID {
		return nil, fmt.Errorf("unsupported proxy-group capabilities: %s", strings.Join(profiles, ", "))
	}
	qualified, err := chatgptavailable.LoadQualified(filepath.Join(productCapabilityRoot(state), "chatgpt-available.json"))
	if err != nil {
		return nil, err
	}
	return map[string][]string{chatgptavailable.ProfileID: qualified}, nil
}

func runtimeStatusOptions(state appinit.RuntimeState) corerun.StatusOptions {
	return corerun.StatusOptions{CorePath: state.Paths.CorePath, ConfigPath: state.Paths.GeneratedConfig, WorkDir: state.Paths.MihomoRuntimeDir}
}

func runtimeStartOptions(state appinit.RuntimeState) corerun.StartOptions {
	return corerun.StartOptions{CorePath: state.Paths.CorePath, ConfigPath: state.Paths.GeneratedConfig, WorkDir: state.Paths.MihomoRuntimeDir}
}

func runtimeRestartOptions(state appinit.RuntimeState) corerun.RestartOptions {
	return corerun.RestartOptions{CorePath: state.Paths.CorePath, ConfigPath: state.Paths.GeneratedConfig, WorkDir: state.Paths.MihomoRuntimeDir, StopTimeout: 5 * time.Second}
}

func refreshCoreVersionCacheWarnings(ctx context.Context, state appinit.RuntimeState, corePath string) []string {
	corePath = normalizeCorePathForState(state, corePath)
	if strings.TrimSpace(corePath) == "" {
		return nil
	}
	if _, err := appinit.RefreshCoreVersionCache(ctx, state.Paths.RuntimeRoot, corePath); err != nil {
		return []string{"core version cache refresh failed: " + err.Error()}
	}
	return nil
}

func normalizeCorePathForState(state appinit.RuntimeState, corePath string) string {
	corePath = strings.TrimSpace(corePath)
	if corePath == "" {
		return state.Paths.CorePath
	}
	if filepath.IsAbs(corePath) {
		return corePath
	}
	root := productWorkspaceRoot(state)
	if root == "" || root == "." {
		return corePath
	}
	return filepath.Join(root, corePath)
}

func routerTakeoverOptions(state appinit.RuntimeState) routertakeover.Options {
	return routertakeover.Options{RuntimeProfile: state.Paths.RuntimeProfilePath, ConfigPath: state.Paths.GeneratedConfig, RuntimeDir: state.Paths.MihomoRuntimeDir}
}

func printProductOK(envelope productEnvelope) error {
	if envelope.Changes == nil {
		envelope.Changes = []string{}
	}
	if envelope.Warnings == nil {
		envelope.Warnings = []string{}
	}
	if envelope.NextActions == nil {
		envelope.NextActions = []string{}
	}
	return printJSON(envelope)
}

func printProductError(err error) error {
	code := "command_failed"
	message := err.Error()
	var details any
	var next []string
	var coded codedProductError
	if errors.As(err, &coded) {
		code = coded.code
		message = coded.message
		details = coded.details
		next = coded.nextActions
	}
	return printJSON(productErrorEnvelope{OK: false, Code: code, Message: message, Details: details, NextActions: next})
}

func hasFlag(args []string, name string) bool {
	want := "--" + name
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func changedIf(changed bool, name string) []string {
	if changed {
		return []string{name}
	}
	return []string{}
}

func emptyAsLeave(value string) string {
	if strings.TrimSpace(value) == "" {
		return "leave"
	}
	return value
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func diagnosticsToWarnings(diagnostics []appinit.Diagnostic) []string {
	warnings := []string{}
	for _, diagnostic := range diagnostics {
		if diagnostic.Level == "warning" || diagnostic.Level == "error" {
			warnings = append(warnings, diagnostic.Step+": "+diagnostic.Message)
		}
	}
	return warnings
}

func pathExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func productRegistryHasPatches(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".json" {
			return true
		}
	}
	return false
}

func pathExistsAny(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
