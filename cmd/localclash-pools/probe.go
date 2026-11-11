package main

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"localclash/internal/capability"
	"localclash/internal/chatgptavailable"
	"localclash/internal/poolcontract"
	"localclash/internal/proxyprobe"
)

var overseasDoH = []string{"https://1.1.1.1/dns-query", "https://8.8.8.8/dns-query"}

type measurement struct {
	name  string
	worst time.Duration
}

func qualifyAuto(ctx context.Context, input poolcontract.Input, names []string, corePath, workDir string) (capability.Result, error) {
	start := time.Now()
	representatives, err := uniqueProxyNames(input.Proxies, names)
	if err != nil {
		return capability.Result{}, err
	}
	qualified := make([]measurement, 0, len(names))
	for batch := range slices.Chunk(representatives, proxyprobe.BatchSize) {
		results, err := probeAutoBatch(ctx, input.Proxies, batch, corePath, workDir)
		if err != nil {
			return capability.Result{}, err
		}
		for _, result := range results {
			if result.worst > 0 {
				qualified = append(qualified, result)
			}
		}
	}
	// Stable ties preserve subscription order.
	sort.SliceStable(qualified, func(i, j int) bool { return qualified[i].worst < qualified[j].worst })
	result := capability.Result{
		Profile: poolcontract.AutoCapability, Candidates: len(names), Probed: len(representatives),
		DeduplicatedCount:      len(names) - len(representatives),
		ObservedQualifiedCount: len(qualified), UnavailableCount: len(representatives) - len(qualified),
		DurationMS: time.Since(start).Milliseconds(),
	}
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

func probeAutoBatch(ctx context.Context, proxies []map[string]any, names []string, corePath, workDir string) ([]measurement, error) {
	session, err := proxyprobe.Start(ctx, proxies, names, proxyprobe.Options{
		CorePath: corePath, RuntimeParent: workDir, RuntimePrefix: "auto-pool-probe-",
	})
	if err != nil {
		return nil, err
	}
	defer session.Close()
	results := make([]measurement, len(names))
	jobs := make(chan int)
	var workers sync.WaitGroup
	for range min(16, len(names)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				client, err := session.HTTPClient(index, 5*time.Second, true)
				if err == nil {
					results[index] = measurement{names[index], measureAuto(ctx, client)}
					client.CloseIdleConnections()
				}
			}
		}()
	}
	for index := range names {
		select {
		case jobs <- index:
		case <-ctx.Done():
			close(jobs)
			workers.Wait()
			return nil, ctx.Err()
		}
	}
	close(jobs)
	workers.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return results, session.Err()
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

func qualify(ctx context.Context, pool poolcontract.Pool, input poolcontract.Input, corePath, workDir, stage string) (capability.Result, error) {
	names, err := proxyprobe.SelectableNames(input.Proxies)
	if err != nil {
		return capability.Result{}, err
	}
	if pool.Capability == poolcontract.AutoCapability {
		return qualifyAuto(ctx, input, names, corePath, workDir)
	}
	return chatgptavailable.RebuildSelectedCandidateWithMihomo(ctx, input.Proxies, names, corePath, workDir, stage, stage)
}
