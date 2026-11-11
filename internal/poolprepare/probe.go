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
	"sync"
	"sync/atomic"
	"time"

	"localclash/internal/capability"
	"localclash/internal/chatgptavailable"
	"localclash/internal/poolcontract"
	"localclash/internal/proxyprobe"
)

var overseasDoH = []string{"https://1.1.1.1/dns-query", "https://8.8.8.8/dns-query"}

type measurement struct {
	name     string
	worst    time.Duration
	canceled bool
}

func qualifyAuto(ctx context.Context, input poolcontract.Input, names []string, opts Options) (capability.Result, error) {
	start := time.Now()
	representatives, err := uniqueProxyNames(input.Proxies, names)
	if err != nil {
		return capability.Result{}, err
	}
	result := capability.Result{
		Profile: poolcontract.AutoCapability, Candidates: len(names),
		DeduplicatedCount: len(names) - len(representatives),
	}
	var qualified []measurement
	for batch := range slices.Chunk(representatives, proxyprobe.BatchSize) {
		remaining := poolcontract.AutoLimit - len(qualified)
		if remaining <= 0 {
			break
		}
		results, err := probeAutoBatch(ctx, input.Proxies, batch, remaining, opts)
		if err != nil {
			return capability.Result{}, err
		}
		for _, attempt := range results {
			if attempt.name == "" {
				continue
			}
			result.Probed++
			switch {
			case attempt.worst > 0:
				qualified = append(qualified, attempt)
			case attempt.canceled:
				result.CanceledCount++
			default:
				result.UnavailableCount++
			}
		}
	}
	// Stable ties preserve subscription order.
	sort.SliceStable(qualified, func(i, j int) bool { return qualified[i].worst < qualified[j].worst })
	result.UnprobedCount = len(representatives) - result.Probed
	result.ObservedQualifiedCount = len(qualified)
	result.DurationMS = time.Since(start).Milliseconds()
	for _, entry := range qualified[:min(len(qualified), poolcontract.AutoLimit)] {
		result.Qualified = append(result.Qualified, entry.name)
	}
	result.QualifiedCount = len(result.Qualified)
	return result, nil
}

func uniqueProxyNames(proxies []map[string]any, eligible []string) ([]string, error) {
	selectable := make(map[string]bool, len(eligible))
	for _, name := range eligible {
		selectable[name] = true
	}
	seen := make(map[string]bool, len(eligible))
	names := make([]string, 0, len(eligible))
	for _, proxy := range proxies {
		name, _ := proxy["name"].(string)
		name = strings.TrimSpace(name)
		if !selectable[name] {
			continue
		}
		fingerprint, err := proxyprobe.Fingerprint(proxy)
		if err != nil {
			return nil, fmt.Errorf("fingerprint automatic pool proxy %q: %w", name, err)
		}
		if !seen[fingerprint] {
			names = append(names, name)
			seen[fingerprint] = true
		}
	}
	return names, nil
}

func probeAutoBatch(ctx context.Context, proxies []map[string]any, names []string, needed int, opts Options) ([]measurement, error) {
	session, err := proxyprobe.Start(ctx, proxies, names, proxyprobe.Options{
		CorePath: opts.CorePath, RuntimeParent: opts.RuntimeDir, RuntimePrefix: "auto-pool-probe-",
	})
	if err != nil {
		return nil, err
	}
	defer session.Close()
	results := runAutoProbes(ctx, names, opts.Concurrency, needed, func(ctx context.Context, index int) time.Duration {
		client, err := session.HTTPClient(index, 5*time.Second, true)
		if err != nil {
			return 0
		}
		defer client.CloseIdleConnections()
		return measureAuto(ctx, client)
	})
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return results, session.Err()
}

func runAutoProbes(ctx context.Context, names []string, concurrency, needed int, probe func(context.Context, int) time.Duration) []measurement {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make([]measurement, len(names))
	jobs := make(chan int)
	var workers sync.WaitGroup
	var qualified atomic.Int32
	for range min(concurrency, len(names)) {
		workers.Go(func() {
			for index := range jobs {
				if ctx.Err() != nil {
					return
				}
				worst := probe(ctx, index)
				results[index] = measurement{name: names[index], worst: worst, canceled: worst == 0 && ctx.Err() != nil}
				if worst > 0 && qualified.Add(1) == int32(needed) {
					cancel()
				}
			}
		})
	}
dispatch:
	for index := range names {
		select {
		case jobs <- index:
		case <-ctx.Done():
			break dispatch
		}
	}
	close(jobs)
	workers.Wait()
	return results
}

func measureAuto(ctx context.Context, client *http.Client) time.Duration {
	start := time.Now()
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
	httpsLatency := time.Since(start)
	dohLatency := measureDoH(ctx, client)
	if dohLatency == 0 {
		return 0
	}
	return max(httpsLatency, dohLatency)
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
			CorePath: opts.CorePath, RuntimeParent: opts.RuntimeDir,
			Definitions: input.Proxies, Concurrency: opts.Concurrency,
		})
		if err != nil {
			return capability.Result{}, err
		}
		snapshot := filepath.Join(opts.RuntimeDir, pool.Provider+".json")
		return chatgptavailable.RebuildSelected(ctx, input.Proxies, names, prober, chatgptavailable.Options{SnapshotPath: snapshot})
	default:
		return capability.Result{}, fmt.Errorf("unsupported pool %q", pool.Capability)
	}
}
