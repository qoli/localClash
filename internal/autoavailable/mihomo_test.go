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

func TestNewMihomoProberDefaultsToOneRetry(t *testing.T) {
	prober, err := NewMihomoProber(MihomoOptions{CorePath: "mihomo", RuntimeParent: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if prober.options.Concurrency != 16 || prober.options.Attempts != 2 || prober.options.RequestTimeout != 5*time.Second || prober.options.RetryDelay != 500*time.Millisecond {
		t.Fatalf("probe defaults = %+v, want 16 workers, two attempts, 5s timeout, and 500ms retry delay", prober.options)
	}
}

func TestProbeCandidateRetriesOneFailure(t *testing.T) {
	calls := 0
	prober := &MihomoProber{
		options: MihomoOptions{Attempts: 2, RetryDelay: time.Nanosecond, RequestTimeout: time.Second},
		probe: func(context.Context, *http.Client, string, time.Duration) g204Result {
			calls++
			if calls == 1 {
				return g204Result{httpStatus: 500, err: context.DeadlineExceeded}
			}
			return g204Result{httpStatus: http.StatusNoContent}
		},
	}
	observation := prober.probeCandidate(context.Background(), Candidate{EndpointFingerprint: "endpoint"}, &http.Client{})
	if !observation.Available || observation.Attempts != 2 || observation.HTTPStatus != http.StatusNoContent || calls != 2 || strings.TrimSpace(observation.Error) != "" {
		t.Fatalf("observation = %+v calls=%d, want second-attempt success", observation, calls)
	}
}

func TestProbeCandidateRejectsAfterTwoFailures(t *testing.T) {
	calls := 0
	prober := &MihomoProber{
		options: MihomoOptions{Attempts: 2, RetryDelay: time.Nanosecond, RequestTimeout: time.Second},
		probe: func(context.Context, *http.Client, string, time.Duration) g204Result {
			calls++
			return g204Result{httpStatus: 500, err: context.DeadlineExceeded}
		},
	}
	observation := prober.probeCandidate(context.Background(), Candidate{EndpointFingerprint: "endpoint"}, &http.Client{})
	if observation.Available || observation.Attempts != 2 || observation.HTTPStatus != 500 || calls != 2 || strings.TrimSpace(observation.Error) == "" {
		t.Fatalf("observation = %+v calls=%d, want rejection after two failures", observation, calls)
	}
}
