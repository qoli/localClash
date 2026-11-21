package poolprepare

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"localclash/internal/poolcontract"
	"localclash/internal/proxyprobe"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestEnhancedAutoQualificationSkipsActiveVideoDegradation(t *testing.T) {
	input, err := poolcontract.NewInput([]map[string]any{
		{"name": "slow", "type": "ss", "server": "slow.example"},
		{"name": "recovered", "type": "ss", "server": "recovered.example"},
	})
	if err != nil {
		t.Fatal(err)
	}
	names := []string{"slow", "recovered"}
	degraded := map[string]bool{"slow": true}
	for _, enhanced := range []bool{false, true} {
		want := names
		if enhanced {
			want = names[1:]
		}
		prober := func(_ context.Context, _ []map[string]any, candidates []autoCandidate, _ int, _ Options) (autoBatchResult, error) {
			if got := candidateNames(candidates); !slices.Equal(got, want) {
				t.Fatalf("enhanced=%t: probed %v, want %v", enhanced, got, want)
			}
			var measured autoBatchResult
			for i := range candidates {
				measured.completed = append(measured.completed, measurement{index: i, worstDoH: time.Millisecond,
					youtube: &youtubeObservation{HTTPStatus: http.StatusNoContent}})
			}
			return measured, nil
		}
		result, err := qualifyAutoWithProbe(t.Context(), input, names, Options{EnhancedFilter: enhanced, VideoDegraded: degraded}, prober)
		if err != nil || !slices.Equal(result.Qualified, want) || result.UnprobedCount != 0 || result.UnavailableCount != 0 {
			t.Fatalf("enhanced=%t: result=%+v, error=%v", enhanced, result, err)
		}
		skipped := result.AutoQuality.VideoDegradedSkipped
		if (enhanced && (skipped == nil || *skipped != 1)) || (!enhanced && skipped != nil) {
			t.Fatalf("enhanced=%t: invalid skip evidence %+v", enhanced, result.AutoQuality)
		}
	}
}

func TestEnhancedAutoQualificationUsesBasicResultsForShortRounds(t *testing.T) {
	var names []string
	var proxies []map[string]any
	position := make(map[string]int)
	for i := range proxyprobe.BatchSize + 3 {
		name := fmt.Sprintf("node-%03d", i)
		names = append(names, name)
		proxies = append(proxies, map[string]any{"name": name, "type": "ss", "server": name + ".example"})
		position[name] = i
	}
	input, err := poolcontract.NewInput(proxies)
	if err != nil {
		t.Fatal(err)
	}
	opts := Options{EnhancedFilter: true, RuntimeDir: t.TempDir()}
	if err := os.MkdirAll(filepath.Join(opts.RuntimeDir, poolcontract.Directory), 0o700); err != nil {
		t.Fatal(err)
	}
	fastestFirst := slices.Clone(names)
	slices.Reverse(fastestFirst)
	for _, test := range []struct {
		name           string
		basicPassed    int
		enhancedPassed int
		want           []string
		fallback       bool
	}{
		{"seven enhanced qualifiers use the complete basic result before the cap", len(names), 7, fastestFirst, true},
		{"the next round with eight uses enhanced qualifiers", len(names), 8, fastestFirst[len(names)-8:], false},
		{"only current basic qualifiers are eligible even below eight", 4, 0, fastestFirst[len(names)-4:], true},
		{"zero basic qualifiers yield an empty result", 0, 0, nil, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			prober := func(_ context.Context, _ []map[string]any, candidates []autoCandidate, _ int, opts Options) (autoBatchResult, error) {
				if !opts.EnhancedFilter {
					t.Fatal("round skipped enhanced filtering")
				}
				var measured autoBatchResult
				for i, candidate := range candidates {
					index := position[candidate.name]
					attempt := measurement{
						index:    i,
						worstDoH: time.Duration(len(names)-index) * time.Millisecond,
						youtube:  &youtubeObservation{HTTPStatus: http.StatusNoContent},
					}
					switch {
					case index < test.basicPassed:
						attempt.enhanced = &enhancedObservation{Checks: []networkObservation{{
							Probe: "ubuntu_updates", Passed: index < test.enhancedPassed,
						}}}
					case index%2 == 0:
						attempt.youtube.HTTPStatus = http.StatusForbidden
					default:
						attempt.worstDoH = 0
					}
					measured.completed = append(measured.completed, attempt)
				}
				return measured, nil
			}
			result, err := qualifyAutoWithProbe(t.Context(), input, names, opts, prober)
			if err != nil {
				t.Fatal(err)
			}
			want := test.want[:min(len(test.want), poolcontract.AutoLimit)]
			if !slices.Equal(result.Qualified, want) || result.QualifiedCount != len(want) {
				t.Fatalf("qualified = %v (%d), want %v", result.Qualified, result.QualifiedCount, want)
			}
			if result.Probed != len(names) || result.UnprobedCount != 0 ||
				result.ObservedQualifiedCount != len(test.want) || result.UnavailableCount != len(names)-len(test.want) {
				t.Fatalf("unexpected probe counts: %+v", result)
			}
			quality := result.AutoQuality
			if quality.UsedBasicFallback != test.fallback || quality.Enhanced.Probed != test.basicPassed ||
				quality.Enhanced.Passed != test.enhancedPassed || quality.HistoryWarning != "" {
				t.Fatalf("unexpected qualification evidence: %+v, enhanced=%+v", quality, quality.Enhanced)
			}
		})
	}
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
