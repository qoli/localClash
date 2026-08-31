package proxyprobe

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

type Options struct {
	CorePath       string
	RuntimeParent  string
	RuntimePrefix  string
	ListenerPrefix string
}

type Session struct {
	ports   []int
	process *probeProcess
}

func Start(ctx context.Context, proxies []map[string]any, candidateNames []string, options Options) (*Session, error) {
	options.CorePath = strings.TrimSpace(options.CorePath)
	if options.CorePath == "" {
		return nil, errors.New("isolated proxy probe core path is required")
	}
	info, err := os.Stat(options.CorePath)
	if err != nil {
		return nil, fmt.Errorf("inspect isolated proxy probe core: %w", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("isolated proxy probe core %q is a directory", options.CorePath)
	}
	options.RuntimeParent = strings.TrimSpace(options.RuntimeParent)
	if options.RuntimeParent == "" {
		return nil, errors.New("isolated proxy probe runtime parent is required")
	}
	if len(proxies) == 0 {
		return nil, errors.New("isolated proxy probe definitions are required")
	}
	if len(candidateNames) == 0 {
		return nil, errors.New("isolated proxy probe candidates are required")
	}
	if options.RuntimePrefix == "" {
		options.RuntimePrefix = "proxy-probe-"
	}
	if options.ListenerPrefix == "" {
		options.ListenerPrefix = "proxy-probe"
	}
	if err := os.MkdirAll(options.RuntimeParent, 0o755); err != nil {
		return nil, fmt.Errorf("create isolated proxy probe runtime parent: %w", err)
	}
	runtimeDir, err := os.MkdirTemp(options.RuntimeParent, options.RuntimePrefix)
	if err != nil {
		return nil, fmt.Errorf("create isolated proxy probe runtime: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(runtimeDir)
		}
	}()

	ports, err := reservePorts(len(candidateNames))
	if err != nil {
		return nil, err
	}
	configPath := filepath.Join(runtimeDir, "config.yaml")
	if err := writeConfig(configPath, proxies, candidateNames, ports, options.ListenerPrefix); err != nil {
		return nil, err
	}
	process, err := startCore(ctx, options.CorePath, runtimeDir, configPath, ports[0])
	if err != nil {
		return nil, err
	}
	process.runtimeDir = runtimeDir
	cleanup = false
	return &Session{ports: ports, process: process}, nil
}

func (s *Session) Len() int {
	if s == nil {
		return 0
	}
	return len(s.ports)
}

func (s *Session) HTTPClient(index int, timeout time.Duration) (*http.Client, error) {
	if s == nil || index < 0 || index >= len(s.ports) {
		return nil, fmt.Errorf("isolated proxy probe candidate index %d is out of range", index)
	}
	proxyURL, err := url.Parse("http://127.0.0.1:" + strconv.Itoa(s.ports[index]))
	if err != nil {
		return nil, fmt.Errorf("build isolated proxy probe URL: %w", err)
	}
	transport := &http.Transport{
		Proxy:               http.ProxyURL(proxyURL),
		DisableKeepAlives:   true,
		TLSHandshakeTimeout: timeout,
	}
	return &http.Client{
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}, nil
}

func (s *Session) Close() {
	if s == nil || s.process == nil {
		return
	}
	s.process.stop()
	_ = os.RemoveAll(s.process.runtimeDir)
}

func reservePorts(count int) ([]int, error) {
	listeners := make([]net.Listener, 0, count)
	defer func() {
		for _, listener := range listeners {
			_ = listener.Close()
		}
	}()
	ports := make([]int, 0, count)
	for range count {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, fmt.Errorf("reserve isolated proxy probe port: %w", err)
		}
		listeners = append(listeners, listener)
		ports = append(ports, listener.Addr().(*net.TCPAddr).Port)
	}
	return ports, nil
}

func writeConfig(path string, proxies []map[string]any, candidateNames []string, ports []int, listenerPrefix string) error {
	if len(candidateNames) != len(ports) {
		return fmt.Errorf("isolated proxy probe has %d candidates but %d ports", len(candidateNames), len(ports))
	}
	listeners := make([]any, 0, len(candidateNames))
	for index, name := range candidateNames {
		name = strings.TrimSpace(name)
		if name == "" {
			return fmt.Errorf("isolated proxy probe candidate %d has no name", index)
		}
		listeners = append(listeners, map[string]any{
			"name":   fmt.Sprintf("%s-%d", listenerPrefix, index),
			"type":   "mixed",
			"listen": "127.0.0.1",
			"port":   ports[index],
			"proxy":  name,
		})
	}
	config := map[string]any{
		"allow-lan":    false,
		"bind-address": "127.0.0.1",
		"ipv6":         true,
		"log-level":    "warning",
		"mode":         "rule",
		"proxies":      proxies,
		"listeners":    listeners,
		"rules":        []string{"MATCH,DIRECT"},
	}
	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("encode isolated proxy probe config: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write isolated proxy probe config: %w", err)
	}
	return nil
}

type probeProcess struct {
	cmd        *exec.Cmd
	waitCh     chan error
	output     *limitedBuffer
	runtimeDir string
	once       sync.Once
}

func startCore(ctx context.Context, corePath, runtimeDir, configPath string, readinessPort int) (*probeProcess, error) {
	processCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(processCtx, corePath, "-d", runtimeDir, "-f", configPath)
	output := &limitedBuffer{limit: 8192}
	cmd.Stdout = output
	cmd.Stderr = output
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start isolated Mihomo proxy probe: %w", err)
	}
	process := &probeProcess{cmd: cmd, waitCh: make(chan error, 1), output: output}
	go func() {
		process.waitCh <- cmd.Wait()
		cancel()
	}()

	deadline := time.NewTimer(12 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	address := "127.0.0.1:" + strconv.Itoa(readinessPort)
	for {
		connection, err := net.DialTimeout("tcp", address, 200*time.Millisecond)
		if err == nil {
			_ = connection.Close()
			return process, nil
		}
		select {
		case waitErr := <-process.waitCh:
			return nil, fmt.Errorf("isolated Mihomo exited before proxy probe was ready: %v: %s", waitErr, output.String())
		case <-ticker.C:
		case <-deadline.C:
			process.stop()
			return nil, fmt.Errorf("isolated Mihomo did not become ready for proxy probe: %s", output.String())
		case <-ctx.Done():
			process.stop()
			return nil, ctx.Err()
		}
	}
}

func (p *probeProcess) stop() {
	p.once.Do(func() {
		if p.cmd.Process == nil {
			return
		}
		_ = p.cmd.Process.Signal(os.Interrupt)
		select {
		case <-p.waitCh:
		case <-time.After(2 * time.Second):
			_ = p.cmd.Process.Kill()
			<-p.waitCh
		}
	})
}

type limitedBuffer struct {
	mu    sync.Mutex
	limit int
	data  []byte
}

func (b *limitedBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	original := len(data)
	remaining := b.limit - len(b.data)
	if remaining > 0 {
		if len(data) > remaining {
			data = data[:remaining]
		}
		b.data = append(b.data, data...)
	}
	return original, nil
}

func (b *limitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(string(bytes.Clone(b.data)))
}
