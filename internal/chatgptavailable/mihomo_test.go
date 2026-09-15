package chatgptavailable

import (
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

func TestNewMihomoProberDefaultsScaleLargeSubscriptions(t *testing.T) {
	corePath := filepath.Join(t.TempDir(), "mihomo")
	if err := os.WriteFile(corePath, []byte("test"), 0o700); err != nil {
		t.Fatal(err)
	}
	prober, err := NewMihomoProber(MihomoOptions{
		CorePath:      corePath,
		RuntimeParent: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if prober.options.Concurrency != 16 || prober.options.RequestTimeout != 5*time.Second || prober.options.Attempts != 2 || prober.options.Endpoint != oauthTokenURL || prober.options.ClientID != oauthClientID {
		t.Fatalf("probe defaults = %+v, want concurrency 16, timeout 5s, two attempts, and OAuth defaults", prober.options)
	}
}

func TestProbeCandidateRetriesInconclusiveFailureAndAccumulatesResponseBytes(t *testing.T) {
	calls := 0
	prober := &MihomoProber{
		options: MihomoOptions{Attempts: 3, RetryDelay: time.Nanosecond, RequestTimeout: time.Second},
		probe: func(context.Context, *http.Client, string, string, time.Duration) oauthProbeResult {
			calls++
			if calls == 1 {
				return oauthProbeResult{decision: oauthTransportFailure, responseBytes: 100, err: context.DeadlineExceeded}
			}
			return oauthProbeResult{decision: oauthRegionSupported, httpStatus: http.StatusUnauthorized, errorCode: "token_expired", responseBytes: 50}
		},
	}
	observation := prober.probeCandidate(context.Background(), Candidate{Fingerprint: "node"}, &http.Client{})
	if !observation.Available || observation.Attempts != 2 || calls != 2 || observation.ResponseBytes != 150 || observation.AdmissionStatus != oauthRegionSupported {
		t.Fatalf("observation = %+v calls=%d, want second-attempt success with cumulative bytes", observation, calls)
	}
}

func TestProbeCandidateStopsAfterConclusiveUnsupportedRegion(t *testing.T) {
	calls := 0
	prober := &MihomoProber{
		options: MihomoOptions{Attempts: 2, RetryDelay: time.Nanosecond, RequestTimeout: time.Second},
		probe: func(context.Context, *http.Client, string, string, time.Duration) oauthProbeResult {
			calls++
			return oauthProbeResult{decision: oauthRegionUnsupported, httpStatus: http.StatusForbidden, explicitReject: true, errorCode: oauthRegionUnsupported, err: errors.New("rejected")}
		},
	}
	observation := prober.probeCandidate(context.Background(), Candidate{Fingerprint: "node"}, &http.Client{})
	if observation.Available || !observation.ServiceRejected || observation.Attempts != 1 || calls != 1 || !strings.Contains(observation.Error, "rejected") {
		t.Fatalf("observation = %+v calls=%d, want immediate conclusive rejection", observation, calls)
	}
}
