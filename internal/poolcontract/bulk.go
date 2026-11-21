package poolcontract

import (
	"encoding/json"
	"errors"
	"maps"
	"strings"

	"localclash/internal/capability"
	"localclash/internal/smartpolicy"
)

const BulkCapability = "youtube.sustained-download.v1"
const BulkMinimum = 30

// Prefer independently repeated throughput, then the current warm ranges.
var bulkPriorities = []smartpolicy.Rule{
	{Label: "4K-stable", Pattern: `^YT/4K-stable\s`, Factor: 5},
	{Label: "HD-stable", Pattern: `^YT/HD-stable\s`, Factor: 3},
	{Label: "4K", Pattern: `^YT/4K\s`, Factor: 2},
	{Label: "HD", Pattern: `^YT/HD\s`, Factor: 1.5},
	{Label: "reserve", Pattern: `^YT/reserve\s`, Factor: 0.5},
}

func BulkWeight(tier string) float64 {
	for _, rule := range bulkPriorities {
		if rule.Label == tier {
			return rule.Factor
		}
	}
	return 0
}
func BulkSmartPriority() []smartpolicy.Rule { return smartpolicy.Clone(bulkPriorities) }

// FillBulk keeps distinct measured exits first, then fills from other selectable
// subscription names. Aliases can fill the floor when distinct definitions run out.
func FillBulk(result *capability.Result, ordered []string) {
	seen := map[string]bool{}
	for _, name := range result.Qualified {
		seen[name] = true
	}
	for _, name := range ordered {
		if len(result.Qualified) >= BulkMinimum {
			break
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		result.Qualified = append(result.Qualified, name)
		result.BulkTiers[name] = "reserve"
		result.BulkQuality.Reserves++
	}
	result.QualifiedCount = len(result.Qualified)
}

func BulkSeed(names []string) capability.Result {
	result := capability.Result{Profile: BulkCapability, Candidates: len(names), UnprobedCount: len(names),
		BulkQuality: &capability.BulkQuality{}, BulkTiers: map[string]string{}}
	FillBulk(&result, names)
	return result
}

// Materialize the measured rank in provider order for Meta fallback and in
// the provider-local adapter name for Smart policy-priority.
func ResultProviderData(pool Pool, input Input, result capability.Result) ([]byte, []string, error) {
	if pool.Capability != BulkCapability || len(result.Qualified) == 0 {
		return ProviderData(pool, input, result.Qualified, result.StatsigCountries)
	}

	proxies, names, err := BulkProxies(input, result)
	if err != nil {
		return nil, nil, err
	}
	data, err := json.Marshal(providerDocument{QualificationVersion: 1, Proxies: proxies})
	return data, names, err
}

func BulkProxies(input Input, result capability.Result) ([]map[string]any, []string, error) {
	if len(result.Qualified) == 0 {
		return []map[string]any{Reject()}, []string{RejectName}, nil
	}

	defs := map[string]map[string]any{}
	for _, p := range input.Proxies {
		name, _ := p["name"].(string)
		defs[strings.TrimSpace(name)] = p
	}
	var proxies []map[string]any
	var names []string
	for _, name := range result.Qualified {
		tier := result.BulkTiers[name]
		p := maps.Clone(defs[name])
		if p == nil || BulkWeight(tier) == 0 {
			return nil, nil, errors.New("invalid YouTube pool member or throughput tier")
		}
		delete(defs, name)
		full := "YT/" + tier + " " + name
		p["name"] = full
		names = append(names, full)
		proxies = append(proxies, p)
	}
	return proxies, names, nil
}
