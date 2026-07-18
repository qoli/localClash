package configpatch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"localclash/internal/configplan"
	"localclash/internal/configrender"
	"localclash/internal/localconfig"
	"localclash/internal/mihomotest"
	"localclash/internal/policytemplate"
	"localclash/internal/rules"
	"localclash/internal/runtimeprofile"
)

const (
	RegistryDirName      = "patches"
	PatchVersion         = 1
	BuilderSchemaVersion = 1

	StatusEnabled    = "enabled"
	StatusDisabled   = "disabled"
	StatusTombstoned = "tombstoned"

	SourceUser           = "user"
	SourcePolicyTemplate = "policy_template"
)

var (
	applyMu        sync.Mutex
	patchIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	orderIDPattern = regexp.MustCompile(`^[0-9]{4}\.[0-9]{6}$`)
)

type Patch struct {
	Version   int                      `json:"version"`
	PatchID   string                   `json:"patch_id"`
	Title     string                   `json:"title,omitempty"`
	Source    string                   `json:"source"`
	SourceRef string                   `json:"source_ref,omitempty"`
	Status    string                   `json:"status"`
	OrderID   string                   `json:"order_id"`
	Summary   string                   `json:"summary,omitempty"`
	Overlay   configplan.OverlayIntent `json:"overlay"`
}

type Operation struct {
	Op                string                   `json:"op"`
	PatchID           string                   `json:"patch_id,omitempty"`
	BasePatchSHA256   string                   `json:"base_patch_sha256,omitempty"`
	Title             string                   `json:"title,omitempty"`
	Source            string                   `json:"source,omitempty"`
	SourceRef         string                   `json:"source_ref,omitempty"`
	Status            string                   `json:"status,omitempty"`
	OrderID           string                   `json:"order_id,omitempty"`
	Summary           string                   `json:"summary,omitempty"`
	Overlay           configplan.OverlayIntent `json:"overlay,omitempty"`
	BeforePatchID     string                   `json:"before_patch_id,omitempty"`
	AfterPatchID      string                   `json:"after_patch_id,omitempty"`
	NormalizedOrderID string                   `json:"normalized_order_id,omitempty"`
}

type Record struct {
	Patch    Patch
	Path     string
	SHA256   string
	Provides []string
}

type Registry struct {
	Dir     string
	Records []Record
	ByID    map[string]Record
}

type PatchSummary struct {
	PatchID  string   `json:"patch_id"`
	Title    string   `json:"title,omitempty"`
	Source   string   `json:"source"`
	Status   string   `json:"status"`
	OrderID  string   `json:"order_id"`
	Summary  string   `json:"summary,omitempty"`
	SHA256   string   `json:"sha256"`
	Path     string   `json:"path,omitempty"`
	Provides []string `json:"provides,omitempty"`
}

type Inventory struct {
	Patches      []PatchSummary `json:"patches"`
	RegistryHash string         `json:"registry_hash"`
	Artifacts    Artifacts      `json:"artifacts"`
	Error        string         `json:"error,omitempty"`
}

type Artifacts struct {
	PatchRegistry string `json:"patch_registry"`
	FinalIntent   string `json:"final_intent"`
	Selection     string `json:"selection,omitempty"`
	Generated     string `json:"generated,omitempty"`
}

type GetResult struct {
	PatchID      string                   `json:"patch_id"`
	Title        string                   `json:"title,omitempty"`
	Source       string                   `json:"source"`
	SourceRef    string                   `json:"source_ref,omitempty"`
	Status       string                   `json:"status"`
	OrderID      string                   `json:"order_id"`
	Summary      string                   `json:"summary,omitempty"`
	SHA256       string                   `json:"sha256"`
	RegistryHash string                   `json:"registry_hash"`
	Path         string                   `json:"path,omitempty"`
	Overlay      configplan.OverlayIntent `json:"overlay"`
	Provides     []string                 `json:"provides"`
	ReferencedBy []string                 `json:"referenced_by"`
	NextActions  []string                 `json:"next_actions,omitempty"`
}

type DraftOptions struct {
	RegistryDir         string
	PolicyTemplate      string
	ConfigPath          string
	SelectionPath       string
	OutputPath          string
	Subscription        string
	SubscriptionConfig  string
	SubscriptionRuntime string
	RulesCache          string
	RuntimeProfilePath  string
	ValidationCache     string
	CorePath            string
	WorkDir             string
	DraftName           string
	Operations          []Operation
	Test                bool
	Generation          int64
	Now                 time.Time
}

type DraftResult struct {
	Valid             bool              `json:"valid"`
	CurrentDraft      CurrentDraft      `json:"current_draft"`
	DraftName         string            `json:"draft_name,omitempty"`
	Impact            Impact            `json:"impact"`
	Operations        []Operation       `json:"operations"`
	BaseHashes        map[string]string `json:"base_hashes"`
	BaseRegistryHash  string            `json:"base_registry_hash"`
	RegistryHash      string            `json:"registry_hash"`
	ApplyArgs         map[string]any    `json:"apply_args"`
	ExplicitApplyArgs map[string]any    `json:"explicit_apply_args"`
	Validation        *BuildResult      `json:"validation,omitempty"`
	Warnings          []string          `json:"warnings,omitempty"`
	NextActions       []string          `json:"next_actions,omitempty"`
}

type CurrentDraft struct {
	Generation int64 `json:"generation"`
	Stale      bool  `json:"stale,omitempty"`
}

type Impact struct {
	PatchesChanged []string `json:"patches_changed"`
	PatchesRemoved []string `json:"patches_removed,omitempty"`
	EnabledBefore  int      `json:"enabled_before"`
	EnabledAfter   int      `json:"enabled_after"`
}

type ApplyOptions struct {
	RegistryDir         string
	PolicyTemplate      string
	ConfigPath          string
	SelectionPath       string
	OutputPath          string
	Subscription        string
	SubscriptionConfig  string
	SubscriptionRuntime string
	RulesCache          string
	RuntimeProfilePath  string
	ValidationCache     string
	CorePath            string
	WorkDir             string
	BackupDir           string
	Operations          []Operation
	BaseHashes          map[string]string
	BaseRegistryHash    string
	Test                bool
	Generation          int64
	Now                 time.Time
}

type ApplyResult struct {
	Applied          bool                   `json:"applied"`
	Valid            bool                   `json:"valid"`
	Generation       int64                  `json:"generation,omitempty"`
	Operations       []Operation            `json:"operations"`
	BaseRegistryHash string                 `json:"base_registry_hash,omitempty"`
	RegistryHash     string                 `json:"registry_hash"`
	Impact           Impact                 `json:"impact"`
	ConfigPath       string                 `json:"config_path"`
	SelectionPath    string                 `json:"selection_path"`
	OutputPath       string                 `json:"output_path"`
	Build            BuildResult            `json:"build"`
	Backups          []BackupResult         `json:"backups,omitempty"`
	Transaction      ApplyTransactionResult `json:"transaction"`
	Warnings         []string               `json:"warnings,omitempty"`
	NextActions      []string               `json:"next_actions,omitempty"`
}

type BuildResult struct {
	ConfigPath    string                      `json:"config_path,omitempty"`
	SelectionPath string                      `json:"selection_path,omitempty"`
	OutputPath    string                      `json:"output_path,omitempty"`
	Resolved      bool                        `json:"resolved"`
	Rendered      bool                        `json:"rendered"`
	Render        configrender.Result         `json:"render,omitempty"`
	MihomoTest    mihomotest.ValidationResult `json:"mihomo_test,omitempty"`
	Warnings      []string                    `json:"warnings,omitempty"`
}

type BackupResult struct {
	Source string `json:"source"`
	Backup string `json:"backup"`
}

type ApplyTransactionResult struct {
	Prepared bool     `json:"prepared"`
	Atomic   bool     `json:"atomic"`
	Targets  []string `json:"targets"`
}

type ImportTemplateOptions struct {
	RegistryDir         string
	PolicyTemplatesDir  string
	PolicyTemplate      string
	ResetPatches        bool
	RefreshTemplateOnly bool
	ConfigPath          string
	SelectionPath       string
	OutputPath          string
	Subscription        string
	SubscriptionConfig  string
	SubscriptionRuntime string
	RulesCache          string
	RuntimeProfilePath  string
	CorePath            string
	WorkDir             string
	ValidationCache     string
	Now                 time.Time
}

type ImportTemplateResult struct {
	Imported            bool                   `json:"imported"`
	ResetPatches        bool                   `json:"reset_patches"`
	RefreshTemplateOnly bool                   `json:"refresh_policy_template_patches"`
	Template            policytemplate.Summary `json:"template"`
	Patches             []PatchSummary         `json:"patches"`
	RegistryHash        string                 `json:"registry_hash"`
	ConfigPath          string                 `json:"config_path"`
	SelectionPath       string                 `json:"selection_path,omitempty"`
	OutputPath          string                 `json:"output_path,omitempty"`
	Build               BuildResult            `json:"build,omitempty"`
	Warnings            []string               `json:"warnings,omitempty"`
	NextActions         []string               `json:"next_actions,omitempty"`
}

type compiledRegistry struct {
	Config       localconfig.Config
	Records      []Record
	RegistryHash string
	Provenance   map[string][]string
}

func Load(dir string) (Registry, error) {
	dir = defaultRegistryDir(dir)
	registry := Registry{Dir: dir, ByID: map[string]Record{}}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return registry, nil
		}
		return registry, err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return registry, err
		}
		var patch Patch
		if err := json.Unmarshal(data, &patch); err != nil {
			return registry, fmt.Errorf("%s: %w", path, err)
		}
		record := Record{
			Patch:    normalizePatch(patch),
			Path:     path,
			SHA256:   canonicalHash(normalizePatch(patch)),
			Provides: providesForOverlay(patch.Overlay),
		}
		registry.Records = append(registry.Records, record)
	}
	if err := validateRecords(registry.Records, true); err != nil {
		return registry, err
	}
	sortRecordsForInventory(registry.Records)
	registry.ByID = byID(registry.Records)
	return registry, nil
}

func InventoryFor(dir, policyTemplate, configPath, selectionPath, outputPath string, limit int) Inventory {
	registry, err := Load(dir)
	out := Inventory{
		Artifacts: Artifacts{
			PatchRegistry: defaultRegistryDir(dir),
			FinalIntent:   configPath,
			Selection:     selectionPath,
			Generated:     outputPath,
		},
	}
	if err != nil {
		out.Error = err.Error()
		return out
	}
	out.RegistryHash = registry.Hash(policyTemplate)
	for _, record := range registry.Records {
		out.Patches = append(out.Patches, summaryForRecord(record))
		if limit > 0 && len(out.Patches) >= limit {
			break
		}
	}
	if out.Patches == nil {
		out.Patches = []PatchSummary{}
	}
	return out
}

func Get(dir, policyTemplate, patchID string) (GetResult, error) {
	registry, err := Load(dir)
	if err != nil {
		return GetResult{}, err
	}
	record, ok := registry.ByID[strings.TrimSpace(patchID)]
	if !ok {
		return GetResult{}, fmt.Errorf("patch %q not found", patchID)
	}
	patch := record.Patch
	return GetResult{
		PatchID:      patch.PatchID,
		Title:        patch.Title,
		Source:       patch.Source,
		SourceRef:    patch.SourceRef,
		Status:       patch.Status,
		OrderID:      patch.OrderID,
		Summary:      patch.Summary,
		SHA256:       record.SHA256,
		RegistryHash: registry.Hash(policyTemplate),
		Path:         record.Path,
		Overlay:      patch.Overlay,
		Provides:     append([]string{}, record.Provides...),
		ReferencedBy: []string{},
		NextActions: []string{
			"edit the complete overlay returned here before calling config_patch_draft with op=upsert_patch",
			"include base_patch_sha256 when updating this patch",
		},
	}, nil
}

func (registry Registry) Hash(policyTemplate string) string {
	entries := make([]registryHashEntry, 0, len(registry.Records))
	for _, record := range registry.Records {
		entries = append(entries, registryHashEntry{
			PatchID: record.Patch.PatchID,
			Source:  record.Patch.Source,
			Status:  record.Patch.Status,
			OrderID: record.Patch.OrderID,
			SHA256:  record.SHA256,
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].PatchID < entries[j].PatchID })
	return canonicalHash(map[string]any{
		"builder_schema_version": BuilderSchemaVersion,
		"policy_template":        strings.TrimSpace(policyTemplate),
		"patches":                entries,
	})
}

type registryHashEntry struct {
	PatchID string `json:"patch_id"`
	Source  string `json:"source"`
	Status  string `json:"status"`
	OrderID string `json:"order_id"`
	SHA256  string `json:"sha256"`
}

func Draft(ctx context.Context, opts DraftOptions) (DraftResult, error) {
	opts = normalizeDraftOptions(opts)
	if len(opts.Operations) == 0 {
		return DraftResult{}, fmt.Errorf("operations is required")
	}
	registry, err := Load(opts.RegistryDir)
	if err != nil {
		return DraftResult{}, err
	}
	baseRegistryHash := registry.Hash(opts.PolicyTemplate)
	records, operations, baseHashes, impact, err := previewOperations(registry, opts.Operations)
	if err != nil {
		return DraftResult{}, err
	}
	pending := Registry{Dir: registry.Dir, Records: records, ByID: byID(records)}
	compiled, err := compileRecords(pending, opts.PolicyTemplate, opts.Now)
	if err != nil {
		return DraftResult{}, err
	}
	result := DraftResult{
		Valid:            true,
		CurrentDraft:     CurrentDraft{Generation: opts.Generation},
		DraftName:        opts.DraftName,
		Impact:           impact,
		Operations:       operations,
		BaseHashes:       baseHashes,
		BaseRegistryHash: baseRegistryHash,
		RegistryHash:     compiled.RegistryHash,
		ApplyArgs: map[string]any{
			"use_current_draft": true,
			"generation":        opts.Generation,
		},
		ExplicitApplyArgs: map[string]any{
			"operations":         operations,
			"base_hashes":        baseHashes,
			"base_registry_hash": baseRegistryHash,
			"generation":         opts.Generation,
		},
		NextActions: []string{"review this draft, then call config_patch_apply with apply_args"},
	}
	if opts.Test {
		build, err := buildArtifacts(ctx, compiled.Config, buildOptionsFromDraft(opts, false))
		defer cleanupTempBuild(build)
		result.Validation = &build
		if err != nil {
			return result, err
		}
		result.Warnings = append(result.Warnings, build.Warnings...)
	}
	return result, nil
}

func Apply(ctx context.Context, opts ApplyOptions) (ApplyResult, error) {
	opts = normalizeApplyOptions(opts)
	if len(opts.Operations) == 0 {
		return ApplyResult{}, fmt.Errorf("operations is required")
	}
	applyMu.Lock()
	defer applyMu.Unlock()

	registry, err := Load(opts.RegistryDir)
	if err != nil {
		return ApplyResult{}, err
	}
	if strings.TrimSpace(opts.BaseRegistryHash) == "" {
		return ApplyResult{}, fmt.Errorf("base_registry_hash is required; call config_patch_draft before config_patch_apply")
	}
	currentHash := registry.Hash(opts.PolicyTemplate)
	if currentHash != opts.BaseRegistryHash {
		return ApplyResult{}, fmt.Errorf("stale patch registry: current registry_hash %s does not match base_registry_hash %s; rerun config_patch_draft", currentHash, opts.BaseRegistryHash)
	}
	if err := verifyBaseHashes(registry, opts.BaseHashes); err != nil {
		return ApplyResult{}, err
	}
	records, operations, _, impact, err := previewOperations(registry, opts.Operations)
	if err != nil {
		return ApplyResult{}, err
	}
	pending := Registry{Dir: registry.Dir, Records: records, ByID: byID(records)}
	compiled, err := compileRecords(pending, opts.PolicyTemplate, opts.Now)
	if err != nil {
		return ApplyResult{}, err
	}
	build, err := buildArtifacts(ctx, compiled.Config, buildOptionsFromApply(opts, false))
	if err != nil {
		return ApplyResult{Valid: false, Operations: operations, BaseRegistryHash: opts.BaseRegistryHash, RegistryHash: compiled.RegistryHash, Impact: impact, Build: build}, err
	}
	defer cleanupTempBuild(build)
	backups, err := backupTargets(registry, pending, opts)
	if err != nil {
		return ApplyResult{Valid: false, Operations: operations, BaseRegistryHash: opts.BaseRegistryHash, RegistryHash: compiled.RegistryHash, Impact: impact, Build: build}, err
	}
	transaction, err := commitTargets(compiled.Config, build, registry, pending, opts, backups)
	if err != nil {
		_ = rollbackBackups(backups)
		removeUnbackedTargets(transaction.Targets, backups)
		return ApplyResult{Valid: false, Operations: operations, BaseRegistryHash: opts.BaseRegistryHash, RegistryHash: compiled.RegistryHash, Impact: impact, Build: build, Backups: backups, Transaction: transaction}, err
	}
	build.SelectionPath = opts.SelectionPath
	build.OutputPath = opts.OutputPath
	build.Render.OutputPath = opts.OutputPath
	return ApplyResult{
		Applied:          true,
		Valid:            true,
		Generation:       opts.Generation,
		Operations:       operations,
		BaseRegistryHash: opts.BaseRegistryHash,
		RegistryHash:     compiled.RegistryHash,
		Impact:           impact,
		ConfigPath:       opts.ConfigPath,
		SelectionPath:    opts.SelectionPath,
		OutputPath:       opts.OutputPath,
		Build:            build,
		Backups:          backups,
		Transaction:      transaction,
		NextActions: []string{
			"call config_status with patches=true to verify patch registry and compiled artifacts",
			"call routing_explain for behavior-level verification",
			"restart_runtime only after user confirmation if the running Mihomo process should load the change",
		},
	}, nil
}

func ImportPolicyTemplate(ctx context.Context, opts ImportTemplateOptions) (ImportTemplateResult, error) {
	opts = normalizeImportOptions(opts)
	if opts.ResetPatches && opts.RefreshTemplateOnly {
		return ImportTemplateResult{}, errors.New("reset_patches and refresh_policy_template_patches are mutually exclusive")
	}
	sources, summary, err := policytemplate.PatchSources(opts.PolicyTemplatesDir, opts.PolicyTemplate)
	if err != nil {
		return ImportTemplateResult{}, err
	}
	if opts.ResetPatches {
		if err := removeRegistryFiles(opts.RegistryDir); err != nil {
			return ImportTemplateResult{}, err
		}
	} else if opts.RefreshTemplateOnly {
		if err := removePolicyTemplateRegistryFiles(opts.RegistryDir); err != nil {
			return ImportTemplateResult{}, err
		}
	}
	if err := os.MkdirAll(opts.RegistryDir, 0o755); err != nil {
		return ImportTemplateResult{}, err
	}
	for _, source := range sources {
		patch := patchFromTemplateSource(summary.ID, source)
		if err := writePatch(opts.RegistryDir, patch); err != nil {
			return ImportTemplateResult{}, err
		}
	}
	registry, err := Load(opts.RegistryDir)
	if err != nil {
		return ImportTemplateResult{}, err
	}
	compiled, err := compileRecords(registry, summary.ID, opts.Now)
	if err != nil {
		return ImportTemplateResult{}, err
	}
	if err := localconfig.Write(opts.ConfigPath, compiled.Config); err != nil {
		return ImportTemplateResult{}, err
	}
	result := ImportTemplateResult{
		Imported:            true,
		ResetPatches:        opts.ResetPatches,
		RefreshTemplateOnly: opts.RefreshTemplateOnly,
		Template:            summary,
		RegistryHash:        compiled.RegistryHash,
		ConfigPath:          opts.ConfigPath,
		SelectionPath:       opts.SelectionPath,
		OutputPath:          opts.OutputPath,
	}
	for _, record := range registry.Records {
		result.Patches = append(result.Patches, summaryForRecord(record))
	}
	if canRenderOptionalArtifacts(opts) {
		build, err := buildArtifacts(ctx, compiled.Config, buildOptionsFromImport(opts))
		result.Build = build
		if err != nil {
			result.Warnings = append(result.Warnings, "generated artifact build skipped: "+err.Error())
		} else {
			result.Warnings = append(result.Warnings, build.Warnings...)
		}
	} else {
		result.NextActions = append(result.NextActions, "call subscriptions_refresh and config_render after subscription/runtime inputs are available")
	}
	return result, nil
}

func Compile(dir, policyTemplate string, now time.Time) (localconfig.Config, string, error) {
	registry, err := Load(dir)
	if err != nil {
		return localconfig.Config{}, "", err
	}
	compiled, err := compileRecords(registry, policyTemplate, now)
	if err != nil {
		return localconfig.Config{}, "", err
	}
	return compiled.Config, compiled.RegistryHash, nil
}

func compileRecords(registry Registry, policyTemplate string, now time.Time) (compiledRegistry, error) {
	if now.IsZero() {
		now = time.Now()
	}
	records := append([]Record{}, registry.Records...)
	if err := validateRecords(records, false); err != nil {
		return compiledRegistry{}, err
	}
	registryHash := (Registry{Dir: registry.Dir, Records: records}).Hash(policyTemplate)
	sort.Slice(records, func(i, j int) bool {
		if records[i].Patch.OrderID == records[j].Patch.OrderID {
			return records[i].Patch.PatchID < records[j].Patch.PatchID
		}
		return records[i].Patch.OrderID < records[j].Patch.OrderID
	})
	config := localconfig.Config{
		Version:          localconfig.ConfigSchemaVersion,
		PolicyTemplate:   strings.TrimSpace(policyTemplate),
		FallbackTarget:   rules.TerminalDirect,
		ProxyGroups:      map[string]localconfig.ProxyGroup{},
		PolicyGroups:     map[string]localconfig.PolicyGroup{},
		TransportRules:   []localconfig.TransportRule{},
		CustomRules:      []localconfig.CustomRule{},
		EnabledRulePacks: []localconfig.RulePackSelection{},
		RuleProviders:    []localconfig.ExternalRuleProvider{},
		Packs:            []localconfig.Pack{},
	}
	provenance := map[string][]string{}
	indexes := newMergeIndexes()
	enabledCount := 0
	tombstonedCount := 0
	for _, record := range records {
		switch record.Patch.Status {
		case StatusEnabled:
			enabledCount++
		case StatusTombstoned:
			tombstonedCount++
			continue
		default:
			continue
		}
		if err := mergeOverlay(&config, indexes, provenance, record); err != nil {
			return compiledRegistry{}, err
		}
	}
	generatedPatches := make([]localconfig.GeneratedPatch, 0, len(records))
	for _, record := range records {
		generatedPatches = append(generatedPatches, localconfig.GeneratedPatch{
			PatchID: record.Patch.PatchID,
			Source:  record.Patch.Source,
			Status:  record.Patch.Status,
			OrderID: record.Patch.OrderID,
			SHA256:  record.SHA256,
		})
	}
	config.Generated = &localconfig.GeneratedMetadata{
		Source:            "patch_registry",
		RegistryDir:       registry.Dir,
		RegistryHash:      registryHash,
		BuilderVersion:    BuilderSchemaVersion,
		BuiltAt:           now.UTC().Format(time.RFC3339),
		PolicyTemplate:    strings.TrimSpace(policyTemplate),
		PatchCount:        len(records),
		EnabledPatchCount: enabledCount,
		TombstonedCount:   tombstonedCount,
		Patches:           generatedPatches,
		Provenance:        provenance,
	}
	return compiledRegistry{Config: config, Records: records, RegistryHash: registryHash, Provenance: provenance}, nil
}

type mergeIndexes struct {
	proxyGroups      map[string]string
	policyGroups     map[string]string
	packs            map[string]string
	transportRules   map[string]string
	customRules      map[string]string
	enabledRulePacks map[string]string
	ruleProviders    map[string]string
}

func newMergeIndexes() mergeIndexes {
	return mergeIndexes{
		proxyGroups:      map[string]string{},
		policyGroups:     map[string]string{},
		packs:            map[string]string{},
		transportRules:   map[string]string{},
		customRules:      map[string]string{},
		enabledRulePacks: map[string]string{},
		ruleProviders:    map[string]string{},
	}
}

func mergeOverlay(config *localconfig.Config, indexes mergeIndexes, provenance map[string][]string, record Record) error {
	patchID := record.Patch.PatchID
	overlay := record.Patch.Overlay
	for _, group := range overlay.ProxyGroups {
		id := strings.TrimSpace(group.ID)
		value := localconfig.ProxyGroup{Mode: group.Mode, Match: group.Match, Nodes: append([]string{}, group.Nodes...), Optional: group.Optional, Reason: group.Reason, Boundary: group.Boundary}
		if err := putMapObject(config.ProxyGroups, indexes.proxyGroups, provenance, "proxy_groups["+id+"]", patchID, id, value); err != nil {
			return err
		}
	}
	for _, group := range overlay.PolicyGroups {
		id := strings.TrimSpace(group.ID)
		value := localconfig.PolicyGroup{Mode: group.Mode, Exits: append([]string{}, group.Exits...), Reason: group.Reason, Boundary: group.Boundary}
		if err := putMapObject(config.PolicyGroups, indexes.policyGroups, provenance, "policy_groups["+id+"]", patchID, id, value); err != nil {
			return err
		}
	}
	for _, pack := range overlay.Packs {
		value := localconfig.Pack{Source: pack.Source, Pack: pack.Pack, Type: pack.Type, Target: pack.Target, Reason: pack.Reason}
		key := rules.PackKey(value.Source, value.Pack)
		if err := appendUniqueObject(&config.Packs, indexes.packs, provenance, "packs["+key+"]", patchID, key, value); err != nil {
			return err
		}
	}
	for _, rule := range overlay.TransportRules {
		key := strings.TrimSpace(rule.ID)
		if err := appendUniqueObject(&config.TransportRules, indexes.transportRules, provenance, "transport_rules["+key+"]", patchID, key, rule); err != nil {
			return err
		}
	}
	for _, custom := range overlay.CustomRules {
		key := strings.TrimSpace(custom.ID)
		if err := appendUniqueObject(&config.CustomRules, indexes.customRules, provenance, "custom_rules["+key+"]", patchID, key, custom); err != nil {
			return err
		}
	}
	for _, pack := range overlay.EnabledRulePacks {
		key := strings.TrimSpace(pack.ID)
		if err := appendUniqueObject(&config.EnabledRulePacks, indexes.enabledRulePacks, provenance, "enabled_rule_packs["+key+"]", patchID, key, pack); err != nil {
			return err
		}
	}
	for _, provider := range overlay.RuleProviders {
		key := strings.TrimSpace(provider.ID)
		if err := appendUniqueObject(&config.RuleProviders, indexes.ruleProviders, provenance, "rule_providers["+key+"]", patchID, key, provider); err != nil {
			return err
		}
	}
	return nil
}

func putMapObject[T any](target map[string]T, index map[string]string, provenance map[string][]string, provKey, patchID, key string, value T) error {
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("%s has empty id", provKey)
	}
	hash := canonicalHash(value)
	if existing, ok := index[key]; ok {
		if existing != hash {
			return fmt.Errorf("patch overlay conflict for %s", provKey)
		}
		provenance[provKey] = appendUnique(provenance[provKey], patchID)
		return nil
	}
	index[key] = hash
	target[key] = value
	provenance[provKey] = appendUnique(provenance[provKey], patchID)
	return nil
}

func appendUniqueObject[T any](target *[]T, index map[string]string, provenance map[string][]string, provKey, patchID, key string, value T) error {
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("%s has empty id", provKey)
	}
	hash := canonicalHash(value)
	if existing, ok := index[key]; ok {
		if existing != hash {
			return fmt.Errorf("patch overlay conflict for %s", provKey)
		}
		provenance[provKey] = appendUnique(provenance[provKey], patchID)
		return nil
	}
	index[key] = hash
	*target = append(*target, value)
	provenance[provKey] = appendUnique(provenance[provKey], patchID)
	return nil
}

func previewOperations(registry Registry, ops []Operation) ([]Record, []Operation, map[string]string, Impact, error) {
	records := append([]Record{}, registry.Records...)
	recordsByID := byID(records)
	baseHashes := map[string]string{}
	changed := map[string]bool{}
	removed := map[string]bool{}
	beforeEnabled := enabledCount(records)
	normalized := make([]Operation, 0, len(ops))
	for _, op := range ops {
		op.Op = strings.TrimSpace(op.Op)
		op.PatchID = strings.TrimSpace(op.PatchID)
		if op.Op == "" {
			return nil, nil, nil, Impact{}, fmt.Errorf("operations[].op is required")
		}
		if op.PatchID == "" {
			return nil, nil, nil, Impact{}, fmt.Errorf("%s requires patch_id", op.Op)
		}
		record, exists := recordsByID[op.PatchID]
		if exists {
			baseHashes[op.PatchID] = record.SHA256
			if op.BasePatchSHA256 != "" && op.BasePatchSHA256 != record.SHA256 {
				return nil, nil, nil, Impact{}, fmt.Errorf("stale patch %q: current sha256 %s does not match base_patch_sha256 %s", op.PatchID, record.SHA256, op.BasePatchSHA256)
			}
		} else {
			baseHashes[op.PatchID] = ""
			if op.BasePatchSHA256 != "" {
				return nil, nil, nil, Impact{}, fmt.Errorf("stale patch %q: patch does not exist but base_patch_sha256 was supplied", op.PatchID)
			}
		}
		switch op.Op {
		case "upsert_patch":
			patch, err := patchFromUpsert(op, record, exists, records)
			if err != nil {
				return nil, nil, nil, Impact{}, err
			}
			if err := validatePatch(patch); err != nil {
				return nil, nil, nil, Impact{}, err
			}
			records = upsertRecord(records, Record{Patch: patch, Path: filepath.Join(registry.Dir, mustCanonicalFilename(patch)), SHA256: canonicalHash(patch), Provides: providesForOverlay(patch.Overlay)})
			changed[patch.PatchID] = true
		case "remove_patch":
			if !exists {
				return nil, nil, nil, Impact{}, fmt.Errorf("patch %q not found", op.PatchID)
			}
			if record.Patch.Source == SourcePolicyTemplate {
				record.Patch.Status = StatusTombstoned
				record.SHA256 = canonicalHash(record.Patch)
				records = upsertRecord(records, record)
				changed[op.PatchID] = true
			} else {
				records = removeRecord(records, op.PatchID)
				removed[op.PatchID] = true
			}
		case "set_patch_status":
			if !exists {
				return nil, nil, nil, Impact{}, fmt.Errorf("patch %q not found", op.PatchID)
			}
			status := strings.TrimSpace(op.Status)
			if !validStatus(status) {
				return nil, nil, nil, Impact{}, fmt.Errorf("unsupported patch status %q", status)
			}
			record.Patch.Status = status
			record.SHA256 = canonicalHash(record.Patch)
			records = upsertRecord(records, record)
			changed[op.PatchID] = true
		case "reorder_patch":
			if !exists {
				return nil, nil, nil, Impact{}, fmt.Errorf("patch %q not found", op.PatchID)
			}
			orderID := strings.TrimSpace(op.OrderID)
			if orderID == "" {
				var err error
				orderID, err = midpointOrderID(recordsByID, op.AfterPatchID, op.BeforePatchID)
				if err != nil {
					return nil, nil, nil, Impact{}, err
				}
			}
			if !validOrderID(orderID) {
				return nil, nil, nil, Impact{}, fmt.Errorf("invalid order_id %q", orderID)
			}
			record.Patch.OrderID = orderID
			record.SHA256 = canonicalHash(record.Patch)
			records = upsertRecord(records, record)
			op.NormalizedOrderID = orderID
			changed[op.PatchID] = true
		default:
			return nil, nil, nil, Impact{}, fmt.Errorf("unsupported patch operation %q", op.Op)
		}
		normalized = append(normalized, op)
		recordsByID = byID(records)
	}
	if err := validateRecords(records, false); err != nil {
		return nil, nil, nil, Impact{}, err
	}
	impact := Impact{EnabledBefore: beforeEnabled, EnabledAfter: enabledCount(records)}
	for id := range changed {
		impact.PatchesChanged = append(impact.PatchesChanged, id)
	}
	for id := range removed {
		impact.PatchesRemoved = append(impact.PatchesRemoved, id)
	}
	sort.Strings(impact.PatchesChanged)
	sort.Strings(impact.PatchesRemoved)
	return records, normalized, baseHashes, impact, nil
}

func patchFromUpsert(op Operation, existing Record, exists bool, records []Record) (Patch, error) {
	source := firstNonEmpty(op.Source, SourceUser)
	status := firstNonEmpty(op.Status, StatusEnabled)
	title := firstNonEmpty(op.Title, op.Summary, op.PatchID)
	orderID := strings.TrimSpace(op.OrderID)
	if exists {
		source = firstNonEmpty(op.Source, existing.Patch.Source, SourceUser)
		status = firstNonEmpty(op.Status, existing.Patch.Status, StatusEnabled)
		title = firstNonEmpty(op.Title, existing.Patch.Title, op.Summary, op.PatchID)
		if orderID == "" {
			orderID = existing.Patch.OrderID
		}
	}
	if orderID == "" {
		var err error
		orderID, err = nextUserOrderID(records)
		if err != nil {
			return Patch{}, err
		}
	}
	return normalizePatch(Patch{
		Version:   PatchVersion,
		PatchID:   op.PatchID,
		Title:     title,
		Source:    source,
		SourceRef: op.SourceRef,
		Status:    status,
		OrderID:   orderID,
		Summary:   op.Summary,
		Overlay:   op.Overlay,
	}), nil
}

func verifyBaseHashes(registry Registry, hashes map[string]string) error {
	for patchID, expected := range hashes {
		record, exists := registry.ByID[patchID]
		if expected == "" {
			if exists {
				return fmt.Errorf("stale patch %q: expected patch to be absent, current sha256 is %s", patchID, record.SHA256)
			}
			continue
		}
		if !exists {
			return fmt.Errorf("stale patch %q: expected sha256 %s but patch is absent", patchID, expected)
		}
		if record.SHA256 != expected {
			return fmt.Errorf("stale patch %q: current sha256 %s does not match expected %s", patchID, record.SHA256, expected)
		}
	}
	return nil
}

type buildOptions struct {
	ConfigPath          string
	SelectionPath       string
	OutputPath          string
	Subscription        string
	SubscriptionConfig  string
	SubscriptionRuntime string
	RulesCache          string
	RuntimeProfilePath  string
	ValidationCache     string
	CorePath            string
	WorkDir             string
	Test                bool
	TempOnly            bool
}

func buildArtifacts(ctx context.Context, config localconfig.Config, opts buildOptions) (BuildResult, error) {
	result := BuildResult{}
	resolved, err := localconfig.Resolve(localconfig.ResolveOptions{
		Config:              config,
		SubscriptionPath:    opts.Subscription,
		SubscriptionConfig:  opts.SubscriptionConfig,
		SubscriptionRuntime: opts.SubscriptionRuntime,
		RulesCache:          opts.RulesCache,
	})
	if err != nil {
		return result, err
	}
	result.Resolved = true
	result.Warnings = append(result.Warnings, resolved.Warnings...)
	targetSelection := opts.SelectionPath
	targetOutput := opts.OutputPath
	tempDir := ""
	if opts.TempOnly {
		var err error
		tempDir, err = os.MkdirTemp("", "localclash-configpatch-build-*")
		if err != nil {
			return result, err
		}
		targetSelection = filepath.Join(tempDir, "localclash-packs.gob")
		targetOutput = filepath.Join(tempDir, "mihomo.yaml")
	}
	if err := localconfig.WriteSelection(targetSelection, resolved.Selection); err != nil {
		return result, err
	}
	renderResult, err := configrender.Render(configrender.Options{
		SourcePath:         opts.Subscription,
		OutputPath:         targetOutput,
		PacksSelectionPath: targetSelection,
		RulesCacheDir:      opts.RulesCache,
		RuntimeProfilePath: opts.RuntimeProfilePath,
		Force:              true,
	})
	if err != nil {
		return result, err
	}
	result.Rendered = true
	result.Render = renderResult
	result.ConfigPath = opts.ConfigPath
	result.SelectionPath = targetSelection
	result.OutputPath = targetOutput
	if opts.Test {
		corePath := opts.CorePath
		if strings.TrimSpace(corePath) == "" {
			if active, err := runtimeprofile.ActiveCorePath(opts.RuntimeProfilePath); err == nil {
				corePath = active
			}
		}
		validation, err := mihomotest.ValidateCached(ctx, mihomotest.ValidationOptions{
			CorePath:   corePath,
			ConfigPath: targetOutput,
			WorkDir:    opts.WorkDir,
			CachePath:  opts.ValidationCache,
			Force:      true,
		})
		result.MihomoTest = validation
		if err != nil {
			return result, err
		}
	}
	return result, nil
}

func cleanupTempBuild(build BuildResult) {
	for _, path := range []string{build.SelectionPath, build.OutputPath} {
		if strings.Contains(filepath.Base(filepath.Dir(path)), "localclash-configpatch-build-") {
			_ = os.RemoveAll(filepath.Dir(path))
			return
		}
	}
}

func backupTargets(current, pending Registry, opts ApplyOptions) ([]BackupResult, error) {
	seen := map[string]bool{}
	paths := []string{opts.ConfigPath, opts.SelectionPath, opts.OutputPath}
	for _, record := range current.Records {
		paths = append(paths, record.Path)
	}
	for _, record := range pending.Records {
		paths = append(paths, filepath.Join(opts.RegistryDir, mustCanonicalFilename(record.Patch)))
	}
	sort.Strings(paths)
	var backups []BackupResult
	stamp := opts.Now.UTC().Format("20060102-150405")
	if err := os.MkdirAll(opts.BackupDir, 0o755); err != nil {
		return nil, err
	}
	for _, path := range paths {
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return backups, err
		}
		backup := filepath.Join(opts.BackupDir, stamp, sanitizeBackupName(path))
		if err := os.MkdirAll(filepath.Dir(backup), 0o755); err != nil {
			return backups, err
		}
		if err := os.WriteFile(backup, data, 0o644); err != nil {
			return backups, err
		}
		backups = append(backups, BackupResult{Source: path, Backup: backup})
	}
	return backups, nil
}

func commitTargets(config localconfig.Config, build BuildResult, current, pending Registry, opts ApplyOptions, backups []BackupResult) (ApplyTransactionResult, error) {
	transaction := ApplyTransactionResult{Prepared: true, Atomic: true}
	if err := os.MkdirAll(opts.RegistryDir, 0o755); err != nil {
		return transaction, err
	}
	stagingRoot := filepath.Dir(opts.ConfigPath)
	if stagingRoot == "" || stagingRoot == "." {
		stagingRoot = "."
	}
	if err := os.MkdirAll(stagingRoot, 0o755); err != nil {
		return transaction, err
	}
	stagingDir, err := os.MkdirTemp(stagingRoot, ".configpatch-apply-*")
	if err != nil {
		return transaction, err
	}
	defer os.RemoveAll(stagingDir)

	pendingIDs := map[string]Record{}
	pendingTargets := map[string]string{}
	for _, record := range pending.Records {
		pendingIDs[record.Patch.PatchID] = record
		pendingTargets[record.Patch.PatchID] = filepath.Join(opts.RegistryDir, mustCanonicalFilename(record.Patch))
	}
	type stagedTarget struct {
		staged string
		target string
	}
	var stagedTargets []stagedTarget
	for _, record := range pending.Records {
		target := filepath.Join(opts.RegistryDir, mustCanonicalFilename(record.Patch))
		data, err := patchJSON(record.Patch)
		if err != nil {
			return transaction, err
		}
		staged, err := writeStagedFile(stagingDir, "patch-", data)
		if err != nil {
			return transaction, err
		}
		stagedTargets = append(stagedTargets, stagedTarget{staged: staged, target: target})
	}
	configStage := filepath.Join(stagingDir, "localclash-intent.json")
	if err := localconfig.Write(configStage, config); err != nil {
		return transaction, err
	}
	stagedTargets = append(stagedTargets, stagedTarget{staged: configStage, target: opts.ConfigPath})
	selectionStage := filepath.Join(stagingDir, "localclash-packs.gob")
	if err := copyFile(build.SelectionPath, selectionStage); err != nil {
		return transaction, err
	}
	stagedTargets = append(stagedTargets, stagedTarget{staged: selectionStage, target: opts.SelectionPath})
	outputStage := filepath.Join(stagingDir, "mihomo.yaml")
	if err := copyFile(build.OutputPath, outputStage); err != nil {
		return transaction, err
	}
	stagedTargets = append(stagedTargets, stagedTarget{staged: outputStage, target: opts.OutputPath})

	for _, record := range current.Records {
		pendingTarget, ok := pendingTargets[record.Patch.PatchID]
		if !ok || (record.Path != "" && record.Path != pendingTarget) {
			if err := os.Remove(record.Path); err != nil && !os.IsNotExist(err) {
				return transaction, err
			}
			transaction.Targets = append(transaction.Targets, record.Path)
			_ = syncDir(filepath.Dir(record.Path))
		}
	}
	for _, item := range stagedTargets {
		if err := os.MkdirAll(filepath.Dir(item.target), 0o755); err != nil {
			return transaction, err
		}
		if err := os.Rename(item.staged, item.target); err != nil {
			return transaction, err
		}
		transaction.Targets = append(transaction.Targets, item.target)
		if err := syncDir(filepath.Dir(item.target)); err != nil {
			return transaction, err
		}
	}
	return transaction, nil
}

func writeStagedFile(dir, pattern string, data []byte) (string, error) {
	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	path := file.Name()
	writeErr := func() error {
		if _, err := file.Write(data); err != nil {
			return err
		}
		if err := file.Sync(); err != nil {
			return err
		}
		return nil
	}()
	closeErr := file.Close()
	if writeErr != nil {
		_ = os.Remove(path)
		return "", writeErr
	}
	if closeErr != nil {
		_ = os.Remove(path)
		return "", closeErr
	}
	return path, nil
}

func copyFile(source, target string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	writeErr := func() error {
		if _, err := file.Write(data); err != nil {
			return err
		}
		return file.Sync()
	}()
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

func syncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func rollbackBackups(backups []BackupResult) error {
	var errs []string
	for _, backup := range backups {
		data, err := os.ReadFile(backup.Backup)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		if err := os.MkdirAll(filepath.Dir(backup.Source), 0o755); err != nil {
			errs = append(errs, err.Error())
			continue
		}
		if err := os.WriteFile(backup.Source, data, 0o644); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func removeUnbackedTargets(targets []string, backups []BackupResult) {
	backed := map[string]bool{}
	for _, backup := range backups {
		backed[backup.Source] = true
	}
	for _, target := range targets {
		if target == "" || backed[target] {
			continue
		}
		_ = os.Remove(target)
	}
}

func patchFromTemplateSource(templateID string, source policytemplate.PatchSource) Patch {
	return normalizePatch(Patch{
		Version:   PatchVersion,
		PatchID:   source.ID,
		Title:     titleFromPatchSource(source),
		Source:    SourcePolicyTemplate,
		SourceRef: source.Path,
		Status:    StatusEnabled,
		OrderID:   templateOrderID(source.ID, source.Index),
		Summary:   source.Description,
		Overlay:   overlayFromConfig(source.Config),
	})
}

func overlayFromConfig(config localconfig.Config) configplan.OverlayIntent {
	overlay := configplan.OverlayIntent{
		Packs:            make([]configplan.OverlayPackIntent, 0, len(config.Packs)),
		TransportRules:   append([]configplan.OverlayTransportRuleIntent{}, config.TransportRules...),
		CustomRules:      append([]configplan.OverlayCustomRuleIntent{}, config.CustomRules...),
		EnabledRulePacks: append([]configplan.OverlayRulePackIntent{}, config.EnabledRulePacks...),
		RuleProviders:    append([]configplan.OverlayRuleProviderIntent{}, config.RuleProviders...),
		ProxyGroups:      make([]configplan.OverlayProxyGroupIntent, 0, len(config.ProxyGroups)),
		PolicyGroups:     make([]configplan.OverlayPolicyGroupIntent, 0, len(config.PolicyGroups)),
	}
	proxyIDs := make([]string, 0, len(config.ProxyGroups))
	for id := range config.ProxyGroups {
		proxyIDs = append(proxyIDs, id)
	}
	sort.Strings(proxyIDs)
	for _, id := range proxyIDs {
		group := config.ProxyGroups[id]
		overlay.ProxyGroups = append(overlay.ProxyGroups, configplan.OverlayProxyGroupIntent{
			ID:       id,
			Nodes:    append([]string{}, group.Nodes...),
			Match:    group.Match,
			Mode:     group.Mode,
			Optional: group.Optional,
			Reason:   group.Reason,
			Boundary: group.Boundary,
		})
	}
	policyIDs := make([]string, 0, len(config.PolicyGroups))
	for id := range config.PolicyGroups {
		policyIDs = append(policyIDs, id)
	}
	sort.Strings(policyIDs)
	for _, id := range policyIDs {
		group := config.PolicyGroups[id]
		overlay.PolicyGroups = append(overlay.PolicyGroups, configplan.OverlayPolicyGroupIntent{
			ID:       id,
			Mode:     group.Mode,
			Exits:    append([]string{}, group.Exits...),
			Reason:   group.Reason,
			Boundary: group.Boundary,
		})
	}
	for _, pack := range config.Packs {
		overlay.Packs = append(overlay.Packs, configplan.OverlayPackIntent{Source: pack.Source, Pack: pack.Pack, Type: pack.Type, Target: pack.Target, Reason: pack.Reason})
	}
	return overlay
}

func normalizePatch(patch Patch) Patch {
	patch.PatchID = strings.TrimSpace(patch.PatchID)
	patch.Title = strings.TrimSpace(patch.Title)
	patch.Source = firstNonEmpty(patch.Source, SourceUser)
	patch.SourceRef = strings.TrimSpace(patch.SourceRef)
	patch.Status = firstNonEmpty(patch.Status, StatusEnabled)
	patch.OrderID = strings.TrimSpace(patch.OrderID)
	patch.Summary = strings.TrimSpace(patch.Summary)
	if patch.Version == 0 {
		patch.Version = PatchVersion
	}
	if patch.Title == "" {
		patch.Title = patch.PatchID
	}
	return patch
}

func validateRecords(records []Record, checkFilenames bool) error {
	ids := map[string]string{}
	lowerIDs := map[string]string{}
	orderIDs := map[string]string{}
	for _, record := range records {
		patch := normalizePatch(record.Patch)
		if err := validatePatch(patch); err != nil {
			return err
		}
		if other, exists := ids[patch.PatchID]; exists {
			return fmt.Errorf("duplicate patch_id %q in %s and %s", patch.PatchID, other, record.Path)
		}
		ids[patch.PatchID] = record.Path
		lower := strings.ToLower(patch.PatchID)
		if other, exists := lowerIDs[lower]; exists && other != patch.PatchID {
			return fmt.Errorf("case-only duplicate patch_id values %q and %q", other, patch.PatchID)
		}
		lowerIDs[lower] = patch.PatchID
		if patch.Status != StatusTombstoned {
			if other, exists := orderIDs[patch.OrderID]; exists {
				return fmt.Errorf("duplicate order_id %q in patches %q and %q", patch.OrderID, other, patch.PatchID)
			}
			orderIDs[patch.OrderID] = patch.PatchID
		}
		if checkFilenames {
			want, err := canonicalFilename(patch)
			if err != nil {
				return err
			}
			if filepath.Base(record.Path) != want {
				return fmt.Errorf("%s: canonical patch filename must be %s", record.Path, want)
			}
		}
	}
	return nil
}

func validatePatch(patch Patch) error {
	if patch.Version != PatchVersion {
		return fmt.Errorf("patch %q has unsupported version %d", patch.PatchID, patch.Version)
	}
	if !validPatchID(patch.PatchID) {
		return fmt.Errorf("invalid patch_id %q", patch.PatchID)
	}
	if !validStatus(patch.Status) {
		return fmt.Errorf("patch %q has unsupported status %q", patch.PatchID, patch.Status)
	}
	if !validOrderID(patch.OrderID) {
		return fmt.Errorf("patch %q has invalid order_id %q", patch.PatchID, patch.OrderID)
	}
	if strings.Contains(patch.Source, "/") || strings.Contains(patch.Source, "\\") {
		return fmt.Errorf("patch %q has invalid source %q", patch.PatchID, patch.Source)
	}
	return nil
}

func validPatchID(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" || strings.Contains(id, "..") || strings.ContainsAny(id, `/\`) {
		return false
	}
	return patchIDPattern.MatchString(id)
}

func validStatus(status string) bool {
	switch status {
	case StatusEnabled, StatusDisabled, StatusTombstoned:
		return true
	default:
		return false
	}
}

func validOrderID(orderID string) bool {
	return orderIDPattern.MatchString(strings.TrimSpace(orderID))
}

func canonicalFilename(patch Patch) (string, error) {
	if !validPatchID(patch.PatchID) {
		return "", fmt.Errorf("invalid patch_id %q", patch.PatchID)
	}
	return patch.PatchID + "_" + safeTitleSlug(patch.Title) + ".json", nil
}

func mustCanonicalFilename(patch Patch) string {
	name, err := canonicalFilename(patch)
	if err != nil {
		panic(err)
	}
	return name
}

func writePatch(dir string, patch Patch) error {
	patch = normalizePatch(patch)
	if err := validatePatch(patch); err != nil {
		return err
	}
	name, err := canonicalFilename(patch)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := patchJSON(patch)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, name), data, 0o644)
}

func patchJSON(patch Patch) ([]byte, error) {
	patch = normalizePatch(patch)
	if err := validatePatch(patch); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(patch, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func removeRegistryFiles(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func removePolicyTemplateRegistryFiles(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var raw struct {
			PatchID string  `json:"patch_id"`
			Source  *string `json:"source"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if raw.Source == nil {
			return fmt.Errorf("%s: patch source is required to refresh policy template patches without removing user patches", path)
		}
		source := strings.TrimSpace(*raw.Source)
		switch source {
		case SourcePolicyTemplate:
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
		case SourceUser:
			continue
		default:
			return fmt.Errorf("%s: patch %q has unsupported source %q", path, strings.TrimSpace(raw.PatchID), source)
		}
	}
	return nil
}

func canonicalHash(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func providesForOverlay(overlay configplan.OverlayIntent) []string {
	var out []string
	for _, item := range overlay.ProxyGroups {
		out = append(out, "proxy_groups["+strings.TrimSpace(item.ID)+"]")
	}
	for _, item := range overlay.PolicyGroups {
		out = append(out, "policy_groups["+strings.TrimSpace(item.ID)+"]")
	}
	for _, item := range overlay.Packs {
		out = append(out, "packs["+rules.PackKey(item.Source, item.Pack)+" -> "+strings.TrimSpace(item.Target)+"]")
	}
	for _, item := range overlay.TransportRules {
		out = append(out, "transport_rules["+strings.TrimSpace(item.ID)+"]")
	}
	for _, item := range overlay.CustomRules {
		out = append(out, "custom_rules["+strings.TrimSpace(item.ID)+"]")
	}
	for _, item := range overlay.EnabledRulePacks {
		out = append(out, "enabled_rule_packs["+strings.TrimSpace(item.ID)+"]")
	}
	for _, item := range overlay.RuleProviders {
		out = append(out, "rule_providers["+strings.TrimSpace(item.ID)+"]")
	}
	sort.Strings(out)
	return out
}

func summaryForRecord(record Record) PatchSummary {
	patch := record.Patch
	return PatchSummary{
		PatchID:  patch.PatchID,
		Title:    patch.Title,
		Source:   patch.Source,
		Status:   patch.Status,
		OrderID:  patch.OrderID,
		Summary:  patch.Summary,
		SHA256:   record.SHA256,
		Path:     record.Path,
		Provides: append([]string{}, record.Provides...),
	}
}

func sortRecordsForInventory(records []Record) {
	sort.Slice(records, func(i, j int) bool {
		a, b := records[i].Patch, records[j].Patch
		if a.Status == StatusTombstoned && b.Status != StatusTombstoned {
			return false
		}
		if a.Status != StatusTombstoned && b.Status == StatusTombstoned {
			return true
		}
		if a.OrderID == b.OrderID {
			return a.PatchID < b.PatchID
		}
		return a.OrderID < b.OrderID
	})
}

func byID(records []Record) map[string]Record {
	out := map[string]Record{}
	for _, record := range records {
		out[record.Patch.PatchID] = record
	}
	return out
}

func upsertRecord(records []Record, next Record) []Record {
	for i, record := range records {
		if record.Patch.PatchID == next.Patch.PatchID {
			records[i] = next
			return records
		}
	}
	return append(records, next)
}

func removeRecord(records []Record, patchID string) []Record {
	out := records[:0]
	for _, record := range records {
		if record.Patch.PatchID != patchID {
			out = append(out, record)
		}
	}
	return out
}

func enabledCount(records []Record) int {
	count := 0
	for _, record := range records {
		if record.Patch.Status == StatusEnabled {
			count++
		}
	}
	return count
}

func midpointOrderID(records map[string]Record, afterPatchID, beforePatchID string) (string, error) {
	afterPatchID = strings.TrimSpace(afterPatchID)
	beforePatchID = strings.TrimSpace(beforePatchID)
	if afterPatchID == "" && beforePatchID == "" {
		return "", fmt.Errorf("reorder_patch requires order_id or before_patch_id/after_patch_id")
	}
	var low int64 = -1
	var high int64 = 10000000
	if afterPatchID != "" {
		record, ok := records[afterPatchID]
		if !ok {
			return "", fmt.Errorf("after_patch_id %q not found", afterPatchID)
		}
		value, err := parseOrderID(record.Patch.OrderID)
		if err != nil {
			return "", err
		}
		low = value
	}
	if beforePatchID != "" {
		record, ok := records[beforePatchID]
		if !ok {
			return "", fmt.Errorf("before_patch_id %q not found", beforePatchID)
		}
		value, err := parseOrderID(record.Patch.OrderID)
		if err != nil {
			return "", err
		}
		high = value
	}
	if high-low <= 1 {
		return "", fmt.Errorf("no order_id space between %q and %q; draft normalize_order_ids first", afterPatchID, beforePatchID)
	}
	return formatOrderID((low + high) / 2), nil
}

func parseOrderID(orderID string) (int64, error) {
	if !validOrderID(orderID) {
		return 0, fmt.Errorf("invalid order_id %q", orderID)
	}
	parts := strings.Split(orderID, ".")
	whole, _ := strconv.ParseInt(parts[0], 10, 64)
	frac, _ := strconv.ParseInt(parts[1], 10, 64)
	return whole*1000000 + frac, nil
}

func formatOrderID(value int64) string {
	if value < 0 {
		value = 0
	}
	return fmt.Sprintf("%04d.%06d", value/1000000, value%1000000)
}

func nextUserOrderID(records []Record) (string, error) {
	var max int64 = 999000000
	for _, record := range records {
		if record.Patch.Status == StatusTombstoned {
			continue
		}
		value, err := parseOrderID(record.Patch.OrderID)
		if err != nil {
			return "", fmt.Errorf("cannot allocate order_id: patch %q has invalid order_id %q; rebuild the affected Patch with an explicit valid order_id", record.Patch.PatchID, record.Patch.OrderID)
		}
		if value > max {
			max = value
		}
	}
	return formatOrderID(max + 1000000), nil
}

func templateOrderID(id string, index int) string {
	switch id {
	case "default.minimal.v1":
		return "0000.000000"
	case "default.region-exits.v1":
		return "0100.000000"
	case "default.direct-baseline.v1":
		return "0200.000000"
	case "default.quic-main.v1":
		return "0300.000000"
	case "default.communication-social.v1":
		return "0400.000000"
	case "default.ai-dev-speedtest.v1":
		return "0500.000000"
	case "default.steam.v1":
		return "0550.000000"
	case "default.platform-media.v1":
		return "0600.000000"
	case "default.games.v1":
		return "0700.000000"
	case "default.syncnext-app-maintenance.v1":
		return "0750.000000"
	case "default.tail-fallback.v1":
		return "0800.000000"
	default:
		return fmt.Sprintf("%04d.000000", (index+1)*100)
	}
}

func titleFromPatchSource(source policytemplate.PatchSource) string {
	title := strings.TrimSpace(source.Description)
	if title == "" {
		title = source.ID
	}
	return title
}

func safeTitleSlug(title string) string {
	title = strings.ToLower(strings.TrimSpace(title))
	var b strings.Builder
	lastDash := false
	for _, r := range title {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		slug = "patch"
	}
	if len(slug) > 80 {
		slug = strings.Trim(slug[:80], "-")
	}
	return slug
}

func sanitizeBackupName(path string) string {
	path = filepath.Clean(path)
	path = strings.TrimPrefix(path, string(filepath.Separator))
	replacer := strings.NewReplacer(string(filepath.Separator), "__", ":", "_")
	return replacer.Replace(path)
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func defaultRegistryDir(dir string) string {
	if strings.TrimSpace(dir) == "" {
		return RegistryDirName
	}
	return dir
}

func normalizeDraftOptions(opts DraftOptions) DraftOptions {
	if opts.RegistryDir == "" {
		opts.RegistryDir = RegistryDirName
	}
	if opts.ConfigPath == "" {
		opts.ConfigPath = "localclash-intent.json"
	}
	if opts.SelectionPath == "" {
		opts.SelectionPath = "localclash-packs.gob"
	}
	if opts.OutputPath == "" {
		opts.OutputPath = filepath.Join(".runtime", "mihomo", "config.yaml")
	}
	if opts.Subscription == "" {
		opts.Subscription = "subscription.gob"
	}
	if opts.SubscriptionConfig == "" {
		opts.SubscriptionConfig = "localclash-subscriptions.json"
	}
	if opts.SubscriptionRuntime == "" {
		opts.SubscriptionRuntime = filepath.Join(".runtime", "subscriptions")
	}
	if opts.RulesCache == "" {
		opts.RulesCache = filepath.Join(".runtime", "rules", "packs")
	}
	if opts.RuntimeProfilePath == "" {
		opts.RuntimeProfilePath = runtimeprofile.DefaultPath
	}
	if opts.WorkDir == "" {
		opts.WorkDir = filepath.Join(".runtime", "mihomo")
	}
	if opts.ValidationCache == "" {
		opts.ValidationCache = mihomotest.DefaultCachePath(opts.WorkDir)
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	return opts
}

func normalizeApplyOptions(opts ApplyOptions) ApplyOptions {
	if opts.RegistryDir == "" {
		opts.RegistryDir = RegistryDirName
	}
	if opts.ConfigPath == "" {
		opts.ConfigPath = "localclash-intent.json"
	}
	if opts.SelectionPath == "" {
		opts.SelectionPath = "localclash-packs.gob"
	}
	if opts.OutputPath == "" {
		opts.OutputPath = filepath.Join(".runtime", "mihomo", "config.yaml")
	}
	if opts.Subscription == "" {
		opts.Subscription = "subscription.gob"
	}
	if opts.SubscriptionConfig == "" {
		opts.SubscriptionConfig = "localclash-subscriptions.json"
	}
	if opts.SubscriptionRuntime == "" {
		opts.SubscriptionRuntime = filepath.Join(".runtime", "subscriptions")
	}
	if opts.RulesCache == "" {
		opts.RulesCache = filepath.Join(".runtime", "rules", "packs")
	}
	if opts.RuntimeProfilePath == "" {
		opts.RuntimeProfilePath = runtimeprofile.DefaultPath
	}
	if opts.WorkDir == "" {
		opts.WorkDir = filepath.Join(".runtime", "mihomo")
	}
	if opts.ValidationCache == "" {
		opts.ValidationCache = mihomotest.DefaultCachePath(opts.WorkDir)
	}
	if opts.BackupDir == "" {
		opts.BackupDir = filepath.Join(".runtime", "backups", "config-patch-apply")
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	return opts
}

func normalizeImportOptions(opts ImportTemplateOptions) ImportTemplateOptions {
	if opts.RegistryDir == "" {
		opts.RegistryDir = RegistryDirName
	}
	if opts.PolicyTemplatesDir == "" {
		opts.PolicyTemplatesDir = policytemplate.DefaultDir
	}
	if opts.PolicyTemplate == "" {
		opts.PolicyTemplate = policytemplate.TemplateMinimal
	}
	if opts.ConfigPath == "" {
		opts.ConfigPath = "localclash-intent.json"
	}
	if opts.SelectionPath == "" {
		opts.SelectionPath = "localclash-packs.gob"
	}
	if opts.OutputPath == "" {
		opts.OutputPath = filepath.Join(".runtime", "mihomo", "config.yaml")
	}
	if opts.Subscription == "" {
		opts.Subscription = "subscription.gob"
	}
	if opts.SubscriptionConfig == "" {
		opts.SubscriptionConfig = "localclash-subscriptions.json"
	}
	if opts.SubscriptionRuntime == "" {
		opts.SubscriptionRuntime = filepath.Join(".runtime", "subscriptions")
	}
	if opts.RulesCache == "" {
		opts.RulesCache = filepath.Join(".runtime", "rules", "packs")
	}
	if opts.RuntimeProfilePath == "" {
		opts.RuntimeProfilePath = runtimeprofile.DefaultPath
	}
	if opts.WorkDir == "" {
		opts.WorkDir = filepath.Join(".runtime", "mihomo")
	}
	if opts.ValidationCache == "" {
		opts.ValidationCache = mihomotest.DefaultCachePath(opts.WorkDir)
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	return opts
}

func buildOptionsFromDraft(opts DraftOptions, finalPaths bool) buildOptions {
	return buildOptions{
		ConfigPath:          opts.ConfigPath,
		SelectionPath:       opts.SelectionPath,
		OutputPath:          opts.OutputPath,
		Subscription:        opts.Subscription,
		SubscriptionConfig:  opts.SubscriptionConfig,
		SubscriptionRuntime: opts.SubscriptionRuntime,
		RulesCache:          opts.RulesCache,
		RuntimeProfilePath:  opts.RuntimeProfilePath,
		ValidationCache:     opts.ValidationCache,
		CorePath:            opts.CorePath,
		WorkDir:             opts.WorkDir,
		Test:                opts.Test,
		TempOnly:            !finalPaths,
	}
}

func buildOptionsFromApply(opts ApplyOptions, finalPaths bool) buildOptions {
	return buildOptions{
		ConfigPath:          opts.ConfigPath,
		SelectionPath:       opts.SelectionPath,
		OutputPath:          opts.OutputPath,
		Subscription:        opts.Subscription,
		SubscriptionConfig:  opts.SubscriptionConfig,
		SubscriptionRuntime: opts.SubscriptionRuntime,
		RulesCache:          opts.RulesCache,
		RuntimeProfilePath:  opts.RuntimeProfilePath,
		ValidationCache:     opts.ValidationCache,
		CorePath:            opts.CorePath,
		WorkDir:             opts.WorkDir,
		Test:                opts.Test,
		TempOnly:            !finalPaths,
	}
}

func buildOptionsFromImport(opts ImportTemplateOptions) buildOptions {
	return buildOptions{
		ConfigPath:          opts.ConfigPath,
		SelectionPath:       opts.SelectionPath,
		OutputPath:          opts.OutputPath,
		Subscription:        opts.Subscription,
		SubscriptionConfig:  opts.SubscriptionConfig,
		SubscriptionRuntime: opts.SubscriptionRuntime,
		RulesCache:          opts.RulesCache,
		RuntimeProfilePath:  opts.RuntimeProfilePath,
		ValidationCache:     opts.ValidationCache,
		CorePath:            opts.CorePath,
		WorkDir:             opts.WorkDir,
		Test:                false,
		TempOnly:            false,
	}
}

func canRenderOptionalArtifacts(opts ImportTemplateOptions) bool {
	if _, err := os.Stat(opts.Subscription); err != nil {
		return false
	}
	if _, err := os.Stat(opts.RuntimeProfilePath); err != nil {
		return false
	}
	return true
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
