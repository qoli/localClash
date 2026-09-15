package chatgptavailable

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

func TestReadStatsigCountryRejectsMissingOrTrailingData(t *testing.T) {
	for _, input := range []string{`{"feature_gates":{}}`, `{"derived_fields":{"country":"US"}} true`} {
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
	if options.Concurrency != 16 || options.RequestTimeout != 5*time.Second || options.Attempts != 2 || options.OAuthEndpoint != oauthTokenURL || options.OAuthClientID != oauthClientID || options.StatsigEndpoint != statsigInitializeURL || options.StatsigClientKey != statsigClientKey {
		t.Fatalf("probe defaults = %+v, want OAuth and Statsig defaults", options)
	}
}

func TestProbeCandidateRequiresOAuthAndStatsigIntersection(t *testing.T) {
	tests := []struct {
		name       string
		oauth      oauthProbeResult
		statsig    statsigProbeResult
		want       bool
		wantReject bool
	}{
		{name: "both-pass", oauth: oauthProbeResult{decision: oauthRegionSupported, httpStatus: 401, errorCode: "token_expired"}, statsig: statsigProbeResult{decision: statsigReachable, httpStatus: 200, country: "US", contentEncoding: "br"}, want: true},
		{name: "oauth-only", oauth: oauthProbeResult{decision: oauthRegionSupported, httpStatus: 401, errorCode: "token_expired"}, statsig: statsigProbeResult{decision: statsigTransportFailure, err: errors.New("statsig timeout")}},
		{name: "statsig-only", oauth: oauthProbeResult{decision: oauthRegionUnsupported, httpStatus: 403, errorCode: oauthRegionUnsupported, explicitReject: true, err: errors.New("unsupported")}, statsig: statsigProbeResult{decision: statsigReachable, httpStatus: 200, country: "SG", contentEncoding: "br"}, wantReject: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			oauthCalls := 0
			statsigCalls := 0
			prober := &MihomoProber{
				options: MihomoOptions{Attempts: 1, RetryDelay: time.Nanosecond, RequestTimeout: time.Second},
				probeOAuth: func(context.Context, *http.Client, string, string, time.Duration) oauthProbeResult {
					oauthCalls++
					return test.oauth
				},
				probeStatsig: func(context.Context, *http.Client, string, string, time.Duration) statsigProbeResult {
					statsigCalls++
					return test.statsig
				},
			}
			observation := prober.probeCandidate(context.Background(), Candidate{Fingerprint: "node"}, &http.Client{})
			if observation.Available != test.want || observation.ServiceRejected != test.wantReject || oauthCalls != 1 || statsigCalls != 1 {
				t.Fatalf("observation = %+v OAuth calls=%d Statsig calls=%d", observation, oauthCalls, statsigCalls)
			}
		})
	}
}

func TestProbeCandidateRetriesEachInconclusiveProbeIndependently(t *testing.T) {
	oauthCalls := 0
	statsigCalls := 0
	prober := &MihomoProber{
		options: MihomoOptions{Attempts: 2, RetryDelay: time.Nanosecond, RequestTimeout: time.Second},
		probeOAuth: func(context.Context, *http.Client, string, string, time.Duration) oauthProbeResult {
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
			return statsigProbeResult{decision: statsigReachable, httpStatus: 200, country: "JP", contentEncoding: "br", compressedBytes: 50, decompressedBytes: 60}
		},
	}
	observation := prober.probeCandidate(context.Background(), Candidate{Fingerprint: "node"}, &http.Client{})
	if !observation.Available || observation.OAuthAttempts != 2 || observation.StatsigAttempts != 2 || observation.OAuthResponseBytes != 30 || observation.CompressedBytes != 80 || observation.DecompressedBytes != 100 {
		t.Fatalf("observation = %+v, want independently retried intersection", observation)
	}
}

func TestProbeCandidateStopsOAuthAfterConclusiveUnsupportedButStillRunsStatsig(t *testing.T) {
	oauthCalls := 0
	statsigCalls := 0
	prober := &MihomoProber{
		options: MihomoOptions{Attempts: 2, RetryDelay: time.Nanosecond, RequestTimeout: time.Second},
		probeOAuth: func(context.Context, *http.Client, string, string, time.Duration) oauthProbeResult {
			oauthCalls++
			return oauthProbeResult{decision: oauthRegionUnsupported, httpStatus: 403, explicitReject: true, errorCode: oauthRegionUnsupported, err: errors.New("unsupported")}
		},
		probeStatsig: func(context.Context, *http.Client, string, string, time.Duration) statsigProbeResult {
			statsigCalls++
			return statsigProbeResult{decision: statsigReachable, httpStatus: 200, country: "TW", contentEncoding: "br"}
		},
	}
	observation := prober.probeCandidate(context.Background(), Candidate{Fingerprint: "node"}, &http.Client{})
	if observation.Available || !observation.ServiceRejected || observation.OAuthAttempts != 1 || observation.StatsigAttempts != 1 || oauthCalls != 1 || statsigCalls != 1 {
		t.Fatalf("observation = %+v OAuth calls=%d Statsig calls=%d", observation, oauthCalls, statsigCalls)
	}
}
