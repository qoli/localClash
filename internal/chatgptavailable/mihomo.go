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

	"localclash/internal/proxyprobe"
)

const (
	oauthTokenURL = "https://auth.openai.com/oauth/token"
	// OAuth client IDs are public application identifiers, not OpenAI account credentials.
	oauthClientID           = "app_WXrF1LSkiTtfYqiL6XtjygvX"
	dummyRefreshToken       = "localclash-dummy-refresh-token"
	oauthRegionSupported    = "region_supported"
	oauthRegionUnsupported  = "unsupported_country_region_territory"
	oauthUnexpectedResponse = "unexpected_response"
	oauthTransportFailure   = "transport_failure"
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
	ClientID       string
}

type MihomoProber struct {
	options MihomoOptions
	probe   func(context.Context, *http.Client, string, string, time.Duration) oauthProbeResult
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
		options.Endpoint = oauthTokenURL
	}
	if strings.TrimSpace(options.ClientID) == "" {
		options.ClientID = oauthClientID
	}
	return &MihomoProber{options: options, probe: probeOAuthToken}, nil
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
					observations[index] = Observation{Fingerprint: candidates[index].Fingerprint, AdmissionStatus: oauthTransportFailure, Error: clientErr.Error()}
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
		result := p.probe(ctx, client, p.options.Endpoint, p.options.ClientID, p.options.RequestTimeout)
		observation.AdmissionStatus = result.decision
		observation.HTTPStatus = result.httpStatus
		observation.ServiceErrorCode = result.errorCode
		observation.ResponseBytes += result.responseBytes
		observation.ServiceRejected = result.explicitReject
		if result.decision == oauthRegionSupported {
			observation.Available = true
			observation.Error = ""
			break
		}
		if result.err == nil {
			observation.Error = "OAuth token probe returned an unclassified result"
		} else {
			observation.Error = result.err.Error()
		}
		if result.decision == oauthRegionUnsupported {
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

type oauthProbeResult struct {
	decision       string
	httpStatus     int
	explicitReject bool
	errorCode      string
	responseBytes  int64
	err            error
}

type oauthTokenRequest struct {
	GrantType    string `json:"grant_type"`
	ClientID     string `json:"client_id"`
	RefreshToken string `json:"refresh_token"`
}

type oauthErrorResponse struct {
	Error struct {
		Code string `json:"code"`
		Type string `json:"type"`
	} `json:"error"`
}

func probeOAuthToken(parent context.Context, client *http.Client, endpoint, clientID string, timeout time.Duration) oauthProbeResult {
	requestURL, err := url.Parse(endpoint)
	if err != nil {
		return oauthTransportError(fmt.Errorf("parse OAuth token URL: %w", err))
	}
	body, err := json.Marshal(oauthTokenRequest{
		GrantType:    "refresh_token",
		ClientID:     clientID,
		RefreshToken: dummyRefreshToken,
	})
	if err != nil {
		return oauthTransportError(fmt.Errorf("encode OAuth token probe: %w", err))
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL.String(), strings.NewReader(string(body)))
	if err != nil {
		return oauthTransportError(fmt.Errorf("create OAuth token probe: %w", err))
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "localClash-chatgpt-capability/1")
	resp, err := client.Do(req)
	if err != nil {
		return oauthTransportError(err)
	}
	defer resp.Body.Close()

	counted := &countingReader{reader: resp.Body}
	response, decodeErr := decodeOAuthError(counted)
	result := oauthProbeResult{
		httpStatus:    resp.StatusCode,
		responseBytes: counted.count,
	}
	if decodeErr != nil {
		result.decision = oauthUnexpectedResponse
		result.err = fmt.Errorf("decode OAuth token response (HTTP %d): %w", resp.StatusCode, decodeErr)
		return result
	}
	result.errorCode = strings.TrimSpace(response.Error.Code)
	switch {
	case resp.StatusCode == http.StatusUnauthorized && result.errorCode == "token_expired":
		result.decision = oauthRegionSupported
		return result
	case resp.StatusCode == http.StatusForbidden && result.errorCode == oauthRegionUnsupported:
		result.decision = oauthRegionUnsupported
		result.explicitReject = true
		result.err = errors.New("OAuth token endpoint rejected the egress region")
		return result
	default:
		result.decision = oauthUnexpectedResponse
		result.err = fmt.Errorf("OAuth token endpoint returned unexpected HTTP %d error code %q", resp.StatusCode, result.errorCode)
		return result
	}
}

func decodeOAuthError(reader io.Reader) (oauthErrorResponse, error) {
	var response oauthErrorResponse
	decoder := json.NewDecoder(reader)
	if err := decoder.Decode(&response); err != nil {
		return oauthErrorResponse{}, err
	}
	if response.Error.Code == "" {
		return oauthErrorResponse{}, errors.New("OAuth token response is missing error.code")
	}
	if token, err := decoder.Token(); err != io.EOF {
		if err != nil {
			return oauthErrorResponse{}, err
		}
		return oauthErrorResponse{}, fmt.Errorf("unexpected JSON token after OAuth token response: %v", token)
	}
	return response, nil
}

func oauthTransportError(err error) oauthProbeResult {
	return oauthProbeResult{decision: oauthTransportFailure, err: err}
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
