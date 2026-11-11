// Package poolcontract defines the on-disk interface between the renderer and the pool worker.
package poolcontract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"runtime"
)

const (
	AutoCapability       = "network.doh.qualified.v1"
	ChatGPTCapability    = "openai.chatgpt.oauth_statsig.v1"
	AutoLimit            = 50
	AutoHTTPSProbeURL    = "https://cp.cloudflare.com/generate_204"
	AutoHTTPSProbeStatus = 204
	RevisionKey          = "x-localclash-pool-revision"
	DisabledKey          = "x-localclash-pools-disabled"
	Directory            = "qualified-pools"
	RejectName           = "localClash 无合格出口"
)

type Pool struct {
	Capability string
	Provider   string
}

func Lookup(capability string) (Pool, bool) {
	switch capability {
	case AutoCapability:
		return Pool{capability, "localclash-qualified-auto"}, true
	case ChatGPTCapability:
		return Pool{capability, "localclash-qualified-chatgpt"}, true
	default:
		return Pool{}, false
	}
}

func (pool Pool) Path(revision string) string {
	return filepath.Join(Directory, revision, pool.Provider+".yaml")
}

func WorkerPath(workspace string) string {
	return filepath.Join(workspace, "bin", runtime.GOOS+"-"+runtime.GOARCH, "localclash-pools")
}

type Input struct {
	Version  int              `yaml:"version"`
	Revision string           `yaml:"revision"`
	Proxies  []map[string]any `yaml:"proxies"`
}

func NewInput(proxies []map[string]any) (Input, error) {
	if len(proxies) == 0 {
		return Input{}, fmt.Errorf("pool input has no subscription proxies")
	}
	data, err := json.Marshal(proxies)
	if err != nil {
		return Input{}, fmt.Errorf("encode pool input: %w", err)
	}
	digest := sha256.Sum256(data)
	return Input{Version: 1, Revision: hex.EncodeToString(digest[:]), Proxies: proxies}, nil
}

func FromSubscription(doc map[string]any) (Input, error) {
	raw, _ := doc["proxies"].([]any)
	proxies := make([]map[string]any, 0, len(raw))
	for i, value := range raw {
		proxy, ok := value.(map[string]any)
		if !ok {
			return Input{}, fmt.Errorf("pool input proxy %d is invalid", i+1)
		}
		proxies = append(proxies, proxy)
	}
	return NewInput(proxies)
}

func Reject() map[string]any {
	return map[string]any{"name": RejectName, "type": "reject"}
}
