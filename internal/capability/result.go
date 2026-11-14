package capability

import (
	"sort"
	"strings"
)

type Result struct {
	Profile                string       `json:"profile"`
	SnapshotPath           string       `json:"snapshot_path"`
	Candidates             int          `json:"candidates"`
	Probed                 int          `json:"probed"`
	PrioritizedCount       int          `json:"prioritized_count,omitempty"`
	UnprobedCount          int          `json:"unprobed_count,omitempty"`
	CanceledCount          int          `json:"canceled_count,omitempty"`
	DeduplicatedCount      int          `json:"deduplicated_count,omitempty"`
	HelperExcludedCount    int          `json:"helper_excluded_count,omitempty"`
	Qualified              []string     `json:"qualified"`
	QualifiedCount         int          `json:"qualified_count"`
	ObservedQualifiedCount int          `json:"observed_qualified_count"`
	RetainedCount          int          `json:"retained_count"`
	UnavailableCount       int          `json:"unavailable_count"`
	DurationMS             int64        `json:"duration_ms"`
	AutoQuality            *AutoQuality `json:"auto_quality,omitempty"`
	// ChatGPTStreaming counts distinct tested exits. Its per-check counters count attempts.
	ChatGPTStreaming *EnhancedProbeSummary `json:"chatgpt_streaming,omitempty"`
}

type AutoQuality struct {
	FirstQualified DoHLatencySummary     `json:"first_qualified"`
	Selected       DoHLatencySummary     `json:"selected"`
	YouTube        YouTubeProbeSummary   `json:"youtube"`
	Enhanced       *EnhancedProbeSummary `json:"enhanced,omitempty"`
	FillDurationMS int64                 `json:"fill_duration_ms,omitempty"`
	DrainedCount   int                   `json:"drained_count"`
	HistoryMatches int                   `json:"history_matches,omitempty"`
	HistoryWarning string                `json:"history_warning,omitempty"`
}

type YouTubeProbeSummary struct {
	Probed int `json:"probed"`
	Passed int `json:"passed"`
}

type NetworkProbeSummary struct {
	Probed int `json:"probed"`
	Passed int `json:"passed"`
}

type EnhancedProbeSummary struct {
	Probed int                            `json:"probed"`
	Passed int                            `json:"passed"`
	Checks map[string]NetworkProbeSummary `json:"checks"`
}

// DoHLatencySummary describes each exit's slowest successful DoH query.
type DoHLatencySummary struct {
	P50MS int64 `json:"p50_ms"`
	P90MS int64 `json:"p90_ms"`
	MaxMS int64 `json:"max_ms"`
}

func QualifiedByProfile(result Result) map[string][]string {
	return map[string][]string{result.Profile: append([]string{}, result.Qualified...)}
}

func Profiles(configured []string) []string {
	seen := map[string]bool{}
	for _, profile := range configured {
		profile = strings.TrimSpace(profile)
		if profile != "" {
			seen[profile] = true
		}
	}
	out := make([]string, 0, len(seen))
	for profile := range seen {
		out = append(out, profile)
	}
	sort.Strings(out)
	return out
}
