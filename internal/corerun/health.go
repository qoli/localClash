package corerun

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"slices"
	"time"

	"localclash/internal/configrender"
	"localclash/internal/mihomoapi"

	"gopkg.in/yaml.v3"
)

func defaultProbeRuntimeHealth(ctx context.Context, configPath string, pid int, timeout time.Duration, waitForLocalProxy bool) error {
	if timeout <= 0 {
		timeout = defaultRuntimeHealthTimeout
	}
	client, err := mihomoapi.NewFromConfig(configPath)
	if err != nil {
		return err
	}
	var proxyClient *mihomoapi.Client
	if waitForLocalProxy {
		proxyClient, err = newLocalProxyHealthClient(configPath, client.Controller)
		if err != nil {
			return err
		}
	}

	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var lastErr error
	for probeCtx.Err() == nil {
		if !processRunning(pid) || processZombie(pid) {
			return fmt.Errorf("runtime pid %d exited before becoming healthy", pid)
		}
		lastErr = checkRuntimeVersion(probeCtx, client, proxyClient)
		if lastErr == nil {
			return nil
		}
		timer := time.NewTimer(200 * time.Millisecond)
		select {
		case <-probeCtx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if lastErr == nil {
		lastErr = probeCtx.Err()
	}
	return fmt.Errorf("mihomo did not become healthy within %s: %w", timeout, lastErr)
}

func checkRuntimeVersion(ctx context.Context, client, proxyClient *mihomoapi.Client) error {
	opts := mihomoapi.RequestOptions{Method: http.MethodGet, Path: "/version", Timeout: defaultRuntimeProbeTimeout, MaxBytes: 64 * 1024}
	direct, err := client.Request(ctx, opts)
	if err != nil {
		return fmt.Errorf("mihomo controller /version: %w", err)
	}
	if proxyClient == nil {
		return nil
	}
	proxied, err := proxyClient.Request(ctx, opts)
	if client.Secret != "" && proxied.StatusCode == http.StatusUnauthorized {
		return nil
	}
	if err != nil {
		return fmt.Errorf("mihomo local proxy /version: %w", err)
	}
	if proxied.StatusCode != direct.StatusCode || proxied.Body != direct.Body || !reflect.DeepEqual(proxied.JSON, direct.JSON) {
		return errors.New("mihomo local proxy /version did not return the controller response")
	}
	return nil
}

func newLocalProxyHealthClient(configPath, controller string) (*mihomoapi.Client, error) {
	host, _, err := net.SplitHostPort(controller)
	if err != nil || !net.ParseIP(host).IsLoopback() {
		return nil, nil
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}
	var config struct {
		MixedPort        int      `yaml:"mixed-port"`
		HTTPPort         int      `yaml:"port"`
		Mode             string   `yaml:"mode"`
		AllowLAN         bool     `yaml:"allow-lan"`
		BindAddress      string   `yaml:"bind-address"`
		Authentication   []string `yaml:"authentication"`
		LANAllowedIPs    []string `yaml:"lan-allowed-ips"`
		LANDisallowedIPs []string `yaml:"lan-disallowed-ips"`
		Rules            []string `yaml:"rules"`
	}
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("read startup proxy settings: %w", err)
	}
	if len(config.Authentication) != 0 || len(config.LANAllowedIPs) != 0 || len(config.LANDisallowedIPs) != 0 {
		return nil, nil
	}
	if config.AllowLAN && config.BindAddress != "" && config.BindAddress != "*" &&
		config.BindAddress != "0.0.0.0" && config.BindAddress != "127.0.0.1" {
		return nil, nil
	}
	switch config.Mode {
	case "direct":
	case "", "rule":
		// The generated DIRECT baseline takes precedence over user routes.
		baseline := configrender.LocalBaselineRuleLines()
		if len(config.Rules) < len(baseline) || !slices.Equal(config.Rules[:len(baseline)], baseline) {
			return nil, nil
		}
	default:
		return nil, nil
	}
	port := config.MixedPort
	if port == 0 {
		port = config.HTTPPort
	}
	if port == 0 {
		return nil, nil
	}
	proxyURL := &url.URL{Scheme: "http", Host: fmt.Sprintf("127.0.0.1:%d", port)}
	// Keep controller credentials out of the proxied request.
	return &mihomoapi.Client{
		Controller: controller,
		HTTPClient: &http.Client{
			Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL), DisableKeepAlives: true},
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}
