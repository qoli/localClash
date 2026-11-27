package chatgptavailable

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/andybalholm/brotli"
)

func TestProbeOAuthTokenAcceptsTokenExpiredAsRegionSupported(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Accept") != "application/json" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected OAuth request: method=%s accept=%q content-type=%q", r.Method, r.Header.Get("Accept"), r.Header.Get("Content-Type"))
		}
		var body oauthTokenRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode OAuth request body: %v", err)
			return
		}
		if body.GrantType != "refresh_token" || body.ClientID != "test-client" || body.RefreshToken != dummyRefreshToken {
			t.Errorf("OAuth request body = %+v", body)
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Could not validate your token.","type":"invalid_request_error","code":"token_expired"}}`))
	}))
	defer server.Close()

	result := probeOAuthToken(context.Background(), server.Client(), server.URL, "test-client", time.Second)
	if result.err != nil || result.decision != oauthRegionSupported || result.httpStatus != http.StatusUnauthorized || result.errorCode != "token_expired" || result.responseBytes == 0 {
		t.Fatalf("result = %+v, want conclusive supported-region observation", result)
	}
}

func TestProbeOAuthTokenRejectsUnsupportedRegion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported","type":"request_forbidden"}}`))
	}))
	defer server.Close()

	result := probeOAuthToken(context.Background(), server.Client(), server.URL, "test-client", time.Second)
	if result.decision != oauthRegionUnsupported || !result.explicitReject || result.httpStatus != http.StatusForbidden || result.errorCode != oauthRegionUnsupported || result.err == nil {
		t.Fatalf("result = %+v, want conclusive unsupported-region rejection", result)
	}
}

func TestProbeOAuthTokenDoesNotAdmitOtherOAuthErrors(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{name: "other-unauthorized", status: http.StatusUnauthorized, body: `{"error":{"code":"invalid_client","type":"invalid_request_error"}}`},
		{name: "other-forbidden", status: http.StatusForbidden, body: `{"error":{"code":"policy_denied","type":"request_forbidden"}}`},
		{name: "missing-code", status: http.StatusUnauthorized, body: `{"error":{"type":"invalid_request_error"}}`},
		{name: "malformed", status: http.StatusUnauthorized, body: `{`},
		{name: "trailing-data", status: http.StatusUnauthorized, body: `{"error":{"code":"token_expired"}} true`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			result := probeOAuthToken(context.Background(), server.Client(), server.URL, "test-client", time.Second)
			if result.decision != oauthUnexpectedResponse || result.explicitReject || result.err == nil {
				t.Fatalf("result = %+v, want explicit unexpected-response failure", result)
			}
		})
	}
}

func TestRequestStatsigRequiresBrotliAndReadsCountry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Accept-Encoding") != "br" || r.Header.Get("Statsig-Api-Key") != "test-key" || r.URL.Query().Get("k") != "test-key" {
			t.Errorf("unexpected Statsig request: method=%s encoding=%q key=%q query=%q", r.Method, r.Header.Get("Accept-Encoding"), r.Header.Get("Statsig-Api-Key"), r.URL.RawQuery)
		}
		w.Header().Set("Content-Encoding", "br")
		writer := brotli.NewWriter(w)
		_, _ = writer.Write([]byte(`{"feature_gates":{"large":{"nested":[1,2,3]}},"derived_fields":{"country":"hk"},"sdk_flags":{}}`))
		_ = writer.Close()
	}))
	defer server.Close()

	result := requestStatsig(context.Background(), server.Client(), server.URL, "test-key", time.Second)
	if result.err != nil || result.decision != statsigReachable || result.country != "HK" || result.contentEncoding != "br" {
		t.Fatalf("result = %+v, want reachable HK Brotli response", result)
	}
	if result.compressedBytes <= 0 || result.decompressedBytes <= result.compressedBytes {
		t.Fatalf("byte accounting = compressed %d decompressed %d", result.compressedBytes, result.decompressedBytes)
	}
}

func TestRequestStatsigRejectsUncompressedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"derived_fields":{"country":"US"}}`))
	}))
	defer server.Close()

	result := requestStatsig(context.Background(), server.Client(), server.URL, "test-key", time.Second)
	if result.err == nil || result.decision != statsigUnexpectedResponse || !strings.Contains(result.err.Error(), "Brotli") {
		t.Fatalf("result = %+v, want explicit uncompressed-response rejection", result)
	}
}

func TestRequestStatsigAllowsObservedResponseWithTwoMiBHeadroom(t *testing.T) {
	// The response captured on 2026-11-19 had these complete stream sizes.
	const observedCompressed = 383152
	const observedDecoded = 4198653
	const headroom = 2 * 1024 * 1024
	if statsigMaxCompressedBytes < observedCompressed+headroom {
		t.Fatal("compressed limit leaves less than 2 MiB of growth room")
	}
	for _, tt := range []struct {
		name  string
		bytes int
		want  string
	}{
		{"observed", observedDecoded, statsigReachable},
		{"two-MiB-growth", observedDecoded + headroom, statsigReachable},
		{"decoded-limit", statsigMaxDecompressedBytes + 1, statsigReadLimitExceeded},
	} {
		t.Run(tt.name, func(t *testing.T) {
			prefix := `{"layer_configs":{"padding":"`
			suffix := `"},"derived_fields":{"country":"TW"}}`
			body := []byte(prefix + strings.Repeat("x", tt.bytes-len(prefix)-len(suffix)) + suffix)
			result := requestStatsigTestBody(t, body)
			if result.decision != tt.want {
				t.Fatalf("result = %+v, want %s", result, tt.want)
			}
			if tt.want == statsigReachable {
				if result.err != nil || result.country != "TW" || result.decompressedBytes != int64(tt.bytes) {
					t.Fatalf("valid response lost its country: %+v", result)
				}
			} else if result.country != "" || result.decompressedBytes != statsigMaxDecompressedBytes+1 ||
				result.err == nil || !strings.Contains(result.err.Error(), "decompressed response exceeds") {
				t.Fatalf("local read limit was not reported accurately: %+v", result)
			}
		})
	}
}

func TestRequestStatsigReportsCompressedReadLimit(t *testing.T) {
	// Incompressible JSON keeps the compressed boundary independent of the
	// decoded boundary instead of exercising only a highly compressible fixture.
	payload := make([]byte, statsigMaxCompressedBytes+1024*1024/2)
	_, _ = rand.New(rand.NewSource(1)).Read(payload)
	body := []byte(`{"layer_configs":{"padding":"` + base64.RawStdEncoding.EncodeToString(payload) + `"},"derived_fields":{"country":"US"}}`)
	result := requestStatsigTestBody(t, body)
	if result.decision != statsigReadLimitExceeded || result.country != "" ||
		result.compressedBytes != statsigMaxCompressedBytes+1 || result.decompressedBytes > statsigMaxDecompressedBytes ||
		result.err == nil || !strings.Contains(result.err.Error(), "compressed response exceeds") {
		t.Fatalf("compressed read limit = %+v", result)
	}
}

func requestStatsigTestBody(t *testing.T, body []byte) statsigProbeResult {
	t.Helper()
	var compressed bytes.Buffer
	writer := brotli.NewWriterOptions(&compressed, brotli.WriterOptions{Quality: 0})
	if _, err := writer.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Encoding", "br")
		_, _ = w.Write(compressed.Bytes())
	}))
	defer server.Close()
	return requestStatsig(t.Context(), server.Client(), server.URL, "test-key", 5*time.Second)
}

func TestReadStatsigCountryRejectsMissingMalformedOrTrailingData(t *testing.T) {
	for _, input := range []string{
		`{"feature_gates":{}}`,
		`{"derived_fields":{"country":"US"},"feature_gates":[{"value":}]}`,
		`{"derived_fields":{"country":"US"}} true`,
	} {
		if country, err := readStatsigCountry(bytes.NewBufferString(input)); err == nil || country != "" {
			t.Fatalf("input %q produced country=%q err=%v, want explicit failure", input, country, err)
		}
	}
}

func TestNewMihomoProberDefaultsIncludeOAuthAndStatsig(t *testing.T) {
	corePath := filepath.Join(t.TempDir(), "mihomo")
	if err := os.WriteFile(corePath, []byte("test"), 0o700); err != nil {
		t.Fatal(err)
	}
	prober, err := NewMihomoProber(MihomoOptions{CorePath: corePath, RuntimeParent: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	options := prober.options
	if options.Concurrency != 16 || options.RequestTimeout != 5*time.Second || options.Attempts != 2 || options.OAuthEndpoint != oauthTokenURL || options.OAuthClientID != oauthClientID || options.StatsigEndpoint != statsigInitializeURL || options.StatsigClientKey != statsigClientKey || options.DNSOverHTTPSEndpoint != dnsOverHTTPSEndpoint || prober.probeIPPaths == nil {
		t.Fatalf("probe defaults = %+v, want OAuth and Statsig defaults", options)
	}
	if prober.probeStreams != nil {
		t.Fatal("basic qualification enabled the streaming probes")
	}
	options.EnhancedFilter = true
	prober, err = NewMihomoProber(options)
	if err != nil || prober.probeStreams == nil {
		t.Fatalf("enhanced qualification has no streaming probes: %v", err)
	}
}

func TestBasicProbeUsesStatsigOnlyForCountryFlag(t *testing.T) {
	tests := []struct {
		name    string
		statsig statsigProbeResult
		country string
	}{
		{name: "both-pass", statsig: statsigProbeResult{decision: statsigReachable, httpStatus: 200, country: "US", contentEncoding: "br"}, country: "US"},
		{name: "statsig-timeout", statsig: statsigProbeResult{decision: statsigTransportFailure, err: errors.New("statsig timeout")}},
		{name: "statsig-local-limit", statsig: statsigProbeResult{decision: statsigReadLimitExceeded, err: errors.New("local response limit")}},
		{name: "statsig-malformed-with-partial-country", statsig: statsigProbeResult{decision: statsigUnexpectedResponse, country: "US", err: errors.New("unexpected EOF")}},
		{name: "statsig-service-rejection", statsig: statsigProbeResult{decision: statsigRejected, httpStatus: 403, explicitReject: true, errorCode: oauthRegionUnsupported, err: errors.New("Statsig rejected")}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			oauthCalls := 0
			statsigCalls := 0
			prober := &MihomoProber{
				probeIPPaths: func(context.Context, *http.Client) IPVerification { return IPVerification{Available: true} },
				options:      MihomoOptions{Attempts: 1, RetryDelay: time.Nanosecond, RequestTimeout: time.Second},
				probeOAuth: func(context.Context, *http.Client, string, string, time.Duration) oauthProbeResult {
					oauthCalls++
					return oauthProbeResult{decision: oauthRegionSupported}
				},
				probeStatsig: func(context.Context, *http.Client, string, string, time.Duration) statsigProbeResult {
					statsigCalls++
					return test.statsig
				},
			}
			observation := prober.probeCandidate(context.Background(), Candidate{Fingerprint: "node"}, &http.Client{})
			if !observation.Available || observation.ServiceRejected ||
				observation.StatsigCountry != test.country ||
				observation.StatsigStatus != test.statsig.decision || observation.StatsigError != errorString(test.statsig.err) ||
				observation.IPVerification == nil || oauthCalls != 1 || statsigCalls != 1 {
				t.Fatalf("observation = %+v OAuth calls=%d Statsig calls=%d", observation, oauthCalls, statsigCalls)
			}
		})
	}
}

func TestEnhancedCountryFilterRequiresMeasuredSupportedCountry(t *testing.T) {
	for _, tc := range []struct {
		name    string
		statsig statsigProbeResult
		want    bool
	}{
		{"HK", statsigProbeResult{decision: statsigReachable, country: "HK"}, false},
		{"MO", statsigProbeResult{decision: statsigReachable, country: "MO"}, false},
		{"CN", statsigProbeResult{decision: statsigReachable, country: "CN"}, false},
		{"unknown", statsigProbeResult{decision: statsigReachable, country: "ZZ"}, false},
		{"missing", statsigProbeResult{decision: statsigUnexpectedResponse}, false},
		{"partial", statsigProbeResult{decision: statsigUnexpectedResponse, country: "US"}, false},
		{"timeout", statsigProbeResult{decision: statsigTransportFailure}, false},
		{"TW", statsigProbeResult{decision: statsigReachable, country: "TW"}, true},
		{"JP", statsigProbeResult{decision: statsigReachable, country: "JP"}, true},
		{"US", statsigProbeResult{decision: statsigReachable, country: "US"}, true},
		{"supported territory", statsigProbeResult{decision: statsigReachable, country: "AX"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, enhanced := range []bool{false, true} {
				prober := &MihomoProber{
					options: MihomoOptions{Attempts: 1, EnhancedFilter: enhanced},
					probeOAuth: func(context.Context, *http.Client, string, string, time.Duration) oauthProbeResult {
						return oauthProbeResult{decision: oauthRegionSupported}
					},
					probeStatsig: func(context.Context, *http.Client, string, string, time.Duration) statsigProbeResult {
						return tc.statsig
					},
					probeIPPaths: func(context.Context, *http.Client) IPVerification { return IPVerification{Available: true} },
				}
				if enhanced {
					prober.probeStreams = func(context.Context, *http.Client) *streamVerification {
						return &streamVerification{SuccessfulPairs: 2}
					}
				}
				got := prober.probeCandidate(t.Context(), Candidate{Name: "US label"}, &http.Client{})
				want := !enhanced || tc.want
				if (got.OAuthAttempts == 1) != want || (got.OAuthAdmissionStatus == oauthRegionSupported) != want ||
					got.Available != want || got.IPVerification == nil || (got.Streaming != nil) != (enhanced && want) {
					t.Fatalf("enhanced=%t: observation %+v, want qualified=%t", enhanced, got, want)
				}
				if enhanced && !want && tc.statsig.decision == statsigReachable &&
					(got.StatsigStatus != statsigUnsupportedCountry || got.StatsigCountry != tc.statsig.country || got.StatsigError == "") {
					t.Fatalf("missing country rejection evidence: %+v", got)
				}
			}
		})
	}
}

func TestProbeCandidateRunsStreamingOnlyAfterIPAdmission(t *testing.T) {
	for _, tt := range []struct {
		name        string
		enhanced    bool
		ipAvailable bool
		pairs       int
		available   bool
	}{
		{"basic", false, true, 0, true},
		{"enhanced with IP rejection", true, false, 2, false},
		{"enhanced with incomplete pairs", true, true, 1, false},
		{"enhanced with two complete pairs", true, true, 2, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			prober := &MihomoProber{
				options: MihomoOptions{Attempts: 1, EnhancedFilter: tt.enhanced},
				probeOAuth: func(context.Context, *http.Client, string, string, time.Duration) oauthProbeResult {
					return oauthProbeResult{decision: oauthRegionSupported}
				},
				probeStatsig: func(context.Context, *http.Client, string, string, time.Duration) statsigProbeResult {
					return statsigProbeResult{decision: statsigReachable, country: "TW"}
				},
				probeIPPaths: func(context.Context, *http.Client) IPVerification {
					return IPVerification{Available: tt.ipAvailable, RegionRejected: !tt.ipAvailable}
				},
			}
			if tt.enhanced {
				prober.probeStreams = func(context.Context, *http.Client) *streamVerification {
					return &streamVerification{SuccessfulPairs: tt.pairs}
				}
			}
			observed := prober.probeCandidate(t.Context(), Candidate{Fingerprint: "node"}, &http.Client{})
			if observed.Available != tt.available || observed.ServiceRejected == tt.ipAvailable ||
				(observed.Streaming != nil) != (tt.enhanced && tt.ipAvailable) {
				t.Fatalf("observation = %+v", observed)
			}
		})
	}
}

func TestProbeCandidateCompletesStatsigRetryBeforeOAuth(t *testing.T) {
	var statsigPassed atomic.Bool
	oauthCalls := 0
	statsigCalls := 0
	prober := &MihomoProber{
		probeIPPaths: func(context.Context, *http.Client) IPVerification { return IPVerification{Available: true} },
		options:      MihomoOptions{Attempts: 2, RetryDelay: time.Nanosecond, RequestTimeout: time.Second, EnhancedFilter: true},
		probeOAuth: func(context.Context, *http.Client, string, string, time.Duration) oauthProbeResult {
			if !statsigPassed.Load() {
				t.Error("OAuth started before the successful Statsig retry")
			}
			oauthCalls++
			if oauthCalls == 1 {
				return oauthProbeResult{decision: oauthTransportFailure, responseBytes: 10, err: context.DeadlineExceeded}
			}
			return oauthProbeResult{decision: oauthRegionSupported, httpStatus: 401, errorCode: "token_expired", responseBytes: 20}
		},
		probeStatsig: func(context.Context, *http.Client, string, string, time.Duration) statsigProbeResult {
			statsigCalls++
			if statsigCalls == 1 {
				return statsigProbeResult{decision: statsigTransportFailure, compressedBytes: 30, decompressedBytes: 40, err: context.DeadlineExceeded}
			}
			statsigPassed.Store(true)
			return statsigProbeResult{decision: statsigReachable, httpStatus: 200, country: "JP", contentEncoding: "br", compressedBytes: 50, decompressedBytes: 60}
		},
	}
	observation := prober.probeCandidate(context.Background(), Candidate{Fingerprint: "node"}, &http.Client{})
	if !observation.Available || observation.OAuthAttempts != 2 || observation.StatsigAttempts != 2 || observation.OAuthResponseBytes != 30 || observation.CompressedBytes != 80 || observation.DecompressedBytes != 100 {
		t.Fatalf("observation = %+v, want independently retried intersection", observation)
	}
}

func TestFailedOAuthSkipsStatsigOnlyInBasicMode(t *testing.T) {
	for _, tc := range []struct {
		name     string
		result   oauthProbeResult
		attempts int
	}{
		{"unsupported region", oauthProbeResult{decision: oauthRegionUnsupported, httpStatus: 403, explicitReject: true, errorCode: oauthRegionUnsupported}, 1},
		{"transport failure", oauthTransportError(context.DeadlineExceeded), 2},
		{"unexpected response", oauthProbeResult{decision: oauthUnexpectedResponse}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, enhanced := range []bool{false, true} {
				prober := &MihomoProber{
					options: MihomoOptions{Attempts: 2, RetryDelay: time.Nanosecond, EnhancedFilter: enhanced},
					probeIPPaths: func(context.Context, *http.Client) IPVerification {
						return IPVerification{Available: true}
					},
					probeOAuth: func(context.Context, *http.Client, string, string, time.Duration) oauthProbeResult {
						return tc.result
					},
					probeStatsig: func(context.Context, *http.Client, string, string, time.Duration) statsigProbeResult {
						return statsigProbeResult{decision: statsigReachable, country: "JP"}
					},
				}
				got := prober.probeCandidate(t.Context(), Candidate{Fingerprint: "node"}, &http.Client{})
				if got.Available || got.ServiceRejected != tc.result.explicitReject || got.OAuthAdmissionStatus != tc.result.decision ||
					got.OAuthAttempts != tc.attempts || (got.StatsigAttempts == 1) != enhanced || (got.StatsigCountry == "JP") != enhanced ||
					got.IPVerification == nil || got.Streaming != nil {
					t.Fatalf("enhanced=%t observation = %+v", enhanced, got)
				}
			}
		})
	}
}

func TestProbeCandidateCancelledAfterStatsigSkipsRemainingChecks(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	prober := &MihomoProber{
		options: MihomoOptions{Attempts: 1, EnhancedFilter: true},
		probeIPPaths: func(context.Context, *http.Client) IPVerification {
			return IPVerification{Available: true}
		},
		probeStatsig: func(context.Context, *http.Client, string, string, time.Duration) statsigProbeResult {
			cancel()
			return statsigProbeResult{decision: statsigReachable, country: "TW"}
		},
	}
	got := prober.probeCandidate(ctx, Candidate{Fingerprint: "node"}, &http.Client{})
	if got.Available || got.StatsigAttempts != 1 || got.OAuthAttempts != 0 || got.IPVerification == nil {
		t.Fatalf("continued qualification after cancellation: %+v", got)
	}
}

func TestRetrySpacingAccountsForCompletedAttempt(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if !waitToRetry(ctx, 1, 2, time.Now().Add(-time.Second)) {
		t.Fatal("slow attempt should already be eligible to retry")
	}
	if waitToRetry(ctx, 2, 2, time.Now()) {
		t.Fatal("exhausted retry budget")
	}
	cancel()
	if waitToRetry(ctx, 1, 2, time.Now().Add(-time.Second)) {
		t.Fatal("cancelled retry after a slow attempt")
	}
}
