package proxyprobe

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestHTTPClientReusesConnectionsWhenRequested(t *testing.T) {
	var opened atomic.Int32
	proxy := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Host != "doh.example" {
			t.Errorf("proxy request host = %q, want doh.example", r.URL.Host)
		}
		_, _ = io.WriteString(w, "response")
	}))
	proxy.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			opened.Add(1)
		}
	}
	proxy.Start()
	defer proxy.Close()
	session := &Session{ports: []int{proxy.Listener.Addr().(*net.TCPAddr).Port}}
	for _, test := range []struct {
		reuse bool
		want  int32
	}{
		{true, 1}, {false, 2},
	} {
		opened.Store(0)
		client, err := session.HTTPClient(0, time.Second, test.reuse)
		if err != nil {
			t.Fatal(err)
		}
		for range 2 {
			resp, err := client.Get("http://doh.example/dns-query")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := io.Copy(io.Discard, resp.Body); err != nil {
				t.Error(err)
			}
			_ = resp.Body.Close()
		}
		client.CloseIdleConnections()
		if got := opened.Load(); got != test.want {
			t.Errorf("reuse=%t opened %d connections for two requests, want %d", test.reuse, got, test.want)
		}
	}
}

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
	if strings.Count(text, "type: http") != 2 {
		t.Fatalf("probe config listeners = %d, want two", strings.Count(text, "type: http"))
	}
}

func TestSessionErrReportsPostReadinessProcessExitWithTailOutput(t *testing.T) {
	output := &limitedBuffer{limit: 16}
	_, _ = output.Write([]byte("discarded-prefix-fatal-tail"))
	process := &probeProcess{done: make(chan struct{}), waitErr: errors.New("exit status 2"), output: output}
	close(process.done)
	session := &Session{process: process}
	err := session.Err()
	if err == nil || !strings.Contains(err.Error(), "exit status 2") || !strings.Contains(err.Error(), "fatal-tail") || strings.Contains(err.Error(), "discarded-prefix") {
		t.Fatalf("session error = %v, want exit status and retained tail output", err)
	}
}

func TestSessionErrRejectsUnavailableReadinessListenerBeforeWaitCompletes(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	process := &probeProcess{done: make(chan struct{}), output: &limitedBuffer{limit: 128}}
	session := &Session{ports: []int{port}, process: process}
	err = session.Err()
	if err == nil || !strings.Contains(err.Error(), "lost 1 of 1 proxy probe listeners") {
		t.Fatalf("session error = %v, want unavailable listener infrastructure error", err)
	}
}

func TestReadyListenerCountRequiresEveryCandidateListener(t *testing.T) {
	first, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	secondPort := second.Addr().(*net.TCPAddr).Port
	ports := []int{first.Addr().(*net.TCPAddr).Port, secondPort}
	if got := readyListenerCount(ports); got != 2 {
		t.Fatalf("ready listeners = %d, want 2", got)
	}
	_ = second.Close()
	if got := readyListenerCount(ports); got != 1 {
		t.Fatalf("ready listeners after close = %d, want 1", got)
	}
}

func TestProxyReadinessRequiresSuccessfulForwarding(t *testing.T) {
	const target = "http://127.0.0.1:19000/"
	var ready atomic.Bool
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.String() != target {
			t.Errorf("readiness request = %s, want %s", r.URL, target)
		}
		if !ready.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer proxy.Close()
	port := proxy.Listener.Addr().(*net.TCPAddr).Port
	client := newProxyHTTPClient(port, time.Second, false)
	ctx := context.Background()
	if readyListenerCount([]int{port}) != 1 || proxyCanForward(ctx, client, target) {
		t.Fatal("listening socket without forwarding was treated as ready")
	}
	ready.Store(true)
	if !proxyCanForward(ctx, client, target) {
		t.Fatal("successful forwarding was not treated as ready")
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
	if err := writeConfig(configPath, proxies, []string{"US 01", "DIRECT"}, []int{19001, 19002}, "test-probe"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, corePath, "-t", "-d", dir, "-f", configPath).CombinedOutput()
	if err != nil {
		t.Fatalf("mihomo rejected probe config: %v\n%s", err, output)
	}
}
