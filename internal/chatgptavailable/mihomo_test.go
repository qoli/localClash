package chatgptavailable

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestClassifyEligibilityAcceptsExpectedDCFingerprint(t *testing.T) {
	result := classifyEligibility(http.StatusForbidden, []byte(`{"cf_details":"Request is not allowed. Please try again later.", "type":"dc"}`))
	if result.decision != eligibilityEligible || result.httpStatus != http.StatusForbidden || result.err != nil {
		t.Fatalf("result = %+v, want eligible 403 dc fingerprint", result)
	}
}

func TestClassifyEligibilitySeparatesExplicitRejectionsFromAmbiguousResponses(t *testing.T) {
	tests := []struct {
		name       string
		httpStatus int
		body       string
		decision   string
		explicit   bool
	}{
		{name: "disallowed ISP", httpStatus: http.StatusForbidden, body: `{"cf_details":"Something went wrong. You may be connected to a disallowed ISP."}`, decision: eligibilityDisallowedISP, explicit: true},
		{name: "unsupported region", httpStatus: http.StatusForbidden, body: `{"error":{"code":"unsupported_country_region_territory"}}`, decision: eligibilityUnsupportedRegion, explicit: true},
		{name: "normal status JSON", httpStatus: http.StatusOK, body: `{"status":"normal"}`, decision: eligibilityUnexpectedResponse},
		{name: "wrong type", httpStatus: http.StatusForbidden, body: `{"cf_details":"Request is not allowed. Please try again later.","type":"other"}`, decision: eligibilityUnexpectedResponse},
		{name: "challenge HTML", httpStatus: http.StatusForbidden, body: `<html>challenge</html>`, decision: eligibilityUnexpectedResponse},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := classifyEligibility(test.httpStatus, []byte(test.body))
			if result.decision != test.decision || result.explicitReject != test.explicit || result.err == nil {
				t.Fatalf("result = %+v, want decision %q explicit=%v", result, test.decision, test.explicit)
			}
		})
	}
}

func TestEvaluateProbeAttemptRequiresBothMobileEligibilityFingerprints(t *testing.T) {
	eligible := classifyEligibility(http.StatusForbidden, []byte(`{"cf_details":"Request is not allowed. Please try again later.","type":"dc"}`))
	if available, rejected, probeError := evaluateProbeAttempt(eligible, eligible); !available || rejected || probeError != "" {
		t.Fatalf("eligible attempt = available %v rejected %v error %q", available, rejected, probeError)
	}

	disallowed := classifyEligibility(http.StatusForbidden, []byte(`{"cf_details":"You may be connected to a disallowed ISP."}`))
	if available, rejected, probeError := evaluateProbeAttempt(eligible, disallowed); available || !rejected || probeError == "" {
		t.Fatalf("disallowed attempt = available %v rejected %v error %q", available, rejected, probeError)
	}

	reset := eligibilityProbeResult{decision: eligibilityTransportFailure, err: errors.New("connection reset by peer")}
	if available, rejected, probeError := evaluateProbeAttempt(reset, eligible); available || rejected || !strings.Contains(probeError, "connection reset by peer") {
		t.Fatalf("reset attempt = available %v rejected %v error %q", available, rejected, probeError)
	}

	unexpected := classifyEligibility(http.StatusForbidden, []byte(`<html>challenge</html>`))
	if available, rejected, probeError := evaluateProbeAttempt(eligible, unexpected); available || rejected || probeError == "" {
		t.Fatalf("unexpected attempt = available %v rejected %v error %q", available, rejected, probeError)
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
	if prober.options.Concurrency != 40 || prober.options.RequestTimeout != 5*time.Second || prober.options.Attempts != 3 {
		t.Fatalf("probe defaults = %+v, want concurrency 40, timeout 5s, attempts 3", prober.options)
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
