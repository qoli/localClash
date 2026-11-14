package proxyprobe

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// Each HTTP probe listener uses one TCP socket. Keep half of the router's
// 1024-descriptor budget available for the core and its outbound connections.
const BatchSize = 512

// Keep a batch's port reservation and startup together when two pools prepare
// concurrently. The core binds these ports after the reservations are closed.
var listenerStartup sync.Mutex

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

	listenerStartup.Lock()
	defer listenerStartup.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	readyListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen for isolated proxy probe readiness: %w", err)
	}
	readyServer := http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})}
	go func() { _ = readyServer.Serve(readyListener) }()
	defer readyServer.Close()

	// Mihomo starts accepting connections before it enables TCP forwarding.
	// Test a local request through DIRECT so readiness does not depend on a node.
	listenerNames := append(slices.Clone(candidateNames), "DIRECT")
	listenerPorts, err := reservePorts(len(listenerNames))
	if err != nil {
		return nil, err
	}
	configPath := filepath.Join(runtimeDir, "config.yaml")
	if err := writeConfig(configPath, proxies, listenerNames, listenerPorts, options.ListenerPrefix); err != nil {
		return nil, err
	}
	probeCore := filepath.Join(runtimeDir, "mihomo-probe")
	if err := copyCore(options.CorePath, probeCore); err != nil {
		return nil, fmt.Errorf("stage isolated Mihomo proxy probe: %w", err)
	}
	ports := listenerPorts[:len(candidateNames)]
	readyPort := listenerPorts[len(candidateNames)]
	process, err := startCore(ctx, probeCore, runtimeDir, configPath, ports, readyPort, "http://"+readyListener.Addr().String())
	if err != nil {
		return nil, err
	}
	process.runtimeDir = runtimeDir
	cleanup = false
	return &Session{ports: ports, process: process}, nil
}

func copyCore(source, target string) error {
	if resolved, err := filepath.EvalSymlinks(source); err == nil {
		if err := os.Link(resolved, target); err == nil {
			return nil
		}
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	return errors.Join(err, out.Close())
}

func (s *Session) Len() int {
	if s == nil {
		return 0
	}
	return len(s.ports)
}

func (s *Session) HTTPClient(index int, timeout time.Duration, reuseConnections bool) (*http.Client, error) {
	if s == nil || index < 0 || index >= len(s.ports) {
		return nil, fmt.Errorf("isolated proxy probe candidate index %d is out of range", index)
	}
	return newProxyHTTPClient(s.ports[index], timeout, reuseConnections), nil
}

// DialContext opens a TCP tunnel through the same per-candidate HTTP listener.
func (s *Session) DialContext(ctx context.Context, index int, address string) (net.Conn, error) {
	if s == nil || index < 0 || index >= len(s.ports) {
		return nil, fmt.Errorf("isolated proxy probe candidate index %d is out of range", index)
	}
	proxy := net.JoinHostPort("127.0.0.1", strconv.Itoa(s.ports[index]))
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", proxy)
	if err != nil {
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	ready := false
	defer func() {
		if !ready {
			conn.Close()
		}
	}()
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return nil, err
		}
	}
	req := &http.Request{Method: http.MethodConnect, URL: &url.URL{Opaque: address}, Host: address}
	if err := req.Write(conn); err != nil {
		return nil, err
	}
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, req)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("proxy CONNECT: HTTP %d", response.StatusCode)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ready = true
	return &tunnelConn{Conn: conn, reader: reader}, nil
}

type tunnelConn struct {
	net.Conn
	reader *bufio.Reader
}

func (conn *tunnelConn) Read(data []byte) (int, error) {
	return conn.reader.Read(data)
}

func newProxyHTTPClient(port int, timeout time.Duration, reuseConnections bool) *http.Client {
	proxyURL := &url.URL{Scheme: "http", Host: net.JoinHostPort("127.0.0.1", strconv.Itoa(port))}
	transport := &http.Transport{
		Proxy:               http.ProxyURL(proxyURL),
		DisableKeepAlives:   !reuseConnections,
		TLSHandshakeTimeout: timeout,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func (s *Session) Err() error {
	if s == nil || s.process == nil {
		return nil
	}
	if err := s.process.exitError(); err != nil {
		return err
	}
	unavailable, err := s.process.unavailableListeners(s.ports)
	if len(unavailable) == 0 && err == nil {
		return s.process.exitError()
	}
	select {
	case <-s.process.done:
		return s.process.exitError()
	case <-time.After(250 * time.Millisecond):
	}
	if len(unavailable) == 0 {
		return fmt.Errorf("inspect isolated Mihomo proxy probe listeners: %w", err)
	}
	detail := "missing from the process listener table"
	if err != nil {
		detail = err.Error()
	}
	return fmt.Errorf("isolated Mihomo lost %d of %d proxy probe listeners; first unavailable listener 127.0.0.1:%d: %s: %s", len(unavailable), len(s.ports), unavailable[0], detail, s.process.output.String())
}

func (p *probeProcess) unavailableListeners(ports []int) ([]int, error) {
	if runtime.GOOS == "linux" {
		return missingLinuxListeners(p.cmd.Process.Pid, ports)
	}
	var unavailable []int
	var firstErr error
	for _, port := range ports {
		connection, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), 200*time.Millisecond)
		if err != nil {
			unavailable = append(unavailable, port)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		_ = connection.Close()
	}
	return unavailable, firstErr
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
			"type":   "http",
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
	done       chan struct{}
	waitErr    error
	output     *limitedBuffer
	runtimeDir string
	once       sync.Once
}

func startCore(ctx context.Context, corePath, runtimeDir, configPath string, ports []int, readyPort int, readyURL string) (*probeProcess, error) {
	processCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(processCtx, corePath, "-d", runtimeDir, "-f", configPath)
	output := &limitedBuffer{limit: 8192}
	cmd.Stdout = output
	cmd.Stderr = output
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start isolated Mihomo proxy probe: %w", err)
	}
	process := &probeProcess{cmd: cmd, done: make(chan struct{}), output: output}
	go func() {
		process.waitErr = cmd.Wait()
		close(process.done)
		cancel()
	}()

	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	const pollInterval = 100 * time.Millisecond
	client := newProxyHTTPClient(readyPort, pollInterval, false)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		forwarding := proxyCanForward(ctx, client, readyURL)
		if forwarding && readyListenerCount(ports) == len(ports) {
			return process, nil
		}
		select {
		case <-process.done:
			return nil, fmt.Errorf("isolated Mihomo exited before proxy probe was ready: %v: %s", process.waitErr, output.String())
		case <-ticker.C:
		case <-deadline.C:
			ready := readyListenerCount(ports)
			process.stop()
			return nil, fmt.Errorf("isolated Mihomo proxy probe not ready before timeout (local forwarding: %t, candidate listeners: %d of %d): %s", forwarding, ready, len(ports), output.String())
		case <-ctx.Done():
			process.stop()
			return nil, ctx.Err()
		}
	}
}

func proxyCanForward(ctx context.Context, client *http.Client, target string) bool {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return false
	}
	response, err := client.Do(request)
	if err != nil {
		return false
	}
	response.Body.Close()
	return response.StatusCode == http.StatusNoContent
}

func readyListenerCount(ports []int) int {
	ready := 0
	for _, port := range ports {
		connection, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), 50*time.Millisecond)
		if err != nil {
			continue
		}
		_ = connection.Close()
		ready++
	}
	return ready
}

func (p *probeProcess) exitError() error {
	select {
	case <-p.done:
		return fmt.Errorf("isolated Mihomo exited during proxy probe: %v: %s", p.waitErr, p.output.String())
	default:
		return nil
	}
}

func (p *probeProcess) stop() {
	p.once.Do(func() {
		if p.cmd.Process == nil {
			return
		}
		select {
		case <-p.done:
			return
		default:
		}
		_ = p.cmd.Process.Signal(os.Interrupt)
		select {
		case <-p.done:
		case <-time.After(2 * time.Second):
			_ = p.cmd.Process.Kill()
			<-p.done
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
	b.data = append(b.data, data...)
	if len(b.data) > b.limit {
		b.data = bytes.Clone(b.data[len(b.data)-b.limit:])
	}
	return original, nil
}

func (b *limitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(string(bytes.Clone(b.data)))
}
