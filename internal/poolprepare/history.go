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
	WorstDoHNS          int64 `json:"worst_doh_ns,omitempty"`
	ConsecutiveFailures int   `json:"consecutive_failures,omitempty"`
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

func (observation autoObservation) afterProbe(worstDoH time.Duration) autoObservation {
	if worstDoH > 0 {
		return autoObservation{WorstDoHNS: worstDoH.Nanoseconds()}
	}
	observation.ConsecutiveFailures++
	return observation
}
