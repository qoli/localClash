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

func TestProbeCandidateSingleFailureDoesNotRetry(t *testing.T) {
	calls := 0
	prober := &MihomoProber{
		options: MihomoOptions{RequestTimeout: time.Second},
		probe: func(context.Context, *http.Client, string, time.Duration) g204Result {
			calls++
			return g204Result{httpStatus: 500, err: context.DeadlineExceeded}
		},
	}
	observation := prober.probeCandidate(context.Background(), Candidate{EndpointFingerprint: "endpoint"}, &http.Client{})
	if observation.Available || observation.Attempts != 1 || observation.HTTPStatus != 500 || calls != 1 || strings.TrimSpace(observation.Error) == "" {
		t.Fatalf("observation = %+v calls=%d", observation, calls)
	}
}
