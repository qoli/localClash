package poolcontract

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const autoQualificationVersion = 3

type providerDocument struct {
	Proxies              []map[string]any `json:"proxies"`
	QualificationVersion int              `json:"x-localclash-qualification-version,omitempty"`
}

func PreparedProvider(pool Pool, path string) (bool, error) {
	if pool.Capability != AutoCapability {
		_, err := os.Stat(path)
		return err == nil, err
	}
	document, err := readProvider(path)
	return document.QualificationVersion == autoQualificationVersion && len(document.Proxies) > 0, err
}

// ReadProviderProxies reads definitions from a previously written pool.
func ReadProviderProxies(path string) ([]map[string]any, error) {
	document, err := readProvider(path)
	return document.Proxies, err
}

func readProvider(path string) (providerDocument, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return providerDocument{}, err
	}
	var document providerDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return providerDocument{}, nil
	}
	return document, nil
}

func ProviderData(pool Pool, input Input, qualified []string) ([]byte, []string, error) {
	known := make(map[string]bool, len(input.Proxies))
	for _, proxy := range input.Proxies {
		name, _ := proxy["name"].(string)
		known[strings.TrimSpace(name)] = true
	}
	var names []string
	var proxies []map[string]any
	seen := make(map[string]bool, len(qualified))
	for _, name := range qualified {
		if !known[name] || seen[name] || name == RejectName {
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
		names = []string{RejectName}
		proxies = []map[string]any{Reject()}
	}
	document := providerDocument{Proxies: proxies}
	if pool.Capability == AutoCapability {
		document.QualificationVersion = autoQualificationVersion
	}
	data, err := json.Marshal(document)
	return data, names, err
}

func WriteFile(path string, data []byte) error {
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
