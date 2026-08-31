package capability

import (
	"sort"
	"strings"
)

type Result struct {
	Profile                string   `json:"profile"`
	SnapshotPath           string   `json:"snapshot_path"`
	Candidates             int      `json:"candidates"`
	Probed                 int      `json:"probed"`
	DeduplicatedCount      int      `json:"deduplicated_count,omitempty"`
	HelperExcludedCount    int      `json:"helper_excluded_count,omitempty"`
	Qualified              []string `json:"qualified"`
	QualifiedCount         int      `json:"qualified_count"`
	ObservedQualifiedCount int      `json:"observed_qualified_count"`
	RetainedCount          int      `json:"retained_count"`
	UnavailableCount       int      `json:"unavailable_count"`
	DurationMS             int64    `json:"duration_ms"`
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
