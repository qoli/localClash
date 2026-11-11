package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"localclash/internal/poolcontract"
	"localclash/internal/subscriptions"
)

func TestStandaloneRefreshAndQualifyPreparedRevision(t *testing.T) {
	const token = "subscription-test-secret"
	const password = "proxy-test-secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("token") != token {
			http.Error(w, "missing subscription token", http.StatusUnauthorized)
			return
		}
		fmt.Fprintf(w, "proxies:\n- {name: test-node, type: ss, server: example.invalid, port: 443, cipher: aes-128-gcm, password: %s}\n", password)
	}))
	defer server.Close()
	workspace := t.TempDir()
	call := func(command, input string, args ...string) []byte {
		t.Helper()
		var stdout, stderr bytes.Buffer
		argv := append([]string{command, "--workspace", workspace}, args...)
		err := run(t.Context(), argv, strings.NewReader(input), &stdout, &stderr)
		for _, secret := range []string{token, password} {
			if strings.Contains(stdout.String(), secret) || strings.Contains(stderr.String(), secret) {
				t.Fatalf("%s exposed credentials", command)
			}
		}
		if err != nil {
			t.Fatalf("%s: %v", command, err)
		}
		return stdout.Bytes()
	}
	call("set", fmt.Sprintf(`{"version":1,"uris":[%q]}`, server.URL+"?token="+token), "--input", "-")
	refreshed := call("refresh", "")
	if !bytes.Contains(refreshed, []byte(`"proxies_count":1`)) {
		t.Fatalf("expected a merged proxy, got %s", refreshed)
	}
	status := call("status", "")
	if !bytes.Contains(status, []byte(`"configured":true`)) {
		t.Fatalf("expected configured status, got %s", status)
	}

	runtime := filepath.Join(workspace, ".runtime", "mihomo")
	if _, err := os.Stat(runtime); !os.IsNotExist(err) {
		t.Fatalf("refresh should not start a runtime: %v", err)
	}
	merged, err := subscriptions.ReadMerged(filepath.Join(workspace, "subscription.gob"))
	if err != nil {
		t.Fatal(err)
	}
	input, err := poolcontract.FromSubscription(merged)
	if err != nil {
		t.Fatal(err)
	}
	for _, profile := range []string{poolcontract.AutoCapability, poolcontract.ChatGPTCapability} {
		pool, _ := poolcontract.Lookup(profile)
		path := filepath.Join(runtime, pool.Path(input.Revision))
		data, _, err := poolcontract.ProviderData(pool, input, []string{"test-node"})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var out struct {
		OK     bool          `json:"ok"`
		Result qualification `json:"result"`
	}
	if err := json.Unmarshal(call("qualify", "", "--mihomo", filepath.Join(workspace, "unused-mihomo")), &out); err != nil {
		t.Fatal(err)
	}
	if !out.OK || out.Result.Revision != input.Revision || out.Result.Proxies != 1 || len(out.Result.Pools) != 2 {
		t.Fatalf("unexpected qualification result %+v", out)
	}
	for _, pool := range out.Result.Pools {
		if !pool.Reused {
			t.Fatalf("expected the prepared provider to be reused, got %+v", pool)
		}
	}
}

func TestSetRejectsInvalidInputWithoutWriting(t *testing.T) {
	for _, input := range []string{
		`{"version":2,"uris":["https://example.invalid/"]}`,
		`{"version":1,"uri":["https://example.invalid/"]}`,
		`{"version":1,"uris":["https://example.invalid/"]} {}`,
	} {
		workspace := t.TempDir()
		var stdout, stderr bytes.Buffer
		err := run(t.Context(), []string{"set", "--workspace", workspace, "--input", "-"},
			strings.NewReader(input), &stdout, &stderr)
		if err == nil {
			t.Fatal("expected an input error")
		}
		if _, err := os.Stat(filepath.Join(workspace, "localclash-subscriptions.json")); !os.IsNotExist(err) {
			t.Fatalf("invalid input should not create configuration: %v", err)
		}
	}
}
