package poolprepare

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"localclash/internal/poolcontract"
)

const autoHistoryVersion = 1

type autoObservation struct {
	WorstDoHNS          int64                `json:"worst_doh_ns,omitempty"`
	ConsecutiveFailures int                  `json:"consecutive_failures,omitempty"`
	YouTube             *youtubeObservation  `json:"youtube,omitempty"`
	Enhanced            *enhancedObservation `json:"enhanced,omitempty"`
}

type youtubeObservation struct {
	Node       string    `json:"node"`
	CheckedAt  time.Time `json:"checked_at"`
	DurationMS int64     `json:"duration_ms"`
	HTTPStatus int       `json:"http_status,omitempty"`
	Error      string    `json:"error,omitempty"`
}

type autoHistory struct {
	Version int                        `json:"version"`
	Nodes   map[string]autoObservation `json:"nodes"`
}

func readAutoHistory(runtimeDir string) (map[string]autoObservation, error) {
	observations := make(map[string]autoObservation)
	if runtimeDir == "" {
		return observations, nil
	}
	path := autoHistoryPath(runtimeDir)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return observations, nil
	}
	if err != nil {
		return observations, fmt.Errorf("read automatic probe history: %w", err)
	}
	var saved autoHistory
	if err := json.Unmarshal(data, &saved); err != nil {
		return observations, fmt.Errorf("decode automatic probe history: %w", err)
	}
	if saved.Version != autoHistoryVersion {
		return observations, fmt.Errorf("unsupported automatic probe history version %d", saved.Version)
	}
	for fingerprint, observation := range saved.Nodes {
		if observation.WorstDoHNS >= 0 && observation.ConsecutiveFailures >= 0 &&
			(observation.WorstDoHNS > 0 || observation.ConsecutiveFailures > 0) {
			observations[fingerprint] = observation
		}
	}
	return observations, nil
}

func writeAutoHistory(runtimeDir string, candidates []autoCandidate, observations map[string]autoObservation) error {
	current := make(map[string]autoObservation)
	for _, candidate := range candidates {
		if observation, ok := observations[candidate.fingerprint]; ok {
			current[candidate.fingerprint] = observation
		}
	}
	data, err := json.Marshal(autoHistory{Version: autoHistoryVersion, Nodes: current})
	if err != nil {
		return err
	}
	if err := poolcontract.WriteFile(autoHistoryPath(runtimeDir), data); err != nil {
		return fmt.Errorf("write automatic probe history: %w", err)
	}
	return nil
}

func autoHistoryPath(runtimeDir string) string {
	return filepath.Join(runtimeDir, poolcontract.Directory, "auto-probe-history.json")
}

func (observation autoObservation) afterProbe(result measurement) autoObservation {
	if result.youtube != nil {
		observation.YouTube = result.youtube
	}
	if result.enhanced != nil {
		observation.Enhanced = result.enhanced
	}
	if result.worstDoH > 0 {
		observation.WorstDoHNS = result.worstDoH.Nanoseconds()
	}
	if result.qualified() {
		observation.ConsecutiveFailures = 0
	} else {
		observation.ConsecutiveFailures++
	}
	return observation
}
