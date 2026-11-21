package poolprepare

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"time"

	"localclash/internal/capability"
	"localclash/internal/poolcontract"
	"localclash/internal/proxyprobe"
)

// YouTube's recommended sustained rates for HD and 4K, in decimal Mbps.
const bulkHDMbps = 5
const bulk4KMbps = 20

// History belongs to a subscription revision, including its dialer definitions.
type bulkObservation struct {
	Checked      time.Time `json:"checked"`
	Mbps         float64   `json:"mbps"`
	PreviousMbps float64   `json:"previous_mbps,omitempty"`
}

func (o bulkObservation) tier() string {
	switch {
	case min(o.Mbps, o.PreviousMbps) >= bulk4KMbps:
		return "4K-stable"
	case min(o.Mbps, o.PreviousMbps) >= bulkHDMbps:
		return "HD-stable"
	case o.Mbps >= bulk4KMbps:
		return "4K"
	case o.Mbps >= bulkHDMbps:
		return "HD"
	default:
		return "reserve"
	}
}

// Start with up to 30 candidates. Subsequent rounds sample a member and a
// challenger serially, including when too few exits ever meet the HD floor.
// This avoids repeating a 180 MiB bootstrap on every short scheduler interval.
func bulkCandidates(candidates []autoCandidate, history map[string]bulkObservation) []autoCandidate {
	ordered := slices.Clone(candidates)
	sort.SliceStable(ordered, func(i, j int) bool { return history[ordered[i].name].Checked.Before(history[ordered[j].name].Checked) })
	var members, others []autoCandidate
	for _, c := range ordered {
		if history[c.name].Mbps >= bulkHDMbps {
			members = append(members, c)
		} else {
			others = append(others, c)
		}
	}
	if len(history) == 0 {
		return ordered[:min(len(ordered), poolcontract.BulkMinimum)]
	}
	var next []autoCandidate
	if len(members) > 0 {
		next = append(next, members[0])
	}
	next = append(next, others[:min(len(others), 2-len(next))]...)
	return next
}

func qualifyBulk(ctx context.Context, input poolcontract.Input, names []string, opts Options) (capability.Result, error) {
	start := time.Now()
	result := capability.Result{Profile: poolcontract.BulkCapability, Candidates: len(names), BulkQuality: &capability.BulkQuality{}}
	pool, _ := poolcontract.Lookup(poolcontract.BulkCapability)
	dir := filepath.Dir(filepath.Join(opts.RuntimeDir, pool.Path(input.Revision)))
	path := filepath.Join(dir, "youtube-throughput-v1.json")
	history := map[string]bulkObservation{}
	data, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(data, &history); err != nil {
			return result, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return result, err
	}
	if history == nil {
		history = map[string]bulkObservation{}
	}
	candidates, _, _, err := autoCandidates(input.Proxies, names, nil, nil)
	if err != nil {
		return result, err
	}
	result.DeduplicatedCount = len(names) - len(candidates)
	batch := bulkCandidates(candidates, history)
	if len(batch) > 0 {
		testNames := make([]string, len(batch))
		for i, c := range batch {
			testNames[i] = c.name
		}
		session, err := proxyprobe.Start(ctx, proxyprobe.RequiredDefinitions(input.Proxies, testNames), testNames, proxyprobe.Options{CorePath: opts.CorePath, RuntimeParent: opts.ProbeDir, RuntimePrefix: "youtube-bulk-"})
		if err != nil {
			return result, err
		}
		defer session.Close()
		for i, name := range testNames {
			if err := ctx.Err(); err != nil {
				return result, err
			}
			client, err := session.HTTPClient(i, 20*time.Second, true)
			if err != nil {
				return result, err
			}
			rate, stage := measureBulkVideo(ctx, client)
			client.CloseIdleConnections()
			current := history[name]
			current.Checked = time.Now().UTC()
			switch stage {
			case "resolve":
				result.BulkQuality.ResolveFailures++
				// Failure to obtain a media URL provides no bandwidth evidence.
			case "range":
				result.BulkQuality.RangeFailures++
				current.Mbps, current.PreviousMbps = 0, 0
			default:
				current.PreviousMbps, current.Mbps = current.Mbps, rate
				if rate >= bulkHDMbps {
					result.ObservedQualifiedCount++
				} else {
					result.BulkQuality.BelowTarget++
				}
			}
			history[name] = current
			result.Probed++
		}
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if err := session.Err(); err != nil {
			return result, err
		}
		if err := os.MkdirAll(dir, 0700); err != nil {
			return result, err
		}
		data, err := json.Marshal(history)
		if err != nil {
			return result, err
		}
		if err := poolcontract.WriteFile(path, data); err != nil {
			return result, err
		}
	}
	result.UnprobedCount = len(candidates) - result.Probed
	result.UnavailableCount = result.Probed - result.ObservedQualifiedCount
	selectBulkMembers(&result, candidates, names, history)
	result.DurationMS = time.Since(start).Milliseconds()
	return result, nil
}

func selectBulkMembers(result *capability.Result, candidates []autoCandidate, names []string, history map[string]bulkObservation) {
	result.BulkTiers = map[string]string{}
	for _, c := range candidates {
		o := history[c.name]
		if o.Mbps >= bulkHDMbps {
			result.Qualified = append(result.Qualified, c.name)
			result.BulkTiers[c.name] = o.tier()
		}
	}
	sort.SliceStable(result.Qualified, func(i, j int) bool {
		a, b := result.Qualified[i], result.Qualified[j]
		wa, wb := poolcontract.BulkWeight(result.BulkTiers[a]), poolcontract.BulkWeight(result.BulkTiers[b])
		if wa != wb {
			return wa > wb
		}
		return history[a].Mbps > history[b].Mbps
	})
	result.BulkQuality.Measured = len(result.Qualified)
	result.RetainedCount = len(result.Qualified) - result.ObservedQualifiedCount
	// A completed slower transfer is better evidence for a reserve than an
	// untested exit. Only promote it if a preferred position is still empty.
	var fill []string
	for _, c := range candidates {
		rate := history[c.name].Mbps
		if rate > 0 && rate < bulkHDMbps {
			fill = append(fill, c.name)
		}
	}
	sort.SliceStable(fill, func(i, j int) bool { return history[fill[i]].Mbps > history[fill[j]].Mbps })
	for _, c := range candidates {
		if history[c.name].Checked.IsZero() {
			fill = append(fill, c.name)
		}
	}
	for _, c := range candidates {
		fill = append(fill, c.name)
	}
	poolcontract.FillBulk(result, append(fill, names...))
}
