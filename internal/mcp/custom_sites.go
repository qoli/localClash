package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"localclash/internal/customsites"
	"localclash/internal/customsitesapply"
)

type customSitesEnvelope struct {
	OK          bool                        `json:"ok"`
	Changed     bool                        `json:"changed"`
	Summary     string                      `json:"summary"`
	CustomSites customsites.Snapshot        `json:"custom_sites"`
	Apply       *customSitesApplyProjection `json:"apply,omitempty"`
	Operation   string                      `json:"operation,omitempty"`
	Entry       *customSiteEntryProjection  `json:"entry,omitempty"`
	Changes     []string                    `json:"changes"`
	Warnings    []string                    `json:"warnings"`
	NextActions []string                    `json:"next_actions"`
	Code        string                      `json:"code,omitempty"`
	Error       string                      `json:"error,omitempty"`
}

type customSitesApplyProjection struct {
	Validated        bool   `json:"validated"`
	Promoted         bool   `json:"promoted"`
	RuntimeRunning   bool   `json:"runtime_running"`
	Reloaded         bool   `json:"reloaded"`
	ReadBack         bool   `json:"read_back"`
	Effective        bool   `json:"effective"`
	PendingNextStart bool   `json:"pending_next_start"`
	RolledBack       bool   `json:"rolled_back"`
	ConfigSHA256     string `json:"config_sha256,omitempty"`
}

type customSiteEntryProjection struct {
	ID       string `json:"id"`
	Match    string `json:"match"`
	Pattern  string `json:"pattern"`
	Sequence uint64 `json:"sequence"`
	AddedAt  string `json:"added_at"`
	Route    string `json:"route"`
}

func (s *Server) callCustomSitesList(args json.RawMessage) (toolResult, error) {
	var in struct{}
	if err := decodeStrictToolInput(args, &in); err != nil {
		return toolResult{}, err
	}
	paths, err := s.customSitePaths()
	if err != nil {
		return toolResult{}, err
	}
	pair, err := customsites.Load(paths)
	if err != nil {
		return toolResult{}, err
	}
	snapshot, err := customsites.SnapshotChecked(pair)
	if err != nil {
		return toolResult{}, err
	}
	return jsonToolResult(customSitesEnvelope{
		OK:          true,
		Summary:     "Durable custom-site routing list read; this layer coexists with and survives synchronization of default policy patches.",
		CustomSites: snapshot,
		Changes:     []string{},
		Warnings:    []string{},
		NextActions: []string{},
	})
}

func (s *Server) callCustomSitesTransact(ctx context.Context, args json.RawMessage) (toolResult, error) {
	var in struct {
		Version    int    `json:"version"`
		Operation  string `json:"operation"`
		Pattern    string `json:"pattern"`
		Route      string `json:"route"`
		ID         string `json:"id"`
		Background *bool  `json:"background"`
		Wait       *bool  `json:"wait"`
	}
	if err := decodeStrictToolInput(args, &in); err != nil {
		return toolResult{}, err
	}
	if s == nil || s.state == nil {
		return toolResult{}, errors.New("custom-site MCP tools require initialized server runtime state")
	}
	if _, err := s.customSitePaths(); err != nil {
		return toolResult{}, err
	}
	transactionCtx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()
	result, err := customsitesapply.TransactRuntime(transactionCtx, *s.state, customsitesapply.TransactionInput{
		Version:   in.Version,
		Operation: strings.TrimSpace(in.Operation),
		Pattern:   in.Pattern,
		Route:     in.Route,
		ID:        in.ID,
	}, func(stage, message string) {
		appendTaskStage(transactionCtx, "custom_sites_progress", stage, map[string]any{"message": message})
	})
	if err != nil {
		failure := customSitesEnvelope{
			OK:          false,
			Summary:     "Custom-site routing transaction failed.",
			CustomSites: result.Snapshot,
			Apply:       projectCustomSitesApply(result.Apply),
			Operation:   result.Operation,
			Entry:       projectCustomSiteEntry(result.Entry),
			Changes:     []string{},
			Warnings:    []string{},
			NextActions: []string{"Inspect apply and custom_sites; existing files remain authoritative unless rolled_back is false."},
			Code:        "custom_sites_transaction_failed",
			Error:       err.Error(),
		}
		return jsonErrorToolResult(failure)
	}
	summary := "Custom-site routing saved in the durable layer shared with LuCI; the runtime is stopped, so it will become effective on the next start."
	if result.Apply.Effective {
		summary = "Custom-site routing saved in the durable layer shared with LuCI and semantically read back from the active runtime."
	}
	return jsonToolResult(customSitesEnvelope{
		OK:          true,
		Changed:     true,
		Summary:     summary,
		CustomSites: result.Snapshot,
		Apply:       projectCustomSitesApply(result.Apply),
		Operation:   result.Operation,
		Entry:       projectCustomSiteEntry(result.Entry),
		Changes:     []string{"custom_sites_updated", "config_rendered"},
		Warnings:    []string{},
		NextActions: []string{},
	})
}

func (s *Server) customSitePaths() (customsites.Paths, error) {
	if s == nil || s.state == nil {
		return customsites.Paths{}, errors.New("custom-site MCP tools require initialized server runtime state")
	}
	paths := customsites.Paths{
		Proxy:  strings.TrimSpace(s.state.Paths.CustomSitesProxy),
		Direct: strings.TrimSpace(s.state.Paths.CustomSitesDirect),
	}
	if paths.Proxy == "" || paths.Direct == "" {
		return customsites.Paths{}, errors.New("custom-site paths are unavailable in server runtime state")
	}
	return paths, nil
}

func projectCustomSitesApply(status customsitesapply.ApplyStatus) *customSitesApplyProjection {
	return &customSitesApplyProjection{
		Validated:        status.Validated,
		Promoted:         status.Promoted,
		RuntimeRunning:   status.RuntimeRunning,
		Reloaded:         status.Reloaded,
		ReadBack:         status.ReadBack,
		Effective:        status.Effective,
		PendingNextStart: status.PendingNextStart,
		RolledBack:       status.RolledBack,
		ConfigSHA256:     status.ConfigSHA256,
	}
}

func projectCustomSiteEntry(entry customsites.Entry) *customSiteEntryProjection {
	if entry.ID == "" && entry.Pattern == "" && entry.Sequence == 0 {
		return nil
	}
	return &customSiteEntryProjection{
		ID:       entry.ID,
		Match:    entry.Match,
		Pattern:  entry.Pattern,
		Sequence: entry.Sequence,
		AddedAt:  entry.AddedAt,
		Route:    entry.Route,
	}
}

func jsonErrorToolResult(value any) (toolResult, error) {
	result, err := jsonToolResult(value)
	if err != nil {
		return toolResult{}, err
	}
	result.IsError = true
	return result, nil
}
