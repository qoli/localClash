package corerun

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"localclash/internal/configrender"
)

func TestRuntimeHealthWaitsForLocalProxyDuringStartup(t *testing.T) {
	tests := []struct {
		name        string
		mode        string
		secret      string
		startup     bool
		proxyProbes int32
	}{
		{name: "startup", mode: "rule", startup: true, proxyProbes: 2},
		{name: "authenticated controller", mode: "rule", secret: "test-token", startup: true, proxyProbes: 2},
		{name: "steady state", mode: "rule"},
		{name: "global routing", mode: "global", startup: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const versionJSON = `{"version":"test"}`
			controller := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if tt.secret != "" && req.Header.Get("Authorization") != "Bearer "+tt.secret {
					http.Error(w, "Unauthorized", http.StatusUnauthorized)
					return
				}
				_, _ = io.WriteString(w, versionJSON)
			}))
			defer controller.Close()
			var proxyProbes atomic.Int32
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.URL.String() != controller.URL+"/version" || req.Header.Get("Authorization") != "" {
					t.Error("proxy request should target the controller version endpoint without credentials")
					http.Error(w, "unexpected request", http.StatusBadRequest)
					return
				}
				if proxyProbes.Add(1) == 1 {
					w.WriteHeader(http.StatusBadGateway)
					return
				}
				if tt.secret != "" {
					http.Error(w, "Unauthorized", http.StatusUnauthorized)
					return
				}
				_, _ = io.WriteString(w, versionJSON)
			}))
			defer proxy.Close()

			controllerAddress := strings.TrimPrefix(controller.URL, "http://")
			_, proxyPort, err := net.SplitHostPort(proxy.Listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			config := fmt.Sprintf("external-controller: %s\nmixed-port: %s\nmode: %s\nsecret: %q\nrules:\n", controllerAddress, proxyPort, tt.mode, tt.secret)
			for _, rule := range configrender.LocalBaselineRuleLines() {
				config += fmt.Sprintf("  - %q\n", rule)
			}
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := defaultProbeRuntimeHealth(context.Background(), path, os.Getpid(), 3*time.Second, tt.startup); err != nil {
				t.Fatal(err)
			}
			if got := proxyProbes.Load(); got != tt.proxyProbes {
				t.Fatalf("proxy probes = %d, want %d", got, tt.proxyProbes)
			}
		})
	}
}
