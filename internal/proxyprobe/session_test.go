package proxyprobe

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestWriteConfigUsesAllDefinitionsAndDedicatedCandidateListeners(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	proxies := []map[string]any{
		{"name": "US 01", "type": "ss", "server": "127.0.0.1", "port": 10001, "cipher": "aes-128-gcm", "password": "secret"},
		{"name": "US alias", "type": "ss", "server": "127.0.0.1", "port": 10001, "cipher": "aes-128-gcm", "password": "secret"},
		{"name": "JP 01", "type": "trojan", "server": "127.0.0.1", "port": 10002, "password": "secret"},
	}
	if err := writeConfig(path, proxies, []string{"US 01", "JP 01"}, []int{19001, 19002}, "test-probe"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{"listeners:", "proxy: US 01", "proxy: JP 01", "name: US alias", "listen: 127.0.0.1"} {
		if !strings.Contains(text, want) {
			t.Fatalf("probe config missing %q:\n%s", want, text)
		}
	}
	if strings.Count(text, "type: mixed") != 2 {
		t.Fatalf("probe config listeners = %d, want two", strings.Count(text, "type: mixed"))
	}
}

func TestGeneratedConfigPassesBundledMihomoValidation(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("bundled validation core is darwin-arm64")
	}
	corePath := filepath.Clean(filepath.Join("..", "..", "bin", "darwin-arm64", "mihomo-meta"))
	if _, err := os.Stat(corePath); err != nil {
		t.Skipf("bundled Mihomo core unavailable: %v", err)
	}
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	proxies := []map[string]any{{
		"name": "US 01", "type": "ss", "server": "127.0.0.1", "port": 10001,
		"cipher": "aes-128-gcm", "password": "secret",
	}}
	if err := writeConfig(configPath, proxies, []string{"US 01"}, []int{19001}, "test-probe"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, corePath, "-t", "-d", dir, "-f", configPath).CombinedOutput()
	if err != nil {
		t.Fatalf("mihomo rejected probe config: %v\n%s", err, output)
	}
}
