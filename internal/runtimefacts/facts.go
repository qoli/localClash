package runtimefacts

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"localclash/internal/corerun"
	"localclash/internal/mihomoapi"
	"localclash/internal/runtimeprofile"
	"localclash/internal/runtimesupervision"

	"gopkg.in/yaml.v3"
)

const SchemaVersion = 1

type Options struct {
	RuntimeProfile string
	ConfigPath     string
	RuntimeDir     string
	LogPath        string
}

type Facts struct {
	SchemaVersion   int    `json:"schema_version"`
	ProfileMode     string `json:"profile_mode"`
	RuntimeRunning  bool   `json:"runtime_running"`
	PID             int    `json:"pid,omitempty"`
	ControllerReady bool   `json:"controller_ready"`
	ControllerError string `json:"controller_error,omitempty"`
	ConfigSHA256    string `json:"config_sha256"`
	DNSListen       string `json:"dns_listen,omitempty"`
	DNSPort         int    `json:"dns_port,omitempty"`
	RedirPort       int    `json:"redir_port,omitempty"`
	TProxyPort      int    `json:"tproxy_port,omitempty"`
	TunEnabled      bool   `json:"tun_enabled"`
	TunDevice       string `json:"tun_device,omitempty"`
	TunAutoRoute    bool   `json:"tun_auto_route"`
	TunAutoRedirect bool   `json:"tun_auto_redirect"`
	IPv6            bool   `json:"ipv6"`
}

func Read(ctx context.Context, opts Options) (Facts, error) {
	if strings.TrimSpace(opts.RuntimeProfile) == "" {
		opts.RuntimeProfile = runtimeprofile.DefaultPath
	}
	if _, err := os.Stat(opts.RuntimeProfile); err != nil {
		return Facts{}, fmt.Errorf("runtime facts require runtime profile %q: %w", opts.RuntimeProfile, err)
	}
	file, _, _, err := runtimeprofile.ActiveProfile(opts.RuntimeProfile)
	if err != nil {
		return Facts{}, err
	}
	configPath := strings.TrimSpace(opts.ConfigPath)
	if configPath == "" {
		return Facts{}, fmt.Errorf("runtime facts require generated config path")
	}
	configData, err := os.ReadFile(configPath)
	if err != nil {
		return Facts{}, fmt.Errorf("runtime facts require generated config %q: %w", configPath, err)
	}
	var mihomo map[string]any
	if err := yaml.Unmarshal(configData, &mihomo); err != nil {
		return Facts{}, fmt.Errorf("runtime facts parse generated config %q: %w", configPath, err)
	}
	if mihomo == nil {
		return Facts{}, fmt.Errorf("runtime facts generated config %q is empty", configPath)
	}
	configSHA256, err := runtimesupervision.HashFile(configPath)
	if err != nil {
		return Facts{}, fmt.Errorf("runtime facts hash generated config %q: %w", configPath, err)
	}
	runtime := corerun.Status(corerun.StatusOptions{
		ConfigPath: configPath,
		WorkDir:    opts.RuntimeDir,
		LogPath:    opts.LogPath,
	})
	facts := Facts{
		SchemaVersion:  SchemaVersion,
		ProfileMode:    file.Mode,
		RuntimeRunning: runtime.Running,
		PID:            runtime.PID,
		ConfigSHA256:   configSHA256,
	}
	if value, ok, valueErr := configInt(mihomo, "redir-port"); valueErr != nil {
		return Facts{}, valueErr
	} else if ok {
		facts.RedirPort = value
	}
	if value, ok, valueErr := configInt(mihomo, "tproxy-port"); valueErr != nil {
		return Facts{}, valueErr
	} else if ok {
		facts.TProxyPort = value
	}
	if value, ok, valueErr := configBool(mihomo, "ipv6"); valueErr != nil {
		return Facts{}, valueErr
	} else if ok {
		facts.IPv6 = value
	}
	if dns, ok, valueErr := configMap(mihomo, "dns"); valueErr != nil {
		return Facts{}, valueErr
	} else if ok {
		if listen, ok, listenErr := configString(dns, "listen"); listenErr != nil {
			return Facts{}, listenErr
		} else if ok {
			facts.DNSListen = listen
			port, err := listenPort(listen)
			if err != nil {
				return Facts{}, fmt.Errorf("runtime facts dns.listen: %w", err)
			}
			facts.DNSPort = port
		}
	}
	if tun, ok, valueErr := configMap(mihomo, "tun"); valueErr != nil {
		return Facts{}, valueErr
	} else if ok {
		if facts.TunEnabled, _, valueErr = configBool(tun, "enable"); valueErr != nil {
			return Facts{}, valueErr
		}
		if facts.TunDevice, _, valueErr = configString(tun, "device"); valueErr != nil {
			return Facts{}, valueErr
		}
		if facts.TunAutoRoute, _, valueErr = configBool(tun, "auto-route"); valueErr != nil {
			return Facts{}, valueErr
		}
		if facts.TunAutoRedirect, _, valueErr = configBool(tun, "auto-redirect"); valueErr != nil {
			return Facts{}, valueErr
		}
	}
	if runtime.Running {
		client, clientErr := mihomoapi.NewFromConfig(configPath)
		if clientErr != nil {
			facts.ControllerError = clientErr.Error()
		} else {
			_, probeErr := client.Request(ctx, mihomoapi.RequestOptions{Method: "GET", Path: "/version", Timeout: 2 * time.Second, MaxBytes: 64 * 1024})
			if probeErr != nil {
				facts.ControllerError = probeErr.Error()
			} else {
				facts.ControllerReady = true
			}
		}
	}
	return facts, nil
}

func configMap(values map[string]any, key string) (map[string]any, bool, error) {
	value, ok := values[key]
	if !ok {
		return nil, false, nil
	}
	typed, ok := value.(map[string]any)
	if !ok {
		return nil, false, fmt.Errorf("runtime facts config field %s must be an object", key)
	}
	return typed, true, nil
}

func configString(values map[string]any, key string) (string, bool, error) {
	value, ok := values[key]
	if !ok {
		return "", false, nil
	}
	typed, ok := value.(string)
	if !ok {
		return "", false, fmt.Errorf("runtime facts config field %s must be a string", key)
	}
	return strings.TrimSpace(typed), true, nil
}

func configBool(values map[string]any, key string) (bool, bool, error) {
	value, ok := values[key]
	if !ok {
		return false, false, nil
	}
	typed, ok := value.(bool)
	if !ok {
		return false, false, fmt.Errorf("runtime facts config field %s must be a boolean", key)
	}
	return typed, true, nil
}

func configInt(values map[string]any, key string) (int, bool, error) {
	value, ok := values[key]
	if !ok {
		return 0, false, nil
	}
	switch typed := value.(type) {
	case int:
		return typed, true, nil
	case int64:
		return int(typed), true, nil
	case float64:
		if typed != float64(int(typed)) {
			return 0, false, fmt.Errorf("runtime facts config field %s must be an integer", key)
		}
		return int(typed), true, nil
	default:
		return 0, false, fmt.Errorf("runtime facts config field %s must be an integer", key)
	}
}

func listenPort(listen string) (int, error) {
	_, portText, err := net.SplitHostPort(strings.TrimSpace(listen))
	if err != nil {
		return 0, fmt.Errorf("invalid listen address %q: %w", listen, err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("invalid listen port %q", portText)
	}
	return port, nil
}
