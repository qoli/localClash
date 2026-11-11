package poolprepare

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"localclash/internal/poolcontract"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestRunAutoProbesDrainsAfterEnoughQualified(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	measured := runAutoProbes(t.Context(), 4, 2, 2, func(ctx context.Context, index int) time.Duration {
		switch index {
		case 0:
			close(started)
			select {
			case <-release:
				return time.Millisecond
			case <-ctx.Done():
				t.Error("in-flight probe was canceled")
				return 0
			}
		case 1:
			<-started
		case 2:
			close(release)
		default:
			t.Errorf("unexpected probe after the pool filled: %d", index)
		}
		return time.Second
	})
	rankByDoH(measured.completed)
	want := []measurement{
		{index: 0, worstDoH: time.Millisecond},
		{index: 1, worstDoH: time.Second},
		{index: 2, worstDoH: time.Second},
	}
	if !slices.Equal(measured.completed, want) || measured.filledAt.IsZero() || measured.drainedCount != 1 {
		t.Fatalf("probe results = %+v, want %+v with one drained probe", measured, want)
	}
}

func TestRunAutoProbesExhaustsCandidatesWhenPoolIsShort(t *testing.T) {
	measured := runAutoProbes(t.Context(), 3, 1, 2, func(_ context.Context, index int) time.Duration {
		if index == 1 {
			return time.Millisecond
		}
		return 0
	})
	want := []measurement{{index: 0}, {index: 1, worstDoH: time.Millisecond}, {index: 2}}
	if !slices.Equal(measured.completed, want) || !measured.filledAt.IsZero() || measured.drainedCount != 0 {
		t.Fatalf("probe results = %+v, want %+v with no drain", measured, want)
	}
}

func TestRankByDoHCanReplaceAnEarlyQualifier(t *testing.T) {
	qualified := []measurement{
		{index: 1, worstDoH: 4 * time.Second},
		{index: 2, worstDoH: 2 * time.Second},
		{index: 0, worstDoH: time.Second},
	}
	first := summarizeDoH(qualified[:2])
	rankByDoH(qualified)
	selected := summarizeDoH(qualified[:2])
	if first.MaxMS != 4000 || selected.MaxMS != 2000 || qualified[0].index != 0 {
		t.Fatalf("first = %+v, selected = %+v, ranked = %+v", first, selected, qualified)
	}
}

func TestMeasureAutoRequiresHTTPSAndBothAddressFamiliesOnBothResolvers(t *testing.T) {
	var requests atomic.Int32
	var missingIPv6 atomic.Bool
	resolver := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		query, err := base64.RawURLEncoding.DecodeString(r.URL.Query().Get("dns"))
		if err != nil || len(query) < 17 || r.Header.Get("Accept") != "application/dns-message" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		kind := binary.BigEndian.Uint16(query[len(query)-4:])
		query[2], query[3], query[7] = 0x81, 0x80, 1
		addressLength := byte(4)
		if kind == 28 {
			addressLength = 16
			if missingIPv6.Load() && r.URL.Path == "/second" {
				query[7] = 0
			}
		}
		response := append(query, 0xc0, 0x0c, 0, byte(kind), 0, 1, 0, 0, 0, 60, 0, addressLength)
		w.Header().Set("Content-Type", "application/dns-message")
		w.Write(append(response, make([]byte, addressLength)...))
	}))
	defer resolver.Close()
	previous := overseasDoH
	t.Cleanup(func() { overseasDoH = previous })
	overseasDoH = []string{resolver.URL + "/first", resolver.URL + "/second"}
	client := resolver.Client()
	resolverTransport := client.Transport
	httpsStatus := http.StatusNoContent
	httpsRequests := 0
	client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != poolcontract.AutoHTTPSProbeURL {
			return resolverTransport.RoundTrip(req)
		}
		if req.Method != http.MethodHead {
			t.Errorf("HTTPS probe used %s, want HEAD", req.Method)
		}
		httpsRequests++
		return &http.Response{StatusCode: httpsStatus, Body: http.NoBody}, nil
	})
	ctx := context.Background()
	if got := measureAuto(ctx, client); got <= 0 || httpsRequests != 1 || requests.Load() != 4 {
		t.Fatalf("healthy endpoints measured %v with %d HTTPS and %d DNS requests", got, httpsRequests, requests.Load())
	}
	missingIPv6.Store(true)
	if got := measureAuto(ctx, client); got != 0 || httpsRequests != 2 || requests.Load() != 8 {
		t.Fatalf("HTTPS passed but one resolver lacked AAAA, got %v with %d HTTPS and %d DNS requests", got, httpsRequests, requests.Load())
	}
	missingIPv6.Store(false)
	httpsStatus = http.StatusOK
	if got := measureAuto(ctx, client); got != 0 || httpsRequests != 3 || requests.Load() != 8 {
		t.Fatalf("HTTPS endpoint returned an unexpected status, got %v with %d HTTPS and %d DNS requests", got, httpsRequests, requests.Load())
	}
}
