package chatgptavailable

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/coder/websocket"
	"localclash/internal/capability"
)

const (
	streamSSEURL         = "https://httpbingo.org/sse?delay=0s&duration=5s&count=6"
	streamWebSocketURL   = "wss://httpbingo.org/websocket/echo"
	streamingConcurrency = 6
	streamingAttempts    = 3
	streamingPasses      = 2
	streamingTimeout     = 16 * time.Second
	sseEvents            = 6
	sseMinimumSpan       = 3 * time.Second
	webSocketEchoGap     = 2500 * time.Millisecond
)

type streamVerification struct {
	SuccessfulPairs int             `json:"successful_pairs"`
	Attempts        []streamAttempt `json:"attempts"`
}

func (v streamVerification) passed() bool {
	return v.SuccessfulPairs >= streamingPasses
}

type streamAttempt struct {
	SSE       streamObservation `json:"sse"`
	WebSocket streamObservation `json:"websocket"`
}

type streamObservation struct {
	Stage      string `json:"stage"`
	Error      string `json:"error,omitempty"`
	HTTPStatus int    `json:"http_status,omitempty"`
	Protocol   string `json:"protocol,omitempty"`
	Messages   int    `json:"messages"`
	FirstMS    int64  `json:"first_ms,omitempty"`
	SpanMS     int64  `json:"span_ms,omitempty"`
	DurationMS int64  `json:"duration_ms"`
}

func (o streamObservation) passed() bool {
	return o.Stage == "complete"
}

func newStreamingSummary(enabled bool) *capability.EnhancedProbeSummary {
	if !enabled {
		return nil
	}
	return &capability.EnhancedProbeSummary{
		Checks: map[string]capability.NetworkProbeSummary{"sse": {}, "websocket": {}},
	}
}

func (v streamVerification) record(summary *capability.EnhancedProbeSummary) {
	summary.Probed++
	if v.passed() {
		summary.Passed++
	}
	for _, attempt := range v.Attempts {
		recordStreamCheck(summary, "sse", attempt.SSE)
		recordStreamCheck(summary, "websocket", attempt.WebSocket)
	}
}

func recordStreamCheck(summary *capability.EnhancedProbeSummary, name string, observation streamObservation) {
	count := summary.Checks[name]
	count.Probed++
	if observation.passed() {
		count.Passed++
	}
	summary.Checks[name] = count
}

func (p *MihomoProber) probeStreaming(ctx context.Context, client *http.Client) *streamVerification {
	client.CloseIdleConnections()
	select {
	case p.streamSlots <- struct{}{}:
		defer func() { <-p.streamSlots }()
	case <-ctx.Done():
		return &streamVerification{}
	}
	return verifyStreaming(ctx, func(ctx context.Context, attempt int) streamAttempt {
		// Isolate attempts at the connection level, through the same candidate listener.
		sseClient := streamHTTPClient(client, true)
		defer sseClient.CloseIdleConnections()
		wsClient := streamHTTPClient(client, false)
		defer wsClient.CloseIdleConnections()
		sse := make(chan streamObservation, 1)
		go func() {
			sse <- p.measureStream(ctx, "sse", attempt, func(ctx context.Context) streamObservation {
				return probeSSE(ctx, sseClient, streamSSEURL)
			})
		}()
		ws := p.measureStream(ctx, "websocket", attempt, func(ctx context.Context) streamObservation {
			return probeWebSocket(ctx, wsClient, streamWebSocketURL, webSocketEchoGap)
		})
		return streamAttempt{SSE: <-sse, WebSocket: ws}
	})
}

func verifyStreaming(ctx context.Context, probe func(context.Context, int) streamAttempt) *streamVerification {
	result := &streamVerification{}
	for attempt := 1; attempt <= streamingAttempts && ctx.Err() == nil; attempt++ {
		pair := probe(ctx, attempt)
		result.Attempts = append(result.Attempts, pair)
		if pair.SSE.passed() && pair.WebSocket.passed() {
			result.SuccessfulPairs++
		}
		if result.passed() || result.SuccessfulPairs+streamingAttempts-attempt < streamingPasses {
			break
		}
	}
	return result
}

func streamHTTPClient(base *http.Client, http2 bool) *http.Client {
	client := *base
	transport := base.Transport.(*http.Transport).Clone()
	transport.Protocols = new(http.Protocols)
	transport.Protocols.SetHTTP1(true)
	transport.Protocols.SetHTTP2(http2)
	// The context owns the whole response budget, including the upgraded socket.
	transport.TLSHandshakeTimeout = 0
	client.Transport = transport
	client.Timeout = 0
	return &client
}

func (p *MihomoProber) measureStream(ctx context.Context, service string, attempt int, probe func(context.Context) streamObservation) streamObservation {
	started := time.Now()
	probeCtx, cancel := context.WithTimeout(ctx, streamingTimeout)
	defer cancel()
	probeCtx, connections := p.traceConnections(probeCtx)
	var phase atomic.Value
	phase.Store("connect")
	probeCtx = httptrace.WithClientTrace(probeCtx, &httptrace.ClientTrace{
		TLSHandshakeStart: func() { phase.Store("tls") },
		WroteRequest:      func(httptrace.WroteRequestInfo) { phase.Store("request") },
	})
	result := probe(probeCtx)
	if result.HTTPStatus == 0 && !result.passed() {
		result.Stage = phase.Load().(string)
	}
	result.DurationMS = time.Since(started).Milliseconds()
	outcome := result.Error
	if result.passed() {
		outcome = "complete"
	}
	p.recordTiming(ctx, started, probeTiming{
		Phase: "streaming", Service: service, Attempt: attempt, Outcome: outcome,
		HTTPStatus: result.HTTPStatus, Protocol: result.Protocol, StreamStage: result.Stage,
		Messages: result.Messages, FirstMS: result.FirstMS, SpanMS: result.SpanMS,
	}, connections)
	return result
}

func probeSSE(ctx context.Context, client *http.Client, endpoint string) streamObservation {
	started := time.Now()
	result := streamObservation{Stage: "request"}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		result.Error = streamError(err)
		return result
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := client.Do(req)
	if err != nil {
		result.Error = streamError(err)
		return result
	}
	defer resp.Body.Close()
	result.Stage, result.HTTPStatus, result.Protocol = "headers", resp.StatusCode, resp.Proto
	media, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	switch {
	case resp.StatusCode != http.StatusOK:
		result.Error = "unexpected_http_status"
	case media != "text/event-stream":
		result.Error = "unexpected_content_type"
	case resp.ProtoMajor != 2:
		result.Error = "expected_http2"
	default:
		result.Stage = "body"
		result.Error = readSSE(resp.Body, &result, started, sseMinimumSpan)
		if result.Error == "" {
			result.Stage = "complete"
		}
	}
	return result
}

func readSSE(body io.Reader, result *streamObservation, started time.Time, minimumSpan time.Duration) string {
	scanner := bufio.NewScanner(body)
	var first time.Time
	var event, data string
	var hasData bool
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimPrefix(strings.TrimPrefix(line, "event:"), " ")
		case strings.HasPrefix(line, "data:"):
			if hasData {
				return "unexpected_sse_event"
			}
			data, hasData = strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "), true
		case line == "":
			if hasData {
				var payload struct {
					ID        *int  `json:"id"`
					Timestamp int64 `json:"timestamp"`
				}
				if event != "ping" || result.Messages == sseEvents ||
					json.Unmarshal([]byte(data), &payload) != nil ||
					payload.ID == nil || *payload.ID != result.Messages || payload.Timestamp <= 0 {
					return "unexpected_sse_event"
				}
				result.recordMessage(started, &first)
			}
			event, data, hasData = "", "", false
		}
	}
	if err := scanner.Err(); err != nil {
		return streamError(err)
	}
	if result.Messages != sseEvents || hasData || event != "" {
		return "eof_before_completion"
	}
	if result.SpanMS < minimumSpan.Milliseconds() {
		return "buffered_stream"
	}
	return ""
}

func probeWebSocket(ctx context.Context, client *http.Client, endpoint string, pause time.Duration) streamObservation {
	started := time.Now()
	result := streamObservation{Stage: "upgrade"}
	conn, resp, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{HTTPClient: client})
	if resp != nil {
		result.HTTPStatus, result.Protocol = resp.StatusCode, resp.Proto
	}
	if err != nil {
		result.Error = streamError(err)
		if resp != nil {
			result.Error = "invalid_websocket_upgrade"
		}
		return result
	}
	defer conn.CloseNow()

	var first time.Time
	result.Stage = "echo"
	for index, payload := range []string{"localclash-stream-1", "localclash-stream-2", "localclash-stream-3"} {
		if index > 0 {
			select {
			case <-time.After(pause):
			case <-ctx.Done():
				result.Error = streamError(ctx.Err())
				return result
			}
		}
		if err := conn.Write(ctx, websocket.MessageText, []byte(payload)); err != nil {
			result.Error = streamError(err)
			return result
		}
		kind, message, err := conn.Read(ctx)
		if err != nil {
			result.Error = streamError(err)
			return result
		}
		if kind != websocket.MessageText || string(message) != payload {
			result.Error = "unexpected_websocket_echo"
			return result
		}
		result.recordMessage(started, &first)
	}
	result.Stage = "complete"
	return result
}

func (o *streamObservation) recordMessage(started time.Time, first *time.Time) {
	now := time.Now()
	if first.IsZero() {
		*first = now
		o.FirstMS = now.Sub(started).Milliseconds()
	}
	o.Messages++
	o.SpanMS = now.Sub(*first).Milliseconds()
}

func streamError(err error) string {
	var networkError net.Error
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &networkError) && networkError.Timeout()):
		return "timeout"
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return "unexpected_eof"
	case errors.Is(err, syscall.ECONNRESET):
		return "connection_reset"
	case websocket.CloseStatus(err) != -1:
		return "websocket_closed"
	}
	switch message := err.Error(); {
	case strings.Contains(message, "stream error:"):
		return "http2_stream_error"
	case strings.Contains(message, "gzip:"):
		return "gzip_error"
	default:
		return "transport_error"
	}
}
