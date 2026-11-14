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

func refresh(ctx context.Context, workDir string, enhancedFilter bool) ([]capability.Result, error) {
	live, data, err := readActiveRuntime(workDir)
	if err != nil {
		return nil, err
	}
	defer live.client.HTTPClient.CloseIdleConnections()
	input, pools, err := decodePoolConfig(data)
	if err != nil || len(pools) == 0 {
		return nil, err
	}
	for _, pool := range pools {
		if _, err := live.members(ctx, pool); err != nil {
			return nil, err
		}
	}
	return rebuild(ctx, input, pools, live.state.LauncherPath, workDir, live, enhancedFilter)
}

func decodePoolConfig(data []byte) (poolcontract.Input, []poolcontract.Pool, error) {
	var config struct {
		Disabled  bool             `yaml:"x-localclash-pools-disabled"`
		Revision  string           `yaml:"x-localclash-pool-revision"`
		Proxies   []map[string]any `yaml:"proxies"`
		Providers map[string]struct {
			Type string `yaml:"type"`
			Path string `yaml:"path"`
		} `yaml:"proxy-providers"`
	}
	if err := yaml.Unmarshal(data, &config); err != nil {
		return poolcontract.Input{}, nil, errors.New("decode loaded Mihomo pool configuration")
	}
	if config.Disabled {
		return poolcontract.Input{}, nil, nil
	}
	input, err := poolcontract.NewInput(config.Proxies)
	if err != nil {
		return poolcontract.Input{}, nil, err
	}
	if input.Revision != config.Revision {
		return poolcontract.Input{}, nil, errors.New("loaded configuration has no matching managed pool revision")
	}
	var pools []poolcontract.Pool
	for _, id := range []string{poolcontract.AutoCapability, poolcontract.ChatGPTCapability} {
		pool, _ := poolcontract.Lookup(id)
		provider, exists := config.Providers[pool.Provider]
		if !exists {
			continue
		}
		if provider.Type != "file" || filepath.Clean(provider.Path) != pool.Path(input.Revision) {
			return poolcontract.Input{}, nil, fmt.Errorf("loaded provider %s has an unexpected type or path", pool.Provider)
		}
		pools = append(pools, pool)
	}
	if len(pools) == 0 {
		return poolcontract.Input{}, nil, errors.New("loaded configuration has no managed pool providers")
	}
	return input, pools, nil
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

func providerData(pool poolcontract.Pool, input poolcontract.Input, qualified []string) ([]byte, []string, error) {
	return poolcontract.ProviderData(pool, input, qualified)
}

func publish(ctx context.Context, live *activeRuntime, pool poolcontract.Pool, path string, input poolcontract.Input, qualified []string) error {
	data, names, err := providerData(pool, input, qualified)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return runtimesupervision.WithLock(live.workDir, func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		current, config, err := readActiveRuntime(live.workDir)
		if err != nil {
			return err
		}
		defer current.client.HTTPClient.CloseIdleConnections()
		if err := verifyPoolInputs(live.state, current.state, config, pool, input.Revision); err != nil {
			return err
		}
		return current.publishProvider(ctx, pool, path, data, names)
	})
}

func verifyPoolInputs(previous, current runtimesupervision.State, config []byte, pool poolcontract.Pool, revision string) error {
	if current.CoreSHA256 != previous.CoreSHA256 {
		return errors.New("Mihomo core changed during pool qualification")
	}
	if current.ConfigSHA256 == previous.ConfigSHA256 {
		return nil
	}
	// A reload can retain the exact proxy definitions and provider that were probed.
	input, pools, err := decodePoolConfig(config)
	if err != nil {
		return fmt.Errorf("verify pool after Mihomo configuration changed: %w", err)
	}
	if input.Revision != revision || !slices.Contains(pools, pool) {
		return errors.New("Mihomo pool inputs changed during qualification")
	}
	return nil
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
		previous, _, err = providerData(poolcontract.Pool{}, poolcontract.Input{}, nil)
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
	if err := poolcontract.WriteFile(path, data); err != nil {
		return err
	}
	if err := live.update(ctx, pool, names); err != nil {
		if rollbackErr := poolcontract.WriteFile(path, previous); rollbackErr != nil {
			return errors.Join(err, fmt.Errorf("restore provider file: %w", rollbackErr))
		}
		rollbackErr := live.update(context.WithoutCancel(ctx), pool, previousNames)
		return errors.Join(err, rollbackErr)
	}
	return nil
}
