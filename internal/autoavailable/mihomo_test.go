package autoavailable

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestProbeGenerate204RequiresExactEmpty204(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		ok     bool
	}{
		{name: "valid", status: http.StatusNoContent, ok: true},
		{name: "wrong status", status: http.StatusOK},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			result := probeGenerate204(context.Background(), server.Client(), server.URL, time.Second)
			if (result.err == nil) != test.ok {
				t.Fatalf("result = %+v, want ok=%v", result, test.ok)
			}
		})
	}
}

func TestProbeCandidateRetriesTransportOrStatusFailure(t *testing.T) {
	calls := 0
	prober := &MihomoProber{
		options: MihomoOptions{Attempts: 3, RetryDelay: time.Nanosecond, RequestTimeout: time.Second},
		probe: func(context.Context, *http.Client, string, time.Duration) g204Result {
			calls++
			if calls == 1 {
				return g204Result{httpStatus: 500, err: context.DeadlineExceeded}
			}
			return g204Result{httpStatus: 204}
		},
	}
	observation := prober.probeCandidate(context.Background(), Candidate{EndpointFingerprint: "endpoint"}, &http.Client{})
	if !observation.Available || observation.Attempts != 2 || observation.HTTPStatus != 204 || calls != 2 || strings.TrimSpace(observation.Error) != "" {
		t.Fatalf("observation = %+v calls=%d", observation, calls)
	}
}
