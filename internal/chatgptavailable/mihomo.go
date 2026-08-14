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

	"gopkg.in/yaml.v3"
)

const (
	iosEligibilityURL     = "https://ios.chat.openai.com/"
	androidEligibilityURL = "https://android.chat.openai.com/"
)

const (
	eligibilityEligible           = "eligible"
	eligibilityDisallowedISP      = "disallowed_isp"
	eligibilityUnsupportedRegion  = "unsupported_region"
	eligibilityUnexpectedResponse = "unexpected_response"
	eligibilityTransportFailure   = "transport_failure"
)

type MihomoOptions struct {
	CorePath       string
	RuntimeParent  string
	Concurrency    int
	Attempts       int
	RequestTimeout time.Duration
	RetryDelay     time.Duration
}

type MihomoProber struct {
	options MihomoOptions
}

func RebuildWithMihomo(ctx context.Context, proxies []map[string]any, corePath, runtimeParent, snapshotPath string) (Result, error) {
	prober, err := NewMihomoProber(MihomoOptions{
		CorePath:      corePath,
		RuntimeParent: runtimeParent,
	})
	if err != nil {
		return Result{}, err
	}
	return Rebuild(ctx, proxies, prober, Options{SnapshotPath: snapshotPath})
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
		options.Concurrency = 40
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
	return &MihomoProber{options: options}, nil
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
		iosResult := make(chan eligibilityProbeResult, 1)
		androidResult := make(chan eligibilityProbeResult, 1)
		go func() {
			iosResult <- probeEligibility(ctx, port, iosEligibilityURL, p.options.RequestTimeout)
		}()
		go func() {
			androidResult <- probeEligibility(ctx, port, androidEligibilityURL, p.options.RequestTimeout)
		}()
		ios := <-iosResult
		android := <-androidResult
		observation.IOSEligibility = ios.decision
		observation.IOSEligibilityStatus = ios.httpStatus
		observation.AndroidEligibility = android.decision
		observation.AndroidEligibilityStatus = android.httpStatus
		available, rejected, probeError := evaluateProbeAttempt(ios, android)
		observation.EligibilityRejected = rejected
		if available {
			observation.Available = true
			observation.Error = ""
			break
		}
		observation.Error = probeError
		if observation.EligibilityRejected {
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

func evaluateProbeAttempt(ios, android eligibilityProbeResult) (bool, bool, string) {
	rejected := ios.explicitReject || android.explicitReject
	available := ios.err == nil && android.err == nil &&
		ios.decision == eligibilityEligible && android.decision == eligibilityEligible
	if available {
		return true, false, ""
	}
	return false, rejected, joinProbeErrors(ios, android)
}

type eligibilityProbeResult struct {
	decision       string
	httpStatus     int
	explicitReject bool
	err            error
}

func probeEligibility(parent context.Context, port int, endpoint string, timeout time.Duration) eligibilityProbeResult {
	proxyURL, err := url.Parse("http://127.0.0.1:" + strconv.Itoa(port))
	if err != nil {
		return transportFailure(err)
	}
	transport := &http.Transport{
		Proxy:               http.ProxyURL(proxyURL),
		DisableKeepAlives:   true,
		TLSHandshakeTimeout: timeout,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return transportFailure(err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "localClash-chatgpt-capability/1")
	resp, err := client.Do(req)
	if err != nil {
		return transportFailure(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4097))
	if err != nil {
		return eligibilityProbeResult{decision: eligibilityTransportFailure, httpStatus: resp.StatusCode, err: err}
	}
	if len(body) > 4096 {
		return eligibilityProbeResult{decision: eligibilityUnexpectedResponse, httpStatus: resp.StatusCode, err: errors.New("response body exceeds 4096 bytes")}
	}
	return classifyEligibility(resp.StatusCode, body)
}

func transportFailure(err error) eligibilityProbeResult {
	return eligibilityProbeResult{decision: eligibilityTransportFailure, err: err}
}

func classifyEligibility(httpStatus int, body []byte) eligibilityProbeResult {
	lowerBody := strings.ToLower(string(body))
	if strings.Contains(lowerBody, "unsupported_country_region_territory") ||
		strings.Contains(lowerBody, "unsupported country") {
		return eligibilityProbeResult{
			decision: eligibilityUnsupportedRegion, httpStatus: httpStatus, explicitReject: true,
			err: errors.New("OpenAI rejected the exit region"),
		}
	}
	if strings.Contains(lowerBody, "disallowed isp") || strings.Contains(lowerBody, "vpn_detected") {
		return eligibilityProbeResult{
			decision: eligibilityDisallowedISP, httpStatus: httpStatus, explicitReject: true,
			err: errors.New("OpenAI rejected the exit ISP"),
		}
	}
	var payload struct {
		CFDetails string `json:"cf_details"`
		Type      string `json:"type"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return eligibilityProbeResult{
			decision:   eligibilityUnexpectedResponse,
			httpStatus: httpStatus,
			err:        fmt.Errorf("response does not match expected OpenAI mobile IP eligibility JSON: %w", err),
		}
	}
	if httpStatus == http.StatusForbidden && strings.EqualFold(strings.TrimSpace(payload.Type), "dc") &&
		strings.Contains(strings.ToLower(payload.CFDetails), "request is not allowed") {
		return eligibilityProbeResult{decision: eligibilityEligible, httpStatus: httpStatus}
	}
	return eligibilityProbeResult{
		decision:   eligibilityUnexpectedResponse,
		httpStatus: httpStatus,
		err:        fmt.Errorf("response does not match expected OpenAI mobile IP eligibility fingerprint (HTTP %d, type=%q)", httpStatus, strings.TrimSpace(payload.Type)),
	}
}

func joinProbeErrors(ios, android eligibilityProbeResult) string {
	parts := make([]string, 0, 2)
	if ios.err != nil {
		parts = append(parts, fmt.Sprintf("ios(decision=%q,http_status=%d): %v", ios.decision, ios.httpStatus, ios.err))
	}
	if android.err != nil {
		parts = append(parts, fmt.Sprintf("android(decision=%q,http_status=%d): %v", android.decision, android.httpStatus, android.err))
	}
	return strings.Join(parts, "; ")
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
