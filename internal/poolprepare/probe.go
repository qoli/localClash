package poolprepare

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"localclash/internal/capability"
	"localclash/internal/chatgptavailable"
	"localclash/internal/poolcontract"
	"localclash/internal/proxyprobe"
)

var overseasDoH = []string{"https://1.1.1.1/dns-query", "https://8.8.8.8/dns-query"}

type measurement struct {
	index    int
	worstDoH time.Duration
}

type autoBatchResult struct {
	completed    []measurement
	filledAt     time.Time
	drainedCount int
}

func qualifyAuto(ctx context.Context, input poolcontract.Input, names []string, opts Options) (capability.Result, error) {
	start := time.Now()
	previous := previousAutoPriority(opts.RuntimeDir, input.Revision)
	representatives, prioritized, err := autoCandidates(input.Proxies, names, previous)
	if err != nil {
		return capability.Result{}, err
	}
	result := capability.Result{
		Profile: poolcontract.AutoCapability, Candidates: len(names),
		PrioritizedCount:  prioritized,
		DeduplicatedCount: len(names) - len(representatives),
		AutoQuality:       &capability.AutoQuality{},
	}
	var qualified []measurement
	offset := 0
	for batch := range slices.Chunk(representatives, proxyprobe.BatchSize) {
		remaining := poolcontract.AutoLimit - len(qualified)
		if remaining <= 0 {
			break
		}
		measured, err := probeAutoBatch(ctx, input.Proxies, batch, remaining, opts)
		if err != nil {
			return capability.Result{}, err
		}
		result.Probed += len(measured.completed)
		if !measured.filledAt.IsZero() {
			result.AutoQuality.FillDurationMS = measured.filledAt.Sub(start).Milliseconds()
			result.AutoQuality.DrainedCount = measured.drainedCount
		}
		for _, attempt := range measured.completed {
			if attempt.worstDoH > 0 {
				attempt.index += offset
				qualified = append(qualified, attempt)
			} else {
				result.UnavailableCount++
			}
		}
		offset += len(batch)
	}
	result.UnprobedCount = len(representatives) - result.Probed
	result.ObservedQualifiedCount = len(qualified)
	limit := min(len(qualified), poolcontract.AutoLimit)
	result.AutoQuality.FirstQualified = summarizeDoH(qualified[:limit])
	rankByDoH(qualified)
	selected := qualified[:limit]
	result.AutoQuality.Selected = summarizeDoH(selected)
	for _, entry := range selected {
		result.Qualified = append(result.Qualified, representatives[entry.index])
	}
	result.QualifiedCount = len(result.Qualified)
	result.DurationMS = time.Since(start).Milliseconds()
	return result, nil
}

func rankByDoH(measured []measurement) {
	sort.Slice(measured, func(i, j int) bool {
		if measured[i].worstDoH == measured[j].worstDoH {
			return measured[i].index < measured[j].index
		}
		return measured[i].worstDoH < measured[j].worstDoH
	})
}

func summarizeDoH(measured []measurement) capability.DoHLatencySummary {
	if len(measured) == 0 {
		return capability.DoHLatencySummary{}
	}
	latencies := make([]time.Duration, len(measured))
	for index, entry := range measured {
		latencies[index] = entry.worstDoH
	}
	slices.Sort(latencies)
	count := len(latencies)
	return capability.DoHLatencySummary{
		P50MS: latencies[(count+1)/2-1].Milliseconds(),
		P90MS: latencies[(count*9+9)/10-1].Milliseconds(),
		MaxMS: latencies[count-1].Milliseconds(),
	}
}

func autoCandidates(proxies []map[string]any, eligible []string, previous map[string]bool) ([]string, int, error) {
	selectable := make(map[string]bool, len(eligible))
	for _, name := range eligible {
		selectable[name] = true
	}
	seen := make(map[string]bool, len(eligible))
	names := make([]string, 0, len(eligible))
	preferred := make([]string, 0, len(previous))
	for _, proxy := range proxies {
		name, _ := proxy["name"].(string)
		name = strings.TrimSpace(name)
		if !selectable[name] {
			continue
		}
		fingerprint, err := proxyprobe.Fingerprint(proxy)
		if err != nil {
			return nil, 0, fmt.Errorf("fingerprint automatic pool proxy %q: %w", name, err)
		}
		if seen[fingerprint] {
			continue
		}
		seen[fingerprint] = true
		if previous[fingerprint] {
			preferred = append(preferred, name)
		} else {
			names = append(names, name)
		}
	}
	return append(preferred, names...), len(preferred), nil
}

func probeAutoBatch(ctx context.Context, proxies []map[string]any, names []string, needed int, opts Options) (autoBatchResult, error) {
	session, err := proxyprobe.Start(ctx, proxies, names, proxyprobe.Options{
		CorePath: opts.CorePath, RuntimeParent: opts.ProbeDir, RuntimePrefix: "auto-pool-probe-",
	})
	if err != nil {
		return autoBatchResult{}, err
	}
	defer session.Close()
	measured := runAutoProbes(ctx, len(names), opts.Concurrency, needed, func(ctx context.Context, index int) time.Duration {
		client, err := session.HTTPClient(index, 5*time.Second, true)
		if err != nil {
			return 0
		}
		defer client.CloseIdleConnections()
		return measureAuto(ctx, client)
	})
	if err := ctx.Err(); err != nil {
		return autoBatchResult{}, err
	}
	return measured, session.Err()
}

func runAutoProbes(ctx context.Context, count, concurrency, needed int, probe func(context.Context, int) time.Duration) autoBatchResult {
	completed := make(chan measurement)
	var measured autoBatchResult
	next, inFlight, qualified := 0, 0, 0
	dispatch := func() {
		index := next
		next++
		inFlight++
		go func() {
			completed <- measurement{index: index, worstDoH: probe(ctx, index)}
		}()
	}
	for next < min(concurrency, count) && ctx.Err() == nil {
		dispatch()
	}
	for inFlight > 0 {
		attempt := <-completed
		inFlight--
		measured.completed = append(measured.completed, attempt)
		if attempt.worstDoH > 0 {
			qualified++
			if qualified == needed {
				measured.filledAt = time.Now()
				measured.drainedCount = inFlight
			}
		}
		if qualified < needed && next < count && ctx.Err() == nil {
			dispatch()
		}
	}
	return measured
}

func measureAuto(ctx context.Context, client *http.Client) time.Duration {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, poolcontract.AutoHTTPSProbeURL, nil)
	if err != nil {
		return 0
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0
	}
	resp.Body.Close()
	if resp.StatusCode != poolcontract.AutoHTTPSProbeStatus {
		return 0
	}
	return measureDoH(ctx, client)
}

func measureDoH(ctx context.Context, client *http.Client) time.Duration {
	var worst time.Duration
	for _, endpoint := range overseasDoH {
		for _, kind := range []uint16{1, 28} {
			start := time.Now()
			if !queryDoH(ctx, client, endpoint, kind) {
				return 0
			}
			worst = max(worst, time.Since(start))
		}
	}
	return worst
}

func queryDoH(ctx context.Context, client *http.Client, endpoint string, kind uint16) bool {
	query := []byte{0, 0, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0}
	query = append(query, "\x03www\x06google\x03com\x00"...)
	query = binary.BigEndian.AppendUint16(query, kind)
	query = binary.BigEndian.AppendUint16(query, 1)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?dns="+base64.RawURLEncoding.EncodeToString(query), nil)
	if err != nil {
		return false
	}
	req.Header.Set("Accept", "application/dns-message")
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "application/dns-message" {
		return false
	}
	// The DNS wire format has a 16-bit maximum message size.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 65536))
	return err == nil && len(body) < 65536 && hasAddressAnswer(body, kind)
}

func hasAddressAnswer(data []byte, kind uint16) bool {
	if len(data) < 12 || binary.BigEndian.Uint16(data) != 0 || data[2]&0x82 != 0x80 || data[3]&15 != 0 {
		return false
	}
	at := 12
	for range binary.BigEndian.Uint16(data[4:]) {
		at = skipDNSName(data, at) + 4
		if at > len(data) {
			return false
		}
	}
	for range binary.BigEndian.Uint16(data[6:]) {
		at = skipDNSName(data, at)
		if at+10 > len(data) {
			return false
		}
		recordType := binary.BigEndian.Uint16(data[at:])
		class := binary.BigEndian.Uint16(data[at+2:])
		length := int(binary.BigEndian.Uint16(data[at+8:]))
		at += 10 + length
		if at > len(data) {
			return false
		}
		if recordType == kind && class == 1 && (kind == 1 && length == 4 || kind == 28 && length == 16) {
			return true
		}
	}
	return false
}

func skipDNSName(data []byte, at int) int {
	for at < len(data) {
		label := data[at]
		switch {
		case label == 0:
			return at + 1
		case label&0xc0 == 0xc0 && at+1 < len(data):
			return at + 2
		case label > 63:
			return len(data) + 1
		}
		at += 1 + int(label)
	}
	return len(data) + 1
}

// Qualify measures a single pool in an isolated Mihomo process.
func Qualify(ctx context.Context, pool poolcontract.Pool, input poolcontract.Input, opts Options) (capability.Result, error) {
	if opts.Concurrency < 0 {
		return capability.Result{}, fmt.Errorf("probe concurrency must be positive")
	}
	names, err := proxyprobe.SelectableNames(input.Proxies)
	if err != nil {
		return capability.Result{}, err
	}
	if opts.Concurrency == 0 {
		opts.Concurrency = DefaultConcurrency
	}
	switch pool.Capability {
	case poolcontract.AutoCapability:
		return qualifyAuto(ctx, input, names, opts)
	case poolcontract.ChatGPTCapability:
		prober, err := chatgptavailable.NewMihomoProber(chatgptavailable.MihomoOptions{
			CorePath: opts.CorePath, RuntimeParent: opts.ProbeDir,
			Definitions: input.Proxies, Concurrency: opts.Concurrency,
		})
		if err != nil {
			return capability.Result{}, err
		}
		snapshot := filepath.Join(opts.ProbeDir, pool.Provider+".json")
		return chatgptavailable.RebuildSelected(ctx, input.Proxies, names, prober, chatgptavailable.Options{SnapshotPath: snapshot})
	default:
		return capability.Result{}, fmt.Errorf("unsupported pool %q", pool.Capability)
	}
}
