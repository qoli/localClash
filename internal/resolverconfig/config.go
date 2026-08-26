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
	version     = 2
	defaultName = "dnsqualify.json"
)

var errExpired = errors.New("resolver qualification expired")

type Status struct {
	Enabled     bool   `json:"enabled"`
	State       string `json:"state"`
	Reason      string `json:"reason,omitempty"`
	Detail      string `json:"detail,omitempty"`
	PolicyCount int    `json:"policy_count,omitempty"`
	ExpiresAt   string `json:"expires_at,omitempty"`
}

type loadResult struct {
	config *config
	Status Status
}

type config struct {
	Version          int                 `json:"version"`
	ExpiresAt        string              `json:"expires_at"`
	NameserverPolicy map[string][]string `json:"nameserver_policy"`
}

func DefaultPath(runtimeProfilePath string) string {
	return filepath.Join(filepath.Dir(runtimeProfilePath), defaultName)
}

// ApplyOptional is the resolver overlay seam. Rejected input disables only the
// optional overlay and never mutates the baseline Mihomo configuration.
func ApplyOptional(path string, mihomo map[string]any) Status {
	loaded, err := load(path)
	if err != nil {
		return rejected(err)
	}
	if loaded.config == nil {
		return loaded.Status
	}
	if err := apply(mihomo, *loaded.config); err != nil {
		return rejected(fmt.Errorf("apply resolver overlay %s: %w", path, err))
	}
	loaded.Status.PolicyCount = len(loaded.config.NameserverPolicy)
	return loaded.Status
}

func rejected(err error) Status {
	return Status{State: "disabled", Reason: "rejected", Detail: err.Error()}
}

func load(path string) (loadResult, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return loadResult{Status: Status{State: "disabled", Reason: "missing"}}, nil
	}
	if err != nil {
		return loadResult{}, fmt.Errorf("read resolver config %s: %w", path, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var candidate config
	if err := decoder.Decode(&candidate); err != nil {
		return loadResult{}, fmt.Errorf("decode resolver config %s: %w", path, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return loadResult{}, fmt.Errorf("decode resolver config %s: unexpected trailing JSON value", path)
		}
		return loadResult{}, fmt.Errorf("decode resolver config %s trailing data: %w", path, err)
	}
	if err := validateAt(candidate, time.Now()); err != nil {
		if errors.Is(err, errExpired) {
			return loadResult{Status: Status{State: "disabled", Reason: "expired", ExpiresAt: candidate.ExpiresAt}}, nil
		}
		return loadResult{}, fmt.Errorf("validate resolver config %s: %w", path, err)
	}
	return loadResult{config: &candidate, Status: Status{Enabled: true, State: "active", ExpiresAt: candidate.ExpiresAt}}, nil
}

func validate(candidate config) error {
	return validateAt(candidate, time.Now())
}

func validateAt(candidate config, now time.Time) error {
	if candidate.Version != version {
		return fmt.Errorf("unsupported resolver config version %d; want %d", candidate.Version, version)
	}
	if len(candidate.NameserverPolicy) == 0 {
		return errors.New("resolver nameserver_policy is required")
	}
	expires, err := time.Parse(time.RFC3339Nano, candidate.ExpiresAt)
	if err != nil {
		return fmt.Errorf("invalid expires_at: %w", err)
	}
	if !now.Before(expires) {
		return fmt.Errorf("%w at %s", errExpired, expires.Format(time.RFC3339Nano))
	}
	return nil
}

func apply(mihomo map[string]any, candidate config) error {
	if err := validate(candidate); err != nil {
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
	for domain := range candidate.NameserverPolicy {
		if _, exists := policy[domain]; exists {
			return fmt.Errorf("resolver overlay nameserver-policy %q conflicts with existing policy", domain)
		}
	}
	for domain, servers := range candidate.NameserverPolicy {
		policy[domain] = append([]string(nil), servers...)
	}
	return nil
}
