package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"localclash/internal/appinit"
	"localclash/internal/corerun"
)

func TestProductSubscriptionVerifyRuntime(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"proxies":{"Old":{"name":"Old"}}}`))
	}))
	defer server.Close()
	config := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(config, []byte("external-controller: "+strings.TrimPrefix(server.URL, "http://")+"\nproxies: [{name: New}]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	state := appinit.RuntimeState{Paths: appinit.RuntimePaths{GeneratedConfig: config}}
	output, err := captureStdoutAllowError(t, func() error {
		_, err := runProductCommand([]string{"subscription", "verify-runtime", "--json"}, state)
		return err
	})
	if err == nil || !strings.Contains(output, `"code": "subscription_runtime_verification_failed"`) || !strings.Contains(output, `"New"`) || !strings.Contains(output, `"Old"`) {
		t.Fatalf("failure lost verification details: %v: %s", err, output)
	}
	output, err = captureStdoutAllowError(t, func() error {
		_, err := runProductCommand([]string{"subscription", "verify-runtime", "--check-support", "--json"}, appinit.RuntimeState{})
		return err
	})
	if err != nil || !strings.Contains(output, `"supported": true`) {
		t.Fatalf("support check requires runtime or config: %v: %s", err, output)
	}
}

func TestProductRestartFailurePreservesResult(t *testing.T) {
	for _, strategy := range []string{"process_restart", "hot_reload"} {
		t.Run(strategy, func(t *testing.T) {
			dir := t.TempDir()
			core := filepath.Join(dir, "lc-mihomo-meta")
			config := filepath.Join(dir, "config.yaml")
			if err := os.WriteFile(core, []byte("#!/bin/sh\nif [ \"$1\" = -v ]; then echo test-core; exit 0; fi\necho invalid-test-config >&2\nexit 1\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(config, []byte("external-controller: 127.0.0.1:1\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			state := appinit.RuntimeState{Paths: appinit.RuntimePaths{CorePath: core, GeneratedConfig: config, MihomoRuntimeDir: filepath.Join(dir, "runtime")}}
			output, err := captureStdoutAllowError(t, func() error {
				_, err := runProductCommand([]string{"runtime", "restart", "--strategy", strategy, "--json"}, state)
				return err
			})
			if err == nil {
				t.Fatal("restart failure must return an error")
			}
			var result struct {
				OK      bool                  `json:"ok"`
				Code    string                `json:"code"`
				Message string                `json:"message"`
				Details corerun.RestartResult `json:"details"`
			}
			if err := json.Unmarshal([]byte(output), &result); err != nil {
				t.Fatalf("invalid failure envelope: %v: %s", err, output)
			}
			if result.OK || result.Code != "runtime_restart_failed" || result.Message == "" || result.Details.Restarted || result.Details.Start.Started {
				t.Fatalf("unexpected failure envelope: %s", output)
			}
			if result.Details.AppliedStrategy != strategy || result.Details.Error == "" || !strings.Contains(result.Message, result.Details.Error) {
				t.Fatalf("restart cause/strategy lost: %s", output)
			}
		})
	}
}
