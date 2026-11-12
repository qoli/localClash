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
	youtube := &youtubeObservation{HTTPStatus: http.StatusNoContent}
	measured := runAutoProbes(t.Context(), 4, 2, 2, func(ctx context.Context, index int) measurement {
		switch index {
		case 0:
			close(started)
			select {
			case <-release:
				return measurement{worstDoH: time.Millisecond, youtube: youtube}
			case <-ctx.Done():
				t.Error("in-flight probe was canceled")
				return measurement{}
			}
		case 1:
			<-started
		case 2:
			close(release)
		default:
			t.Errorf("unexpected probe after the pool filled: %d", index)
		}
		return measurement{worstDoH: time.Second, youtube: youtube}
	})
	rankByDoH(measured.completed)
	want := []measurement{
		{index: 0, worstDoH: time.Millisecond, youtube: youtube},
		{index: 1, worstDoH: time.Second, youtube: youtube},
		{index: 2, worstDoH: time.Second, youtube: youtube},
	}
	if !slices.Equal(measured.completed, want) || measured.filledAt.IsZero() || measured.drainedCount != 1 {
		t.Fatalf("probe results = %+v, want %+v with one drained probe", measured, want)
	}
}

func TestRunAutoProbesExhaustsCandidatesWhenPoolIsShort(t *testing.T) {
	want := []measurement{
		{index: 0, youtube: &youtubeObservation{HTTPStatus: http.StatusNoContent}},
		{index: 1, worstDoH: time.Millisecond, youtube: &youtubeObservation{Error: "timeout"}},
		{index: 2, worstDoH: time.Millisecond, youtube: &youtubeObservation{HTTPStatus: http.StatusOK}},
		{index: 3},
	}
	measured := runAutoProbes(t.Context(), len(want), 1, 1, func(_ context.Context, index int) measurement {
		return want[index]
	})
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
	youtubeStatus := http.StatusForbidden
	youtubeRequests := 0
	client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case poolcontract.AutoHTTPSProbeURL:
			if req.Method != http.MethodHead {
				t.Errorf("Cloudflare probe used %s, want HEAD", req.Method)
			}
			httpsRequests++
			return &http.Response{StatusCode: httpsStatus, Body: http.NoBody}, nil
		case autoYouTubeProbeURL:
			if req.Method != http.MethodHead {
				t.Errorf("YouTube probe used %s, want HEAD", req.Method)
			}
			youtubeRequests++
			return &http.Response{StatusCode: youtubeStatus, Body: http.NoBody}, nil
		default:
			return resolverTransport.RoundTrip(req)
		}
	})
	ctx := t.Context()
	got := measureAuto(ctx, client)
	if got.qualified() || got.worstDoH <= 0 || got.youtube == nil || got.youtube.HTTPStatus != http.StatusForbidden ||
		httpsRequests != 1 || youtubeRequests != 1 || requests.Load() != 4 {
		t.Fatalf("YouTube returned 403 with healthy DoH, got %+v and %d Cloudflare, %d YouTube, %d DNS requests",
			got, httpsRequests, youtubeRequests, requests.Load())
	}
	missingIPv6.Store(true)
	youtubeStatus = http.StatusNoContent
	got = measureAuto(ctx, client)
	if got.qualified() || got.worstDoH != 0 || got.youtube == nil || got.youtube.HTTPStatus != http.StatusNoContent ||
		httpsRequests != 2 || youtubeRequests != 2 || requests.Load() != 8 {
		t.Fatalf("both HTTPS probes passed but a resolver lacked AAAA, got %+v and %d Cloudflare, %d YouTube, %d DNS requests",
			got, httpsRequests, youtubeRequests, requests.Load())
	}
	missingIPv6.Store(false)
	httpsStatus = http.StatusOK
	got = measureAuto(ctx, client)
	if got != (measurement{}) || httpsRequests != 3 || youtubeRequests != 2 || requests.Load() != 8 {
		t.Fatalf("Cloudflare returned 200, got %+v and %d Cloudflare, %d YouTube, %d DNS requests",
			got, httpsRequests, youtubeRequests, requests.Load())
	}
	httpsStatus = http.StatusNoContent
	got = measureAuto(ctx, client)
	if !got.qualified() || httpsRequests != 4 || youtubeRequests != 3 || requests.Load() != 12 {
		t.Fatalf("Cloudflare, YouTube and DoH passed, got %+v and %d Cloudflare, %d YouTube, %d DNS requests",
			got, httpsRequests, youtubeRequests, requests.Load())
	}
}

func TestMeasureAutoOverlapsYouTubeWithDoH(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	youtubeStarted := make(chan struct{})
	dohStarted := make(chan struct{})
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case poolcontract.AutoHTTPSProbeURL:
			return &http.Response{StatusCode: http.StatusNoContent, Body: http.NoBody}, nil
		case autoYouTubeProbeURL:
			close(youtubeStarted)
			select {
			case <-dohStarted:
				return nil, context.DeadlineExceeded
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		default:
			close(dohStarted)
			select {
			case <-youtubeStarted:
				return &http.Response{StatusCode: http.StatusBadGateway, Body: http.NoBody}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	})}
	got := measureAuto(ctx, client)
	if ctx.Err() != nil {
		t.Fatal("YouTube and DoH did not start while the other request was in flight")
	}
	if got.worstDoH != 0 || got.youtube == nil || got.youtube.Error != "timeout" ||
		got.youtube.HTTPStatus != 0 || got.youtube.CheckedAt.IsZero() || got.youtube.DurationMS < 0 {
		t.Fatalf("expected a timed YouTube timeout and a failed DoH check, got %+v, YouTube %+v", got, got.youtube)
	}
}
