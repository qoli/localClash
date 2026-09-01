package chatgptavailable

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/andybalholm/brotli"
	"localclash/internal/proxyprobe"
)

const (
	statsigInitializeURL = "https://ab.chatgpt.com/v1/initialize"
	// Statsig client SDK keys are public application identifiers, not OpenAI account credentials.
	statsigClientKey            = "client-zUdXdSTygXJdzoE0sWTkP8GKTVsUMF2IRM7ShVO2JAG"
	statsigMaxCompressedBytes   = 1024 * 1024
	statsigMaxDecompressedBytes = 4 * 1024 * 1024
)

const (
	statsigReachable          = "reachable"
	statsigRejected           = "rejected"
	statsigUnexpectedResponse = "unexpected_response"
	statsigTransportFailure   = "transport_failure"
)

type MihomoOptions struct {
	CorePath       string
	RuntimeParent  string
	Definitions    []map[string]any
	Concurrency    int
	Attempts       int
	RequestTimeout time.Duration
	RetryDelay     time.Duration
	Endpoint       string
	ClientKey      string
}

type MihomoProber struct {
	options MihomoOptions
	probe   func(context.Context, *http.Client, string, string, time.Duration) statsigProbeResult
}

func RebuildWithMihomo(ctx context.Context, proxies []map[string]any, corePath, runtimeParent, snapshotPath string) (Result, error) {
	return RebuildCandidateWithMihomo(ctx, proxies, corePath, runtimeParent, snapshotPath, snapshotPath)
}

func RebuildCandidateWithMihomo(ctx context.Context, proxies []map[string]any, corePath, runtimeParent, snapshotPath, previousSnapshotPath string) (Result, error) {
	eligible, err := SelectableProxyNames(proxies)
	if err != nil {
		return Result{}, err
	}
	return RebuildSelectedCandidateWithMihomo(ctx, proxies, eligible, corePath, runtimeParent, snapshotPath, previousSnapshotPath)
}

func RebuildSelectedCandidateWithMihomo(ctx context.Context, proxies []map[string]any, eligible []string, corePath, runtimeParent, snapshotPath, previousSnapshotPath string) (Result, error) {
	prober, err := NewMihomoProber(MihomoOptions{
		CorePath:      corePath,
		RuntimeParent: runtimeParent,
		Definitions:   proxies,
	})
	if err != nil {
		return Result{}, err
	}
	return RebuildSelected(ctx, proxies, eligible, prober, Options{SnapshotPath: snapshotPath, PreviousSnapshotPath: previousSnapshotPath})
}

func NewMihomoProber(options MihomoOptions) (*MihomoProber, error) {
	options.CorePath = strings.TrimSpace(options.CorePath)
	if options.CorePath == "" {
		return nil, errors.New("ChatGPT capability probe core path is required")
	}
	info, err := os.Stat(options.CorePath)
	if err != nil {
		return nil, fmt.Errorf("inspect ChatGPT capability probe core: %w", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("ChatGPT capability probe core %q is a directory", options.CorePath)
	}
	if strings.TrimSpace(options.RuntimeParent) == "" {
		return nil, errors.New("ChatGPT capability probe runtime parent is required")
	}
	if options.Concurrency <= 0 {
		options.Concurrency = 16
	}
	if options.Attempts <= 0 {
		options.Attempts = 2
	}
	if options.RequestTimeout <= 0 {
		options.RequestTimeout = 5 * time.Second
	}
	if options.RetryDelay <= 0 {
		options.RetryDelay = 500 * time.Millisecond
	}
	if strings.TrimSpace(options.Endpoint) == "" {
		options.Endpoint = statsigInitializeURL
	}
	if strings.TrimSpace(options.ClientKey) == "" {
		options.ClientKey = statsigClientKey
	}
	return &MihomoProber{options: options, probe: probeStatsig}, nil
}

func (p *MihomoProber) Probe(ctx context.Context, candidates []Candidate) ([]Observation, error) {
	if len(candidates) == 0 {
		return nil, errors.New("ChatGPT capability probe candidates are required")
	}
	names := make([]string, len(candidates))
	definitions := p.options.Definitions
	if len(definitions) == 0 {
		definitions = make([]map[string]any, 0, len(candidates))
	}
	for index, candidate := range candidates {
		names[index] = candidate.Name
		if len(p.options.Definitions) == 0 {
			candidateDefinitions := candidate.Definitions
			if len(candidateDefinitions) == 0 {
				candidateDefinitions = []map[string]any{candidate.Proxy}
			}
			definitions = append(definitions, candidateDefinitions...)
		}
	}
	session, err := proxyprobe.Start(ctx, definitions, names, proxyprobe.Options{
		CorePath: p.options.CorePath, RuntimeParent: p.options.RuntimeParent,
		RuntimePrefix: "chatgpt-probe-", ListenerPrefix: "chatgpt-probe",
	})
	if err != nil {
		return nil, err
	}
	defer session.Close()

	observations := make([]Observation, len(candidates))
	jobs := make(chan int)
	var wg sync.WaitGroup
	workers := p.options.Concurrency
	if workers > len(candidates) {
		workers = len(candidates)
	}
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				client, clientErr := session.HTTPClient(index, p.options.RequestTimeout)
				if clientErr != nil {
					observations[index] = Observation{Fingerprint: candidates[index].Fingerprint, Error: clientErr.Error()}
					continue
				}
				observations[index] = p.probeCandidate(ctx, candidates[index], client)
			}
		}()
	}
	for index := range candidates {
		select {
		case jobs <- index:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return nil, ctx.Err()
		}
	}
	close(jobs)
	wg.Wait()
	if err := session.Err(); err != nil {
		return nil, err
	}
	return observations, nil
}

func SelectableProxyNames(proxies []map[string]any) ([]string, error) {
	byName := make(map[string]bool, len(proxies))
	referencedDialers := make(map[string]bool)
	for index, proxy := range proxies {
		name := strings.TrimSpace(stringValue(proxy["name"]))
		if name == "" {
			return nil, fmt.Errorf("ChatGPT capability candidate %d has no name", index)
		}
		if byName[name] {
			return nil, fmt.Errorf("ChatGPT capability contains duplicate proxy name %q", name)
		}
		byName[name] = true
		if dialer := strings.TrimSpace(stringValue(proxy["dialer-proxy"])); dialer != "" {
			referencedDialers[dialer] = true
		}
	}
	eligible := make([]string, 0, len(proxies)-len(referencedDialers))
	for _, proxy := range proxies {
		name := strings.TrimSpace(stringValue(proxy["name"]))
		if !referencedDialers[name] {
			eligible = append(eligible, name)
		}
	}
	return eligible, nil
}

func (p *MihomoProber) probeCandidate(ctx context.Context, candidate Candidate, client *http.Client) Observation {
	started := time.Now()
	observation := Observation{Fingerprint: candidate.Fingerprint}
	for attempt := 1; attempt <= p.options.Attempts; attempt++ {
		observation.Attempts = attempt
		result := p.probe(ctx, client, p.options.Endpoint, p.options.ClientKey, p.options.RequestTimeout)
		observation.StatsigStatus = result.decision
		observation.StatsigHTTPStatus = result.httpStatus
		observation.StatsigCountry = result.country
		observation.ContentEncoding = result.contentEncoding
		observation.CompressedBytes += result.compressedBytes
		observation.DecompressedBytes += result.decompressedBytes
		observation.ServiceRejected = result.explicitReject
		if result.err == nil && result.decision == statsigReachable {
			observation.Available = true
			observation.Error = ""
			break
		}
		observation.Error = result.err.Error()
		if attempt < p.options.Attempts {
			select {
			case <-time.After(p.options.RetryDelay):
			case <-ctx.Done():
				observation.Error = ctx.Err().Error()
				attempt = p.options.Attempts
			}
		}
	}
	observation.Duration = time.Since(started)
	return observation
}

type statsigProbeResult struct {
	decision          string
	httpStatus        int
	explicitReject    bool
	country           string
	contentEncoding   string
	compressedBytes   int64
	decompressedBytes int64
	err               error
}

func probeStatsig(parent context.Context, client *http.Client, endpoint, clientKey string, timeout time.Duration) statsigProbeResult {
	return requestStatsig(parent, client, endpoint, clientKey, timeout)
}

func requestStatsig(parent context.Context, client *http.Client, endpoint, clientKey string, timeout time.Duration) statsigProbeResult {
	requestURL, err := url.Parse(endpoint)
	if err != nil {
		return statsigTransportError(fmt.Errorf("parse Statsig initialize URL: %w", err))
	}
	query := requestURL.Query()
	query.Set("k", clientKey)
	query.Set("st", "localclash")
	query.Set("sv", "1")
	requestURL.RawQuery = query.Encode()
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL.String(), strings.NewReader("{}"))
	if err != nil {
		return statsigTransportError(err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Encoding", "br")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Statsig-Api-Key", clientKey)
	req.Header.Set("User-Agent", "localClash-chatgpt-capability/1")
	resp, err := client.Do(req)
	if err != nil {
		return statsigTransportError(err)
	}
	defer resp.Body.Close()
	result := statsigProbeResult{
		httpStatus:      resp.StatusCode,
		contentEncoding: strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding"))),
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		result.decision = statsigRejected
		result.explicitReject = true
		result.err = fmt.Errorf("Statsig initialize rejected the probe (HTTP %d)", resp.StatusCode)
		return result
	}
	if resp.StatusCode != http.StatusOK {
		result.decision = statsigUnexpectedResponse
		result.err = fmt.Errorf("Statsig initialize returned HTTP %d", resp.StatusCode)
		return result
	}
	if result.contentEncoding != "br" {
		result.decision = statsigUnexpectedResponse
		result.err = fmt.Errorf("Statsig initialize did not return required Brotli encoding (content-encoding=%q)", result.contentEncoding)
		return result
	}
	compressed := &countingReader{reader: io.LimitReader(resp.Body, statsigMaxCompressedBytes+1)}
	decompressed := &countingReader{reader: io.LimitReader(brotli.NewReader(compressed), statsigMaxDecompressedBytes+1)}
	country, decodeErr := readStatsigCountry(decompressed)
	result.compressedBytes = compressed.count
	result.decompressedBytes = decompressed.count
	if compressed.count > statsigMaxCompressedBytes {
		result.decision = statsigUnexpectedResponse
		result.err = fmt.Errorf("Statsig initialize compressed response exceeds %d bytes", statsigMaxCompressedBytes)
		return result
	}
	if decompressed.count > statsigMaxDecompressedBytes {
		result.decision = statsigUnexpectedResponse
		result.err = fmt.Errorf("Statsig initialize decompressed response exceeds %d bytes", statsigMaxDecompressedBytes)
		return result
	}
	if decodeErr != nil {
		result.decision = statsigUnexpectedResponse
		result.err = fmt.Errorf("decode Statsig initialize response: %w", decodeErr)
		return result
	}
	if country == "" {
		result.decision = statsigUnexpectedResponse
		result.err = errors.New("Statsig initialize response is missing derived_fields.country")
		return result
	}
	result.decision = statsigReachable
	result.country = country
	return result
}

func statsigTransportError(err error) statsigProbeResult {
	return statsigProbeResult{decision: statsigTransportFailure, err: err}
}

func readStatsigCountry(reader io.Reader) (string, error) {
	decoder := json.NewDecoder(reader)
	token, err := decoder.Token()
	if err != nil {
		return "", err
	}
	if token != json.Delim('{') {
		return "", errors.New("Statsig initialize response must be a JSON object")
	}
	var country string
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return "", err
		}
		key, ok := keyToken.(string)
		if !ok {
			return "", errors.New("Statsig initialize response contains a non-string object key")
		}
		if key == "derived_fields" {
			var fields struct {
				Country string `json:"country"`
			}
			if err := decoder.Decode(&fields); err != nil {
				return "", err
			}
			country = strings.ToUpper(strings.TrimSpace(fields.Country))
			continue
		}
		if err := skipJSONValue(decoder); err != nil {
			return "", err
		}
	}
	if _, err := decoder.Token(); err != nil {
		return "", err
	}
	if token, err := decoder.Token(); err != io.EOF {
		if err != nil {
			return "", err
		}
		return "", fmt.Errorf("unexpected JSON token after Statsig initialize response: %v", token)
	}
	if country == "" {
		return "", errors.New("Statsig initialize response is missing derived_fields.country")
	}
	return country, nil
}

func skipJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		for decoder.More() {
			if _, err := decoder.Token(); err != nil {
				return err
			}
			if err := skipJSONValue(decoder); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := skipJSONValue(decoder); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delim)
	}
	_, err = decoder.Token()
	return err
}

type countingReader struct {
	reader io.Reader
	count  int64
}

func (r *countingReader) Read(data []byte) (int, error) {
	n, err := r.reader.Read(data)
	r.count += int64(n)
	return n, err
}
