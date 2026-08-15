package chatgptavailable

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/andybalholm/brotli"
	"gopkg.in/yaml.v3"
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
	Concurrency    int
	Attempts       int
	RequestTimeout time.Duration
	RetryDelay     time.Duration
	Endpoint       string
	ClientKey      string
}

type MihomoProber struct {
	options MihomoOptions
	probe   func(context.Context, int, string, string, time.Duration) statsigProbeResult
}

func RebuildWithMihomo(ctx context.Context, proxies []map[string]any, corePath, runtimeParent, snapshotPath string) (Result, error) {
	return RebuildCandidateWithMihomo(ctx, proxies, corePath, runtimeParent, snapshotPath, snapshotPath)
}

func RebuildCandidateWithMihomo(ctx context.Context, proxies []map[string]any, corePath, runtimeParent, snapshotPath, previousSnapshotPath string) (Result, error) {
	prober, err := NewMihomoProber(MihomoOptions{
		CorePath:      corePath,
		RuntimeParent: runtimeParent,
	})
	if err != nil {
		return Result{}, err
	}
	return Rebuild(ctx, proxies, prober, Options{SnapshotPath: snapshotPath, PreviousSnapshotPath: previousSnapshotPath})
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
		options.Attempts = 3
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
	if err := os.MkdirAll(p.options.RuntimeParent, 0o755); err != nil {
		return nil, fmt.Errorf("create ChatGPT capability probe runtime parent: %w", err)
	}
	runtimeDir, err := os.MkdirTemp(p.options.RuntimeParent, "chatgpt-probe-")
	if err != nil {
		return nil, fmt.Errorf("create ChatGPT capability probe runtime: %w", err)
	}
	defer os.RemoveAll(runtimeDir)

	ports, err := reservePorts(len(candidates))
	if err != nil {
		return nil, err
	}
	configPath := filepath.Join(runtimeDir, "config.yaml")
	if err := writeProbeConfig(configPath, candidates, ports); err != nil {
		return nil, err
	}

	process, err := startProbeCore(ctx, p.options.CorePath, runtimeDir, configPath, ports[0])
	if err != nil {
		return nil, err
	}
	defer process.stop()

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
				observations[index] = p.probeCandidate(ctx, candidates[index], ports[index])
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
	return observations, nil
}

func (p *MihomoProber) probeCandidate(ctx context.Context, candidate Candidate, port int) Observation {
	started := time.Now()
	observation := Observation{Fingerprint: candidate.Fingerprint}
	for attempt := 1; attempt <= p.options.Attempts; attempt++ {
		observation.Attempts = attempt
		result := p.probe(ctx, port, p.options.Endpoint, p.options.ClientKey, p.options.RequestTimeout)
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
		if observation.ServiceRejected {
			break
		}
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

func probeStatsig(parent context.Context, port int, endpoint, clientKey string, timeout time.Duration) statsigProbeResult {
	proxyURL, err := url.Parse("http://127.0.0.1:" + strconv.Itoa(port))
	if err != nil {
		return statsigTransportError(err)
	}
	transport := &http.Transport{
		Proxy:               http.ProxyURL(proxyURL),
		DisableKeepAlives:   true,
		TLSHandshakeTimeout: timeout,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
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

func reservePorts(count int) ([]int, error) {
	listeners := make([]net.Listener, 0, count)
	defer func() {
		for _, listener := range listeners {
			_ = listener.Close()
		}
	}()
	ports := make([]int, 0, count)
	for range count {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, fmt.Errorf("reserve ChatGPT capability probe port: %w", err)
		}
		listeners = append(listeners, listener)
		ports = append(ports, listener.Addr().(*net.TCPAddr).Port)
	}
	return ports, nil
}

func writeProbeConfig(path string, candidates []Candidate, ports []int) error {
	proxies := make([]any, 0, len(candidates))
	listeners := make([]any, 0, len(candidates))
	for index, candidate := range candidates {
		definitions := candidate.Definitions
		if len(definitions) == 0 {
			definitions = []map[string]any{candidate.Proxy}
		}
		for _, definition := range definitions {
			proxies = append(proxies, definition)
		}
		listeners = append(listeners, map[string]any{
			"name":   fmt.Sprintf("chatgpt-probe-%d", index),
			"type":   "mixed",
			"listen": "127.0.0.1",
			"port":   ports[index],
			"proxy":  candidate.Name,
		})
	}
	config := map[string]any{
		"allow-lan":    false,
		"bind-address": "127.0.0.1",
		"ipv6":         true,
		"log-level":    "warning",
		"mode":         "rule",
		"proxies":      proxies,
		"listeners":    listeners,
		"rules":        []string{"MATCH,DIRECT"},
	}
	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("encode ChatGPT capability probe config: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write ChatGPT capability probe config: %w", err)
	}
	return nil
}

type probeProcess struct {
	cmd    *exec.Cmd
	waitCh chan error
	output *limitedBuffer
	once   sync.Once
}

func startProbeCore(ctx context.Context, corePath, runtimeDir, configPath string, readinessPort int) (*probeProcess, error) {
	processCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(processCtx, corePath, "-d", runtimeDir, "-f", configPath)
	output := &limitedBuffer{limit: 8192}
	cmd.Stdout = output
	cmd.Stderr = output
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start isolated Mihomo for ChatGPT capability probe: %w", err)
	}
	process := &probeProcess{cmd: cmd, waitCh: make(chan error, 1), output: output}
	go func() {
		process.waitCh <- cmd.Wait()
		cancel()
	}()

	deadline := time.NewTimer(12 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	address := "127.0.0.1:" + strconv.Itoa(readinessPort)
	for {
		connection, err := net.DialTimeout("tcp", address, 200*time.Millisecond)
		if err == nil {
			_ = connection.Close()
			return process, nil
		}
		select {
		case waitErr := <-process.waitCh:
			return nil, fmt.Errorf("isolated Mihomo exited before ChatGPT capability probe was ready: %v: %s", waitErr, output.String())
		case <-ticker.C:
		case <-deadline.C:
			process.stop()
			return nil, fmt.Errorf("isolated Mihomo did not become ready for ChatGPT capability probe: %s", output.String())
		case <-ctx.Done():
			process.stop()
			return nil, ctx.Err()
		}
	}
}

func (p *probeProcess) stop() {
	p.once.Do(func() {
		if p.cmd.Process == nil {
			return
		}
		_ = p.cmd.Process.Signal(os.Interrupt)
		select {
		case <-p.waitCh:
		case <-time.After(2 * time.Second):
			_ = p.cmd.Process.Kill()
			<-p.waitCh
		}
	})
}

type limitedBuffer struct {
	mu    sync.Mutex
	limit int
	data  []byte
}

func (b *limitedBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	original := len(data)
	remaining := b.limit - len(b.data)
	if remaining > 0 {
		if len(data) > remaining {
			data = data[:remaining]
		}
		b.data = append(b.data, data...)
	}
	return original, nil
}

func (b *limitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(string(bytes.Clone(b.data)))
}
