package chatgptavailable

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sync"
)

const dnsOverHTTPSEndpoint = "https://cloudflare-dns.com/dns-query"

// IPVerification requires positive admission through an IP target, in addition
// to the domain probes. Any explicit region rejection vetoes every positive.
type IPVerification struct {
	Available      bool                `json:"available"`
	RegionRejected bool                `json:"region_rejected"`
	Resolutions    []IPResolution      `json:"resolutions"`
	Paths          []IPPathObservation `json:"paths"`
}

type IPResolution struct {
	Endpoint  string   `json:"endpoint"`
	Host      string   `json:"host"`
	Family    string   `json:"family"`
	Addresses []string `json:"addresses"`
	Attempts  int      `json:"attempts"`
	Error     string   `json:"error,omitempty"`
}

type IPPathObservation struct {
	Endpoint          string `json:"endpoint"`
	Host              string `json:"host"`
	Family            string `json:"family"`
	Address           string `json:"address"`
	Status            string `json:"status"`
	Attempts          int    `json:"attempts"`
	HTTPStatus        int    `json:"http_status,omitempty"`
	ErrorCode         string `json:"error_code,omitempty"`
	Country           string `json:"country,omitempty"`
	ContentEncoding   string `json:"content_encoding,omitempty"`
	ResponseBytes     int64  `json:"response_bytes,omitempty"`
	CompressedBytes   int64  `json:"compressed_bytes,omitempty"`
	DecompressedBytes int64  `json:"decompressed_bytes,omitempty"`
	Error             string `json:"error,omitempty"`
}

func (p *MihomoProber) verifyIPPaths(ctx context.Context, client *http.Client) IPVerification {
	// Resolve through this candidate over HTTPS: bare-WAN UDP DNS can be poisoned,
	// and a broken candidate must not determine targets for every other candidate.
	type measurement struct {
		endpoint string
		url      string
		family   string
	}
	measurements := []measurement{
		{"oauth", p.options.OAuthEndpoint, "ipv4"},
		{"statsig", p.options.StatsigEndpoint, "ipv4"},
		{"oauth", p.options.OAuthEndpoint, "ipv6"},
		{"statsig", p.options.StatsigEndpoint, "ipv6"},
	}
	resolutions := make([]IPResolution, len(measurements))
	paths := make([][]IPPathObservation, len(measurements))
	var wg sync.WaitGroup
	for index, measurement := range measurements {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resolution := IPResolution{Endpoint: measurement.endpoint, Family: measurement.family, Addresses: []string{}}
			endpoint, err := url.Parse(measurement.url)
			if err != nil || endpoint.Hostname() == "" {
				resolution.Error = fmt.Sprintf("invalid %s endpoint %q", measurement.endpoint, measurement.url)
				resolutions[index] = resolution
				return
			}
			resolution.Host = endpoint.Hostname()
			var addresses []netip.Addr
			for attempt := 1; attempt <= p.options.Attempts; attempt++ {
				resolution.Attempts = attempt
				addresses, err = p.resolveIPs(ctx, client, resolution.Host, measurement.family)
				if err == nil || !waitToRetry(ctx, attempt, p.options.Attempts, p.options.RetryDelay) {
					break
				}
			}
			resolution.Error = errorString(err)
			for _, address := range addresses {
				resolution.Addresses = append(resolution.Addresses, address.String())
				path := IPPathObservation{Endpoint: measurement.endpoint, Host: resolution.Host, Family: measurement.family, Address: address.String()}
				ipClient, err := ipTargetClient(client, endpoint, address)
				if err != nil {
					path.Status, path.Error = oauthTransportFailure, err.Error()
				} else if measurement.endpoint == "oauth" {
					outcome := p.probeOAuthEndpointAttempts(ctx, ipClient, measurement.url)
					path.Status, path.HTTPStatus, path.ErrorCode = outcome.result.decision, outcome.result.httpStatus, outcome.result.errorCode
					path.Attempts, path.ResponseBytes, path.Error = outcome.attempts, outcome.responseBytes, errorString(outcome.result.err)
				} else {
					outcome := p.probeStatsigEndpointAttempts(ctx, ipClient, measurement.url)
					path.Status, path.HTTPStatus, path.ErrorCode = outcome.result.decision, outcome.result.httpStatus, outcome.result.errorCode
					path.Attempts, path.Country, path.ContentEncoding = outcome.attempts, outcome.result.country, outcome.result.contentEncoding
					path.CompressedBytes, path.DecompressedBytes, path.Error = outcome.compressedBytes, outcome.decompressedBytes, errorString(outcome.result.err)
				}
				if ipClient != nil {
					ipClient.CloseIdleConnections()
				}
				paths[index] = append(paths[index], path)
			}
			resolutions[index] = resolution
		}()
	}
	wg.Wait()
	verification := IPVerification{Resolutions: resolutions, Paths: []IPPathObservation{}}
	for _, observations := range paths {
		verification.Paths = append(verification.Paths, observations...)
	}
	verification.Available, verification.RegionRejected = admitIPPaths(verification.Paths)
	return verification
}

func admitIPPaths(paths []IPPathObservation) (bool, bool) {
	oauth, statsig := map[string]bool{}, map[string]bool{}
	for _, path := range paths {
		if path.HTTPStatus == http.StatusForbidden && path.ErrorCode == oauthRegionUnsupported {
			return false, true
		}
		if path.Endpoint == "oauth" && path.Status == oauthRegionSupported {
			oauth[path.Family] = true
		}
		if path.Endpoint == "statsig" && path.Status == statsigReachable {
			statsig[path.Family] = true
		}
	}
	return (oauth["ipv4"] && statsig["ipv4"]) || (oauth["ipv6"] && statsig["ipv6"]), false
}

func (p *MihomoProber) resolveIPs(parent context.Context, client *http.Client, host, family string) ([]netip.Addr, error) {
	endpoint, err := url.Parse(p.options.DNSOverHTTPSEndpoint)
	if err != nil {
		return nil, err
	}
	recordType, recordNumber := "A", 1
	if family == "ipv6" {
		recordType, recordNumber = "AAAA", 28
	}
	query := endpoint.Query()
	query.Set("name", host)
	query.Set("type", recordType)
	endpoint.RawQuery = query.Encode()
	ctx, cancel := context.WithTimeout(parent, p.options.RequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/dns-json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("resolve %s %s over HTTPS: HTTP %d", host, recordType, resp.StatusCode)
	}
	var response struct {
		Status *int
		Answer []struct {
			Type int
			Data string
		}
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("decode %s %s DNS response: %w", host, recordType, err)
	}
	if response.Status == nil || *response.Status != 0 {
		return nil, fmt.Errorf("resolve %s %s: unsuccessful DNS response", host, recordType)
	}
	addresses := []netip.Addr{}
	seen := map[netip.Addr]bool{}
	for _, answer := range response.Answer {
		if answer.Type != recordNumber {
			continue
		}
		address, err := netip.ParseAddr(answer.Data)
		if err != nil || (family == "ipv6" && !address.Is6()) || (family == "ipv4" && !address.Is4()) {
			return nil, fmt.Errorf("invalid %s address in DNS response: %q", recordType, answer.Data)
		}
		if !seen[address] {
			seen[address] = true
			addresses = append(addresses, address)
		}
	}
	return addresses, nil
}

// Rewrite only the CONNECT destination. The application URL, HTTP Host, TLS
// server name and normal certificate validation retain the original hostname.
func ipTargetClient(base *http.Client, endpoint *url.URL, address netip.Addr) (*http.Client, error) {
	transport, ok := base.Transport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("IP target probe requires an HTTP transport")
	}
	transport = transport.Clone()
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{}
	}
	transport.TLSClientConfig.ServerName = endpoint.Hostname()
	// Clone can inherit h2 ALPN from the already-used domain transport. A custom
	// TLS config otherwise disables automatic HTTP/2 setup, leaving HTTP/1 code
	// to read an HTTP/2 connection. Keep protocol handling aligned with ALPN.
	transport.ForceAttemptHTTP2 = true
	port := endpoint.Port()
	if port == "" {
		port = "443"
		if endpoint.Scheme == "http" {
			port = "80"
		}
	}
	client := *base
	client.Transport = &ipTargetTransport{Transport: transport, host: endpoint.Host, target: net.JoinHostPort(address.String(), port)}
	return &client, nil
}

type ipTargetTransport struct {
	*http.Transport
	host   string
	target string
}

func (t *ipTargetTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host != t.host {
		return nil, fmt.Errorf("IP probe for %s cannot request %s", t.host, req.URL.Host)
	}
	request := req.Clone(req.Context())
	requestURL := *req.URL
	requestURL.Host = t.target
	request.URL = &requestURL
	if request.Host == "" {
		request.Host = req.URL.Host
	}
	return t.Transport.RoundTrip(request)
}
