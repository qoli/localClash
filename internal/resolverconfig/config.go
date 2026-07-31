package resolverconfig

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	Version               = 1
	ScopeMainlandServices = "geosite:cn"
	DefaultName           = "dnsqualify.json"
	ModeDNSQualify        = "dnsqualify"
)

type Config struct {
	Version     int         `json:"version"`
	Scope       string      `json:"scope"`
	Resolver    Resolver    `json:"resolver"`
	Measurement Measurement `json:"measurement"`
}

type Resolver struct {
	CandidateID string `json:"candidate_id"`
	Source      string `json:"source"`
	Transport   string `json:"transport"`
	Endpoint    string `json:"endpoint"`
	Interface   string `json:"interface,omitempty"`
}

type Measurement struct {
	ReportSHA256     string `json:"report_sha256"`
	ReportFinishedAt string `json:"report_finished_at"`
	ResolvPath       string `json:"resolv_path"`
	GeneratedAt      string `json:"generated_at"`
}

func DefaultPath(runtimeProfilePath string) string {
	return filepath.Join(filepath.Dir(runtimeProfilePath), DefaultName)
}

// Load treats a missing file as the optional feature being disabled. Once a
// file exists, every field is strict and malformed state fails explicitly.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read resolver config %s: %w", path, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var config Config
	if err := decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("decode resolver config %s: %w", path, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, fmt.Errorf("decode resolver config %s: unexpected trailing JSON value", path)
		}
		return nil, fmt.Errorf("decode resolver config %s trailing data: %w", path, err)
	}
	if err := Validate(config); err != nil {
		return nil, fmt.Errorf("validate resolver config %s: %w", path, err)
	}
	return &config, nil
}

func Validate(config Config) error {
	if config.Version != Version || config.Scope != ScopeMainlandServices {
		return errors.New("unsupported resolver config contract")
	}
	if strings.TrimSpace(config.Resolver.CandidateID) == "" || strings.TrimSpace(config.Resolver.Source) == "" {
		return errors.New("resolver candidate_id and source are required")
	}
	switch config.Resolver.Transport {
	case "udp":
		if net.ParseIP(config.Resolver.Endpoint) == nil {
			return errors.New("UDP resolver endpoint must be an IP address")
		}
	case "doh":
		if !strings.HasPrefix(config.Resolver.Endpoint, "https://") {
			return errors.New("DoH resolver endpoint must use https")
		}
	default:
		return fmt.Errorf("unsupported resolver transport %q", config.Resolver.Transport)
	}
	if len(config.Measurement.ReportSHA256) != sha256.Size*2 || strings.TrimSpace(config.Measurement.ResolvPath) == "" {
		return errors.New("resolver measurement provenance is incomplete")
	}
	if _, err := time.Parse(time.RFC3339Nano, config.Measurement.ReportFinishedAt); err != nil {
		return fmt.Errorf("invalid report_finished_at: %w", err)
	}
	if _, err := time.Parse(time.RFC3339Nano, config.Measurement.GeneratedAt); err != nil {
		return fmt.Errorf("invalid generated_at: %w", err)
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
	policy[ScopeMainlandServices] = []string{config.Resolver.Endpoint}
	return nil
}
