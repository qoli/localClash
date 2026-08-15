package chatgptavailable

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/andybalholm/brotli"
)

func TestRequestStatsigRequiresBrotliAndReadsCountryWithoutMaterializingResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Accept-Encoding") != "br" || r.Header.Get("Statsig-Api-Key") != "test-key" || r.URL.Query().Get("k") != "test-key" {
			t.Errorf("unexpected Statsig request: method=%s encoding=%q key=%q query=%q", r.Method, r.Header.Get("Accept-Encoding"), r.Header.Get("Statsig-Api-Key"), r.URL.RawQuery)
		}
		w.Header().Set("Content-Encoding", "br")
		writer := brotli.NewWriter(w)
		_, _ = writer.Write([]byte(`{"feature_gates":{"large":{"nested":[1,2,3]}},"derived_fields":{"country":"hk"},"sdk_flags":{}}`))
		_ = writer.Close()
	}))
	defer server.Close()

	result := requestStatsig(context.Background(), server.Client(), server.URL, "test-key", time.Second)
	if result.err != nil || result.decision != statsigReachable || result.country != "HK" || result.contentEncoding != "br" {
		t.Fatalf("result = %+v, want reachable HK Brotli response", result)
	}
	if result.compressedBytes <= 0 || result.decompressedBytes <= result.compressedBytes {
		t.Fatalf("byte accounting = compressed %d decompressed %d", result.compressedBytes, result.decompressedBytes)
	}
}

func TestRequestStatsigRejectsUncompressedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"derived_fields":{"country":"US"}}`))
	}))
	defer server.Close()

	result := requestStatsig(context.Background(), server.Client(), server.URL, "test-key", time.Second)
	if result.err == nil || result.decision != statsigUnexpectedResponse || !strings.Contains(result.err.Error(), "Brotli") {
		t.Fatalf("result = %+v, want explicit uncompressed-response rejection", result)
	}
}

func TestReadStatsigCountryRejectsMissingOrTrailingData(t *testing.T) {
	for _, input := range []string{`{"feature_gates":{}}`, `{"derived_fields":{"country":"US"}} true`} {
		if country, err := readStatsigCountry(bytes.NewBufferString(input)); err == nil || country != "" {
			t.Fatalf("input %q produced country=%q err=%v, want explicit failure", input, country, err)
		}
	}
}

func TestWriteProbeConfigUsesDedicatedProxyListeners(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	candidates := []Candidate{
		{Name: "US 01", Proxy: map[string]any{"name": "US 01", "type": "ss", "server": "127.0.0.1", "port": 10001, "cipher": "aes-128-gcm", "password": "secret"}},
		{Name: "JP 01", Proxy: map[string]any{"name": "JP 01", "type": "trojan", "server": "127.0.0.1", "port": 10002, "password": "secret"}},
	}
	if err := writeProbeConfig(path, candidates, []int{19001, 19002}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{"listeners:", "type: mixed", "proxy: US 01", "proxy: JP 01", "listen: 127.0.0.1"} {
		if !strings.Contains(text, want) {
			t.Fatalf("probe config missing %q:\n%s", want, text)
		}
	}
}

func TestNewMihomoProberDefaultsScaleLargeSubscriptions(t *testing.T) {
	corePath := filepath.Join(t.TempDir(), "mihomo")
	if err := os.WriteFile(corePath, []byte("test"), 0o700); err != nil {
		t.Fatal(err)
	}
	prober, err := NewMihomoProber(MihomoOptions{
		CorePath:      corePath,
		RuntimeParent: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if prober.options.Concurrency != 16 || prober.options.RequestTimeout != 5*time.Second || prober.options.Attempts != 3 || prober.options.Endpoint != statsigInitializeURL || prober.options.ClientKey == "" {
		t.Fatalf("probe defaults = %+v, want concurrency 16, timeout 5s, attempts 3, and Statsig defaults", prober.options)
	}
}

func TestProbeCandidateRetriesOnlyFailureAndAccumulatesTransferBytes(t *testing.T) {
	calls := 0
	prober := &MihomoProber{
		options: MihomoOptions{Attempts: 3, RetryDelay: time.Nanosecond, RequestTimeout: time.Second},
		probe: func(context.Context, int, string, string, time.Duration) statsigProbeResult {
			calls++
			if calls == 1 {
				return statsigProbeResult{decision: statsigTransportFailure, compressedBytes: 100, decompressedBytes: 200, err: context.DeadlineExceeded}
			}
			return statsigProbeResult{decision: statsigReachable, httpStatus: 200, country: "US", contentEncoding: "br", compressedBytes: 50, decompressedBytes: 100}
		},
	}
	observation := prober.probeCandidate(context.Background(), Candidate{Fingerprint: "node"}, 19001)
	if !observation.Available || observation.Attempts != 2 || calls != 2 || observation.CompressedBytes != 150 || observation.DecompressedBytes != 300 {
		t.Fatalf("observation = %+v calls=%d, want second-attempt success with cumulative bytes", observation, calls)
	}
}

func TestProbeCandidateDoesNotRetryExplicitServiceRejection(t *testing.T) {
	calls := 0
	prober := &MihomoProber{
		options: MihomoOptions{Attempts: 3, RetryDelay: time.Nanosecond, RequestTimeout: time.Second},
		probe: func(context.Context, int, string, string, time.Duration) statsigProbeResult {
			calls++
			return statsigProbeResult{decision: statsigRejected, httpStatus: 401, explicitReject: true, err: errors.New("rejected")}
		},
	}
	observation := prober.probeCandidate(context.Background(), Candidate{Fingerprint: "node"}, 19001)
	if observation.Available || !observation.ServiceRejected || observation.Attempts != 1 || calls != 1 {
		t.Fatalf("observation = %+v calls=%d, want one explicit-rejection attempt", observation, calls)
	}
}

func TestWriteProbeConfigPreservesDeduplicatedAliasDefinitions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	candidates := []Candidate{{
		Name:  "US 01",
		Proxy: map[string]any{"name": "US 01", "type": "ss", "server": "127.0.0.1", "port": 10001, "cipher": "aes-128-gcm", "password": "secret"},
		Definitions: []map[string]any{
			{"name": "US 01", "type": "ss", "server": "127.0.0.1", "port": 10001, "cipher": "aes-128-gcm", "password": "secret"},
			{"name": "US alias", "type": "ss", "server": "127.0.0.1", "port": 10001, "cipher": "aes-128-gcm", "password": "secret"},
		},
	}}
	if err := writeProbeConfig(path, candidates, []int{19001}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "name: US 01") || !strings.Contains(text, "name: US alias") {
		t.Fatalf("probe config did not preserve all proxy definitions:\n%s", text)
	}
	if strings.Count(text, "type: mixed") != 1 {
		t.Fatalf("probe config listeners = %d, want one deduplicated probe listener:\n%s", strings.Count(text, "type: mixed"), text)
	}
}

func TestProbeConfigPassesBundledMihomoValidation(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("bundled validation core is darwin-arm64")
	}
	corePath := filepath.Clean(filepath.Join("..", "..", "bin", "darwin-arm64", "mihomo-meta"))
	if _, err := os.Stat(corePath); err != nil {
		t.Skipf("bundled Mihomo core unavailable: %v", err)
	}
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	candidates := []Candidate{{
		Name: "US 01",
		Proxy: map[string]any{
			"name": "US 01", "type": "ss", "server": "127.0.0.1", "port": 10001,
			"cipher": "aes-128-gcm", "password": "secret",
		},
	}}
	if err := writeProbeConfig(configPath, candidates, []int{19001}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, corePath, "-t", "-d", dir, "-f", configPath).CombinedOutput()
	if err != nil {
		t.Fatalf("mihomo rejected probe config: %v\n%s", err, output)
	}
}
