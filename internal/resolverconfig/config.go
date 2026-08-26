package resolverconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

const (
	Version        = 2
	DefaultName    = "dnsqualify.json"
	ModeDNSQualify = "dnsqualify_overlay"
)

var ErrExpired = errors.New("resolver qualification expired")

type Status struct {
	Enabled   bool   `json:"enabled"`
	State     string `json:"state"`
	Reason    string `json:"reason,omitempty"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

type LoadResult struct {
	Config *Config
	Status Status
}

type Config struct {
	Version          int                 `json:"version"`
	ExpiresAt        string              `json:"expires_at"`
	NameserverPolicy map[string][]string `json:"nameserver_policy"`
}

func DefaultPath(runtimeProfilePath string) string {
	return filepath.Join(filepath.Dir(runtimeProfilePath), DefaultName)
}

// Load treats a missing file as the optional optimization being disabled. An
// existing file is authoritative and must satisfy the complete v2 contract.
func Load(path string) (LoadResult, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return LoadResult{Status: Status{State: "disabled", Reason: "missing"}}, nil
	}
	if err != nil {
		return LoadResult{}, fmt.Errorf("read resolver config %s: %w", path, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var config Config
	if err := decoder.Decode(&config); err != nil {
		return LoadResult{}, fmt.Errorf("decode resolver config %s: %w", path, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return LoadResult{}, fmt.Errorf("decode resolver config %s: unexpected trailing JSON value", path)
		}
		return LoadResult{}, fmt.Errorf("decode resolver config %s trailing data: %w", path, err)
	}
	if err := ValidateAt(config, time.Now()); err != nil {
		if errors.Is(err, ErrExpired) {
			return LoadResult{Status: Status{State: "disabled", Reason: "expired", ExpiresAt: config.ExpiresAt}}, nil
		}
		return LoadResult{}, fmt.Errorf("validate resolver config %s: %w", path, err)
	}
	return LoadResult{Config: &config, Status: Status{Enabled: true, State: "active", ExpiresAt: config.ExpiresAt}}, nil
}

func Validate(config Config) error {
	return ValidateAt(config, time.Now())
}

func ValidateAt(config Config, now time.Time) error {
	if config.Version != Version {
		return fmt.Errorf("unsupported resolver config version %d; want %d", config.Version, Version)
	}
	if len(config.NameserverPolicy) == 0 {
		return errors.New("resolver nameserver_policy is required")
	}
	expires, err := time.Parse(time.RFC3339Nano, config.ExpiresAt)
	if err != nil {
		return fmt.Errorf("invalid expires_at: %w", err)
	}
	if !now.Before(expires) {
		return fmt.Errorf("%w at %s", ErrExpired, expires.Format(time.RFC3339Nano))
	}
	return nil
}

func Apply(mihomo map[string]any, config Config) error {
	if err := Validate(config); err != nil {
		return err
	}
	dns, ok := mihomo["dns"].(map[string]any)
	if !ok {
		return errors.New("runtime profile DNS configuration is missing or invalid")
	}
	policy, ok := dns["nameserver-policy"].(map[string]any)
	if !ok {
		return errors.New("runtime profile nameserver-policy is missing or invalid")
	}
	for domain := range config.NameserverPolicy {
		if _, exists := policy[domain]; exists {
			return fmt.Errorf("dnsqualify nameserver-policy %q conflicts with existing policy", domain)
		}
	}
	for domain, servers := range config.NameserverPolicy {
		policy[domain] = append([]string(nil), servers...)
	}
	return nil
}
