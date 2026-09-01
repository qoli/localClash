package subscriptions

import (
	"context"
	"encoding/base64"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestStatusNoConfig(t *testing.T) {
	dir := t.TempDir()

	result, err := Status(StatusOptions{
		ConfigPath: filepath.Join(dir, "localclash-subscriptions.json"),
		MergedPath: filepath.Join(dir, "subscription.gob"),
		RuntimeDir: filepath.Join(dir, ".runtime", "subscriptions"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Configured {
		t.Fatal("configured = true, want false")
	}
	if !strings.Contains(result.Message, "ask the user") {
		t.Fatalf("message = %q, want bootstrap hint", result.Message)
	}
}

func TestStatusConfigExistsArtifactsMissingAndMergedCounts(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "localclash-subscriptions.json")
	merged := filepath.Join(dir, "subscription.gob")
	runtimeDir := filepath.Join(dir, ".runtime", "subscriptions")
	writeTestFile(t, config, `version: 1
sources:
  - id: primary
    url: https://example.com/sub?token=secret-token
`)
	writeTestFile(t, merged, `proxies:
  - name: SG 01
    type: ss
proxy-groups:
  - name: PROXY
    type: select
rules:
  - MATCH,PROXY
`)

	result, err := Status(StatusOptions{ConfigPath: config, MergedPath: merged, RuntimeDir: runtimeDir})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Configured || len(result.Sources) != 1 {
		t.Fatalf("status = %+v, want one configured source", result)
	}
	if result.Sources[0].Exists {
		t.Fatal("artifact exists = true, want false")
	}
	if result.Merged.ProxiesCount != 1 || result.Merged.ProxyGroupsCount != 1 || result.Merged.RulesCount != 1 {
		t.Fatalf("merged = %+v, want counts 1/1/1", result.Merged)
	}
	assertNoTokenLeak(t, result)
}

func TestConfigureWritesValidMultiSourcesAndMasksURLs(t *testing.T) {
	dir := t.TempDir()
	replace := true
	url1 := "https://example.com/sub?token=secret-token"
	url2 := "https://example.net/path/profile?token=backup-secret"
	config := filepath.Join(dir, "localclash-subscriptions.json")

	result, err := Configure(ConfigureOptions{
		ConfigPath: config,
		Replace:    &replace,
		URLs:       []string{url1, url2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Configured || len(result.Sources) != 2 {
		t.Fatalf("result = %+v, want two configured sources", result)
	}
	if result.Sources[0].ID != mustSourceID(t, url1) || result.Sources[1].ID != mustSourceID(t, url2) {
		t.Fatalf("source ids = %+v, want generated short hash ids", result.Sources)
	}
	if result.Sources[0].DisplayName != "01" || result.Sources[1].DisplayName != "02" {
		t.Fatalf("source display names = %+v, want 01/02", result.Sources)
	}
	data := readTestFile(t, filepath.Join(dir, "localclash-subscriptions.json"))
	if !strings.Contains(data, "secret-token") {
		t.Fatal("config should contain the real URL token on disk")
	}
	if !strings.Contains(data, `"display_name": "01"`) || !strings.Contains(data, `"display_name": "02"`) {
		t.Fatalf("config missing display names:\n%s", data)
	}
	assertNoTokenLeak(t, result)

	raw, err := Get(StatusOptions{ConfigPath: config})
	if err != nil {
		t.Fatal(err)
	}
	if raw.Count != 2 || len(raw.URLs) != 2 || raw.URLs[0] != url1 || raw.URLs[1] != url2 {
		t.Fatalf("get = %+v, want original URLs", raw)
	}
}

func TestConfigureRejectsInvalidInputs(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name string
		urls []string
	}{
		{name: "empty", urls: nil},
		{name: "bad scheme", urls: []string{"file:///tmp/sub.json"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Configure(ConfigureOptions{
				ConfigPath: filepath.Join(dir, tt.name+".gob"),
				URLs:       tt.urls,
			})
			if err == nil {
				t.Fatal("expected configure error")
			}
		})
	}
}

func TestConfigureAcceptsExplicitSourceDisplayNames(t *testing.T) {
	dir := t.TempDir()
	result, err := Configure(ConfigureOptions{
		ConfigPath: filepath.Join(dir, "localclash-subscriptions.json"),
		Sources: []Source{
			{DisplayName: "09", URI: "https://example.com/primary?token=secret-token"},
			{DisplayName: "10", URI: "https://example.net/backup?token=backup-secret"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Sources[0].DisplayName != "09" || result.Sources[1].DisplayName != "10" {
		t.Fatalf("source display names = %+v, want explicit values", result.Sources)
	}
	assertNoTokenLeak(t, result)
}

func TestConfigureRejectsInvalidSourceDisplayNames(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name        string
		displayName string
		want        string
	}{
		{name: "zero", displayName: "00", want: "two digits from 01 to 99"},
		{name: "too long", displayName: "100", want: "two digits from 01 to 99"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Configure(ConfigureOptions{
				ConfigPath: filepath.Join(dir, tt.name+".json"),
				Sources: []Source{{
					DisplayName: tt.displayName,
					URI:         "https://example.com/sub?token=secret-token",
				}},
			})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
			assertNoTokenLeak(t, err.Error())
		})
	}
}

func TestConfigureDeduplicatesURIInputs(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "localclash-subscriptions.json")
	uri := "vless://uuid@example.com:443?security=tls&type=tcp#VLESS"

	result, err := Configure(ConfigureOptions{
		ConfigPath: config,
		URIs: []string{
			"https://example.com/sub",
			"https://example.com/sub",
			uri,
			uri,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sources) != 2 {
		t.Fatalf("sources = %+v, want remote source plus one inline source", result.Sources)
	}
	raw, err := Get(StatusOptions{ConfigPath: config})
	if err != nil {
		t.Fatal(err)
	}
	if raw.Count != 2 || len(raw.URIs) != 2 || len(raw.URLs) != 1 {
		t.Fatalf("get = %+v, want two source URIs and one legacy URL", raw)
	}
	assertNoTokenLeak(t, result)
}

func TestRefreshFetchesArtifactsAndPrefixesMultiSourceNodes(t *testing.T) {
	userAgents := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userAgents <- r.UserAgent()
		switch r.URL.Path {
		case "/primary":
			_, _ = w.Write([]byte(`proxies:
  - name: Same
    type: ss
    server: primary.example
    password: secret
proxy-groups:
  - name: PROXY
    type: select
rules:
  - MATCH,PROXY
`))
		case "/backup":
			_, _ = w.Write([]byte(`proxies:
  - name: Same
    type: trojan
    server: backup.example
    password: secret
  - name: Unique
    type: ss
    server: unique.example
    password: secret
`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	primaryURL := server.URL + "/primary?token=primary-secret"
	backupURL := server.URL + "/backup?token=backup-secret"
	primaryID := mustSourceID(t, primaryURL)
	backupID := mustSourceID(t, backupURL)
	paths := writeRefreshConfig(t, []Source{
		{ID: "primary", URL: primaryURL},
		{ID: "backup", URL: backupURL},
	})

	result, err := Refresh(context.Background(), RefreshOptions{
		ConfigPath: paths.config,
		RuntimeDir: paths.runtimeDir,
		MergedPath: paths.merged,
		UserAgent:  "test-clash-ua",
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if gotUA := <-userAgents; gotUA != "test-clash-ua" {
			t.Fatalf("User-Agent = %q, want test-clash-ua", gotUA)
		}
	}
	if len(result.Sources) != 2 {
		t.Fatalf("sources = %+v, want two summaries", result.Sources)
	}
	assertFileExists(t, filepath.Join(paths.runtimeDir, primaryID+".gob"))
	assertFileExists(t, filepath.Join(paths.runtimeDir, backupID+".gob"))
	assertFileExists(t, paths.merged)
	if result.Merged.ProxiesCount != 3 || result.Merged.RenamedProxiesCount != 3 {
		t.Fatalf("merged = %+v, want 3 proxies and 3 renamed", result.Merged)
	}
	merged := readTestFile(t, paths.merged)
	for _, want := range []string{"[01] Same", "[02] Same", "[02] Unique"} {
		if !strings.Contains(merged, want) {
			t.Fatalf("merged subscription missing %q:\n%s", want, merged)
		}
	}
	if strings.Contains(merged, primaryID) || strings.Contains(merged, backupID) {
		t.Fatalf("merged subscription should not expose source ids:\n%s", merged)
	}
	assertNoTokenLeak(t, result)
}

func TestMergeSubscriptionsRewritesDialerProxyWithinItsSource(t *testing.T) {
	sources := []Source{
		{ID: "first", DisplayName: "01"},
		{ID: "second", DisplayName: "02"},
	}
	docs := map[string]subscriptionDoc{
		"first": mustParseSubscription(t, "first", `proxies:
  - name: chain
    type: ss
    dialer-proxy: relay
  - name: relay
    type: ss
`),
		"second": mustParseSubscription(t, "second", `proxies:
  - name: chain
    type: ss
    dialer-proxy: relay
  - name: relay
    type: ss
`),
	}

	merged, renamed, err := mergeSubscriptions(sources, docs)
	if err != nil {
		t.Fatal(err)
	}
	if renamed != 4 {
		t.Fatalf("renamed = %d, want 4", renamed)
	}
	proxies := mergedProxiesByName(t, merged)
	for chain, relay := range map[string]string{
		"[01] chain": "[01] relay",
		"[02] chain": "[02] relay",
	} {
		if got := stringValue(proxies[chain]["dialer-proxy"]); got != relay {
			t.Fatalf("proxy %q dialer-proxy = %q, want %q", chain, got, relay)
		}
	}
}

func TestMergeSubscriptionsPreservesExternalDialerProxyReference(t *testing.T) {
	sources := []Source{
		{ID: "first", DisplayName: "01"},
		{ID: "second", DisplayName: "02"},
	}
	docs := map[string]subscriptionDoc{
		"first": mustParseSubscription(t, "first", `proxies:
  - name: chain
    type: ss
    dialer-proxy: local-dialer-group
`),
		"second": mustParseSubscription(t, "second", `proxies:
  - name: other
    type: ss
`),
	}

	merged, _, err := mergeSubscriptions(sources, docs)
	if err != nil {
		t.Fatal(err)
	}
	proxies := mergedProxiesByName(t, merged)
	if got := stringValue(proxies["[01] chain"]["dialer-proxy"]); got != "local-dialer-group" {
		t.Fatalf("dialer-proxy = %q, want external policy-group name unchanged", got)
	}
}

func TestMergeSubscriptionsRejectsAmbiguousDialerProxyReference(t *testing.T) {
	sources := []Source{{ID: "first", DisplayName: "01"}}
	docs := map[string]subscriptionDoc{
		"first": mustParseSubscription(t, "first", `proxies:
  - name: chain
    type: ss
    dialer-proxy: relay
  - name: relay
    type: ss
  - name: relay
    type: trojan
`),
	}

	_, _, err := mergeSubscriptions(sources, docs)
	if err == nil || !strings.Contains(err.Error(), `dialer-proxy "relay" is ambiguous: 2 proxies share that name`) {
		t.Fatalf("error = %v, want explicit ambiguous dialer-proxy failure", err)
	}
}

func TestMergeSubscriptionsRejectsInvalidDialerProxyReference(t *testing.T) {
	sources := []Source{{ID: "first", DisplayName: "01"}}
	docs := map[string]subscriptionDoc{
		"first": mustParseSubscription(t, "first", `proxies:
  - name: chain
    type: ss
    dialer-proxy: ""
`),
	}

	_, _, err := mergeSubscriptions(sources, docs)
	if err == nil || !strings.Contains(err.Error(), `proxy "chain" has invalid dialer-proxy`) {
		t.Fatalf("error = %v, want explicit invalid dialer-proxy failure", err)
	}
}

func TestRefreshRemoteProxyURILines(t *testing.T) {
	const body = `vless://uuid@example.com:443?security=tls&type=tcp#VLESS
vmess://eyJ2IjoiMiIsInBzIjoiVk1lc3MiLCJhZGQiOiJ2bWVzcy5leGFtcGxlIiwicG9ydCI6IjQ0MyIsImlkIjoiYjgzMTM4MWQtNjMyNC00ZDUzLWFkNGYtOGNkYTQ4YjMwODExIiwiYWlkIjoiMCIsInNjeSI6ImF1dG8iLCJuZXQiOiJ3cyIsInR5cGUiOiJub25lIiwiaG9zdCI6ImNkbi5leGFtcGxlIiwicGF0aCI6Ii9lZGdlIiwidGxzIjoidGxzIn0=
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	paths := writeRefreshConfig(t, []Source{{URI: server.URL + "/sub?token=secret-token"}})

	result, err := Refresh(context.Background(), RefreshOptions{
		ConfigPath: paths.config,
		RuntimeDir: paths.runtimeDir,
		MergedPath: paths.merged,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Merged.ProxiesCount != 2 {
		t.Fatalf("merged = %+v, want two proxies from URI lines", result.Merged)
	}
	if result.Sources[0].Format != subscriptionFormatProxyURILines {
		t.Fatalf("source format = %q, want proxy URI lines", result.Sources[0].Format)
	}
	merged := readTestFile(t, paths.merged)
	for _, want := range []string{"name: VLESS", "name: VMess"} {
		if !strings.Contains(merged, want) {
			t.Fatalf("merged subscription missing %q:\n%s", want, merged)
		}
	}
	assertNoTokenLeak(t, result)
}

func TestRefreshRemoteProxyURILinesIgnoresNonURILines(t *testing.T) {
	const body = `REMARKS=oixCloud
STATUS=traffic: 2.85 TiB/3.01 TiB
anytls://pass@example.com:443?sni=edge.example.com&insecure=1#AnyTLS
not a proxy line
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	paths := writeRefreshConfig(t, []Source{{URI: server.URL + "/sub?token=secret-token"}})

	result, err := Refresh(context.Background(), RefreshOptions{
		ConfigPath: paths.config,
		RuntimeDir: paths.runtimeDir,
		MergedPath: paths.merged,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Merged.ProxiesCount != 1 {
		t.Fatalf("merged = %+v, want one proxy from URI lines", result.Merged)
	}
	if result.Sources[0].Format != subscriptionFormatProxyURILines {
		t.Fatalf("source format = %q, want proxy URI lines", result.Sources[0].Format)
	}
	merged := readTestFile(t, paths.merged)
	for _, want := range []string{"name: AnyTLS", "type: anytls"} {
		if !strings.Contains(merged, want) {
			t.Fatalf("merged subscription missing %q:\n%s", want, merged)
		}
	}
	assertNoTokenLeak(t, result)
}

func TestRefreshRemoteBase64ProxyURILines(t *testing.T) {
	const decodedBody = `REMARKS=oixCloud
STATUS=traffic: 2.85 TiB/3.01 TiB
anytls://pass@example.com:443?sni=edge.example.com&insecure=1#AnyTLS
`
	encodedBody := base64.StdEncoding.EncodeToString([]byte(decodedBody))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(encodedBody))
	}))
	defer server.Close()
	paths := writeRefreshConfig(t, []Source{{URI: server.URL + "/sub?token=secret-token"}})

	result, err := Refresh(context.Background(), RefreshOptions{
		ConfigPath: paths.config,
		RuntimeDir: paths.runtimeDir,
		MergedPath: paths.merged,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Merged.ProxiesCount != 1 {
		t.Fatalf("merged = %+v, want one proxy from base64 URI lines", result.Merged)
	}
	if result.Sources[0].Format != subscriptionFormatProxyURILines {
		t.Fatalf("source format = %q, want proxy URI lines", result.Sources[0].Format)
	}
	merged := readTestFile(t, paths.merged)
	for _, want := range []string{"name: AnyTLS", "type: anytls"} {
		if !strings.Contains(merged, want) {
			t.Fatalf("merged subscription missing %q:\n%s", want, merged)
		}
	}
	assertNoTokenLeak(t, result)
}

func TestRefreshInlineProxyURILinesDeduplicatesByURIString(t *testing.T) {
	vless := "vless://uuid@example.com:443?security=tls&type=tcp#VLESS"
	hy2 := "hysteria2://pass@example.com:8443?insecure=1#HY2"
	paths := writeRefreshConfigFromURIs(t, []string{vless, vless, hy2})

	result, err := Refresh(context.Background(), RefreshOptions{
		ConfigPath: paths.config,
		RuntimeDir: paths.runtimeDir,
		MergedPath: paths.merged,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sources) != 1 || result.Sources[0].Type != sourceTypeInlineProxyURIs {
		t.Fatalf("sources = %+v, want one inline source", result.Sources)
	}
	if result.Merged.ProxiesCount != 2 || result.Merged.RenamedProxiesCount != 0 {
		t.Fatalf("merged = %+v, want two deduplicated inline proxies without source prefix", result.Merged)
	}
	merged := readTestFile(t, paths.merged)
	for _, want := range []string{"name: VLESS", "name: HY2"} {
		if !strings.Contains(merged, want) {
			t.Fatalf("merged subscription missing %q:\n%s", want, merged)
		}
	}
	assertNoTokenLeak(t, result)
}

func TestRefreshFetchesSelectedSourcesInParallel(t *testing.T) {
	started := make(chan string, 2)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- r.URL.Path
		<-release
		_, _ = w.Write([]byte(`proxies:
  - name: ` + strings.TrimPrefix(r.URL.Path, "/") + `
    type: ss
    server: example.com
    password: secret
`))
	}))
	defer server.Close()
	paths := writeRefreshConfig(t, []Source{
		{URL: server.URL + "/first?token=primary-secret"},
		{URL: server.URL + "/second?token=backup-secret"},
	})

	errs := make(chan error, 1)
	go func() {
		_, err := Refresh(context.Background(), RefreshOptions{
			ConfigPath: paths.config,
			RuntimeDir: paths.runtimeDir,
			MergedPath: paths.merged,
		})
		errs <- err
	}()

	got := map[string]bool{}
	for len(got) < 2 {
		select {
		case path := <-started:
			got[path] = true
		case <-time.After(2 * time.Second):
			close(release)
			t.Fatalf("timed out waiting for both fetches to start, got %v", got)
		}
	}
	close(release)
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	if !got["/first"] || !got["/second"] {
		t.Fatalf("started paths = %v, want both sources", got)
	}
}

func TestRefreshReusesFetchedDocsAndWritesRawArtifacts(t *testing.T) {
	const body = `# raw marker should be preserved
proxies:
  - name: HK 01
    type: ss
    server: hk.example
    password: secret
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	rawURL := server.URL + "/sub?token=secret-token"
	id := mustSourceID(t, rawURL)
	paths := writeRefreshConfig(t, []Source{{URL: rawURL}})
	var events []StageEvent

	result, err := Refresh(context.Background(), RefreshOptions{
		ConfigPath: paths.config,
		RuntimeDir: paths.runtimeDir,
		MergedPath: paths.merged,
		OnStage: func(event StageEvent) {
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	artifact := readTestFile(t, filepath.Join(paths.runtimeDir, id+".gob"))
	if !strings.Contains(artifact, "# raw marker should be preserved") {
		t.Fatalf("artifact did not preserve raw subscription body:\n%s", artifact)
	}
	if len(result.Artifacts) != 1 || result.Artifacts[0].SourceID != id || result.Artifacts[0].DisplayName != "01" || len(result.Artifacts[0].Proxies) != 1 {
		t.Fatalf("artifacts = %+v, want one in-memory artifact", result.Artifacts)
	}
	event := findStageEvent(t, events, "read_artifacts", "done")
	if got := event.Fields["disk_reads"]; got != 0 {
		t.Fatalf("read_artifacts disk_reads = %v, want 0", got)
	}
	if got := event.Fields["memory_docs"]; got != 1 {
		t.Fatalf("read_artifacts memory_docs = %v, want 1", got)
	}
	assertNoTokenLeak(t, result)
}

func TestRefreshHTTPErrorLogsDisplayIdentityAndSafeResponseDetails(t *testing.T) {
	const secret = "provider-secret-token"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprintf(w, `{"message":"subscription expired","token":"%s","request":"%s"}`, secret, r.URL.String())
	}))
	defer server.Close()

	dir := t.TempDir()
	config := filepath.Join(dir, "localclash-subscriptions.json")
	rawURL := server.URL + "/sub?token=" + secret
	writeTestFile(t, config, fmt.Sprintf(`version: 1
sources:
  - id: S-internal-only
    display_name: "07"
    type: remote_subscription
    uri: %s
`, rawURL))
	var events []StageEvent
	_, err := Refresh(context.Background(), RefreshOptions{
		ConfigPath: config,
		RuntimeDir: filepath.Join(dir, ".runtime", "subscriptions"),
		MergedPath: filepath.Join(dir, "subscription.gob"),
		OnStage: func(event StageEvent) {
			events = append(events, event)
		},
	})
	if err == nil {
		t.Fatal("refresh error = nil, want HTTP 400 failure")
	}
	errorText := err.Error()
	for _, want := range []string{"subscription 07", server.URL + "/sub?...", "HTTP 400 Bad Request", "subscription expired"} {
		if !strings.Contains(errorText, want) {
			t.Fatalf("error = %q, want %q", errorText, want)
		}
	}
	for _, banned := range []string{secret, "S-internal-only"} {
		if strings.Contains(errorText, banned) {
			t.Fatalf("error leaked %q: %s", banned, errorText)
		}
	}

	event := findStageEvent(t, events, "fetch_subscription_response", "error")
	if got := event.Fields["display_id"]; got != "07" {
		t.Fatalf("display_id = %v, want 07", got)
	}
	if got := event.Fields["status_code"]; got != http.StatusBadRequest {
		t.Fatalf("status_code = %v, want 400", got)
	}
	preview, _ := event.Fields["response_body_preview"].(string)
	if !strings.Contains(preview, "subscription expired") || !strings.Contains(preview, "<redacted>") {
		t.Fatalf("response_body_preview = %q, want provider message with redaction", preview)
	}
	data, marshalErr := json.Marshal(events)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	for _, banned := range []string{secret, "S-internal-only"} {
		if strings.Contains(string(data), banned) {
			t.Fatalf("stage log leaked %q: %s", banned, data)
		}
	}

	selection := findStageEvent(t, events, "select_sources", "done")
	selected, ok := selection.Fields["selected_sources"].([]map[string]any)
	if !ok || len(selected) != 1 || selected[0]["display_id"] != "07" || selected[0]["uri"] != server.URL+"/sub?..." {
		t.Fatalf("selected_sources = %#v, want display ID to masked URI mapping", selection.Fields["selected_sources"])
	}
}

func TestRefreshRecoversTruncatedSubscriptionWithVerifiedRanges(t *testing.T) {
	body := largeSubscriptionBody(1800)
	server := newTruncatedRangeServer(t, body, "success")
	defer server.Close()
	paths := writeRefreshConfig(t, []Source{{URI: server.URL + "/sub?token=secret-token"}})
	var events []StageEvent

	result, err := Refresh(context.Background(), RefreshOptions{
		ConfigPath: paths.config,
		RuntimeDir: paths.runtimeDir,
		MergedPath: paths.merged,
		OnStage: func(event StageEvent) {
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Merged.ProxiesCount != 1800 {
		t.Fatalf("merged proxies = %d, want 1800", result.Merged.ProxiesCount)
	}
	if artifact := readTestFile(t, filepath.Join(paths.runtimeDir, result.Sources[0].ID+".gob")); artifact != string(body) {
		t.Fatalf("recovered artifact differs from complete response: got %d bytes, want %d", len(artifact), len(body))
	}
	recovery := findStageEvent(t, events, "range_chunk_recovery", "done")
	if chunks, ok := recovery.Fields["range_chunks"].(int); !ok || chunks < 2 {
		t.Fatalf("range_chunks = %#v, want at least 2", recovery.Fields["range_chunks"])
	}
	if got := recovery.Fields["range_total_bytes"]; got != len(body) {
		t.Fatalf("range_total_bytes = %#v, want %d", got, len(body))
	}
	read := findStageEvent(t, events, "read_subscription_response", "done")
	if got := read.Fields["recovered"]; got != true {
		t.Fatalf("read recovered = %#v, want true", got)
	}
	assertNoTokenLeak(t, events)
}

func TestRefreshRangeRecoveryAcceptsClampedSmallPayload(t *testing.T) {
	body := largeSubscriptionBody(80)
	server := newTruncatedRangeServer(t, body, "success")
	defer server.Close()
	paths := writeRefreshConfig(t, []Source{{URI: server.URL + "/sub?token=secret-token"}})

	result, err := Refresh(context.Background(), RefreshOptions{
		ConfigPath: paths.config,
		RuntimeDir: paths.runtimeDir,
		MergedPath: paths.merged,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Merged.ProxiesCount != 80 {
		t.Fatalf("merged proxies = %d, want 80", result.Merged.ProxiesCount)
	}
}

func TestRefreshUsesHTTP1ForInitialSubscriptionRequest(t *testing.T) {
	const body = `proxies:
  - name: HK 01
    type: ss
    server: hk.example
    password: secret
`
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sub" {
			if r.Proto != "HTTP/1.1" {
				t.Errorf("subscription request protocol = %s, want HTTP/1.1", r.Proto)
			}
			_, _ = w.Write([]byte(body))
			return
		}
		_, _ = w.Write([]byte("warm"))
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()

	originalDefaultTransport := http.DefaultTransport
	base := server.Client().Transport.(*http.Transport).Clone()
	base.ForceAttemptHTTP2 = true
	http.DefaultTransport = base
	defer func() {
		http.DefaultTransport = originalDefaultTransport
	}()

	warmResponse, err := (&http.Client{Transport: base}).Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if warmResponse.ProtoMajor != 2 {
		t.Fatalf("warm response protocol = %s, want HTTP/2", warmResponse.Proto)
	}
	_ = warmResponse.Body.Close()

	paths := writeRefreshConfig(t, []Source{{URI: server.URL + "/sub?token=secret-token"}})
	result, err := Refresh(context.Background(), RefreshOptions{
		ConfigPath: paths.config,
		RuntimeDir: paths.runtimeDir,
		MergedPath: paths.merged,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Merged.ProxiesCount != 1 {
		t.Fatalf("merged proxies = %d, want 1", result.Merged.ProxiesCount)
	}
}

func TestSubscriptionHTTPClientDoesNotInheritHTTP2ALPN(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sub" && r.Header.Get("Range") != "bytes=0-3" {
			t.Errorf("Range = %q, want bytes=0-3", r.Header.Get("Range"))
		}
		w.Header().Set("Content-Range", "bytes 0-3/4")
		_, _ = w.Write([]byte("test"))
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()

	originalDefaultTransport := http.DefaultTransport
	base := server.Client().Transport.(*http.Transport).Clone()
	base.ForceAttemptHTTP2 = true
	http.DefaultTransport = base
	defer func() {
		http.DefaultTransport = originalDefaultTransport
	}()

	warmResponse, err := (&http.Client{Transport: base}).Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if warmResponse.ProtoMajor != 2 {
		t.Fatalf("warm response protocol = %s, want HTTP/2", warmResponse.Proto)
	}
	_ = warmResponse.Body.Close()

	req, err := http.NewRequest(http.MethodGet, server.URL+"/sub?token=secret-token", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Range", "bytes=0-3")
	response, err := subscriptionHTTPClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.Proto != "HTTP/1.1" {
		t.Fatalf("range response protocol = %s, want HTTP/1.1", response.Proto)
	}
}

func TestSafeTransportErrorRedactsSubscriptionURI(t *testing.T) {
	source := Source{DisplayName: "01", URI: "https://example.com/sub?token=secret-token&url=https%3A%2F%2Forigin.example%2Fplaylist%3Fkey%3Dnested-secret"}
	raw := fmt.Errorf("Get %q: transport failed", source.URI)
	got := safeTransportError(raw, source)
	for _, banned := range []string{"secret-token", "nested-secret", "token=", "key="} {
		if strings.Contains(got, banned) {
			t.Fatalf("safe transport error leaked %q: %s", banned, got)
		}
	}
	if !strings.Contains(got, "https://example.com/sub?...") || !strings.Contains(got, "transport failed") {
		t.Fatalf("safe transport error = %q, want masked URI and cause", got)
	}
}

func TestRefreshRangeRecoveryRejectsUnverifiableResponses(t *testing.T) {
	body := largeSubscriptionBody(1800)
	tests := []struct {
		name string
		mode string
		want string
	}{
		{name: "missing content range", mode: "missing_content_range", want: "Content-Range"},
		{name: "changing total", mode: "changing_total", want: "total changed"},
		{name: "overlap mismatch", mode: "overlap_mismatch", want: "overlap mismatch"},
		{name: "short chunk", mode: "short_chunk", want: "could not be read"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newTruncatedRangeServer(t, body, tt.mode)
			defer server.Close()
			paths := writeRefreshConfig(t, []Source{{URI: server.URL + "/sub?token=secret-token"}})
			var events []StageEvent

			_, err := Refresh(context.Background(), RefreshOptions{
				ConfigPath: paths.config,
				RuntimeDir: paths.runtimeDir,
				MergedPath: paths.merged,
				OnStage: func(event StageEvent) {
					events = append(events, event)
				},
			})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
			findStageEvent(t, events, "range_chunk_recovery", "error")
			if _, statErr := os.Stat(paths.merged); !os.IsNotExist(statErr) {
				t.Fatalf("merged artifact exists after rejected recovery: %v", statErr)
			}
			assertNoTokenLeak(t, events)
			assertNoTokenLeak(t, err.Error())
		})
	}
}

func TestRefreshSingleSourcePreservesNodeNames(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`proxies:
  - name: HK 01
    type: ss
    server: hk.example
    password: secret
  - name: SG 01
    type: ss
    server: sg.example
    password: secret
`))
	}))
	defer server.Close()
	paths := writeRefreshConfig(t, []Source{{ID: "primary", URL: server.URL + "/sub?token=secret-token"}})

	result, err := Refresh(context.Background(), RefreshOptions{
		ConfigPath: paths.config,
		RuntimeDir: paths.runtimeDir,
		MergedPath: paths.merged,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Merged.ProxiesCount != 2 || result.Merged.RenamedProxiesCount != 0 {
		t.Fatalf("merged = %+v, want 2 proxies and no renamed nodes", result.Merged)
	}
	merged := readTestFile(t, paths.merged)
	for _, want := range []string{"name: HK 01", "name: SG 01"} {
		if !strings.Contains(merged, want) {
			t.Fatalf("merged subscription missing %q:\n%s", want, merged)
		}
	}
	if strings.Contains(merged, "[primary]") {
		t.Fatalf("single-source merged subscription should not add source prefix:\n%s", merged)
	}
	assertNoTokenLeak(t, result)
}

func TestRefreshUsesSourceIDDisplayNameFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/primary":
			_, _ = w.Write([]byte(`proxies:
  - name: Same
    type: ss
    server: primary.example
    password: secret
`))
		case "/backup":
			_, _ = w.Write([]byte(`proxies:
  - name: Same
    type: ss
    server: backup.example
    password: secret
`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	config := filepath.Join(dir, "localclash-subscriptions.json")
	writeTestFile(t, config, fmt.Sprintf(`{
  "version": 1,
  "sources": [
    {
      "id": "S-12345678",
      "type": "remote_subscription",
      "uri": "%s/primary?token=secret-token"
    },
    {
      "id": "S-abcd1234",
      "type": "remote_subscription",
      "uri": "%s/backup?token=backup-secret"
    }
  ]
}`, server.URL, server.URL))

	result, err := Refresh(context.Background(), RefreshOptions{
		ConfigPath: config,
		RuntimeDir: filepath.Join(dir, ".runtime", "subscriptions"),
		MergedPath: filepath.Join(dir, "subscription.gob"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Sources[0].DisplayName != "12" || result.Sources[1].DisplayName != "ab" {
		t.Fatalf("source display fallback = %+v, want 12/ab", result.Sources)
	}
	merged := readTestFile(t, filepath.Join(dir, "subscription.gob"))
	for _, want := range []string{"[12] Same", "[ab] Same"} {
		if !strings.Contains(merged, want) {
			t.Fatalf("merged subscription missing %q:\n%s", want, merged)
		}
	}
	if strings.Contains(merged, "[S-") {
		t.Fatalf("merged subscription should not expose source id prefix:\n%s", merged)
	}
	status, err := Status(StatusOptions{
		ConfigPath: config,
		RuntimeDir: filepath.Join(dir, ".runtime", "subscriptions"),
		MergedPath: filepath.Join(dir, "subscription.gob"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if status.Sources[0].DisplayName != "12" || status.Sources[1].DisplayName != "ab" {
		t.Fatalf("status display fallback = %+v, want 12/ab", status.Sources)
	}
	assertNoTokenLeak(t, result)
}

func TestRefreshUnknownIDsReturnError(t *testing.T) {
	paths := writeRefreshConfig(t, []Source{{ID: "primary", URL: "https://example.com/sub?token=secret-token"}})

	_, err := Refresh(context.Background(), RefreshOptions{
		ConfigPath: paths.config,
		RuntimeDir: paths.runtimeDir,
		MergedPath: paths.merged,
		IDs:        []string{"missing"},
	})
	if err == nil || !strings.Contains(err.Error(), "unknown subscription source id") {
		t.Fatalf("error = %v, want unknown id", err)
	}
	assertNoTokenLeak(t, err.Error())
}

func TestRefreshRejectsInvalidResponses(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "empty", body: ""},
		{name: "invalid yaml", body: ":\n"},
		{name: "no proxies", body: "rules: []\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()
			paths := writeRefreshConfig(t, []Source{{ID: "primary", URL: server.URL + "/sub?token=secret-token"}})

			_, err := Refresh(context.Background(), RefreshOptions{
				ConfigPath: paths.config,
				RuntimeDir: paths.runtimeDir,
				MergedPath: paths.merged,
			})
			if err == nil {
				t.Fatal("expected refresh error")
			}
			assertNoTokenLeak(t, err.Error())
		})
	}
}

func TestRefreshRejectsRemoteTextWithoutProxyURILines(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hello world\nREMARKS=oixCloud\n"))
	}))
	defer server.Close()
	paths := writeRefreshConfig(t, []Source{{URI: server.URL + "/sub?token=secret-token"}})

	_, err := Refresh(context.Background(), RefreshOptions{
		ConfigPath: paths.config,
		RuntimeDir: paths.runtimeDir,
		MergedPath: paths.merged,
	})
	if err == nil || !strings.Contains(err.Error(), "has no MVP proxy URI lines") {
		t.Fatalf("error = %v, want explicit input format rejection", err)
	}
	assertNoTokenLeak(t, err.Error())
}

type refreshPaths struct {
	dir        string
	config     string
	runtimeDir string
	merged     string
}

func mustParseSubscription(t *testing.T, sourceID, content string) subscriptionDoc {
	t.Helper()
	doc, err := parseSubscription(sourceID, []byte(content))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func mergedProxiesByName(t *testing.T, merged map[string]any) map[string]map[string]any {
	t.Helper()
	result := map[string]map[string]any{}
	for _, rawProxy := range anySlice(merged["proxies"]) {
		proxy, ok := rawProxy.(map[string]any)
		if !ok {
			t.Fatalf("merged proxy = %#v, want map", rawProxy)
		}
		result[stringValue(proxy["name"])] = proxy
	}
	return result
}

func writeRefreshConfig(t *testing.T, sources []Source) refreshPaths {
	t.Helper()
	uris := make([]string, 0, len(sources))
	for _, source := range sources {
		uris = append(uris, sourcePrimaryURI(source))
	}
	return writeRefreshConfigFromURIs(t, uris)
}

func writeRefreshConfigFromURIs(t *testing.T, uris []string) refreshPaths {
	t.Helper()
	dir := t.TempDir()
	paths := refreshPaths{
		dir:        dir,
		config:     filepath.Join(dir, "localclash-subscriptions.json"),
		runtimeDir: filepath.Join(dir, ".runtime", "subscriptions"),
		merged:     filepath.Join(dir, "subscription.gob"),
	}
	_, err := Configure(ConfigureOptions{ConfigPath: paths.config, URIs: uris})
	if err != nil {
		t.Fatal(err)
	}
	return paths
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	var data []byte
	var err error
	switch filepath.Ext(path) {
	case ".json":
		var doc any
		if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
			t.Fatal(err)
		}
		data, err = json.MarshalIndent(doc, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
	case ".gob":
		gob.Register(map[string]any{})
		gob.Register([]any{})
		var doc map[string]any
		if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
			t.Fatal(err)
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		encodeErr := gob.NewEncoder(file).Encode(subscriptionArtifact{Version: 1, Data: doc, Raw: []byte(content)})
		closeErr := file.Close()
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
		return
	default:
		data = []byte(content)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	if filepath.Ext(path) == ".gob" {
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		var artifact subscriptionArtifact
		if err := gob.NewDecoder(file).Decode(&artifact); err != nil {
			t.Fatal(err)
		}
		if len(artifact.Raw) > 0 {
			return string(artifact.Raw)
		}
		data, err := yaml.Marshal(artifact.Data)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func findStageEvent(t *testing.T, events []StageEvent, stage, event string) StageEvent {
	t.Helper()
	for _, candidate := range events {
		if candidate.Stage == stage && candidate.Event == event {
			return candidate
		}
	}
	t.Fatalf("missing stage event %s/%s in %+v", stage, event, events)
	return StageEvent{}
}

func largeSubscriptionBody(proxyCount int) []byte {
	var body strings.Builder
	body.WriteString("proxies:\n")
	for i := 0; i < proxyCount; i++ {
		fmt.Fprintf(&body, "  - name: Node %04d\n    type: ss\n    server: node-%04d.example\n    password: test-value\n", i, i)
	}
	return []byte(body.String())
}

func newTruncatedRangeServer(t *testing.T, body []byte, mode string) *httptest.Server {
	t.Helper()
	rangeCalls := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rangeHeader := r.Header.Get("Range")
		if rangeHeader == "" {
			w.Header().Set("Content-Type", "text/yaml")
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			w.WriteHeader(http.StatusOK)
			limit := 4096
			if limit > len(body) {
				limit = len(body) / 2
			}
			_, _ = w.Write(body[:limit])
			return
		}

		rangeCalls++
		start, end, err := parseTestRange(rangeHeader)
		if err != nil {
			t.Errorf("Range = %q: %v", rangeHeader, err)
			http.Error(w, "bad range", http.StatusBadRequest)
			return
		}
		if start >= len(body) {
			http.Error(w, "range outside body", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		if end >= len(body) {
			end = len(body) - 1
		}
		responseBody := append([]byte(nil), body[start:end+1]...)
		headerTotal := len(body)
		switch mode {
		case "missing_content_range":
		case "changing_total":
			if rangeCalls >= 2 {
				headerTotal++
			}
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, headerTotal))
		case "overlap_mismatch":
			if rangeCalls == 2 {
				responseBody[0] ^= 0xff
			}
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, headerTotal))
		case "short_chunk":
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, headerTotal))
			if len(responseBody) > 0 {
				responseBody = responseBody[:len(responseBody)-1]
			}
		default:
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, headerTotal))
		}
		w.Header().Set("Content-Type", "text/yaml")
		w.Header().Set("Content-Length", strconv.Itoa(end-start+1))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(responseBody)
	}))
}

func parseTestRange(value string) (int, int, error) {
	value = strings.TrimPrefix(value, "bytes=")
	parts := strings.Split(value, "-")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid byte range")
	}
	start, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, err
	}
	end, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, err
	}
	return start, end, nil
}

func mustSourceID(t *testing.T, rawURL string) string {
	t.Helper()
	canonicalURL, err := canonicalSubscriptionURL(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	return sourceIDFromCanonicalURL(canonicalURL, map[string]bool{})
}

func assertNoTokenLeak(t *testing.T, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, banned := range []string{"secret-token", "primary-secret", "backup-secret", "token=", "password: secret"} {
		if strings.Contains(text, banned) {
			t.Fatalf("value leaked %q in %s", banned, text)
		}
	}
}
