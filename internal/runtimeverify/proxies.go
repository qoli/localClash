// Package runtimeverify compares controller readback with generated materials.
package runtimeverify

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sort"

	"localclash/internal/mihomoapi"
	"localclash/internal/runtimesupervision"

	"gopkg.in/yaml.v3"
)

type ProxyResult struct {
	Verified     bool     `json:"verified"`
	ConfigSHA256 string   `json:"config_sha256"`
	Expected     int      `json:"expected"`
	Loaded       int      `json:"loaded"`
	Missing      []string `json:"missing"`
	Unexpected   []string `json:"unexpected"`
}

// Proxies checks both additions and removals, ignoring only Mihomo's built-ins
// and nodes read back from providers explicitly declared in this config. It
// returns names and counts, never proxy credentials or the controller payload.
func Proxies(ctx context.Context, configPath string) (ProxyResult, error) {
	result := ProxyResult{Missing: []string{}, Unexpected: []string{}}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return result, err
	}
	var config struct {
		Proxies   []struct{ Name string } `yaml:"proxies"`
		Groups    []struct{ Name string } `yaml:"proxy-groups"`
		Providers map[string]any          `yaml:"proxy-providers"`
	}
	if err := yaml.Unmarshal(data, &config); err != nil {
		return result, fmt.Errorf("parse generated config for runtime verification: %w", err)
	}
	expected := map[string]bool{}
	for _, proxy := range config.Proxies {
		expected[proxy.Name] = true
	}
	for _, group := range config.Groups {
		expected[group.Name] = true
	}
	if len(expected) == 0 || expected[""] {
		return result, errors.New("generated config has no valid proxy names for runtime verification")
	}
	result.Expected = len(expected)
	sum := sha256.Sum256(data)
	result.ConfigSHA256 = hex.EncodeToString(sum[:])
	client, err := mihomoapi.NewFromConfig(configPath)
	if err != nil {
		return result, err
	}
	read := func(path string, target any) error {
		if err := client.ReadJSON(ctx, path, target); err != nil {
			return fmt.Errorf("read controller %s: %w", path, err)
		}
		return nil
	}
	var actual struct {
		Proxies map[string]struct{ Name string } `json:"proxies"`
	}
	if err := read("/proxies", &actual); err != nil {
		return result, err
	}
	if len(actual.Proxies) == 0 {
		return result, errors.New("controller /proxies returned no proxies")
	}
	allowed := map[string]bool{"DIRECT": true, "REJECT": true, "REJECT-DROP": true, "COMPATIBLE": true, "PASS": true, "PASS-RULE": true, "GLOBAL": true}
	if len(config.Providers) > 0 {
		var providers struct {
			Providers map[string]struct {
				Proxies []struct{ Name string } `json:"proxies"`
			} `json:"providers"`
		}
		if err := read("/providers/proxies", &providers); err != nil {
			return result, err
		}
		for name := range config.Providers {
			provider, ok := providers.Providers[name]
			if !ok {
				return result, fmt.Errorf("controller is missing configured proxy provider %q", name)
			}
			for _, proxy := range provider.Proxies {
				allowed[proxy.Name] = true
			}
		}
	}
	for name := range expected {
		if proxy, ok := actual.Proxies[name]; !ok || proxy.Name != name {
			result.Missing = append(result.Missing, name)
		}
	}
	for name := range actual.Proxies {
		if !expected[name] && !allowed[name] {
			result.Unexpected = append(result.Unexpected, name)
		}
	}
	result.Loaded = len(actual.Proxies)
	sort.Strings(result.Missing)
	sort.Strings(result.Unexpected)
	if len(result.Missing) > 0 || len(result.Unexpected) > 0 {
		return result, errors.New("loaded subscription nodes or groups do not match the generated config")
	}
	after, err := runtimesupervision.HashFile(configPath)
	if err != nil || after != result.ConfigSHA256 {
		return result, errors.New("generated config changed during runtime verification")
	}
	result.Verified = true
	return result, nil
}
