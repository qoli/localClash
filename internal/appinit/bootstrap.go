package appinit

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"localclash/internal/customsites"
	"localclash/internal/rules"
	"localclash/internal/runtimeprofile"
	"localclash/internal/subscriptions"
	"localclash/internal/workspace"
)

type Options struct {
	RuntimeRoot         string
	RuleSourcesDir      string
	RulesCacheDir       string
	GeneratedConfig     string
	SubscriptionConfig  string
	SubscriptionPath    string
	SubscriptionRuntime string
	MihomoRuntimeDir    string
	CorePath            string
	PacksSelectionPath  string
	RuntimeProfilePath  string
	CustomSitesProxy    string
	CustomSitesDirect   string
}

type RuntimeState struct {
	Paths        RuntimePaths      `json:"paths"`
	Core         CoreState         `json:"core"`
	Subscription SubscriptionState `json:"subscription"`
	Rules        RulesState        `json:"rules"`
	Config       ConfigState       `json:"config"`
	Warnings     []string          `json:"warnings"`
	Diagnostics  []Diagnostic      `json:"diagnostics"`
}

type RuntimePaths struct {
	WorkspaceRoot       string `json:"workspace_root,omitempty"`
	RuntimeRoot         string `json:"runtime_root"`
	RuleSourcesDir      string `json:"rule_sources_dir"`
	RulesCacheDir       string `json:"rules_cache_dir"`
	PacksDir            string `json:"packs_dir"`
	GeneratedConfig     string `json:"generated_config"`
	SubscriptionConfig  string `json:"subscription_config"`
	SubscriptionPath    string `json:"subscription_path"`
	SubscriptionRuntime string `json:"subscription_runtime"`
	MihomoRuntimeDir    string `json:"mihomo_runtime_dir"`
	CorePath            string `json:"core_path"`
	PacksSelectionPath  string `json:"packs_selection_path,omitempty"`
	RuntimeProfilePath  string `json:"runtime_profile_path"`
	CustomSitesProxy    string `json:"custom_sites_proxy"`
	CustomSitesDirect   string `json:"custom_sites_direct"`
}

type CoreState struct {
	Path           string `json:"path"`
	Exists         bool   `json:"exists"`
	Missing        bool   `json:"missing"`
	Version        string `json:"version,omitempty"`
	SmartSupported bool   `json:"smart_supported"`
}

type SubscriptionState struct {
	Configured bool   `json:"configured"`
	Path       string `json:"path"`
	Available  bool   `json:"available"`
	Sources    int    `json:"sources"`
	Diagnostic string `json:"diagnostic,omitempty"`
}

type RulesState struct {
	SourcesDiscovered int                         `json:"sources_discovered"`
	CacheDir          string                      `json:"cache_dir"`
	CatalogAvailable  bool                        `json:"catalog_available"`
	PacksGenerated    bool                        `json:"packs_generated"`
	Packs             []rules.PackSummary         `json:"packs"`
	Details           map[string]rules.PackDetail `json:"-"`
	Diagnostic        string                      `json:"diagnostic,omitempty"`
}

type ConfigState struct {
	Path       string `json:"path"`
	Rendered   bool   `json:"rendered"`
	Available  bool   `json:"available"`
	Diagnostic string `json:"diagnostic,omitempty"`
}

type Diagnostic struct {
	Step    string `json:"step"`
	Level   string `json:"level"`
	Message string `json:"message"`
}

var defaultWorkDirCandidates = []string{"/root/localclash"}

func Bootstrap(ctx context.Context, opts Options) RuntimeState {
	opts = normalizeOptions(opts)
	state := RuntimeState{
		Paths: RuntimePaths{
			WorkspaceRoot:       workspaceRootFromOptions(opts),
			RuntimeRoot:         opts.RuntimeRoot,
			RuleSourcesDir:      opts.RuleSourcesDir,
			RulesCacheDir:       opts.RulesCacheDir,
			PacksDir:            opts.RulesCacheDir,
			GeneratedConfig:     opts.GeneratedConfig,
			SubscriptionConfig:  opts.SubscriptionConfig,
			SubscriptionPath:    opts.SubscriptionPath,
			SubscriptionRuntime: opts.SubscriptionRuntime,
			MihomoRuntimeDir:    opts.MihomoRuntimeDir,
			CorePath:            opts.CorePath,
			PacksSelectionPath:  opts.PacksSelectionPath,
			RuntimeProfilePath:  opts.RuntimeProfilePath,
			CustomSitesProxy:    opts.CustomSitesProxy,
			CustomSitesDirect:   opts.CustomSitesDirect,
		},
		Core:   CoreState{Path: opts.CorePath},
		Rules:  RulesState{CacheDir: opts.RulesCacheDir, Details: map[string]rules.PackDetail{}},
		Config: ConfigState{Path: opts.GeneratedConfig, Available: fileExists(opts.GeneratedConfig)},
	}
	ensureDirs(&state, opts)
	inspectCore(ctx, &state, opts)
	inspectSubscription(&state, opts)
	ensureRulesCatalog(ctx, &state, opts)
	inspectGeneratedConfig(&state, opts)
	return state
}

func normalizeOptions(opts Options) Options {
	baseDir := defaultBaseDir(opts)
	if strings.TrimSpace(opts.RuntimeRoot) == "" {
		opts.RuntimeRoot = defaultPath(baseDir, ".runtime")
	}
	if strings.TrimSpace(opts.RuleSourcesDir) == "" {
		opts.RuleSourcesDir = defaultPath(baseDir, "rule-sources")
	}
	if strings.TrimSpace(opts.RulesCacheDir) == "" {
		opts.RulesCacheDir = filepath.Join(opts.RuntimeRoot, "rules", "packs")
	}
	if strings.TrimSpace(opts.SubscriptionConfig) == "" {
		opts.SubscriptionConfig = defaultPath(baseDir, "localclash-subscriptions.json")
	}
	if strings.TrimSpace(opts.SubscriptionPath) == "" {
		opts.SubscriptionPath = defaultPath(baseDir, "subscription.gob")
	}
	if strings.TrimSpace(opts.SubscriptionRuntime) == "" {
		opts.SubscriptionRuntime = filepath.Join(opts.RuntimeRoot, "subscriptions")
	}
	if strings.TrimSpace(opts.MihomoRuntimeDir) == "" {
		opts.MihomoRuntimeDir = filepath.Join(opts.RuntimeRoot, "mihomo")
	}
	if strings.TrimSpace(opts.GeneratedConfig) == "" {
		opts.GeneratedConfig = filepath.Join(opts.MihomoRuntimeDir, "config.yaml")
	}
	if strings.TrimSpace(opts.PacksSelectionPath) == "" && fileExists(defaultPath(baseDir, "localclash-packs.gob")) {
		opts.PacksSelectionPath = defaultPath(baseDir, "localclash-packs.gob")
	}
	if strings.TrimSpace(opts.RuntimeProfilePath) == "" {
		opts.RuntimeProfilePath = defaultPath(baseDir, runtimeprofile.DefaultPath)
	}
	customSitePaths := customsites.DefaultPaths(baseDir)
	if strings.TrimSpace(opts.CustomSitesProxy) == "" {
		opts.CustomSitesProxy = customSitePaths.Proxy
	}
	if strings.TrimSpace(opts.CustomSitesDirect) == "" {
		opts.CustomSitesDirect = customSitePaths.Direct
	}
	if strings.TrimSpace(opts.CorePath) == "" {
		if corePath, err := runtimeprofile.ActiveCorePath(opts.RuntimeProfilePath); err == nil && strings.TrimSpace(corePath) != "" {
			opts.CorePath = defaultPath(baseDir, corePath)
		} else {
			opts.CorePath = defaultPath(baseDir, runtimeprofile.MetaCorePath)
		}
	}
	return opts
}

func defaultBaseDir(opts Options) string {
	if hasExplicitPath(opts) {
		return ""
	}
	if dir := strings.TrimSpace(os.Getenv(workspace.EnvVar)); dir != "" {
		return dir
	}
	if looksLikeWorkDir(".") {
		return ""
	}
	for _, candidate := range defaultWorkDirCandidates {
		if looksLikeWorkDir(candidate) {
			return candidate
		}
	}
	return ""
}

func hasExplicitPath(opts Options) bool {
	return strings.TrimSpace(opts.RuntimeRoot) != "" ||
		strings.TrimSpace(opts.RuleSourcesDir) != "" ||
		strings.TrimSpace(opts.RulesCacheDir) != "" ||
		strings.TrimSpace(opts.GeneratedConfig) != "" ||
		strings.TrimSpace(opts.SubscriptionConfig) != "" ||
		strings.TrimSpace(opts.SubscriptionPath) != "" ||
		strings.TrimSpace(opts.SubscriptionRuntime) != "" ||
		strings.TrimSpace(opts.MihomoRuntimeDir) != "" ||
		strings.TrimSpace(opts.CorePath) != "" ||
		strings.TrimSpace(opts.PacksSelectionPath) != "" ||
		strings.TrimSpace(opts.RuntimeProfilePath) != "" ||
		strings.TrimSpace(opts.CustomSitesProxy) != "" ||
		strings.TrimSpace(opts.CustomSitesDirect) != ""
}

func looksLikeWorkDir(dir string) bool {
	for _, marker := range []string{
		"localclash-runtime.json",
		"localclash-intent.json",
		"subscription.gob",
		filepath.Join(".runtime", "mihomo", "config.yaml"),
		filepath.Join("generated", "mihomo.yaml"),
		"policy-templates",
		"rule-sources",
	} {
		if fileExists(filepath.Join(dir, marker)) {
			return true
		}
	}
	return false
}

func defaultPath(baseDir, path string) string {
	if baseDir == "" || path == "" || filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(baseDir, path)
}

func workspaceRootFromOptions(opts Options) string {
	root := workspace.FromRuntimeRoot(opts.RuntimeRoot)
	if root == "" {
		return ""
	}
	return filepath.Clean(root)
}

func ensureDirs(state *RuntimeState, opts Options) {
	for _, dir := range []string{opts.RuntimeRoot, opts.RulesCacheDir, opts.SubscriptionRuntime, opts.MihomoRuntimeDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			state.addDiagnostic("runtime_dirs", "warning", err.Error())
		}
	}
	if err := workspace.EnsureMarker(state.Paths.WorkspaceRoot); err != nil {
		state.addDiagnostic("workspace_marker", "warning", err.Error())
	}
}

func inspectCore(ctx context.Context, state *RuntimeState, opts Options) {
	state.Core.Path = opts.CorePath
	info, err := os.Stat(opts.CorePath)
	if err != nil || info.IsDir() {
		state.Core.Missing = true
		state.addDiagnostic("core", "warning", "mihomo core is missing; run core download before starting runtime")
		return
	}
	state.Core.Exists = true
	if cached, ok := readCoreVersionCache(CoreVersionCachePath(opts.RuntimeRoot), opts.CorePath); ok {
		state.Core = cached
		return
	}
	core, err := inspectCoreLive(ctx, opts.CorePath)
	if err == nil {
		state.Core = core
		if err := writeCoreVersionCache(CoreVersionCachePath(opts.RuntimeRoot), core, timeNow()); err != nil {
			state.addDiagnostic("core_version_cache", "warning", "cannot write core version cache: "+err.Error())
		}
	}
}

func inspectSubscription(state *RuntimeState, opts Options) {
	status, err := subscriptions.Status(subscriptions.StatusOptions{
		ConfigPath: opts.SubscriptionConfig,
		MergedPath: opts.SubscriptionPath,
		RuntimeDir: opts.SubscriptionRuntime,
	})
	if err != nil {
		state.Subscription.Diagnostic = err.Error()
		state.addDiagnostic("subscription", "warning", err.Error())
		return
	}
	state.Subscription.Configured = status.Configured
	state.Subscription.Path = opts.SubscriptionPath
	state.Subscription.Available = status.Merged.Exists && status.Merged.ProxiesCount > 0
	state.Subscription.Sources = len(status.Sources)
	if !state.Subscription.Available {
		if status.Message != "" {
			state.Subscription.Diagnostic = status.Message
		} else {
			state.Subscription.Diagnostic = "effective subscription.gob is unavailable; run subscriptions_refresh"
		}
		state.addDiagnostic("subscription", "warning", state.Subscription.Diagnostic)
	}
}

func ensureRulesCatalog(ctx context.Context, state *RuntimeState, opts Options) {
	sources, err := rules.LoadSources(opts.RuleSourcesDir)
	if err != nil {
		state.Rules.Diagnostic = err.Error()
		state.addDiagnostic("rules_sources", "warning", err.Error())
	} else {
		state.Rules.SourcesDiscovered = len(sources)
	}
	catalog, err := rules.LoadPackCatalog(opts.RulesCacheDir)
	if err != nil {
		if _, adaptErr := rules.Adapt(ctx, rules.Options{SourcesDir: opts.RuleSourcesDir, CacheDir: opts.RulesCacheDir}); adaptErr != nil {
			state.Rules.Diagnostic = adaptErr.Error()
			state.addDiagnostic("packs_catalog", "warning", adaptErr.Error())
			return
		}
		state.Rules.PacksGenerated = true
		catalog, err = rules.LoadPackCatalog(opts.RulesCacheDir)
	}
	if err != nil {
		state.Rules.Diagnostic = err.Error()
		state.addDiagnostic("packs_catalog", "warning", err.Error())
		return
	}
	state.Rules.CatalogAvailable = true
	state.Rules.Packs = catalog.Packs
	state.Rules.Details = catalog.Details
}

func inspectGeneratedConfig(state *RuntimeState, opts Options) {
	state.Config.Available = fileExists(opts.GeneratedConfig)
	if state.Config.Available {
		return
	}
	if !state.Subscription.Available {
		state.Config.Diagnostic = "generated config is missing and effective subscription is unavailable; run subscriptions_refresh before config_render"
	} else {
		state.Config.Diagnostic = "generated config is missing; run config_render to build .runtime/mihomo/config.yaml from durable localClash state"
	}
	state.addDiagnostic("generated_config", "warning", state.Config.Diagnostic)
}

func defaultWorkDirPath(runtimeRoot, name string) string {
	if filepath.Base(runtimeRoot) == ".runtime" {
		return filepath.Join(filepath.Dir(runtimeRoot), name)
	}
	return name
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func (state *RuntimeState) addDiagnostic(step, level, message string) {
	state.Diagnostics = append(state.Diagnostics, Diagnostic{Step: step, Level: level, Message: message})
	if level == "warning" {
		state.Warnings = append(state.Warnings, message)
	}
}
