package chatgptavailable

import (
	"context"
	"encoding/json"
	"net/http/httptrace"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

const probeTimingEnv = "LOCALCLASH_CHATGPT_PROBE_TIMINGS"

type timingContextKey int

const (
	timingCandidate timingContextKey = iota
	timingScope
)

type probeTiming struct {
	Event        string `json:"event"`
	Phase        string `json:"phase"`
	Candidate    int    `json:"candidate,omitempty"`
	Scope        string `json:"scope,omitempty"`
	Service      string `json:"service,omitempty"`
	Attempt      int    `json:"attempt,omitempty"`
	DurationMS   int64  `json:"duration_ms"`
	Outcome      string `json:"outcome,omitempty"`
	HTTPStatus   int    `json:"http_status,omitempty"`
	Connections  int32  `json:"connections,omitempty"`
	Reused       int32  `json:"reused,omitempty"`
	Addresses    int    `json:"addresses,omitempty"`
	Candidates   int    `json:"candidates,omitempty"`
	Definitions  int    `json:"definitions,omitempty"`
	WireBytes    int64  `json:"wire_bytes,omitempty"`
	DecodedBytes int64  `json:"decoded_bytes,omitempty"`
	BodyMS       int64  `json:"body_ms,omitempty"`
	Protocol     string `json:"protocol,omitempty"`
	StreamStage  string `json:"stream_stage,omitempty"`
	Messages     int    `json:"messages,omitempty"`
	FirstMS      int64  `json:"first_ms,omitempty"`
	SpanMS       int64  `json:"span_ms,omitempty"`
}

type probeTimings struct {
	mu      sync.Mutex
	encoder *json.Encoder
}

func newProbeTimings() *probeTimings {
	if os.Getenv(probeTimingEnv) != "1" {
		return nil
	}
	return &probeTimings{encoder: json.NewEncoder(os.Stderr)}
}

func (p *MihomoProber) recordTiming(ctx context.Context, started time.Time, event probeTiming, connections *timedConnections) {
	if p.timings == nil {
		return
	}
	event.Event = "chatgpt_probe_timing"
	event.DurationMS = time.Since(started).Milliseconds()
	event.Candidate, _ = ctx.Value(timingCandidate).(int)
	if event.Scope == "" {
		event.Scope, _ = ctx.Value(timingScope).(string)
	}
	if connections != nil {
		event.Connections = connections.total.Load()
		event.Reused = connections.reused.Load()
	}
	p.timings.mu.Lock()
	defer p.timings.mu.Unlock()
	_ = p.timings.encoder.Encode(event)
}

type timedConnections struct {
	total  atomic.Int32
	reused atomic.Int32
}

func (p *MihomoProber) traceConnections(ctx context.Context) (context.Context, *timedConnections) {
	if p.timings == nil {
		return ctx, nil
	}
	connections := &timedConnections{}
	return httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		GotConn: func(info httptrace.GotConnInfo) {
			connections.total.Add(1)
			if info.Reused {
				connections.reused.Add(1)
			}
		},
	}), connections
}
