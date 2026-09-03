package subscriptions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRefresh522UsesCacheAndRefreshesHealthySource(t *testing.T) {
	var degraded atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if degraded.Load() && r.URL.Path == "/cached" {
			w.WriteHeader(522)
			fmt.Fprint(w, "<html>token=secret-token</html>")
			return
		}
		name := "old"
		if degraded.Load() {
			name = "new"
		}
		fmt.Fprintf(w, "proxies:\n  - name: %s\n    type: ss\n", name)
	}))
	defer server.Close()
	uri := server.URL + "/cached?token=secret-token"
	paths := writeRefreshConfig(t, []Source{{URI: uri}, {URI: server.URL + "/healthy"}})
	opts := RefreshOptions{ConfigPath: paths.config, RuntimeDir: paths.runtimeDir, MergedPath: paths.merged, Force: true}
	if _, err := Refresh(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	cache := artifactPath(paths.runtimeDir, mustSourceID(t, uri))
	before, err := os.ReadFile(cache)
	if err != nil {
		t.Fatal(err)
	}
	oldTime := time.Unix(1700000000, 0)
	if err := os.Chtimes(cache, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	degraded.Store(true)
	var events []StageEvent
	opts.OnStage = func(event StageEvent) { events = append(events, event) }
	result, err := Refresh(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Refreshed || len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "HTTP 522") || !strings.Contains(result.Warnings[0], "using cached") {
		t.Fatalf("expected explicit cache warning: %+v", result)
	}
	if result.Sources[0].Status != "cached" || result.Sources[1].Status != "ok" || result.Merged.ProxiesCount != 2 {
		t.Fatalf("expected cached and freshly fetched sources in merge: %+v", result)
	}
	if len(result.Artifacts) != 2 || result.Artifacts[0].Proxies[0]["name"] != "old" || result.Artifacts[1].Proxies[0]["name"] != "new" {
		t.Fatalf("incorrect in-memory artifacts: %+v", result.Artifacts)
	}
	after, err := os.ReadFile(cache)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(cache)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) || !info.ModTime().Equal(oldTime) {
		t.Fatal("cache fallback rewrote the old source artifact or refreshed its age")
	}
	event := findStageEvent(t, events, "refresh_source", "warning")
	if event.Fields["status"] != "cached" || event.Fields["status_code"] != 522 || event.Fields["warning"] != result.Warnings[0] {
		t.Fatalf("missing structured cache evidence: %+v", event)
	}
	assertNoTokenLeak(t, result)
	encoded, err := json.Marshal(events)
	if err != nil || strings.Contains(string(encoded), "secret-token") {
		t.Fatalf("stage events leaked credential or could not be encoded: %v", err)
	}
}

func TestRefreshCacheFallbackFailureBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		cache  string
		want   string
	}{
		{"missing", 522, "missing", "cache fallback unavailable"},
		{"corrupt", 522, "corrupt", "cache fallback unavailable"},
		{"no proxies", 522, "empty", "subscription has no proxies"},
		{"invalid proxy", 522, "invalid", "proxy without name"},
		{"changed URI", 522, "changed_uri", "source ID does not match"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				fmt.Fprint(w, "HTTP 522 not a subscription")
			}))
			defer server.Close()
			uri := server.URL + "/sub"
			paths := writeRefreshConfig(t, []Source{{URI: uri}})
			cache := artifactPath(paths.runtimeDir, mustSourceID(t, uri))
			doc := subscriptionDoc{Data: map[string]any{"proxies": []any{map[string]any{"name": "old", "type": "ss"}}}}
			switch tc.cache {
			case "empty":
				doc.Data = map[string]any{"proxies": []any{}}
			case "invalid":
				doc.Data = map[string]any{"proxies": []any{map[string]any{"type": "ss"}}}
			case "changed_uri":
				config, err := readConfig(paths.config)
				if err != nil {
					t.Fatal(err)
				}
				config.Sources[0].URI = server.URL + "/different"
				if err := writeConfig(paths.config, config); err != nil {
					t.Fatal(err)
				}
			}
			if tc.cache != "missing" {
				if err := writeSubscriptionArtifact(cache, doc); err != nil {
					t.Fatal(err)
				}
			}
			if tc.cache == "corrupt" {
				if err := os.WriteFile(cache, []byte("not a gob"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := writeSubscriptionArtifact(paths.merged, doc); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(paths.merged)
			if err != nil {
				t.Fatal(err)
			}
			_, err = Refresh(context.Background(), RefreshOptions{ConfigPath: paths.config, RuntimeDir: paths.runtimeDir, MergedPath: paths.merged})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
			if tc.status == 522 {
				var httpErr *subscriptionHTTPError
				if !errors.As(err, &httpErr) || httpErr.statusCode != 522 {
					t.Fatalf("original HTTP cause lost: %v", err)
				}
			}
			after, err := os.ReadFile(paths.merged)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("failed refresh changed merged artifact")
			}
		})
	}
}

func TestRefreshRemoteFailuresUseValidCache(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		want   string
	}{
		{"bad request", 400, "HTTP 400"},
		{"authentication", 401, "HTTP 401"},
		{"rate limit", 429, "HTTP 429"},
		{"server error", 500, "HTTP 500"},
		{"origin timeout", 522, "HTTP 522"},
		{"invalid response", 200, "neither Mihomo YAML"},
		{"connection failure", 0, "request failed"},
		{"truncated response", -1, "response could not be read"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.status == -1 {
					if r.Header.Get("Range") != "" {
						w.WriteHeader(http.StatusServiceUnavailable)
						return
					}
					w.Header().Set("Content-Length", "9999")
					fmt.Fprint(w, "truncated")
					return
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, "not a subscription")
			}))
			defer server.Close()
			uri := server.URL + "/sub?token=secret-token"
			paths := writeRefreshConfig(t, []Source{{URI: uri}})
			doc := subscriptionDoc{Data: map[string]any{"proxies": []any{map[string]any{"name": "cached", "type": "ss"}}}}
			if err := writeSubscriptionArtifact(artifactPath(paths.runtimeDir, mustSourceID(t, uri)), doc); err != nil {
				t.Fatal(err)
			}
			if tc.status == 0 {
				server.Close()
			}
			result, err := Refresh(context.Background(), RefreshOptions{ConfigPath: paths.config, RuntimeDir: paths.runtimeDir, MergedPath: paths.merged})
			if err != nil {
				t.Fatal(err)
			}
			if !result.Refreshed || len(result.Sources) != 1 || result.Sources[0].Status != "cached" || result.Merged.ProxiesCount != 1 || len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], tc.want) {
				t.Fatalf("expected successful cache recovery with %q warning: %+v", tc.want, result)
			}
			assertNoTokenLeak(t, result)
		})
	}
}

func TestRefreshCancellationDoesNotUseCache(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cancel()
		<-r.Context().Done()
	}))
	defer server.Close()
	uri := server.URL + "/sub"
	paths := writeRefreshConfig(t, []Source{{URI: uri}})
	doc := subscriptionDoc{Data: map[string]any{"proxies": []any{map[string]any{"name": "cached"}}}}
	if err := writeSubscriptionArtifact(artifactPath(paths.runtimeDir, mustSourceID(t, uri)), doc); err != nil {
		t.Fatal(err)
	}
	var events []StageEvent
	_, err := Refresh(ctx, RefreshOptions{ConfigPath: paths.config, RuntimeDir: paths.runtimeDir, MergedPath: paths.merged, OnStage: func(event StageEvent) { events = append(events, event) }})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	for _, event := range events {
		if event.Event == "warning" {
			t.Fatalf("cancellation was converted into cache warning: %+v", event)
		}
	}
}
