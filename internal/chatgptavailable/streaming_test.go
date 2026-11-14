package chatgptavailable

import (
	"compress/gzip"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestStreamingAdmissionRequiresTwoCompletePairs(t *testing.T) {
	tests := []struct {
		name   string
		pairs  []string
		passes int
	}{
		{"both pass twice", []string{"both", "both"}, 2},
		{"one failed pair can be retested", []string{"sse", "both", "both"}, 2},
		{"different protocols passing separately never add up", []string{"sse", "websocket"}, 0},
		{"one full pair is insufficient", []string{"both", "sse", "websocket"}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := verifyStreaming(t.Context(), func(_ context.Context, attempt int) streamAttempt {
				if attempt > len(tt.pairs) {
					t.Fatal("attempted an extra pair after admission was decided")
				}
				var pair streamAttempt
				outcome := tt.pairs[attempt-1]
				if outcome == "both" || outcome == "sse" {
					pair.SSE.Stage = "complete"
				}
				if outcome == "both" || outcome == "websocket" {
					pair.WebSocket.Stage = "complete"
				}
				return pair
			})
			if got.SuccessfulPairs != tt.passes || len(got.Attempts) != len(tt.pairs) || got.passed() != (tt.passes == 2) {
				t.Fatalf("verification = %+v", got)
			}
			summary := newStreamingSummary(true)
			got.record(summary)
			if summary.Probed != 1 || summary.Checks["sse"].Probed != len(tt.pairs) ||
				summary.Checks["websocket"].Probed != len(tt.pairs) || (summary.Passed == 1) != got.passed() {
				t.Fatalf("summary = %+v", summary)
			}
		})
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if got := verifyStreaming(ctx, func(context.Context, int) streamAttempt {
		t.Fatal("called probe after cancellation")
		return streamAttempt{}
	}); len(got.Attempts) != 0 {
		t.Fatalf("cancelled verification = %+v", got)
	}
}

func TestReadSSEChecksEventSequenceAndFraming(t *testing.T) {
	complete := sseTestEvents(sseEvents)
	tests := []struct {
		name, body, expected string
		span                 time.Duration
	}{
		{"complete", complete, "", 0},
		{"buffered", complete, "buffered_stream", sseMinimumSpan},
		{"missing events", sseTestEvents(4), "eof_before_completion", 0},
		{"unframed last event", strings.TrimSuffix(complete, "\n"), "eof_before_completion", 0},
		{"reordered", strings.Replace(complete, "\"id\":2", "\"id\":1", 1), "unexpected_sse_event", 0},
		{"wrong type", strings.Replace(complete, "event: ping", "event: done", 1), "unexpected_sse_event", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var observation streamObservation
			if outcome := readSSE(strings.NewReader(tt.body), &observation, time.Now(), tt.span); outcome != tt.expected {
				t.Fatalf("readSSE = %q, want %q", outcome, tt.expected)
			}
		})
	}
}

func TestProbeSSERejectsIncompleteHTTP2Bodies(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if r.URL.Path == "/gzip" {
			w.Header().Set("Content-Encoding", "gzip")
			writer := gzip.NewWriter(w)
			_, _ = writer.Write([]byte(sseTestEvents(1)))
			_ = writer.Flush() // Intentionally omit the gzip footer.
			return
		}
		_, _ = w.Write([]byte(sseTestEvents(1)))
		w.(http.Flusher).Flush()
		panic(http.ErrAbortHandler)
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()

	client := streamHTTPClient(server.Client(), true)
	defer client.CloseIdleConnections()
	for path, expected := range map[string]string{"/gzip": "unexpected_eof", "/reset": "http2_stream_error"} {
		result := probeSSE(t.Context(), client, server.URL+path)
		if result.Stage != "body" || result.HTTPStatus != 200 || result.Protocol != "HTTP/2.0" ||
			result.Error != expected || result.passed() {
			t.Fatalf("%s: %+v, want %s after HTTP/2 headers", path, result, expected)
		}
	}
}

func TestProbeWebSocketRequiresAllEchoes(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		for index := range 3 {
			kind, message, err := conn.Read(t.Context())
			if err != nil {
				return
			}
			if r.URL.Path == "/close" && index == 1 {
				return
			}
			if err := conn.Write(t.Context(), kind, message); err != nil {
				return
			}
		}
	}))
	defer server.Close()
	client := streamHTTPClient(server.Client(), false)
	defer client.CloseIdleConnections()
	secureURL := "wss" + strings.TrimPrefix(server.URL, "https")
	if result := probeWebSocket(t.Context(), client, secureURL, 0); !result.passed() || result.HTTPStatus != 101 || result.Messages != 3 {
		t.Fatalf("complete WebSocket = %+v", result)
	}
	if result := probeWebSocket(t.Context(), client, secureURL+"/close", 0); result.passed() || result.HTTPStatus != 101 ||
		result.Stage != "echo" || result.Messages != 1 || result.Error == "" {
		t.Fatalf("premature WebSocket close = %+v", result)
	}
}

func sseTestEvents(count int) string {
	var body strings.Builder
	for index := range count {
		fmt.Fprintf(&body, "event: ping\ndata: {\"id\":%d,\"timestamp\":1}\n\n", index)
	}
	return body.String()
}
