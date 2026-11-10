package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"localclash/internal/capability"
	"localclash/internal/corerun"
	"localclash/internal/mihomoapi"
	"localclash/internal/poolcontract"
	"localclash/internal/runtimesupervision"

	"gopkg.in/yaml.v3"
)

type activeRuntime struct {
	workDir string
	state   runtimesupervision.State
	client  *mihomoapi.Client
}

func refresh(ctx context.Context, workDir string) ([]capability.Result, error) {
	live, data, err := readActiveRuntime(workDir)
	if err != nil {
		return nil, err
	}
	var config struct {
		Revision  string           `yaml:"x-localclash-pool-revision"`
		Proxies   []map[string]any `yaml:"proxies"`
		Providers map[string]struct {
			Type string `yaml:"type"`
			Path string `yaml:"path"`
		} `yaml:"proxy-providers"`
	}
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, errors.New("decode loaded Mihomo pool configuration")
	}
	input, err := poolcontract.NewInput(config.Proxies)
	if err != nil {
		return nil, err
	}
	if input.Revision != config.Revision {
		return nil, errors.New("loaded configuration has no matching managed pool revision")
	}
	var pools []poolcontract.Pool
	for _, id := range []string{poolcontract.AutoCapability, poolcontract.ChatGPTCapability} {
		pool, _ := poolcontract.Lookup(id)
		provider, exists := config.Providers[pool.Provider]
		if !exists {
			continue
		}
		if provider.Type != "file" || filepath.Clean(provider.Path) != pool.Path(input.Revision) {
			return nil, fmt.Errorf("loaded provider %s has an unexpected type or path", pool.Provider)
		}
		if _, err := live.members(ctx, pool); err != nil {
			return nil, err
		}
		pools = append(pools, pool)
	}
	if len(pools) == 0 {
		return nil, errors.New("loaded configuration has no managed pool providers")
	}
	defer live.client.HTTPClient.CloseIdleConnections()
	return rebuild(ctx, input, pools, live.state.LauncherPath, workDir, live)
}

func readActiveRuntime(workDir string) (*activeRuntime, []byte, error) {
	state, err := runtimesupervision.Read(workDir)
	if err != nil {
		return nil, nil, fmt.Errorf("read active Mihomo generation: %w", err)
	}
	if state.State != runtimesupervision.StateRunning {
		return nil, nil, errors.New("managed Mihomo is not running")
	}
	boot, err := runtimesupervision.CurrentBootID()
	if err != nil || state.BootID != boot {
		return nil, nil, errors.New("managed Mihomo generation is from another boot")
	}
	if err := corerun.ValidateManagedRuntimeProcess(state.PID, state.CorePath, state.ConfigPath, workDir, state.LauncherPath); err != nil {
		return nil, nil, fmt.Errorf("verify active Mihomo process: %w", err)
	}
	data, err := os.ReadFile(state.ConfigPath)
	if err != nil {
		return nil, nil, err
	}
	hash := sha256.Sum256(data)
	if hex.EncodeToString(hash[:]) != state.ConfigSHA256 {
		return nil, nil, errors.New("rendered configuration differs from the loaded Mihomo generation")
	}
	var endpoints struct {
		Controller string `yaml:"external-controller"`
		Secret     string `yaml:"secret"`
	}
	if err := yaml.Unmarshal(data, &endpoints); err != nil {
		return nil, nil, errors.New("decode loaded Mihomo controller settings")
	}
	client, err := mihomoapi.New(endpoints.Controller, endpoints.Secret)
	if err != nil {
		return nil, nil, err
	}
	host, _, err := net.SplitHostPort(client.Controller)
	if ip := net.ParseIP(host); err != nil || (host != "localhost" && (ip == nil || !ip.IsLoopback())) {
		return nil, nil, errors.New("pool refresh requires a local Mihomo controller")
	}
	client.HTTPClient = &http.Client{
		Transport: &http.Transport{},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return &activeRuntime{workDir: workDir, state: state, client: client}, data, nil
}

func providerPath(pool poolcontract.Pool) string {
	return "/providers/proxies/" + pool.Provider
}

func (live *activeRuntime) members(ctx context.Context, pool poolcontract.Pool) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, mihomoapi.DefaultTimeout)
	defer cancel()
	address := url.URL{Scheme: "http", Host: live.client.Controller, Path: providerPath(pool)}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address.String(), nil)
	if err != nil {
		return nil, err
	}
	if live.client.Secret != "" {
		req.Header.Set("Authorization", "Bearer "+live.client.Secret)
	}
	resp, err := live.client.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("read Mihomo provider %s: HTTP %d", pool.Provider, resp.StatusCode)
	}
	var provider struct {
		Proxies []struct{ Name string } `json:"proxies"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&provider); err != nil {
		return nil, fmt.Errorf("decode Mihomo provider membership: %w", err)
	}
	if provider.Proxies == nil {
		return nil, errors.New("Mihomo provider response has no proxy list")
	}
	names := make([]string, 0, len(provider.Proxies))
	for _, proxy := range provider.Proxies {
		if proxy.Name == "" {
			return nil, errors.New("Mihomo provider response has an unnamed proxy")
		}
		names = append(names, proxy.Name)
	}
	return names, nil
}

func (live *activeRuntime) update(ctx context.Context, pool poolcontract.Pool, expected []string) error {
	_, err := live.client.Request(ctx, mihomoapi.RequestOptions{Method: http.MethodPut, Path: providerPath(pool)})
	if err != nil {
		return err
	}
	got, err := live.members(ctx, pool)
	if err != nil {
		return err
	}
	if !slices.Equal(got, expected) {
		return fmt.Errorf("Mihomo provider membership mismatch (expected %d, read %d)", len(expected), len(got))
	}
	return nil
}

func providerData(input poolcontract.Input, qualified []string) ([]byte, []string, error) {
	known := make(map[string]bool, len(input.Proxies))
	for _, proxy := range input.Proxies {
		name, _ := proxy["name"].(string)
		known[strings.TrimSpace(name)] = true
	}
	var names []string
	var proxies []map[string]any
	seen := make(map[string]bool, len(qualified))
	for _, name := range qualified {
		if !known[name] || seen[name] || name == poolcontract.RejectName {
			return nil, nil, errors.New("qualified pool contains an invalid or duplicate subscription node")
		}
		seen[name] = true
	}
	// Publish in source order so latency changes alone do not rotate adapters.
	for _, proxy := range input.Proxies {
		name, _ := proxy["name"].(string)
		if seen[strings.TrimSpace(name)] {
			names = append(names, name)
			proxies = append(proxies, proxy)
		}
	}
	if len(proxies) == 0 {
		names = []string{poolcontract.RejectName}
		proxies = []map[string]any{poolcontract.Reject()}
	}
	data, err := json.Marshal(map[string]any{"proxies": proxies})
	return data, names, err
}

func publish(ctx context.Context, live *activeRuntime, pool poolcontract.Pool, path string, input poolcontract.Input, qualified []string) error {
	data, names, err := providerData(input, qualified)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if live == nil {
		return replaceFile(path, data)
	}
	return runtimesupervision.WithLock(live.workDir, func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		current, _, err := readActiveRuntime(live.workDir)
		if err != nil {
			return err
		}
		defer current.client.HTTPClient.CloseIdleConnections()
		if current.state.PID != live.state.PID || current.state.ConfigSHA256 != live.state.ConfigSHA256 || current.state.CoreSHA256 != live.state.CoreSHA256 {
			return errors.New("Mihomo generation changed during pool qualification")
		}
		return live.publishProvider(ctx, pool, path, data, names)
	})
}

func (live *activeRuntime) publishProvider(ctx context.Context, pool poolcontract.Pool, path string, data []byte, names []string) error {
	previous, err := os.ReadFile(path)
	missing := errors.Is(err, os.ErrNotExist)
	if err != nil && !missing {
		return fmt.Errorf("read published provider before update: %w", err)
	}
	previousNames, err := live.members(ctx, pool)
	if err != nil {
		return err
	}
	if missing {
		if !slices.Equal(previousNames, []string{poolcontract.RejectName}) {
			return errors.New("loaded provider has members but its provider file is missing")
		}
		previous, _, err = providerData(poolcontract.Input{}, nil)
		if err != nil {
			return err
		}
	}
	if !missing && bytes.Equal(previous, data) {
		if slices.Equal(previousNames, names) {
			return nil
		}
		return live.update(ctx, pool, names)
	}
	if err := replaceFile(path, data); err != nil {
		return err
	}
	if err := live.update(ctx, pool, names); err != nil {
		if rollbackErr := replaceFile(path, previous); rollbackErr != nil {
			return errors.Join(err, fmt.Errorf("restore provider file: %w", rollbackErr))
		}
		rollbackErr := live.update(context.WithoutCancel(ctx), pool, previousNames)
		return errors.Join(err, rollbackErr)
	}
	return nil
}

func replaceFile(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".pool-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(data)
	if err := errors.Join(writeErr, file.Sync(), file.Close()); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
