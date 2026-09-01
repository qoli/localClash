package subscriptions

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/gob"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

const DefaultUserAgent = "clash-verge/v1.5.1"
const sourceIDPrefix = "S-"
const sourceIDHashLength = 8
const maxSourceDisplayIndex = 99
const refreshFetchConcurrency = 4
const subscriptionRangeChunkSize = 64 * 1024
const subscriptionRangeOverlap = 256
const maxRangeRecoveryBytes = 32 * 1024 * 1024
const (
	sourceTypeRemoteSubscription = "remote_subscription"
	sourceTypeInlineProxyURIs    = "inline_proxy_uris"

	subscriptionFormatMihomoYAML    = "mihomo_yaml"
	subscriptionFormatProxyURILines = "proxy_uri_lines"
)

type Source struct {
	ID          string   `json:"id" yaml:"id"`
	DisplayName string   `json:"display_name" yaml:"display_name"`
	Type        string   `json:"type,omitempty" yaml:"type,omitempty"`
	URI         string   `json:"uri,omitempty" yaml:"uri,omitempty"`
	URIs        []string `json:"uris,omitempty" yaml:"uris,omitempty"`
	URL         string   `json:"url,omitempty" yaml:"url,omitempty"` // legacy config key
}

type Config struct {
	Version           int      `json:"version"`
	G204FilterEnabled bool     `json:"g204_filter_enabled"`
	Sources           []Source `json:"sources"`
}

type StatusOptions struct {
	ConfigPath string
	MergedPath string
	RuntimeDir string
}

type ConfigureOptions struct {
	ConfigPath        string
	Sources           []Source
	URIs              []string
	URLs              []string
	Replace           *bool
	G204FilterEnabled bool
}

type RefreshOptions struct {
	ConfigPath string
	IDs        []string
	RuntimeDir string
	MergedPath string
	Force      bool
	UserAgent  string
	OnStage    func(StageEvent) `json:"-"`
}

type StageEvent struct {
	Stage      string         `json:"stage"`
	Event      string         `json:"event"`
	DurationMS int64          `json:"duration_ms,omitempty"`
	Error      string         `json:"error,omitempty"`
	Fields     map[string]any `json:"fields,omitempty"`
}

type StatusResult struct {
	Configured        bool            `json:"configured"`
	G204FilterEnabled bool            `json:"g204_filter_enabled"`
	Config            string          `json:"config"`
	Sources           []SourceStatus  `json:"sources"`
	Merged            ArtifactSummary `json:"merged"`
	Message           string          `json:"message,omitempty"`
}

type SourceStatus struct {
	ID               string `json:"id"`
	DisplayName      string `json:"display_name"`
	Type             string `json:"type"`
	URI              string `json:"uri,omitempty"`
	URL              string `json:"url,omitempty"`
	URIHash          string `json:"uri_hash,omitempty"`
	Artifact         string `json:"artifact"`
	Exists           bool   `json:"exists"`
	ProxiesCount     int    `json:"proxies_count,omitempty"`
	ProxyGroupsCount int    `json:"proxy_groups_count,omitempty"`
	RulesCount       int    `json:"rules_count,omitempty"`
	UpdatedAt        string `json:"updated_at,omitempty"`
}

type ArtifactSummary struct {
	Path                string `json:"path"`
	Exists              bool   `json:"exists"`
	ProxiesCount        int    `json:"proxies_count,omitempty"`
	ProxyGroupsCount    int    `json:"proxy_groups_count,omitempty"`
	RulesCount          int    `json:"rules_count,omitempty"`
	RenamedProxiesCount int    `json:"renamed_proxies_count,omitempty"`
	UpdatedAt           string `json:"updated_at,omitempty"`
}

type ConfigureResult struct {
	Config            string             `json:"config"`
	Configured        bool               `json:"configured"`
	G204FilterEnabled bool               `json:"g204_filter_enabled"`
	Sources           []ConfiguredSource `json:"sources"`
	Message           string             `json:"message"`
}

type ConfiguredSource struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Type        string `json:"type"`
	URI         string `json:"uri,omitempty"`
	URL         string `json:"url,omitempty"`
	URIHash     string `json:"uri_hash,omitempty"`
}

type GetResult struct {
	Config            string             `json:"config"`
	Configured        bool               `json:"configured"`
	G204FilterEnabled bool               `json:"g204_filter_enabled"`
	Sources           []ConfiguredSource `json:"sources"`
	URIs              []string           `json:"uris"`
	URLs              []string           `json:"urls"`
	Count             int                `json:"count"`
	Message           string             `json:"message,omitempty"`
}

type RefreshResult struct {
	Refreshed         bool                   `json:"refreshed"`
	G204FilterEnabled bool                   `json:"g204_filter_enabled"`
	Sources           []RefreshSourceSummary `json:"sources"`
	Merged            ArtifactSummary        `json:"merged"`
	Warnings          []string               `json:"warnings"`
	Artifacts         []RefreshArtifact      `json:"-"`
	MergedDoc         map[string]any         `json:"-"`
}

type RefreshSourceSummary struct {
	ID               string `json:"id"`
	DisplayName      string `json:"display_name"`
	Artifact         string `json:"artifact"`
	ProxiesCount     int    `json:"proxies_count,omitempty"`
	ProxyGroupsCount int    `json:"proxy_groups_count,omitempty"`
	RulesCount       int    `json:"rules_count,omitempty"`
	Type             string `json:"type"`
	Format           string `json:"format,omitempty"`
	Status           string `json:"status"`
}

type RefreshArtifact struct {
	SourceID    string
	DisplayName string
	Proxies     []map[string]any
}

type subscriptionDoc struct {
	Data   map[string]any
	Raw    []byte
	Format string
}

type subscriptionArtifact struct {
	Version int
	Data    map[string]any
	Raw     []byte
	Format  string
}

var sourceIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
var sourceDisplayNamePattern = regexp.MustCompile(`^[0-9]{2}$`)
var sensitiveResponseFieldPattern = regexp.MustCompile(`(?i)(token|access_token|api[_-]?key|secret|password|passwd|authorization)(["']?[[:space:]]*[:=][[:space:]]*["']?)[^&[:space:]"',}]+`)
var responseURLPattern = regexp.MustCompile(`https?://[^[:space:]"'<>]+`)
var contentRangePattern = regexp.MustCompile(`^bytes ([0-9]+)-([0-9]+)/([0-9]+)$`)

func init() {
	gob.Register(map[string]any{})
	gob.Register([]any{})
	gob.Register([]map[string]any{})
}

func Status(opts StatusOptions) (StatusResult, error) {
	opts = normalizeStatusOptions(opts)
	result := StatusResult{
		Config: opts.ConfigPath,
		Merged: ArtifactSummary{
			Path: opts.MergedPath,
		},
	}
	config, err := readConfig(opts.ConfigPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			result.Message = "subscription sources are not configured; ask the user for one or more subscription URIs, then run subscriptions_configure"
			result.Merged = summarizeArtifact(opts.MergedPath)
			return result, nil
		}
		return StatusResult{}, err
	}
	result.Configured = true
	result.G204FilterEnabled = config.G204FilterEnabled
	for _, source := range config.Sources {
		artifact := artifactPath(opts.RuntimeDir, source.ID)
		summary := summarizeArtifact(artifact)
		result.Sources = append(result.Sources, SourceStatus{
			ID:               source.ID,
			DisplayName:      sourceDisplayName(source),
			Type:             sourceType(source),
			URI:              MaskURI(sourcePrimaryURI(source)),
			URL:              legacyURLForResult(source),
			URIHash:          sourceURIHash(source),
			Artifact:         artifact,
			Exists:           summary.Exists,
			ProxiesCount:     summary.ProxiesCount,
			ProxyGroupsCount: summary.ProxyGroupsCount,
			RulesCount:       summary.RulesCount,
			UpdatedAt:        summary.UpdatedAt,
		})
	}
	result.Merged = summarizeArtifact(opts.MergedPath)
	return result, nil
}

func Get(opts StatusOptions) (GetResult, error) {
	opts = normalizeStatusOptions(opts)
	result := GetResult{
		Config: opts.ConfigPath,
	}
	config, err := readConfig(opts.ConfigPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			result.Message = "subscription sources are not configured; ask the user for one or more subscription URIs, then run subscriptions_configure"
			return result, nil
		}
		return GetResult{}, err
	}
	result.Configured = true
	result.G204FilterEnabled = config.G204FilterEnabled
	for _, source := range config.Sources {
		uri := sourcePrimaryURI(source)
		result.Sources = append(result.Sources, ConfiguredSource{
			ID:          source.ID,
			DisplayName: sourceDisplayName(source),
			Type:        sourceType(source),
			URI:         uri,
			URL:         legacyURLForGet(source),
			URIHash:     sourceURIHash(source),
		})
		if uri != "" {
			result.URIs = append(result.URIs, uri)
		}
		if sourceType(source) == sourceTypeRemoteSubscription {
			result.URLs = append(result.URLs, uri)
		}
	}
	result.Count = len(result.Sources)
	return result, nil
}

func Configure(opts ConfigureOptions) (ConfigureResult, error) {
	opts = normalizeConfigureOptions(opts)
	if opts.Replace != nil && !*opts.Replace {
		return ConfigureResult{}, fmt.Errorf("replace=false is not supported in this version")
	}
	sources, err := configureSources(opts)
	if err != nil {
		return ConfigureResult{}, err
	}
	config := Config{Version: 1, G204FilterEnabled: opts.G204FilterEnabled, Sources: sources}
	if err := writeConfig(opts.ConfigPath, config); err != nil {
		return ConfigureResult{}, err
	}
	result := ConfigureResult{
		Config:            opts.ConfigPath,
		Configured:        true,
		G204FilterEnabled: opts.G204FilterEnabled,
		Message:           "Subscription sources configured. Run subscriptions_refresh to update local artifacts.",
	}
	for _, source := range sources {
		result.Sources = append(result.Sources, ConfiguredSource{
			ID:          source.ID,
			DisplayName: source.DisplayName,
			Type:        sourceType(source),
			URI:         MaskURI(sourcePrimaryURI(source)),
			URL:         legacyMaskedURL(source),
			URIHash:     sourceURIHash(source),
		})
	}
	return result, nil
}

func SourcesFromURLs(rawURLs []string) ([]Source, error) {
	return SourcesFromURIs(rawURLs)
}

func configureSources(opts ConfigureOptions) ([]Source, error) {
	if len(opts.Sources) > 0 {
		return normalizeSources(opts.Sources)
	}
	rawURIs := opts.URIs
	if len(rawURIs) == 0 {
		rawURIs = opts.URLs
	}
	if len(rawURIs) == 0 {
		return nil, fmt.Errorf("uris is required")
	}
	return SourcesFromURIs(rawURIs)
}

func SourcesFromURIs(rawURIs []string) ([]Source, error) {
	inputs := make([]string, 0, len(rawURIs))
	for i, raw := range rawURIs {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			return nil, fmt.Errorf("uris[%d] must not be empty", i)
		}
		inputs = append(inputs, trimmed)
	}
	return normalizeSourcesFromURIs(inputs)
}

func Refresh(ctx context.Context, opts RefreshOptions) (RefreshResult, error) {
	opts = normalizeRefreshOptions(opts)
	stage := subscriptionStageEmitter(opts.OnStage)

	finish := stage("read_config", map[string]any{"config": opts.ConfigPath})
	config, err := readConfig(opts.ConfigPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			finish(err, nil)
			return RefreshResult{}, fmt.Errorf("subscription sources are not configured; run subscriptions_configure first")
		}
		finish(err, nil)
		return RefreshResult{}, err
	}
	if len(config.Sources) == 0 {
		finish(fmt.Errorf("subscription sources are empty"), nil)
		return RefreshResult{}, fmt.Errorf("subscription sources are empty; run subscriptions_configure first")
	}
	finish(nil, map[string]any{"source_count": len(config.Sources)})

	finish = stage("validate_sources", nil)
	if err := validateSources(config.Sources); err != nil {
		finish(err, nil)
		return RefreshResult{}, err
	}
	finish(nil, nil)

	finish = stage("select_sources", map[string]any{"requested_count": len(opts.IDs)})
	selected, err := selectedSourceIDs(config.Sources, opts.IDs)
	if err != nil {
		finish(err, nil)
		return RefreshResult{}, err
	}
	selectedCount := 0
	for _, ok := range selected {
		if ok {
			selectedCount++
		}
	}
	selectedSources := make([]map[string]any, 0, selectedCount)
	for _, source := range config.Sources {
		if selected[source.ID] {
			selectedSources = append(selectedSources, sourceStageFields(source))
		}
	}
	finish(nil, map[string]any{"selected_count": selectedCount, "selected_sources": selectedSources})

	finish = stage("ensure_runtime_dir", map[string]any{"runtime_dir": opts.RuntimeDir})
	if err := os.MkdirAll(opts.RuntimeDir, 0o755); err != nil {
		finish(err, nil)
		return RefreshResult{}, err
	}
	finish(nil, nil)

	refreshed, docs, err := refreshSelectedSources(ctx, config.Sources, selected, opts, stage)
	if err != nil {
		return RefreshResult{}, err
	}

	finish = stage("read_artifacts", nil)
	result := RefreshResult{Refreshed: true, G204FilterEnabled: config.G204FilterEnabled, Warnings: []string{}}
	diskReads := 0
	for _, source := range config.Sources {
		path := artifactPath(opts.RuntimeDir, source.ID)
		doc, ok := docs[source.ID]
		if !ok {
			var err error
			doc, err = readSubscription(path)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					if selected[source.ID] {
						finish(err, sourceStageFields(source))
						return RefreshResult{}, fmt.Errorf("%s artifact was not written", sourceLogLabel(source))
					}
					result.Warnings = append(result.Warnings, fmt.Sprintf("%s has no local artifact; run subscriptions_refresh for that source", sourceLogLabel(source)))
					continue
				}
				finish(err, sourceStageFields(source))
				return RefreshResult{}, err
			}
			docs[source.ID] = doc
			diskReads++
		}
		summary := summarizeMap(doc.Data)
		status := "existing"
		if refreshed[source.ID] {
			status = "ok"
		}
		result.Artifacts = append(result.Artifacts, RefreshArtifact{
			SourceID:    source.ID,
			DisplayName: sourceDisplayName(source),
			Proxies:     proxyMaps(doc.Data),
		})
		result.Sources = append(result.Sources, RefreshSourceSummary{
			ID:               source.ID,
			DisplayName:      sourceDisplayName(source),
			Artifact:         path,
			ProxiesCount:     summary.ProxiesCount,
			ProxyGroupsCount: summary.ProxyGroupsCount,
			RulesCount:       summary.RulesCount,
			Type:             sourceType(source),
			Format:           doc.Format,
			Status:           status,
		})
	}
	if len(docs) == 0 {
		finish(fmt.Errorf("no subscription artifacts are available to merge"), nil)
		return RefreshResult{}, fmt.Errorf("no subscription artifacts are available to merge")
	}
	finish(nil, map[string]any{"artifact_count": len(docs), "memory_docs": len(docs) - diskReads, "disk_reads": diskReads})

	finish = stage("merge_subscriptions", nil)
	merged, renamed, err := mergeSubscriptions(config.Sources, docs)
	if err != nil {
		finish(err, nil)
		return RefreshResult{}, err
	}
	finish(nil, map[string]any{"renamed_proxies": renamed})

	finish = stage("write_merged_subscription", map[string]any{"merged": opts.MergedPath})
	if err := writeSubscriptionArtifact(opts.MergedPath, subscriptionDoc{Data: merged}); err != nil {
		finish(err, nil)
		return RefreshResult{}, err
	}
	result.Merged = summarizeArtifact(opts.MergedPath)
	result.Merged.RenamedProxiesCount = renamed
	result.MergedDoc = merged
	sort.Strings(result.Warnings)
	finish(nil, map[string]any{
		"proxies":         result.Merged.ProxiesCount,
		"renamed_proxies": renamed,
	})
	return result, nil
}

type sourceRefreshOutcome struct {
	sourceID  string
	refreshed bool
	doc       subscriptionDoc
	err       error
}

func refreshSelectedSources(ctx context.Context, sources []Source, selected map[string]bool, opts RefreshOptions, stage func(string, map[string]any) func(error, map[string]any)) (map[string]bool, map[string]subscriptionDoc, error) {
	selectedCount := 0
	for _, source := range sources {
		if selected[source.ID] {
			selectedCount++
		}
	}
	if selectedCount == 0 {
		return map[string]bool{}, map[string]subscriptionDoc{}, nil
	}
	workerCount := refreshFetchConcurrency
	if selectedCount < workerCount {
		workerCount = selectedCount
	}

	jobs := make(chan Source)
	results := make(chan sourceRefreshOutcome, selectedCount)
	var wg sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for source := range jobs {
				results <- refreshOneSource(ctx, source, opts, stage)
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, source := range sources {
			if !selected[source.ID] {
				continue
			}
			select {
			case <-ctx.Done():
				return
			case jobs <- source:
			}
		}
	}()
	go func() {
		wg.Wait()
		close(results)
	}()

	outcomes := map[string]sourceRefreshOutcome{}
	for result := range results {
		outcomes[result.sourceID] = result
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	for _, source := range sources {
		if !selected[source.ID] {
			continue
		}
		outcome, ok := outcomes[source.ID]
		if !ok {
			return nil, nil, fmt.Errorf("%s was not refreshed", sourceLogLabel(source))
		}
		if outcome.err != nil {
			return nil, nil, outcome.err
		}
	}
	refreshed := map[string]bool{}
	docs := map[string]subscriptionDoc{}
	for _, source := range sources {
		if outcome, ok := outcomes[source.ID]; ok && outcome.refreshed {
			refreshed[source.ID] = true
			docs[source.ID] = outcome.doc
		}
	}
	return refreshed, docs, nil
}

func refreshOneSource(ctx context.Context, source Source, opts RefreshOptions, stage func(string, map[string]any) func(error, map[string]any)) sourceRefreshOutcome {
	identity := sourceStageFields(source)
	finish := stage("refresh_source", identity)
	doc, err := refreshSource(ctx, source, opts.UserAgent, stage)
	if err != nil {
		finish(err, nil)
		return sourceRefreshOutcome{sourceID: source.ID, err: err}
	}
	finish(nil, nil)

	artifact := artifactPath(opts.RuntimeDir, source.ID)
	finish = stage("write_source_artifact", mergeStageFields(identity, map[string]any{"artifact_dir": filepath.Dir(artifact)}))
	if err := writeSubscriptionArtifact(artifact, doc); err != nil {
		finish(err, nil)
		return sourceRefreshOutcome{sourceID: source.ID, err: err}
	}
	summary := summarizeMap(doc.Data)
	finish(nil, map[string]any{
		"bytes":        len(doc.Raw),
		"format":       doc.Format,
		"proxies":      summary.ProxiesCount,
		"proxy_groups": summary.ProxyGroupsCount,
		"rules":        summary.RulesCount,
	})
	return sourceRefreshOutcome{sourceID: source.ID, refreshed: true, doc: doc}
}

func subscriptionStageEmitter(callback func(StageEvent)) func(string, map[string]any) func(error, map[string]any) {
	var mu sync.Mutex
	emit := func(event StageEvent) {
		mu.Lock()
		defer mu.Unlock()
		callback(event)
	}
	return func(stage string, fields map[string]any) func(error, map[string]any) {
		if callback == nil {
			return func(error, map[string]any) {}
		}
		started := time.Now()
		startedFields := cloneStageFields(fields)
		emit(StageEvent{Stage: stage, Event: "started", Fields: startedFields})
		return func(err error, doneFields map[string]any) {
			event := StageEvent{
				Stage:      stage,
				Event:      "done",
				DurationMS: time.Since(started).Milliseconds(),
				Fields:     mergeStageFields(startedFields, doneFields),
			}
			if err != nil {
				event.Event = "error"
				event.Error = err.Error()
			}
			emit(event)
		}
	}
}

func normalizeStatusOptions(opts StatusOptions) StatusOptions {
	if strings.TrimSpace(opts.ConfigPath) == "" {
		opts.ConfigPath = "localclash-subscriptions.json"
	}
	if strings.TrimSpace(opts.MergedPath) == "" {
		opts.MergedPath = "subscription.gob"
	}
	if strings.TrimSpace(opts.RuntimeDir) == "" {
		opts.RuntimeDir = ".runtime/subscriptions"
	}
	return opts
}

func normalizeConfigureOptions(opts ConfigureOptions) ConfigureOptions {
	if strings.TrimSpace(opts.ConfigPath) == "" {
		opts.ConfigPath = "localclash-subscriptions.json"
	}
	return opts
}

func normalizeRefreshOptions(opts RefreshOptions) RefreshOptions {
	if strings.TrimSpace(opts.ConfigPath) == "" {
		opts.ConfigPath = "localclash-subscriptions.json"
	}
	if strings.TrimSpace(opts.RuntimeDir) == "" {
		opts.RuntimeDir = ".runtime/subscriptions"
	}
	if strings.TrimSpace(opts.MergedPath) == "" {
		opts.MergedPath = "subscription.gob"
	}
	if strings.TrimSpace(opts.UserAgent) == "" {
		opts.UserAgent = DefaultUserAgent
	}
	return opts
}

func readConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return Config{}, err
	}
	return config, nil
}

func G204FilterEnabled(path string) (bool, error) {
	config, err := readConfig(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return config.G204FilterEnabled, nil
}

func writeConfig(path string, config Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func normalizeSources(sources []Source) ([]Source, error) {
	if len(sources) > maxSourceDisplayIndex {
		return nil, fmt.Errorf("subscription sources support at most %d entries for two-digit display_name values", maxSourceDisplayIndex)
	}
	normalized := make([]Source, 0, len(sources))
	seenCanonicalURI := map[string]bool{}
	usedIDs := map[string]bool{}
	usedDisplayNames := map[string]bool{}
	canonicalURIs := make([]string, 0, len(sources))
	for i, source := range sources {
		canonicalURI, err := canonicalSourceURI(source)
		if err != nil {
			return nil, fmt.Errorf("sources[%d] %w", i, err)
		}
		if seenCanonicalURI[canonicalURI] {
			return nil, fmt.Errorf("duplicate subscription URI at sources[%d]", i)
		}
		seenCanonicalURI[canonicalURI] = true
		canonicalURIs = append(canonicalURIs, canonicalURI)
	}
	for i, source := range sources {
		id := sourceIDFromCanonicalURI(canonicalURIs[i], usedIDs)
		displayName, err := normalizeSourceDisplayName(source.DisplayName, i)
		if err != nil {
			return nil, fmt.Errorf("sources[%d] %w", i, err)
		}
		if usedDisplayNames[displayName] {
			return nil, fmt.Errorf("duplicate subscription source display_name %q at sources[%d]", displayName, i)
		}
		usedIDs[id] = true
		usedDisplayNames[displayName] = true
		normalized = append(normalized, normalizeSourceForConfig(source, id, displayName))
	}
	return normalized, nil
}

func normalizeSourceDisplayName(raw string, index int) (string, error) {
	displayName := strings.TrimSpace(raw)
	if displayName == "" {
		if index >= maxSourceDisplayIndex {
			return "", fmt.Errorf("source display_name is required after %d sources", maxSourceDisplayIndex)
		}
		displayName = fmt.Sprintf("%02d", index+1)
	}
	if !sourceDisplayNamePattern.MatchString(displayName) || displayName == "00" {
		return "", fmt.Errorf("source display_name %q is invalid; use two digits from 01 to 99", displayName)
	}
	return displayName, nil
}

func normalizeSourcesFromURIs(rawURIs []string) ([]Source, error) {
	var remoteSources []Source
	var inlineURIs []string
	seenRemote := map[string]bool{}
	seenInline := map[string]bool{}
	for i, rawURI := range rawURIs {
		kind, canonical, err := classifyInputURI(rawURI)
		if err != nil {
			return nil, fmt.Errorf("uris[%d] %w", i, err)
		}
		switch kind {
		case sourceTypeRemoteSubscription:
			if seenRemote[canonical] {
				continue
			}
			seenRemote[canonical] = true
			remoteSources = append(remoteSources, Source{Type: sourceTypeRemoteSubscription, URI: strings.TrimSpace(rawURI)})
		case sourceTypeInlineProxyURIs:
			trimmed := strings.TrimSpace(rawURI)
			if seenInline[trimmed] {
				continue
			}
			seenInline[trimmed] = true
			inlineURIs = append(inlineURIs, trimmed)
		default:
			return nil, fmt.Errorf("uris[%d] unsupported source type %q", i, kind)
		}
	}
	sources := make([]Source, 0, len(remoteSources)+1)
	sources = append(sources, remoteSources...)
	if len(inlineURIs) > 0 {
		sources = append(sources, Source{Type: sourceTypeInlineProxyURIs, URIs: inlineURIs})
	}
	return normalizeSources(sources)
}

func classifyInputURI(rawURI string) (string, string, error) {
	trimmed := strings.TrimSpace(rawURI)
	if trimmed == "" {
		return "", "", fmt.Errorf("uri is required")
	}
	scheme, rest, ok := strings.Cut(trimmed, "://")
	if !ok {
		return "", "", fmt.Errorf("uri must include a scheme")
	}
	scheme = strings.ToLower(scheme)
	switch {
	case scheme == "http" || scheme == "https":
		canonical, err := canonicalRemoteSubscriptionURI(trimmed)
		if err != nil {
			return "", "", err
		}
		return sourceTypeRemoteSubscription, canonical, nil
	case proxyURISchemes[scheme] && strings.TrimSpace(rest) != "":
		return sourceTypeInlineProxyURIs, "proxy:" + trimmed, nil
	default:
		return "", "", fmt.Errorf("uri scheme %q is not supported; use http/https or an MVP proxy URI scheme", scheme)
	}
}

func canonicalSourceURI(source Source) (string, error) {
	switch sourceType(source) {
	case sourceTypeRemoteSubscription:
		return canonicalRemoteSubscriptionURI(sourcePrimaryURI(source))
	case sourceTypeInlineProxyURIs:
		if len(source.URIs) == 0 {
			return "", fmt.Errorf("inline proxy URI source requires uris")
		}
		for i, uri := range source.URIs {
			kind, _, err := classifyInputURI(uri)
			if err != nil {
				return "", fmt.Errorf("inline proxy URI %d is invalid: %w", i, err)
			}
			if kind != sourceTypeInlineProxyURIs {
				return "", fmt.Errorf("inline proxy URI %d must use an MVP proxy URI scheme", i)
			}
		}
		return "inline:" + strings.Join(source.URIs, "\n"), nil
	default:
		return "", fmt.Errorf("source type %q is not supported", sourceType(source))
	}
}

func canonicalSubscriptionURL(rawURL string) (string, error) {
	return canonicalRemoteSubscriptionURI(rawURL)
}

func canonicalRemoteSubscriptionURI(rawURI string) (string, error) {
	if rawURI == "" {
		return "", fmt.Errorf("uri is required")
	}
	parsed, err := url.Parse(rawURI)
	if err != nil {
		return "", fmt.Errorf("uri is invalid")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("uri must use http or https")
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("uri must include a host")
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Fragment = ""
	return parsed.String(), nil
}

func sourceIDFromCanonicalURL(canonicalURL string, used map[string]bool) string {
	return sourceIDFromCanonicalURI(canonicalURL, used)
}

func sourceIDFromCanonicalURI(canonicalURI string, used map[string]bool) string {
	sum := sha256.Sum256([]byte(canonicalURI))
	encoded := hex.EncodeToString(sum[:])
	for length := sourceIDHashLength; length <= len(encoded); length += 2 {
		id := sourceIDPrefix + encoded[:length]
		if !used[id] {
			return id
		}
	}
	return sourceIDPrefix + encoded
}

func validateSources(sources []Source) error {
	usedDisplayNames := map[string]bool{}
	for i, source := range sources {
		if err := validateSource(source); err != nil {
			return fmt.Errorf("sources[%d] %w", i, err)
		}
		displayName := strings.TrimSpace(source.DisplayName)
		if displayName == "" {
			continue
		}
		if usedDisplayNames[displayName] {
			return fmt.Errorf("duplicate subscription source display_name %q at sources[%d]", displayName, i)
		}
		usedDisplayNames[displayName] = true
	}
	return nil
}

func validateSource(source Source) error {
	if source.ID == "" {
		return fmt.Errorf("source id is required")
	}
	if !sourceIDPattern.MatchString(source.ID) {
		return fmt.Errorf("source id %q is invalid; use only letters, digits, underscore, and hyphen", source.ID)
	}
	displayName := strings.TrimSpace(source.DisplayName)
	if displayName != "" && (!sourceDisplayNamePattern.MatchString(displayName) || displayName == "00") {
		return fmt.Errorf("source %q display_name %q is invalid; use two digits from 01 to 99", source.ID, source.DisplayName)
	}
	_, err := canonicalSourceURI(source)
	return err
}

func sourceDisplayName(source Source) string {
	displayName := strings.TrimSpace(source.DisplayName)
	if displayName != "" {
		return displayName
	}
	id := strings.TrimSpace(source.ID)
	trimmed := strings.TrimPrefix(id, sourceIDPrefix)
	if trimmed == "" {
		trimmed = id
	}
	return firstSourceIDChars(trimmed, 2)
}

func firstSourceIDChars(value string, count int) string {
	if len(value) <= count {
		return value
	}
	return value[:count]
}

func selectedSourceIDs(sources []Source, ids []string) (map[string]bool, error) {
	known := map[string]bool{}
	for _, source := range sources {
		known[source.ID] = true
	}
	selected := map[string]bool{}
	if len(ids) == 0 {
		for _, source := range sources {
			selected[source.ID] = true
		}
		return selected, nil
	}
	for _, rawID := range ids {
		id := strings.TrimSpace(rawID)
		if !known[id] {
			return nil, fmt.Errorf("unknown subscription source id %q", id)
		}
		selected[id] = true
	}
	return selected, nil
}

func refreshSource(ctx context.Context, source Source, userAgent string, stage func(string, map[string]any) func(error, map[string]any)) (subscriptionDoc, error) {
	switch sourceType(source) {
	case sourceTypeRemoteSubscription:
		return fetchSource(ctx, source, userAgent, stage)
	case sourceTypeInlineProxyURIs:
		fields := mergeStageFields(sourceStageFields(source), map[string]any{"uri_count": len(source.URIs)})
		finish := stage("parse_inline_source", fields)
		doc, err := parseProxyURIList(sourceLogLabel(source), []byte(strings.Join(source.URIs, "\n")))
		if err != nil {
			finish(err, nil)
			return subscriptionDoc{}, err
		}
		finish(nil, map[string]any{"format": doc.Format, "proxies": len(proxyMaps(doc.Data))})
		return doc, nil
	default:
		return subscriptionDoc{}, fmt.Errorf("%s type %q is not supported", sourceLogLabel(source), sourceType(source))
	}
}

func fetchSource(ctx context.Context, source Source, userAgent string, stage func(string, map[string]any) func(error, map[string]any)) (subscriptionDoc, error) {
	client := subscriptionHTTPClient()
	defer client.CloseIdleConnections()

	identity := sourceStageFields(source)
	finish := stage("build_subscription_request", mergeStageFields(identity, map[string]any{"method": http.MethodGet, "user_agent": userAgent}))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourcePrimaryURI(source), nil)
	if err != nil {
		err = fmt.Errorf("%s request could not be created", sourceLogLabel(source))
		finish(err, nil)
		return subscriptionDoc{}, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "*/*")
	finish(nil, nil)

	finish = stage("fetch_subscription_response", identity)
	resp, err := client.Do(req)
	if err != nil {
		err = fmt.Errorf("%s request failed: %s", sourceLogLabel(source), safeTransportError(err, source))
		finish(err, map[string]any{"failure_kind": "transport"})
		return subscriptionDoc{}, err
	}
	defer resp.Body.Close()
	responseFields := map[string]any{
		"status":            resp.Status,
		"status_code":       resp.StatusCode,
		"protocol":          resp.Proto,
		"content_type":      resp.Header.Get("Content-Type"),
		"content_encoding":  resp.Header.Get("Content-Encoding"),
		"content_length":    resp.ContentLength,
		"transfer_encoding": append([]string(nil), resp.TransferEncoding...),
		"uncompressed":      resp.Uncompressed,
		"response_uri":      MaskURI(resp.Request.URL.String()),
		"redirected":        resp.Request.URL.String() != sourcePrimaryURI(source),
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4097))
		preview := safeResponsePreview(body, source)
		if preview != "" {
			responseFields["response_body_preview"] = preview
		}
		if len(body) > 4096 {
			responseFields["response_body_truncated"] = true
		}
		if readErr != nil {
			responseFields["response_read_error"] = readErr.Error()
		}
		message := fmt.Sprintf("%s request failed: HTTP %s", sourceLogLabel(source), resp.Status)
		if preview != "" {
			message += "; response: " + preview
		}
		err = errors.New(message)
		finish(err, responseFields)
		return subscriptionDoc{}, err
	}
	finish(nil, responseFields)

	finish = stage("read_subscription_response", identity)
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		initialErr := err
		partialSum := sha256.Sum256(body)
		_ = resp.Body.Close()
		recovered, recovery, recoveryErr := recoverSubscriptionWithRanges(ctx, client, source, userAgent, stage, initialErr, len(body))
		if recoveryErr != nil {
			err = fmt.Errorf("%s response could not be read: %v; range chunk recovery failed: %w", sourceLogLabel(source), initialErr, recoveryErr)
			finish(err, map[string]any{
				"bytes_received":      len(body),
				"partial_body_sha256": hex.EncodeToString(partialSum[:]),
				"payload_logged":      false,
				"recovery":            "range_chunks",
				"recovery_error":      recoveryErr.Error(),
			})
			return subscriptionDoc{}, err
		}
		body = recovered
		finish(nil, map[string]any{
			"bytes":              len(body),
			"initial_read_error": initialErr.Error(),
			"recovered":          true,
			"recovery":           "range_chunks",
			"range_chunks":       recovery.Chunks,
			"range_total_bytes":  recovery.TotalBytes,
			"body_sha256":        recovery.BodySHA256,
		})
	} else {
		finish(nil, map[string]any{"bytes": len(body), "recovered": false})
	}

	finish = stage("parse_subscription_response", mergeStageFields(identity, map[string]any{"bytes": len(body)}))
	doc, err := parseRemoteSubscription(sourceLogLabel(source), body)
	if err != nil {
		bodySum := sha256.Sum256(body)
		finish(err, map[string]any{"body_sha256": hex.EncodeToString(bodySum[:]), "payload_logged": false})
		return subscriptionDoc{}, err
	}
	doc.Raw = append([]byte(nil), body...)
	summary := summarizeMap(doc.Data)
	finish(nil, map[string]any{"format": doc.Format, "proxies": summary.ProxiesCount, "proxy_groups": summary.ProxyGroupsCount, "rules": summary.RulesCount})
	return doc, nil
}

type rangeRecoverySummary struct {
	Chunks     int
	TotalBytes int
	BodySHA256 string
}

type rangeResponse struct {
	Body         []byte
	Start        int
	End          int
	Total        int
	ValidatorKey string
	Validator    string
}

func recoverSubscriptionWithRanges(ctx context.Context, client *http.Client, source Source, userAgent string, stage func(string, map[string]any) func(error, map[string]any), trigger error, partialBytes int) ([]byte, rangeRecoverySummary, error) {
	identity := sourceStageFields(source)
	finish := stage("range_chunk_recovery", mergeStageFields(identity, map[string]any{
		"trigger_error": trigger.Error(),
		"partial_bytes": partialBytes,
		"chunk_size":    subscriptionRangeChunkSize,
		"overlap_bytes": subscriptionRangeOverlap,
		"protocol":      "HTTP/1.1",
	}))
	firstEnd := subscriptionRangeChunkSize - 1
	first, err := fetchSubscriptionRange(ctx, client, source, userAgent, 0, firstEnd, "assemble", 1, "", "", stage)
	if err != nil {
		finish(err, nil)
		return nil, rangeRecoverySummary{}, err
	}
	if first.Total <= 0 || first.Total > maxRangeRecoveryBytes {
		err := fmt.Errorf("range response total %d is outside allowed range 1..%d", first.Total, maxRangeRecoveryBytes)
		finish(err, map[string]any{"range_total_bytes": first.Total})
		return nil, rangeRecoverySummary{}, err
	}
	if first.End >= first.Total {
		err := fmt.Errorf("range response end %d exceeds total %d", first.End, first.Total)
		finish(err, nil)
		return nil, rangeRecoverySummary{}, err
	}

	body := append([]byte(nil), first.Body...)
	chunks := 1
	previousEnd := first.End
	for len(body) < first.Total {
		start := previousEnd + 1 - subscriptionRangeOverlap
		if start < 0 {
			start = 0
		}
		end := start + subscriptionRangeChunkSize - 1
		if end >= first.Total {
			end = first.Total - 1
		}
		chunks++
		part, err := fetchSubscriptionRange(ctx, client, source, userAgent, start, end, "assemble", chunks, first.ValidatorKey, first.Validator, stage)
		if err != nil {
			finish(err, map[string]any{"range_chunks": chunks - 1, "range_total_bytes": first.Total})
			return nil, rangeRecoverySummary{}, err
		}
		if part.Total != first.Total {
			err := fmt.Errorf("range response total changed from %d to %d", first.Total, part.Total)
			finish(err, map[string]any{"range_chunks": chunks})
			return nil, rangeRecoverySummary{}, err
		}
		if part.ValidatorKey != first.ValidatorKey || part.Validator != first.Validator {
			err := fmt.Errorf("range response validator changed during recovery")
			finish(err, map[string]any{"range_chunks": chunks})
			return nil, rangeRecoverySummary{}, err
		}
		overlap := previousEnd - start + 1
		if overlap < 0 || overlap > len(part.Body) || start+overlap > len(body) {
			err := fmt.Errorf("range overlap %d is invalid", overlap)
			finish(err, map[string]any{"range_chunks": chunks})
			return nil, rangeRecoverySummary{}, err
		}
		if !bytes.Equal(body[start:start+overlap], part.Body[:overlap]) {
			err := fmt.Errorf("range overlap mismatch at bytes %d-%d", start, start+overlap-1)
			finish(err, map[string]any{"range_chunks": chunks})
			return nil, rangeRecoverySummary{}, err
		}
		body = append(body, part.Body[overlap:]...)
		previousEnd = part.End
	}
	if len(body) != first.Total {
		err := fmt.Errorf("assembled range body has %d bytes, expected %d", len(body), first.Total)
		finish(err, map[string]any{"range_chunks": chunks})
		return nil, rangeRecoverySummary{}, err
	}

	verifyFirst, err := fetchSubscriptionRange(ctx, client, source, userAgent, 0, first.End, "verify_first", chunks+1, first.ValidatorKey, first.Validator, stage)
	if err != nil {
		finish(err, map[string]any{"range_chunks": chunks})
		return nil, rangeRecoverySummary{}, err
	}
	if verifyFirst.Total != first.Total || verifyFirst.ValidatorKey != first.ValidatorKey || verifyFirst.Validator != first.Validator || !bytes.Equal(verifyFirst.Body, body[:len(verifyFirst.Body)]) {
		err := fmt.Errorf("range first-boundary verification changed during recovery")
		finish(err, map[string]any{"range_chunks": chunks})
		return nil, rangeRecoverySummary{}, err
	}

	lastStart := first.Total - subscriptionRangeChunkSize
	if lastStart < 0 {
		lastStart = 0
	}
	verifyLast, err := fetchSubscriptionRange(ctx, client, source, userAgent, lastStart, first.Total-1, "verify_last", chunks+2, first.ValidatorKey, first.Validator, stage)
	if err != nil {
		finish(err, map[string]any{"range_chunks": chunks})
		return nil, rangeRecoverySummary{}, err
	}
	if verifyLast.Total != first.Total || verifyLast.ValidatorKey != first.ValidatorKey || verifyLast.Validator != first.Validator || !bytes.Equal(verifyLast.Body, body[lastStart:]) {
		err := fmt.Errorf("range last-boundary verification changed during recovery")
		finish(err, map[string]any{"range_chunks": chunks})
		return nil, rangeRecoverySummary{}, err
	}

	bodySum := sha256.Sum256(body)
	summary := rangeRecoverySummary{Chunks: chunks, TotalBytes: len(body), BodySHA256: hex.EncodeToString(bodySum[:])}
	finish(nil, map[string]any{
		"range_chunks":       chunks,
		"verification_reads": 2,
		"range_total_bytes":  len(body),
		"body_sha256":        summary.BodySHA256,
		"validator":          rangeValidatorName(first.ValidatorKey),
	})
	return body, summary, nil
}

func fetchSubscriptionRange(ctx context.Context, client *http.Client, source Source, userAgent string, start, end int, phase string, index int, validatorKey, validator string, stage func(string, map[string]any) func(error, map[string]any)) (rangeResponse, error) {
	identity := sourceStageFields(source)
	fields := mergeStageFields(identity, map[string]any{
		"chunk_index": index,
		"phase":       phase,
		"range_start": start,
		"range_end":   end,
		"protocol":    "HTTP/1.1",
	})
	finish := stage("range_chunk", fields)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourcePrimaryURI(source), nil)
	if err != nil {
		err = fmt.Errorf("range request could not be created")
		finish(err, nil)
		return rangeResponse{}, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
	if validatorKey != "" && validator != "" {
		req.Header.Set("If-Range", validator)
	}
	resp, err := client.Do(req)
	if err != nil {
		err = fmt.Errorf("range request %d-%d failed: %s", start, end, safeTransportError(err, source))
		finish(err, map[string]any{"failure_kind": "transport"})
		return rangeResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		err = fmt.Errorf("range request %d-%d failed: HTTP %s", start, end, resp.Status)
		finish(err, map[string]any{"status": resp.Status, "status_code": resp.StatusCode, "response_uri": MaskURI(resp.Request.URL.String())})
		return rangeResponse{}, err
	}
	actualStart, actualEnd, total, err := parseContentRange(resp.Header.Get("Content-Range"))
	if err != nil {
		finish(err, map[string]any{"status": resp.Status, "status_code": resp.StatusCode})
		return rangeResponse{}, err
	}
	if actualStart != start || actualEnd > end || (actualEnd != end && actualEnd != total-1) {
		err = fmt.Errorf("range response was bytes %d-%d, expected %d-%d", actualStart, actualEnd, start, end)
		finish(err, map[string]any{"range_total_bytes": total})
		return rangeResponse{}, err
	}
	expectedBytes := actualEnd - actualStart + 1
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, int64(expectedBytes+1)))
	if readErr != nil {
		err = fmt.Errorf("range response %d-%d could not be read: %w", start, end, readErr)
		finish(err, map[string]any{"bytes_received": len(body), "expected_bytes": expectedBytes})
		return rangeResponse{}, err
	}
	if len(body) != expectedBytes {
		err = fmt.Errorf("range response %d-%d returned %d bytes, expected %d", start, end, len(body), expectedBytes)
		finish(err, map[string]any{"bytes_received": len(body), "expected_bytes": expectedBytes})
		return rangeResponse{}, err
	}
	responseValidatorKey, responseValidator := rangeValidator(resp.Header)
	if validatorKey != "" && (responseValidatorKey != validatorKey || responseValidator != validator) {
		err = fmt.Errorf("range response validator changed")
		finish(err, nil)
		return rangeResponse{}, err
	}
	finish(nil, map[string]any{
		"status":            resp.Status,
		"status_code":       resp.StatusCode,
		"bytes":             len(body),
		"range_total_bytes": total,
		"response_uri":      MaskURI(resp.Request.URL.String()),
		"validator":         rangeValidatorName(responseValidatorKey),
	})
	return rangeResponse{Body: body, Start: actualStart, End: actualEnd, Total: total, ValidatorKey: responseValidatorKey, Validator: responseValidator}, nil
}

// subscriptionHTTPClient pins all subscription requests to HTTP/1.1 so the
// initial download and verified Range recovery share one transport policy.
func subscriptionHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{}
	} else {
		transport.TLSClientConfig = transport.TLSClientConfig.Clone()
	}
	transport.TLSClientConfig.NextProtos = []string{"http/1.1"}
	transport.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
	transport.ForceAttemptHTTP2 = false
	transport.Protocols = new(http.Protocols)
	transport.Protocols.SetHTTP1(true)
	transport.Protocols.SetHTTP2(false)
	return &http.Client{Transport: transport}
}

func parseContentRange(value string) (int, int, int, error) {
	matches := contentRangePattern.FindStringSubmatch(strings.TrimSpace(value))
	if matches == nil {
		return 0, 0, 0, fmt.Errorf("range response Content-Range %q is invalid", value)
	}
	values := make([]int, 3)
	for i := range values {
		parsed, err := strconv.ParseInt(matches[i+1], 10, 64)
		if err != nil || parsed > int64(maxRangeRecoveryBytes) {
			return 0, 0, 0, fmt.Errorf("range response Content-Range %q is outside supported limits", value)
		}
		values[i] = int(parsed)
	}
	if values[0] < 0 || values[1] < values[0] || values[2] <= values[1] {
		return 0, 0, 0, fmt.Errorf("range response Content-Range %q is inconsistent", value)
	}
	return values[0], values[1], values[2], nil
}

func rangeValidator(header http.Header) (string, string) {
	if value := strings.TrimSpace(header.Get("ETag")); value != "" {
		return "etag", value
	}
	if value := strings.TrimSpace(header.Get("Last-Modified")); value != "" {
		return "last_modified", value
	}
	return "", ""
}

func rangeValidatorName(key string) string {
	if key == "" {
		return "overlap_and_boundary_recheck"
	}
	return key
}

func sourceStageFields(source Source) map[string]any {
	return map[string]any{
		"display_id": sourceDisplayName(source),
		"type":       sourceType(source),
		"uri":        MaskURI(sourcePrimaryURI(source)),
	}
}

func sourceLogLabel(source Source) string {
	return fmt.Sprintf("subscription %s (%s)", sourceDisplayName(source), MaskURI(sourcePrimaryURI(source)))
}

func cloneStageFields(fields map[string]any) map[string]any {
	return mergeStageFields(nil, fields)
}

func mergeStageFields(base, extra map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range base {
		out[key] = value
	}
	for key, value := range extra {
		out[key] = value
	}
	return out
}

func safeResponsePreview(body []byte, source Source) string {
	const maxPreviewBytes = 4096
	if len(body) > maxPreviewBytes {
		body = body[:maxPreviewBytes]
	}
	preview := strings.ToValidUTF8(string(body), "�")
	preview = strings.Join(strings.Fields(preview), " ")
	if preview == "" {
		return ""
	}
	rawURI := sourcePrimaryURI(source)
	if rawURI != "" {
		preview = strings.ReplaceAll(preview, rawURI, MaskURI(rawURI))
		if parsed, err := url.Parse(rawURI); err == nil {
			for _, values := range parsed.Query() {
				for _, value := range values {
					if value != "" {
						preview = strings.ReplaceAll(preview, value, "<redacted>")
					}
				}
			}
		}
	}
	preview = sensitiveResponseFieldPattern.ReplaceAllString(preview, `${1}${2}<redacted>`)
	preview = responseURLPattern.ReplaceAllStringFunc(preview, MaskURI)
	return preview
}

func safeTransportError(err error, source Source) string {
	if err == nil {
		return "transport error"
	}
	if text := safeResponsePreview([]byte(err.Error()), source); text != "" {
		return text
	}
	return "transport error"
}

func parseRemoteSubscription(sourceID string, data []byte) (subscriptionDoc, error) {
	doc, yamlErr := parseSubscription(sourceID, data)
	if yamlErr == nil {
		return doc, nil
	}
	doc, uriErr := parseProxyURIList(sourceID, data)
	if uriErr == nil {
		return doc, nil
	}
	if decoded, err := decodeBase64BytesStrict(data); err == nil && !bytes.Equal(bytes.TrimSpace(decoded), bytes.TrimSpace(data)) {
		doc, decodedYAMLErr := parseSubscription(sourceID, decoded)
		if decodedYAMLErr == nil {
			return doc, nil
		}
		doc, decodedURIErr := parseProxyURIList(sourceID, decoded)
		if decodedURIErr == nil {
			return doc, nil
		}
	}
	return subscriptionDoc{}, fmt.Errorf("source %q response is neither Mihomo YAML nor MVP proxy URI lines: yaml: %v; proxy_uri_lines: %v", sourceID, yamlErr, uriErr)
}

func parseSubscription(sourceID string, data []byte) (subscriptionDoc, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return subscriptionDoc{}, fmt.Errorf("source %q response was empty", sourceID)
	}
	var raw any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return subscriptionDoc{}, fmt.Errorf("source %q response is not valid YAML", sourceID)
	}
	doc, ok := raw.(map[string]any)
	if !ok {
		return subscriptionDoc{}, fmt.Errorf("source %q subscription YAML must be a map", sourceID)
	}
	proxies, ok := doc["proxies"].([]any)
	if !ok || len(proxies) == 0 {
		return subscriptionDoc{}, fmt.Errorf("source %q subscription has no proxies", sourceID)
	}
	for _, rawProxy := range proxies {
		proxy, ok := rawProxy.(map[string]any)
		if !ok {
			return subscriptionDoc{}, fmt.Errorf("source %q subscription contains an invalid proxy entry", sourceID)
		}
		if strings.TrimSpace(stringValue(proxy["name"])) == "" {
			return subscriptionDoc{}, fmt.Errorf("source %q subscription contains a proxy without name", sourceID)
		}
	}
	return subscriptionDoc{Data: doc, Format: subscriptionFormatMihomoYAML}, nil
}

func readSubscription(path string) (subscriptionDoc, error) {
	file, err := os.Open(path)
	if err != nil {
		return subscriptionDoc{}, err
	}
	defer file.Close()
	var artifact subscriptionArtifact
	if err := gob.NewDecoder(file).Decode(&artifact); err != nil {
		return subscriptionDoc{}, err
	}
	if artifact.Version != 1 {
		return subscriptionDoc{}, fmt.Errorf("subscription artifact schema version mismatch: expected 1, got %d; run localclash subscriptions refresh", artifact.Version)
	}
	if len(artifact.Data) == 0 {
		return subscriptionDoc{}, fmt.Errorf("subscription artifact %q is empty; run localclash subscriptions refresh", path)
	}
	return subscriptionDoc{Data: artifact.Data, Raw: artifact.Raw, Format: artifact.Format}, nil
}

func writeSubscriptionArtifact(path string, doc subscriptionDoc) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	encodeErr := gob.NewEncoder(file).Encode(subscriptionArtifact{Version: 1, Data: doc.Data, Raw: doc.Raw, Format: doc.Format})
	closeErr := file.Close()
	if encodeErr != nil {
		_ = os.Remove(tmp)
		return encodeErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	return os.Rename(tmp, path)
}

func mergeSubscriptions(sources []Source, docs map[string]subscriptionDoc) (map[string]any, int, error) {
	prefixSource := len(docs) > 1
	usedNames := map[string]bool{}
	var mergedProxies []any
	renamed := 0
	for _, source := range sources {
		doc, ok := docs[source.ID]
		if !ok {
			continue
		}
		proxies, sourceRenamed, err := renameSourceProxies(source, doc, prefixSource, usedNames)
		if err != nil {
			return nil, 0, err
		}
		mergedProxies = append(mergedProxies, proxies...)
		renamed += sourceRenamed
	}
	return map[string]any{"proxies": mergedProxies}, renamed, nil
}

type plannedProxyRename struct {
	proxy        map[string]any
	originalName string
	newName      string
}

func renameSourceProxies(source Source, doc subscriptionDoc, prefixSource bool, usedNames map[string]bool) ([]any, int, error) {
	plans := make([]plannedProxyRename, 0, len(anySlice(doc.Data["proxies"])))
	renamedTargets := map[string][]string{}
	renamed := 0

	// Plan every final name before rewriting references. A dialer-proxy may
	// point forward to a proxy that appears later in the same subscription.
	for _, rawProxy := range anySlice(doc.Data["proxies"]) {
		proxy, ok := rawProxy.(map[string]any)
		if !ok {
			return nil, 0, fmt.Errorf("source %q subscription contains an invalid proxy entry", source.ID)
		}
		proxy = cloneMap(proxy)
		originalName := stringValue(proxy["name"])
		if strings.TrimSpace(originalName) == "" {
			return nil, 0, fmt.Errorf("source %q subscription contains a proxy without name", source.ID)
		}

		newName := originalName
		if prefixSource {
			newName = "[" + sourceDisplayName(source) + "] " + newName
		}
		// Mihomo requires unique proxy names, but unsafe subscription
		// payloads can contain duplicates. Normalize duplicates during
		// merge so the generated artifact remains selector-safe.
		newName = uniqueProxyName(newName, usedNames)
		usedNames[newName] = true
		renamedTargets[originalName] = append(renamedTargets[originalName], newName)
		plans = append(plans, plannedProxyRename{proxy: proxy, originalName: originalName, newName: newName})
		if newName != originalName {
			renamed++
		}
	}

	proxies := make([]any, 0, len(plans))
	for _, plan := range plans {
		plan.proxy["name"] = plan.newName
		rawDialerProxy, hasDialerProxy := plan.proxy["dialer-proxy"]
		if hasDialerProxy {
			dialerProxy, ok := rawDialerProxy.(string)
			if !ok || strings.TrimSpace(dialerProxy) == "" {
				return nil, 0, fmt.Errorf("source %q proxy %q has invalid dialer-proxy; expected a non-empty proxy or policy-group name", source.ID, plan.originalName)
			}
			targets := renamedTargets[dialerProxy]
			switch len(targets) {
			case 0:
				// The reference may name a localClash-owned policy group, which
				// is outside the subscription source and must remain unchanged.
			case 1:
				plan.proxy["dialer-proxy"] = targets[0]
			default:
				return nil, 0, fmt.Errorf("source %q proxy %q dialer-proxy %q is ambiguous: %d proxies share that name", source.ID, plan.originalName, dialerProxy, len(targets))
			}
		}
		proxies = append(proxies, plan.proxy)
	}

	return proxies, renamed, nil
}

func uniqueProxyName(name string, used map[string]bool) string {
	if !used[name] {
		return name
	}
	base := name
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s (%d)", base, i)
		if !used[candidate] {
			return candidate
		}
	}
}

func summarizeArtifact(path string) ArtifactSummary {
	result := ArtifactSummary{Path: path}
	info, err := os.Stat(path)
	if err != nil {
		return result
	}
	if info.IsDir() {
		return result
	}
	result.Exists = true
	result.UpdatedAt = info.ModTime().Format(time.RFC3339)
	doc, err := readSubscription(path)
	if err != nil {
		return result
	}
	counts := summarizeMap(doc.Data)
	result.ProxiesCount = counts.ProxiesCount
	result.ProxyGroupsCount = counts.ProxyGroupsCount
	result.RulesCount = counts.RulesCount
	return result
}

func summarizeMap(doc map[string]any) ArtifactSummary {
	return ArtifactSummary{
		ProxiesCount:     len(anySlice(doc["proxies"])),
		ProxyGroupsCount: len(anySlice(doc["proxy-groups"])),
		RulesCount:       len(anySlice(doc["rules"])),
	}
}

func MaskURL(rawURL string) string {
	return MaskURI(rawURL)
}

func MaskURI(rawURI string) string {
	kind, _, err := classifyInputURI(rawURI)
	if err == nil && kind == sourceTypeInlineProxyURIs {
		scheme, _, _ := strings.Cut(strings.TrimSpace(rawURI), "://")
		return strings.ToLower(scheme) + "://<redacted>"
	}
	parsed, err := url.Parse(rawURI)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "<invalid-uri>"
	}
	path := parsed.EscapedPath()
	if path == "" {
		path = "/"
	}
	const maxPath = 40
	if len(path) > maxPath {
		path = path[:maxPath] + "..."
	}
	masked := parsed.Scheme + "://" + parsed.Host + path
	if parsed.RawQuery != "" {
		masked += "?..."
	}
	return masked
}

func sourceType(source Source) string {
	if source.Type != "" {
		return source.Type
	}
	if source.URL != "" || source.URI != "" {
		return sourceTypeRemoteSubscription
	}
	if len(source.URIs) > 0 {
		return sourceTypeInlineProxyURIs
	}
	return ""
}

func sourcePrimaryURI(source Source) string {
	if strings.TrimSpace(source.URI) != "" {
		return strings.TrimSpace(source.URI)
	}
	if strings.TrimSpace(source.URL) != "" {
		return strings.TrimSpace(source.URL)
	}
	if len(source.URIs) == 1 {
		return strings.TrimSpace(source.URIs[0])
	}
	return ""
}

func normalizeSourceForConfig(source Source, id, displayName string) Source {
	switch sourceType(source) {
	case sourceTypeRemoteSubscription:
		return Source{ID: id, DisplayName: displayName, Type: sourceTypeRemoteSubscription, URI: sourcePrimaryURI(source)}
	case sourceTypeInlineProxyURIs:
		uris := make([]string, 0, len(source.URIs))
		seen := map[string]bool{}
		for _, rawURI := range source.URIs {
			uri := strings.TrimSpace(rawURI)
			if uri == "" || seen[uri] {
				continue
			}
			seen[uri] = true
			uris = append(uris, uri)
		}
		return Source{ID: id, DisplayName: displayName, Type: sourceTypeInlineProxyURIs, URIs: uris}
	default:
		return Source{ID: id, DisplayName: displayName}
	}
}

func sourceURIHash(source Source) string {
	canonical, err := canonicalSourceURI(source)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])[:sourceIDHashLength]
}

func legacyURLForResult(source Source) string {
	if sourceType(source) != sourceTypeRemoteSubscription {
		return ""
	}
	return MaskURI(sourcePrimaryURI(source))
}

func legacyMaskedURL(source Source) string {
	return legacyURLForResult(source)
}

func legacyURLForGet(source Source) string {
	if sourceType(source) != sourceTypeRemoteSubscription {
		return ""
	}
	return sourcePrimaryURI(source)
}

func artifactPath(runtimeDir, id string) string {
	return filepath.Join(runtimeDir, id+".gob")
}

func anySlice(value any) []any {
	if values, ok := value.([]any); ok {
		return values
	}
	return nil
}

func proxyMaps(doc map[string]any) []map[string]any {
	raw := anySlice(doc["proxies"])
	proxies := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if proxy, ok := item.(map[string]any); ok {
			proxies = append(proxies, proxy)
		}
	}
	return proxies
}

func cloneMap(in map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range in {
		out[key] = value
	}
	return out
}

func stringValue(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}
