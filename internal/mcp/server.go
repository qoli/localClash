package mcp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"localclash/internal/appinit"
	"localclash/internal/configinspect"
	"localclash/internal/configplan"
	"localclash/internal/configrender"
	"localclash/internal/corerun"
	"localclash/internal/doctor"
	"localclash/internal/envinspect"
	"localclash/internal/fileops"
	"localclash/internal/localconfig"
	"localclash/internal/mihomotest"
	"localclash/internal/policytemplate"
	"localclash/internal/routertakeover"
	"localclash/internal/rules"
	"localclash/internal/runtimeprofile"
	"localclash/internal/subscriptions"
)

type Server struct {
	state      *appinit.RuntimeState
	startedAt  time.Time
	taskCtx    context.Context
	taskCancel context.CancelFunc
	taskWG     sync.WaitGroup
}

var routerTakeoverStatus = routertakeover.Status

func NewServer() *Server {
	return newServer(nil)
}

func NewServerWithState(state appinit.RuntimeState) *Server {
	return newServer(&state)
}

func newServer(state *appinit.RuntimeState) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	return &Server{
		state:      state,
		startedAt:  time.Now().UTC(),
		taskCtx:    ctx,
		taskCancel: cancel,
	}
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if s.taskCancel != nil {
		s.taskCancel()
	}
	done := make(chan struct{})
	go func() {
		s.taskWG.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Server) taskBaseContext() context.Context {
	if s != nil && s.taskCtx != nil {
		return s.taskCtx
	}
	return context.Background()
}

func (s *Server) runtimeInfo() ServerRuntimeInfo {
	startedAt := s.startedAt
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}
	info := ServerRuntimeInfo{
		StartedAt: startedAt.Format(time.RFC3339),
	}
	if wd, err := os.Getwd(); err == nil {
		info.WorkingDir = wd
	}
	exe, err := os.Executable()
	if err != nil {
		info.BinaryError = err.Error()
		return info
	}
	info.Binary = exe
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		info.Binary = resolved
	}
	sum, err := sha256File(info.Binary)
	if err != nil {
		info.BinaryError = err.Error()
		return info
	}
	info.BinarySHA256 = sum
	return info
}

func sha256File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type callParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type toolResult struct {
	Content           []toolContent `json:"content"`
	StructuredContent any           `json:"structuredContent,omitempty"`
	IsError           bool          `json:"isError,omitempty"`
}

type toolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type HTTPOptions struct {
	Addr string
	Path string
}

func NormalizeHTTPOptions(opts HTTPOptions) HTTPOptions {
	if strings.TrimSpace(opts.Addr) == "" {
		opts.Addr = "127.0.0.1:8765"
	}
	if strings.TrimSpace(opts.Path) == "" {
		opts.Path = "/mcp"
	}
	if !strings.HasPrefix(opts.Path, "/") {
		opts.Path = "/" + opts.Path
	}
	return opts
}

func HTTPURL(opts HTTPOptions) string {
	opts = NormalizeHTTPOptions(opts)
	return "http://" + opts.Addr + opts.Path
}

func ListenAndServeHTTPWithState(ctx context.Context, state appinit.RuntimeState, opts HTTPOptions) error {
	opts = NormalizeHTTPOptions(opts)
	mcpServer := NewServerWithState(state)
	srv := &http.Server{
		Addr:              opts.Addr,
		Handler:           mcpServer.HTTPHandler(opts.Path),
		ReadHeaderTimeout: 5 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		err := srv.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errCh <- err
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return err
		}
		if err := mcpServer.Shutdown(shutdownCtx); err != nil {
			return err
		}
		return ctx.Err()
	case err := <-errCh:
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if shutdownErr := mcpServer.Shutdown(shutdownCtx); shutdownErr != nil && err == nil {
			return shutdownErr
		}
		return err
	}
}

func (s *Server) HTTPHandler(path string) http.Handler {
	opts := NormalizeHTTPOptions(HTTPOptions{Path: path})
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeCORSHeaders(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	})
	mux.HandleFunc(opts.Path, func(w http.ResponseWriter, r *http.Request) {
		writeCORSHeaders(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		started := time.Now()
		defer r.Body.Close()
		var raw json.RawMessage
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&raw); err != nil {
			response := errorResponse(nil, -32700, "parse error")
			s.logHTTPMCPCall(r, rpcLogSummary{Method: "parse_error"}, http.StatusOK, response, time.Since(started))
			writeJSON(w, http.StatusOK, response)
			return
		}
		summary := summarizeRPCLog(raw)
		response := s.Handle(r.Context(), raw)
		if response == nil {
			s.logHTTPMCPCall(r, summary, http.StatusAccepted, nil, time.Since(started))
			w.WriteHeader(http.StatusAccepted)
			return
		}
		s.logHTTPMCPCall(r, summary, http.StatusOK, response, time.Since(started))
		writeJSON(w, http.StatusOK, response)
	})
	return mux
}

type rpcLogSummary struct {
	Method string
	Tool   string
	Args   string
}

func summarizeRPCLog(raw json.RawMessage) rpcLogSummary {
	var req rpcRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return rpcLogSummary{Method: "parse_error"}
	}
	out := rpcLogSummary{Method: req.Method}
	if req.Method != "tools/call" || len(req.Params) == 0 {
		return out
	}
	var call callParams
	if err := json.Unmarshal(req.Params, &call); err != nil {
		return out
	}
	out.Tool = call.Name
	if len(call.Arguments) != 0 {
		out.Args = summarizeLogArgs(call.Arguments)
	}
	return out
}

func summarizeLogArgs(raw json.RawMessage) string {
	var values map[string]any
	if err := json.Unmarshal(raw, &values); err != nil {
		return "invalid_json"
	}
	if len(values) == 0 {
		return "{}"
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+summarizeLogValue(key, values[key]))
	}
	return strings.Join(parts, ",")
}

func summarizeLogValue(key string, value any) string {
	if isSensitiveLogKey(key) {
		return "<redacted>"
	}
	switch v := value.(type) {
	case string:
		v = strings.ReplaceAll(v, "\n", `\n`)
		v = strings.ReplaceAll(v, "\r", `\r`)
		if len(v) > 80 {
			v = v[:77] + "..."
		}
		return strconv.Quote(v)
	case float64, bool, nil:
		return fmt.Sprint(v)
	default:
		return fmt.Sprintf("<%T>", value)
	}
}

func isSensitiveLogKey(key string) bool {
	lowered := strings.ToLower(key)
	for _, token := range []string{"url", "token", "secret", "password", "authorization", "cookie", "credential"} {
		if strings.Contains(lowered, token) {
			return true
		}
	}
	return false
}

func (s *Server) logHTTPMCPCall(r *http.Request, summary rpcLogSummary, httpStatus int, response *rpcResponse, duration time.Duration) {
	parts := []string{
		"mcp_http",
		"method=" + r.Method,
		"path=" + r.URL.Path,
		"rpc=" + safeLogToken(summary.Method),
	}
	if summary.Tool != "" {
		parts = append(parts, "tool="+safeLogToken(summary.Tool))
	}
	if summary.Args != "" {
		parts = append(parts, "args="+summary.Args)
	}
	parts = append(parts,
		fmt.Sprintf("http_status=%d", httpStatus),
		fmt.Sprintf("duration_ms=%d", duration.Milliseconds()),
	)
	record := map[string]any{
		"ts":          time.Now().UTC().Format(time.RFC3339),
		"event":       "mcp_http",
		"http_method": r.Method,
		"path":        r.URL.Path,
		"rpc":         summary.Method,
		"http_status": httpStatus,
		"duration_ms": duration.Milliseconds(),
	}
	if summary.Tool != "" {
		record["tool"] = summary.Tool
	}
	if summary.Args != "" {
		record["args"] = summary.Args
	}
	if response == nil {
		parts = append(parts, "response=notification")
		record["response"] = "notification"
	} else if response.Error != nil {
		parts = append(parts,
			fmt.Sprintf("error_code=%d", response.Error.Code),
			"error="+strconv.Quote(response.Error.Message),
		)
		record["error_code"] = response.Error.Code
		record["error"] = response.Error.Message
	} else {
		parts = append(parts, "response=ok")
		record["response"] = "ok"
	}
	fmt.Fprintln(os.Stderr, strings.Join(parts, " "))
	appendBoundedJSONLog(s.serviceLogPath("mcp-http.jsonl"), record, serviceLogMaxBytes())
}

func (s *Server) serviceLogPath(name string) string {
	if s.state != nil {
		runtimeDir := strings.TrimSpace(s.state.Paths.MihomoRuntimeDir)
		if runtimeDir != "" && runtimeDir != "." {
			parent := filepath.Dir(runtimeDir)
			if parent != "." && parent != "" {
				return filepath.Join(parent, "logs", name)
			}
		}
	}
	return filepath.Join(".runtime", "logs", name)
}

func safeLogToken(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	return strings.NewReplacer(" ", "_", "\n", "_", "\r", "_", "\t", "_").Replace(value)
}

func writeCORSHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, MCP-Protocol-Version, Mcp-Session-Id")
	w.Header().Set("Access-Control-Expose-Headers", "Mcp-Session-Id")
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func (s *Server) Handle(ctx context.Context, data []byte) *rpcResponse {
	var req rpcRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return errorResponse(nil, -32700, "parse error")
	}
	if len(req.ID) == 0 {
		return nil
	}
	switch req.Method {
	case "initialize":
		protocolVersion := "2025-06-18"
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if len(req.Params) != 0 && json.Unmarshal(req.Params, &params) == nil && params.ProtocolVersion != "" {
			protocolVersion = params.ProtocolVersion
		}
		return resultResponse(req.ID, map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities": map[string]any{
				"tools": map[string]any{
					"listChanged": false,
				},
			},
			"serverInfo": map[string]any{
				"name":    "localclash",
				"version": "dev",
			},
		})
	case "tools/list":
		return resultResponse(req.ID, map[string]any{"tools": ListedTools()})
	case "tools/call":
		result, err := s.callTool(ctx, req.Params)
		if err != nil {
			return errorResponse(req.ID, -32602, err.Error())
		}
		return resultResponse(req.ID, result)
	default:
		return errorResponse(req.ID, -32601, "method not found")
	}
}

func (s *Server) callTool(ctx context.Context, params json.RawMessage) (toolResult, error) {
	var call callParams
	if err := json.Unmarshal(params, &call); err != nil {
		return toolResult{}, err
	}
	if call.Name == "" {
		return toolResult{}, errors.New("tool name is required")
	}
	args := call.Arguments
	if len(args) == 0 {
		args = []byte("{}")
	}
	if !isJSONObject(args) {
		return toolResult{}, fmt.Errorf("tool arguments for %q must be a JSON object, not a JSON string or array; send \"arguments\":{\"field\":\"value\"}, not \"arguments\":\"{...}\"", call.Name)
	}
	switch call.Name {
	case "config_status":
		return s.callConfigStatus(args)
	case "config_configure":
		return s.callConfigConfigure(args)
	case "config_render":
		return s.callMaybeAsyncTool(ctx, "config_render", args, s.callConfigRender)
	case "config_patch_apply":
		return s.callMaybeAsyncTool(ctx, "config_patch_apply", args, s.callConfigPatchApply)
	case "config_patch_create":
		return s.callMaybeAsyncTool(ctx, "config_patch_create", args, s.callConfigPatchCreate)
	case "doctor":
		return s.callDoctor(ctx, args)
	case "environment_inspect":
		return s.callEnvironmentInspect(ctx, args)
	case "nl_file":
		return callNLFile(args)
	case "packs_list":
		return s.callPacksList(args)
	case "packs_get":
		return s.callPacksGet(args)
	case "pack_rules_read":
		return s.callPackRulesRead(ctx, args)
	case "pack_rules_prefetch":
		return s.callPackRulesPrefetch(ctx, args)
	case "pack_rules_query":
		return s.callPackRulesQuery(ctx, args)
	case "subscription_nodes_list":
		return s.callSubscriptionNodesList(args)
	case "subscription_nodes_search":
		return s.callSubscriptionNodesSearch(args)
	case "runtime_status":
		return s.callRuntimeStatus(args)
	case "runtime_profile_status":
		return s.callRuntimeProfileStatus(args)
	case "router_takeover_status":
		return s.callRouterTakeoverStatus(ctx, args)
	case "routing_explain":
		return s.runLoggedSyncTool(ctx, "routing_explain", args, s.callRoutingExplain)
	case "subscriptions_status":
		return s.callSubscriptionsStatus(args)
	case "tools_list":
		return jsonToolResult(ToolSummaries(s.runtimeInfo()))
	case "subscriptions_configure":
		return s.callSubscriptionsConfigure(args)
	case "subscriptions_refresh":
		return s.callMaybeAsyncTool(ctx, "subscriptions_refresh", args, s.callSubscriptionsRefresh)
	case "proxy_group_build":
		return s.callProxyGroupBuild(args)
	case "policy_group_build":
		return callPolicyGroupBuild(args)
	case "custom_rules_build":
		return callCustomRulesBuild(args)
	case "rule_provider_build":
		return callRuleProviderBuild(args)
	case "run_runtime":
		return s.callMaybeAsyncTool(ctx, "run_runtime", args, s.callRunRuntimeSync)
	case "restart_runtime":
		return s.callMaybeAsyncTool(ctx, "restart_runtime", args, s.callRestartRuntimeSync)
	case "router_takeover_apply":
		return s.callMaybeAsyncTool(ctx, "router_takeover_apply", args, s.callRouterTakeoverApplySync)
	case "router_takeover_stop":
		return s.callMaybeAsyncTool(ctx, "router_takeover_stop", args, s.callRouterTakeoverStopSync)
	case "sed_file":
		return callSedFile(args)
	case "stop_runtime":
		return s.callMaybeAsyncTool(ctx, "stop_runtime", args, s.callStopRuntimeSync)
	default:
		return toolResult{}, fmt.Errorf("unknown tool %q", call.Name)
	}
}

func isJSONObject(data json.RawMessage) bool {
	trimmed := bytes.TrimSpace(data)
	return len(trimmed) > 0 && trimmed[0] == '{'
}

func decodeToolInput(args json.RawMessage, out any) error {
	if len(bytes.TrimSpace(args)) == 0 {
		args = []byte("{}")
	}
	if !isJSONObject(args) {
		return fmt.Errorf("tool arguments must be a JSON object")
	}
	return json.Unmarshal(args, out)
}

func callNLFile(args json.RawMessage) (toolResult, error) {
	var in struct {
		Path       string `json:"path"`
		StartLine  int    `json:"start_line"`
		LimitLines int    `json:"limit_lines"`
		MaxBytes   int    `json:"max_bytes"`
	}
	if err := decodeToolInput(args, &in); err != nil {
		return toolResult{}, err
	}
	result, err := fileops.NLFile(fileops.NLFileOptions{
		Path:       in.Path,
		StartLine:  in.StartLine,
		LimitLines: in.LimitLines,
		MaxBytes:   in.MaxBytes,
	})
	if err != nil {
		return toolResult{}, err
	}
	return jsonToolResult(result)
}

func callSedFile(args json.RawMessage) (toolResult, error) {
	var in struct {
		Path           string         `json:"path"`
		DryRun         *bool          `json:"dry_run"`
		ExpectedSHA256 string         `json:"expected_sha256"`
		Edits          []fileops.Edit `json:"edits"`
	}
	if err := decodeToolInput(args, &in); err != nil {
		return toolResult{}, err
	}
	dryRun := true
	if in.DryRun != nil {
		dryRun = *in.DryRun
	}
	result, err := fileops.SedFile(fileops.SedFileOptions{
		Path:           in.Path,
		DryRun:         dryRun,
		ExpectedSHA256: in.ExpectedSHA256,
		Edits:          in.Edits,
	})
	if err != nil {
		return toolResult{}, err
	}
	return jsonToolResult(result)
}

func callConfigBaseInspect(args json.RawMessage) (toolResult, error) {
	var in struct {
		Config string `json:"config"`
		Limit  int    `json:"limit"`
	}
	if err := decodeToolInput(args, &in); err != nil {
		return toolResult{}, err
	}
	result, err := configinspect.InspectBase(configinspect.Options{ConfigPath: in.Config, Limit: in.Limit})
	if err != nil {
		return toolResult{}, err
	}
	return jsonToolResult(result)
}

func callConfigOverlayInspect(args json.RawMessage) (toolResult, error) {
	var in struct {
		Config string `json:"config"`
		Limit  int    `json:"limit"`
	}
	if err := decodeToolInput(args, &in); err != nil {
		return toolResult{}, err
	}
	result, err := configinspect.InspectOverlay(configinspect.Options{ConfigPath: in.Config, Limit: in.Limit})
	if err != nil {
		return toolResult{}, err
	}
	return jsonToolResult(result)
}

func (s *Server) callConfigIntentInspect(args json.RawMessage) (toolResult, error) {
	var in struct {
		View                string `json:"view"`
		Config              string `json:"config"`
		Subscription        string `json:"subscription"`
		SubscriptionConfig  string `json:"subscription_config"`
		SubscriptionRuntime string `json:"subscription_runtime"`
		RulesCache          string `json:"rules_cache"`
		RuntimeProfile      string `json:"runtime_profile"`
		Limit               int    `json:"limit"`
	}
	if err := decodeToolInput(args, &in); err != nil {
		return toolResult{}, err
	}
	view := strings.TrimSpace(in.View)
	if view == "" {
		view = "durable"
	}
	if view != "durable" && view != "working" && view != "effective_preview" {
		return toolResult{}, fmt.Errorf("unsupported config intent view %q", in.View)
	}
	s.applyConfigIntentInspectDefaults(&in.Subscription, &in.RulesCache, &in.RuntimeProfile, &in.SubscriptionConfig, &in.SubscriptionRuntime)
	result, err := configinspect.InspectIntent(configinspect.IntentOptions{
		ConfigPath:          in.Config,
		Subscription:        in.Subscription,
		SubscriptionConfig:  in.SubscriptionConfig,
		SubscriptionRuntime: in.SubscriptionRuntime,
		RulesCache:          in.RulesCache,
		Limit:               in.Limit,
	})
	if err != nil {
		return toolResult{}, err
	}
	if view == "durable" {
		return jsonToolResult(result)
	}
	return s.configIntentInspectWorkingResult(configIntentInspectWorkingInput{
		View:                view,
		Config:              in.Config,
		Subscription:        in.Subscription,
		SubscriptionConfig:  in.SubscriptionConfig,
		SubscriptionRuntime: in.SubscriptionRuntime,
		RulesCache:          in.RulesCache,
		RuntimeProfile:      in.RuntimeProfile,
		Limit:               in.Limit,
		Intent:              result,
	})
}

type configIntentInspectWorkingInput struct {
	View                string
	Config              string
	Subscription        string
	SubscriptionConfig  string
	SubscriptionRuntime string
	RulesCache          string
	RuntimeProfile      string
	Limit               int
	Intent              configinspect.IntentResult
}

func (s *Server) configIntentInspectWorkingResult(in configIntentInspectWorkingInput) (toolResult, error) {
	if in.Config == "" {
		in.Config = "localclash.json"
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	baseline := configrender.LocalBaselineRuleLines()
	result := configIntentInspectContextResult{
		View:                  in.View,
		SubscriptionAvailable: fileExists(in.Subscription),
		Inputs: configIntentInspectInputs{
			Subscription:        in.Subscription,
			RulesCache:          in.RulesCache,
			RuntimeProfilePath:  in.RuntimeProfile,
			LocalClashConfig:    in.Config,
			SubscriptionConfig:  in.SubscriptionConfig,
			SubscriptionRuntime: in.SubscriptionRuntime,
		},
		LocalSafetyBaseline: configIntentInspectRuleSlice{RuleCount: len(baseline), Rules: limitStrings(baseline, limit)},
		Intent:              in.Intent,
		NextActions:         []string{},
	}
	if status, err := runtimeprofile.StatusFor(in.RuntimeProfile); err == nil {
		result.RuntimeProfile = &status
	} else {
		result.Warnings = append(result.Warnings, "runtime profile unavailable: "+err.Error())
	}
	if in.View == "working" {
		if !result.SubscriptionAvailable {
			result.NextActions = append(result.NextActions, "call subscriptions_status", "call subscriptions_configure if no sources are configured", "call subscriptions_refresh to create subscription.gob before previewing effective rules")
		} else {
			result.NextActions = append(result.NextActions, "use view=effective_preview when the agent needs the effective generated rules without requiring Mihomo to have been started")
		}
		return jsonToolResult(result)
	}
	if !result.SubscriptionAvailable {
		result.NextActions = append(result.NextActions, "call subscriptions_status", "call subscriptions_configure if no sources are configured", "call subscriptions_refresh to create subscription.gob before previewing effective rules")
		return jsonToolResult(result)
	}
	if err := renderConfigIntentPreview(&result, in); err != nil {
		return toolResult{}, err
	}
	return jsonToolResult(result)
}

type configIntentInspectContextResult struct {
	View                  string                       `json:"view"`
	SubscriptionAvailable bool                         `json:"subscription_available"`
	PreviewRendered       bool                         `json:"preview_rendered"`
	PreviewSource         string                       `json:"preview_source,omitempty"`
	Inputs                configIntentInspectInputs    `json:"inputs"`
	RuntimeProfile        *runtimeprofile.Status       `json:"runtime_profile,omitempty"`
	LocalSafetyBaseline   configIntentInspectRuleSlice `json:"local_safety_baseline"`
	Intent                configinspect.IntentResult   `json:"intent"`
	Overlay               *configinspect.OverlayResult `json:"overlay,omitempty"`
	Effective             *configinspect.BaseSummary   `json:"effective_summary,omitempty"`
	Warnings              []string                     `json:"warnings,omitempty"`
	NextActions           []string                     `json:"next_actions,omitempty"`
}

type configIntentInspectInputs struct {
	Subscription        string `json:"subscription"`
	RulesCache          string `json:"rules_cache"`
	RuntimeProfilePath  string `json:"runtime_profile"`
	LocalClashConfig    string `json:"localclash_config"`
	SubscriptionConfig  string `json:"subscription_config"`
	SubscriptionRuntime string `json:"subscription_runtime"`
}

type configIntentInspectRuleSlice struct {
	RuleCount int      `json:"rule_count"`
	Rules     []string `json:"rules_sample"`
}

func renderConfigIntentPreview(result *configIntentInspectContextResult, in configIntentInspectWorkingInput) error {
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	tempDir, err := os.MkdirTemp("", "localclash-intent-preview-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)
	var selectionPath string
	if in.Intent.Exists && in.Intent.Resolved {
		config, err := localconfig.Load(in.Config)
		if err != nil {
			result.Warnings = append(result.Warnings, "localClash intent cannot be loaded for effective preview: "+err.Error())
		} else {
			resolved, err := localconfig.Resolve(localconfig.ResolveOptions{
				Config:              config,
				SubscriptionPath:    in.Subscription,
				SubscriptionConfig:  in.SubscriptionConfig,
				SubscriptionRuntime: in.SubscriptionRuntime,
				RulesCache:          in.RulesCache,
			})
			if err != nil {
				result.Warnings = append(result.Warnings, "localClash intent cannot be resolved for effective preview: "+err.Error())
			} else {
				selectionPath = filepath.Join(tempDir, "localclash-packs.gob")
				if err := localconfig.WriteSelection(selectionPath, resolved.Selection); err != nil {
					return err
				}
			}
		}
	} else if in.Intent.Exists && in.Intent.ResolveError != "" {
		result.Warnings = append(result.Warnings, "localClash intent is present but unresolved: "+in.Intent.ResolveError)
	}

	outputPath := filepath.Join(tempDir, "mihomo.yaml")
	_, err = configrender.Render(configrender.Options{
		SourcePath:         in.Subscription,
		OutputPath:         outputPath,
		PacksSelectionPath: selectionPath,
		RulesCacheDir:      in.RulesCache,
		RuntimeProfilePath: in.RuntimeProfile,
		Force:              true,
	})
	if err != nil {
		return err
	}
	result.PreviewRendered = true
	result.PreviewSource = "temporary_render"
	base, err := configinspect.InspectBase(configinspect.Options{ConfigPath: outputPath, Limit: limit})
	if err != nil {
		return err
	}
	result.Effective = &base.Summary
	overlay, err := configinspect.InspectOverlay(configinspect.Options{ConfigPath: outputPath, Limit: limit})
	if err != nil {
		return err
	}
	result.Overlay = &overlay
	switch {
	case !in.Intent.Exists:
		result.NextActions = append(result.NextActions, "no durable localClash overlay exists; effective rules are local safety baseline plus DIRECT fallback only", "use config_configure with policy_template=minimal or localclash-default before adding routing patches")
	case in.Intent.Resolved:
		result.NextActions = append(result.NextActions, "review effective_summary and overlay before starting or applying runtime changes")
	default:
		result.NextActions = append(result.NextActions, "repair localclash.json intent before applying or starting runtime")
	}
	return nil
}

func (s *Server) applyConfigIntentInspectDefaults(subscription, rulesCache, runtimeProfile, subscriptionConfig, subscriptionRuntime *string) {
	setDefault := func(value *string, fallback string) {
		if *value == "" && fallback != "" {
			*value = fallback
		}
	}
	if s.state != nil {
		setDefault(subscription, s.state.Paths.SubscriptionPath)
		setDefault(rulesCache, s.state.Paths.RulesCacheDir)
		setDefault(runtimeProfile, s.state.Paths.RuntimeProfilePath)
		setDefault(subscriptionConfig, s.state.Paths.SubscriptionConfig)
		setDefault(subscriptionRuntime, s.state.Paths.SubscriptionRuntime)
	}
	setDefault(subscription, "subscription.gob")
	setDefault(rulesCache, filepath.Join(".runtime", "rules", "packs"))
	setDefault(runtimeProfile, runtimeprofile.DefaultPath)
	setDefault(subscriptionConfig, "localclash-subscriptions.json")
	setDefault(subscriptionRuntime, filepath.Join(".runtime", "subscriptions"))
}

func (s *Server) callProxyGroupBuild(args json.RawMessage) (toolResult, error) {
	var in struct {
		ID                  string             `json:"id"`
		Mode                string             `json:"mode"`
		Match               *localconfig.Match `json:"match"`
		Nodes               []string           `json:"nodes"`
		Reason              string             `json:"reason"`
		Boundary            string             `json:"boundary"`
		Subscription        string             `json:"subscription"`
		SubscriptionConfig  string             `json:"subscription_config"`
		SubscriptionRuntime string             `json:"subscription_runtime"`
	}
	if err := decodeToolInput(args, &in); err != nil {
		return toolResult{}, err
	}
	if s.state != nil {
		if in.Subscription == "" {
			in.Subscription = s.state.Paths.SubscriptionPath
		}
		if in.SubscriptionConfig == "" {
			in.SubscriptionConfig = s.state.Paths.SubscriptionConfig
		}
		if in.SubscriptionRuntime == "" {
			in.SubscriptionRuntime = s.state.Paths.SubscriptionRuntime
		}
	}
	id := strings.TrimSpace(in.ID)
	if id == "" {
		return toolResult{}, fmt.Errorf("id is required")
	}
	group := localconfig.ProxyGroup{
		Mode:     in.Mode,
		Match:    in.Match,
		Nodes:    append([]string{}, in.Nodes...),
		Reason:   in.Reason,
		Boundary: in.Boundary,
	}
	resolved, err := localconfig.Resolve(localconfig.ResolveOptions{
		Config:              localconfig.Config{ProxyGroups: map[string]localconfig.ProxyGroup{id: group}},
		SubscriptionPath:    in.Subscription,
		SubscriptionConfig:  in.SubscriptionConfig,
		SubscriptionRuntime: in.SubscriptionRuntime,
	})
	if err != nil {
		return toolResult{}, err
	}
	resolvedGroup := resolved.Config.ProxyGroups[id]
	proxyGroupIntent := configplan.OverlayProxyGroupIntent{
		ID:       id,
		Mode:     resolvedGroup.Mode,
		Match:    resolvedGroup.Match,
		Nodes:    append([]string{}, resolvedGroup.Nodes...),
		Reason:   resolvedGroup.Reason,
		Boundary: resolvedGroup.Boundary,
	}
	return jsonToolResult(map[string]any{
		"proxy_group":    proxyGroupIntent,
		"id":             id,
		"target":         id,
		"selected_nodes": resolvedGroup.SelectedNodes,
	})
}

func callPolicyGroupBuild(args json.RawMessage) (toolResult, error) {
	var in struct {
		ID       string   `json:"id"`
		Mode     string   `json:"mode"`
		Exits    []string `json:"exits"`
		Reason   string   `json:"reason"`
		Boundary string   `json:"boundary"`
	}
	if err := decodeToolInput(args, &in); err != nil {
		return toolResult{}, err
	}
	id := strings.TrimSpace(in.ID)
	if id == "" {
		return toolResult{}, fmt.Errorf("id is required")
	}
	mode := strings.ToLower(strings.TrimSpace(in.Mode))
	if mode != "manual" && mode != "auto" && mode != "smart" {
		return toolResult{}, fmt.Errorf("policy group %q mode must be manual, auto, or smart", id)
	}
	exits, err := normalizePolicyGroupBuildExits(id, in.Exits)
	if err != nil {
		return toolResult{}, err
	}
	policyGroupIntent := configplan.OverlayPolicyGroupIntent{
		ID:       id,
		Mode:     mode,
		Exits:    exits,
		Reason:   in.Reason,
		Boundary: in.Boundary,
	}
	return jsonToolResult(map[string]any{
		"policy_group": policyGroupIntent,
		"id":           id,
		"target":       id,
		"exits":        exits,
	})
}

func normalizePolicyGroupBuildExits(groupID string, rawExits []string) ([]string, error) {
	if len(rawExits) == 0 {
		return nil, fmt.Errorf("policy group %q exits is required", groupID)
	}
	exits := make([]string, 0, len(rawExits))
	seen := map[string]bool{}
	for _, rawExit := range rawExits {
		exit := canonicalBuildTarget(rawExit)
		if exit == "" {
			exit = strings.TrimSpace(rawExit)
		}
		if exit == "" {
			return nil, fmt.Errorf("policy group %q has an empty exit", groupID)
		}
		if seen[exit] {
			continue
		}
		seen[exit] = true
		exits = append(exits, exit)
	}
	return exits, nil
}

func canonicalBuildTarget(target string) string {
	switch strings.ToLower(strings.TrimSpace(target)) {
	case "direct", "reject", "proxy", "manual":
		return strings.ToUpper(strings.TrimSpace(target))
	default:
		return ""
	}
}

func callCustomRulesBuild(args json.RawMessage) (toolResult, error) {
	var in localconfig.CustomRule
	if err := decodeToolInput(args, &in); err != nil {
		return toolResult{}, err
	}
	id := strings.TrimSpace(in.ID)
	target := strings.TrimSpace(in.Target)
	if id == "" {
		return toolResult{}, fmt.Errorf("id is required")
	}
	if target == "" {
		return toolResult{}, fmt.Errorf("target is required")
	}
	if len(in.Rules) == 0 {
		return toolResult{}, fmt.Errorf("rules is required")
	}
	selection := rules.Selection{
		Version: 1,
		CustomRules: []rules.CustomRule{{
			ID:     id,
			Target: "DIRECT",
			Reason: in.Reason,
			Rules:  customRuleLinesForBuild(in.Rules),
		}},
	}
	if _, err := rules.RenderFragment(selection, map[string]rules.PackCache{}); err != nil {
		return toolResult{}, err
	}
	in.ID = id
	in.Target = target
	return jsonToolResult(map[string]any{
		"custom_rule": in,
		"id":          id,
		"target":      target,
		"rule_count":  len(in.Rules),
	})
}

func callRuleProviderBuild(args json.RawMessage) (toolResult, error) {
	var in localconfig.ExternalRuleProvider
	if err := decodeToolInput(args, &in); err != nil {
		return toolResult{}, err
	}
	provider, err := localconfig.NormalizeRuleProvider(in)
	if err != nil {
		return toolResult{}, err
	}
	selection := rules.Selection{
		Version: 1,
		RuleProviders: []rules.ExternalRuleProvider{{
			ID:       provider.ID,
			Target:   "DIRECT",
			Reason:   provider.Reason,
			Type:     provider.Type,
			Behavior: provider.Behavior,
			Format:   provider.Format,
			Path:     provider.Path,
			URL:      provider.URL,
			Interval: provider.Interval,
		}},
	}
	if _, err := rules.RenderFragment(selection, map[string]rules.PackCache{}); err != nil {
		return toolResult{}, err
	}
	return jsonToolResult(map[string]any{
		"rule_provider": provider,
		"id":            provider.ID,
		"target":        provider.Target,
		"provider": map[string]any{
			"type":     provider.Type,
			"behavior": provider.Behavior,
			"format":   provider.Format,
			"path":     provider.Path,
			"url":      provider.URL,
			"interval": provider.Interval,
		},
	})
}

func customRuleLinesForBuild(lines []localconfig.CustomRuleLine) []rules.CustomRuleLine {
	out := make([]rules.CustomRuleLine, 0, len(lines))
	for _, line := range lines {
		out = append(out, rules.CustomRuleLine{Type: line.Type, Value: line.Value, NoResolve: line.NoResolve})
	}
	return out
}

type configToolInput struct {
	Config              string `json:"config"`
	Subscription        string `json:"subscription"`
	SubscriptionConfig  string `json:"subscription_config"`
	SubscriptionRuntime string `json:"subscription_runtime"`
	RulesCache          string `json:"rules_cache"`
	RuntimeProfile      string `json:"runtime_profile"`
	Core                string `json:"core"`
	RuntimeDir          string `json:"runtime_dir"`
	ValidationCache     string `json:"validation_cache"`
	Selection           string `json:"selection"`
	Output              string `json:"output"`
	PatchesDir          string `json:"patches_dir"`
	Limit               int    `json:"limit"`
	Detail              bool   `json:"detail"`
	Resolve             *bool  `json:"resolve"`
	Force               *bool  `json:"force"`
}

type configFileStatus struct {
	Path    string `json:"path"`
	Present bool   `json:"present"`
	Size    int64  `json:"size,omitempty"`
	ModTime string `json:"mod_time,omitempty"`
	Error   string `json:"error,omitempty"`
}

type configRenderState struct {
	Needed          bool     `json:"needed"`
	Stale           bool     `json:"stale"`
	CanRender       bool     `json:"can_render"`
	MissingInputs   []string `json:"missing_inputs,omitempty"`
	RecommendedTool string   `json:"recommended_tool,omitempty"`
}

type configPatchSummary struct {
	PatchID     string `json:"patch_id"`
	SummaryPath string `json:"summary_path"`
	Valid       *bool  `json:"valid,omitempty"`
}

func (s *Server) callConfigStatus(args json.RawMessage) (toolResult, error) {
	var in configToolInput
	if err := decodeToolInput(args, &in); err != nil {
		return toolResult{}, err
	}
	s.applyConfigToolDefaults(&in)
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	resolve := in.Detail
	if in.Resolve != nil {
		resolve = *in.Resolve
	}
	intent, err := configinspect.InspectIntent(configinspect.IntentOptions{
		ConfigPath:          in.Config,
		Subscription:        in.Subscription,
		SubscriptionConfig:  in.SubscriptionConfig,
		SubscriptionRuntime: in.SubscriptionRuntime,
		RulesCache:          in.RulesCache,
		Limit:               limit,
		SkipResolve:         !resolve,
	})
	if err != nil {
		return toolResult{}, err
	}
	if !in.Detail && !resolve {
		trimConfigStatusIntent(&intent)
	}
	generated := inspectConfigFile(in.Output)
	status := map[string]any{
		"model":           "localclash.json is source_of_truth; generated/mihomo.yaml is build_artifact; .runtime/patches contains review_artifacts",
		"source_of_truth": inspectConfigFile(in.Config),
		"generated":       generated,
		"subscription":    inspectConfigFile(in.Subscription),
		"runtime_profile": inspectConfigFile(in.RuntimeProfile),
		"selection":       inspectConfigFile(in.Selection),
		"inputs": map[string]any{
			"config":               in.Config,
			"subscription":         in.Subscription,
			"subscription_config":  in.SubscriptionConfig,
			"subscription_runtime": in.SubscriptionRuntime,
			"rules_cache":          in.RulesCache,
			"runtime_profile":      in.RuntimeProfile,
			"selection":            in.Selection,
			"output":               in.Output,
			"patches_dir":          in.PatchesDir,
		},
		"intent":     intent,
		"render":     s.configRenderState(in, intent, generated.Present),
		"validation": s.configValidationState(in, generated.Present),
		"patches":    listConfigPatches(in.PatchesDir, limit),
		"usage_guidance": []string{
			"config_status is the preferred tool for checking durable localClash routing intent and generated overlay state.",
			"By default config_status is lightweight and does not resolve subscription-node matches or inspect generated overlay details; pass detail=true when a full audit is needed.",
			"generated_summary omits raw Mihomo rule/provider identifiers; use nl_file only when explicit generated config line evidence is needed.",
			"Use intent.packs and overlay pack metadata to verify localClash-managed pack routing.",
			"runtime_status only reports whether Mihomo is running; it does not prove that a pending config change is loaded by a running process.",
		},
	}
	if in.Detail && generated.Present {
		if base, err := configinspect.InspectBase(configinspect.Options{ConfigPath: in.Output, Limit: limit}); err == nil {
			status["generated_summary"] = base.Summary
		} else {
			status["generated_error"] = err.Error()
		}
		if overlay, err := configinspect.InspectOverlay(configinspect.Options{ConfigPath: in.Output, Limit: limit}); err == nil {
			status["overlay"] = overlay
		}
	}
	status["next_actions"] = configStatusNextActions(status["render"].(configRenderState))
	return jsonToolResult(status)
}

func (s *Server) configValidationState(in configToolInput, generatedPresent bool) any {
	if !generatedPresent {
		return map[string]any{
			"status":       "missing_generated_config",
			"cache_path":   validationCachePath(in.ValidationCache, in.RuntimeDir),
			"next_actions": []string{"call config_render before runtime validation"},
		}
	}
	if strings.TrimSpace(in.Core) == "" {
		return map[string]any{
			"status":       "missing_core_path",
			"cache_path":   validationCachePath(in.ValidationCache, in.RuntimeDir),
			"next_actions": []string{"configure or download a Mihomo core before runtime validation"},
		}
	}
	return mihomotest.CacheStatus(context.Background(), mihomotest.ValidationOptions{
		CorePath:   in.Core,
		ConfigPath: in.Output,
		WorkDir:    in.RuntimeDir,
		CachePath:  validationCachePath(in.ValidationCache, in.RuntimeDir),
	})
}

func trimConfigStatusIntent(intent *configinspect.IntentResult) {
	for i := range intent.ProxyGroups {
		intent.ProxyGroups[i].Nodes = nil
		intent.ProxyGroups[i].SelectedNodes = nil
	}
	for i := range intent.CustomRules {
		intent.CustomRules[i].Rules = nil
	}
}

func (s *Server) configRenderState(in configToolInput, intent configinspect.IntentResult, generatedPresent bool) configRenderState {
	missing := missingRenderInputs(in)
	canRender := len(missing) == 0 && (!intent.Exists || intent.Resolved || (intent.Valid && intent.ResolveSkipped && intent.ResolveError == ""))
	stale := !generatedPresent || isGeneratedStale(in.Output, []string{in.Config, in.Subscription, in.RuntimeProfile, in.Selection})
	state := configRenderState{
		Needed:        !generatedPresent || stale,
		Stale:         generatedPresent && stale,
		CanRender:     canRender,
		MissingInputs: missing,
	}
	if state.Needed && state.CanRender {
		state.RecommendedTool = "config_render"
	}
	return state
}

func configStatusNextActions(render configRenderState) []string {
	if len(render.MissingInputs) > 0 {
		actions := []string{}
		for _, missing := range render.MissingInputs {
			switch missing {
			case "subscription":
				actions = append(actions, "call subscriptions_status", "call subscriptions_refresh to create subscription.gob")
			}
		}
		return actions
	}
	if !render.CanRender {
		return []string{"inspect intent.resolve_error in config_status and repair localclash.json or subscription node references before rendering"}
	}
	if render.RecommendedTool != "" {
		return []string{"call " + render.RecommendedTool + " to rebuild generated/mihomo.yaml from durable localClash state"}
	}
	return []string{"generated/mihomo.yaml is present; use config_patch_create for reviewed routing changes"}
}

func (s *Server) callConfigRender(ctx context.Context, args json.RawMessage) (toolResult, error) {
	var in configToolInput
	if err := decodeToolInput(args, &in); err != nil {
		return toolResult{}, err
	}
	s.applyConfigToolDefaults(&in)
	appendTaskStage(ctx, "stage_started", "validate_inputs", nil)
	missing := missingRenderInputs(in)
	if len(missing) > 0 {
		appendTaskStage(ctx, "stage_error", "validate_inputs", map[string]any{"missing_inputs": missing})
		return jsonToolResult(map[string]any{
			"rendered":       false,
			"missing_inputs": missing,
			"next_actions":   configStatusNextActions(configRenderState{MissingInputs: missing}),
		})
	}
	appendTaskStage(ctx, "stage_done", "validate_inputs", nil)
	force := true
	if in.Force != nil {
		force = *in.Force
	}
	result, err := renderCurrentConfig(ctx, in, force)
	if err != nil {
		return toolResult{}, err
	}
	return jsonToolResult(result)
}

func renderCurrentConfig(ctx context.Context, in configToolInput, force bool) (map[string]any, error) {
	selectionPath := ""
	source := "base"
	warnings := []string{}
	if fileExists(in.Config) {
		finish := startTaskStage(ctx, "resolve_localclash_config", map[string]any{"config": in.Config})
		config, err := localconfig.Load(in.Config)
		if err != nil {
			finishTaskStage(finish, err, nil)
			return nil, err
		}
		resolved, err := localconfig.Resolve(localconfig.ResolveOptions{
			Config:              config,
			SubscriptionPath:    in.Subscription,
			SubscriptionConfig:  in.SubscriptionConfig,
			SubscriptionRuntime: in.SubscriptionRuntime,
			RulesCache:          in.RulesCache,
			OnStage:             localConfigTaskLogger(ctx, "resolve_localclash_config"),
		})
		if err != nil {
			finishTaskStage(finish, err, nil)
			return nil, err
		}
		if err := localconfig.WriteSelection(in.Selection, resolved.Selection); err != nil {
			finishTaskStage(finish, err, nil)
			return nil, err
		}
		selectionPath = in.Selection
		source = "durable_state"
		warnings = append(warnings, resolved.Warnings...)
		finishTaskStage(finish, nil, map[string]any{
			"selection":          in.Selection,
			"proxy_groups":       len(resolved.ProxyGroups),
			"policy_groups":      len(resolved.PolicyGroups),
			"packs":              len(resolved.Packs),
			"custom_rules":       len(resolved.CustomRules),
			"enabled_rule_packs": len(resolved.RulePacks),
		})
	}
	finish := startTaskStage(ctx, "render_generated_config", map[string]any{"output": in.Output})
	result, err := configrender.Render(configrender.Options{
		SourcePath:         in.Subscription,
		OutputPath:         in.Output,
		Force:              force,
		PacksSelectionPath: selectionPath,
		RulesCacheDir:      in.RulesCache,
		RuntimeProfilePath: in.RuntimeProfile,
		OnStage:            configRenderTaskLogger(ctx, "render_generated_config"),
	})
	if err != nil {
		finishTaskStage(finish, err, nil)
		return nil, err
	}
	finishTaskStage(finish, nil, map[string]any{"proxy_count": result.ProxyCount, "rule_count": result.RuleCount, "core": result.Core})
	return map[string]any{
		"rendered":        true,
		"source":          source,
		"source_of_truth": in.Config,
		"selection":       selectionPath,
		"output":          in.Output,
		"render":          result,
		"patches_ignored": true,
		"warnings":        warnings,
	}, nil
}

func (s *Server) callConfigPatchCreate(ctx context.Context, args json.RawMessage) (toolResult, error) {
	var in struct {
		PatchName            string                   `json:"patch_name"`
		Subscription         string                   `json:"subscription"`
		RulesCache           string                   `json:"rules_cache"`
		RuntimeProfileConfig string                   `json:"runtime_profile"`
		OutputDir            string                   `json:"patches_dir"`
		ConfigPath           string                   `json:"config"`
		SubscriptionConfig   string                   `json:"subscription_config"`
		SubscriptionRuntime  string                   `json:"subscription_runtime"`
		Test                 *bool                    `json:"test"`
		Core                 string                   `json:"core"`
		RuntimeDir           string                   `json:"runtime_dir"`
		Overlay              configplan.OverlayIntent `json:"overlay"`
	}
	if err := decodeToolInput(args, &in); err != nil {
		return toolResult{}, err
	}
	test := false
	if in.Test != nil {
		test = *in.Test
	}
	if s.state != nil {
		if in.Subscription == "" {
			in.Subscription = s.state.Paths.SubscriptionPath
		}
		if in.RulesCache == "" {
			in.RulesCache = s.state.Paths.RulesCacheDir
		}
		if in.RuntimeProfileConfig == "" {
			in.RuntimeProfileConfig = s.state.Paths.RuntimeProfilePath
		}
		if in.SubscriptionConfig == "" {
			in.SubscriptionConfig = s.state.Paths.SubscriptionConfig
		}
		if in.SubscriptionRuntime == "" {
			in.SubscriptionRuntime = s.state.Paths.SubscriptionRuntime
		}
		if in.Core == "" {
			in.Core = s.state.Paths.CorePath
		}
		if in.RuntimeDir == "" {
			in.RuntimeDir = s.state.Paths.MihomoRuntimeDir
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	result, err := configplan.Render(ctx, configplan.Options{
		PlanName:            in.PatchName,
		Subscription:        in.Subscription,
		RulesCache:          in.RulesCache,
		RuntimeProfilePath:  in.RuntimeProfileConfig,
		OutputDir:           in.OutputDir,
		ConfigPath:          in.ConfigPath,
		SubscriptionConfig:  in.SubscriptionConfig,
		SubscriptionRuntime: in.SubscriptionRuntime,
		Test:                test,
		CorePath:            in.Core,
		WorkDir:             in.RuntimeDir,
		Overlay:             in.Overlay,
		OnStage:             configPlanTaskLogger(ctx),
	})
	if err != nil {
		return toolResult{}, err
	}
	return jsonToolResult(result)
}

func (s *Server) callConfigPatchApply(ctx context.Context, args json.RawMessage) (toolResult, error) {
	var in struct {
		PatchID              string `json:"patch_id"`
		PatchesDir           string `json:"patches_dir"`
		SummaryPath          string `json:"summary_path"`
		Subscription         string `json:"subscription"`
		RulesCache           string `json:"rules_cache"`
		RuntimeProfileConfig string `json:"runtime_profile"`
		ConfigPath           string `json:"config"`
		SubscriptionConfig   string `json:"subscription_config"`
		SubscriptionRuntime  string `json:"subscription_runtime"`
		Selection            string `json:"selection"`
		Output               string `json:"output"`
		BackupDir            string `json:"backup_dir"`
		Test                 *bool  `json:"test"`
		Core                 string `json:"core"`
		RuntimeDir           string `json:"runtime_dir"`
	}
	if err := decodeToolInput(args, &in); err != nil {
		return toolResult{}, err
	}
	test := true
	if in.Test != nil {
		test = *in.Test
	}
	if s.state != nil {
		if in.RulesCache == "" {
			in.RulesCache = s.state.Paths.RulesCacheDir
		}
		if in.RuntimeProfileConfig == "" {
			in.RuntimeProfileConfig = s.state.Paths.RuntimeProfilePath
		}
		if in.SubscriptionConfig == "" {
			in.SubscriptionConfig = s.state.Paths.SubscriptionConfig
		}
		if in.SubscriptionRuntime == "" {
			in.SubscriptionRuntime = s.state.Paths.SubscriptionRuntime
		}
		if in.Selection == "" && s.state.Paths.PacksSelectionPath != "" {
			in.Selection = s.state.Paths.PacksSelectionPath
		}
		if in.Output == "" {
			in.Output = s.state.Paths.GeneratedConfig
		}
		if in.Core == "" {
			in.Core = s.state.Paths.CorePath
		}
		if in.RuntimeDir == "" {
			in.RuntimeDir = s.state.Paths.MihomoRuntimeDir
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	result, err := configplan.Apply(ctx, configplan.ApplyOptions{
		PlanID:              in.PatchID,
		PlansDir:            in.PatchesDir,
		SummaryPath:         in.SummaryPath,
		Subscription:        in.Subscription,
		RulesCache:          in.RulesCache,
		RuntimeProfilePath:  in.RuntimeProfileConfig,
		ConfigPath:          in.ConfigPath,
		SubscriptionConfig:  in.SubscriptionConfig,
		SubscriptionRuntime: in.SubscriptionRuntime,
		SelectionPath:       in.Selection,
		OutputPath:          in.Output,
		CorePath:            in.Core,
		WorkDir:             in.RuntimeDir,
		BackupDir:           in.BackupDir,
		Test:                test,
		TestExplicit:        in.Test != nil,
		OnStage:             configPlanTaskLogger(ctx),
	})
	if err != nil {
		if result.PlanID != "" {
			tool, encodeErr := jsonToolResult(result)
			if encodeErr != nil {
				return toolResult{}, encodeErr
			}
			tool.IsError = true
			return tool, nil
		}
		return toolResult{}, err
	}
	return jsonToolResult(result)
}

func (s *Server) callPacksList(args json.RawMessage) (toolResult, error) {
	var in struct {
		Source string `json:"source"`
		Name   string `json:"name"`
		Target string `json:"target"`
		Limit  int    `json:"limit"`
		Cache  string `json:"cache"`
	}
	if err := decodeToolInput(args, &in); err != nil {
		return toolResult{}, err
	}
	if s.state != nil {
		if in.Cache == "" {
			in.Cache = s.state.Paths.RulesCacheDir
		}
	}
	if in.Limit == 0 {
		in.Limit = 50
	}
	result, err := rules.ListPacks(rules.PackListOptions{
		CacheDir: in.Cache,
		Source:   in.Source,
		Name:     in.Name,
		Target:   in.Target,
		Limit:    in.Limit,
	})
	if err != nil {
		return toolResult{}, err
	}
	return jsonToolResult(result)
}

func (s *Server) callPacksGet(args json.RawMessage) (toolResult, error) {
	var in struct {
		ID         *string `json:"id"`
		Source     string  `json:"source"`
		Pack       string  `json:"pack"`
		Cache      string  `json:"cache"`
		RuntimeDir string  `json:"runtime_dir"`
	}
	if err := decodeToolInput(args, &in); err != nil {
		return toolResult{}, err
	}
	if s.state != nil {
		if in.Cache == "" {
			in.Cache = s.state.Paths.RulesCacheDir
		}
		if in.RuntimeDir == "" {
			in.RuntimeDir = s.state.Paths.MihomoRuntimeDir
		}
	}
	if in.ID != nil {
		return toolResult{}, legacyPackIDError("pack id", *in.ID, in.Cache)
	}
	result, err := rules.GetPack(rules.PackGetOptions{CacheDir: in.Cache, RuntimeDir: in.RuntimeDir, Source: in.Source, Pack: in.Pack})
	if err != nil {
		return toolResult{}, err
	}
	return jsonToolResult(result)
}

func (s *Server) callPackRulesRead(ctx context.Context, args json.RawMessage) (toolResult, error) {
	var in struct {
		ID            *string `json:"id"`
		Source        string  `json:"source"`
		Pack          string  `json:"pack"`
		Component     string  `json:"component"`
		Limit         int     `json:"limit"`
		Refresh       bool    `json:"refresh"`
		Cache         string  `json:"cache"`
		Sources       string  `json:"sources"`
		ProviderCache string  `json:"provider_cache"`
	}
	if err := decodeToolInput(args, &in); err != nil {
		return toolResult{}, err
	}
	s.applyPackRulesDefaults(&in.Cache, &in.Sources, &in.ProviderCache)
	if in.ID != nil {
		return toolResult{}, legacyPackIDError("pack id", *in.ID, in.Cache)
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	result, err := rules.ReadPackRules(ctx, rules.PackRulesReadOptions{
		SourcesDir:    in.Sources,
		CacheDir:      in.Cache,
		ProviderCache: in.ProviderCache,
		Source:        in.Source,
		Pack:          in.Pack,
		Component:     in.Component,
		Limit:         in.Limit,
		Refresh:       in.Refresh,
	})
	if err != nil {
		return toolResult{}, err
	}
	return jsonToolResult(result)
}

func (s *Server) callPackRulesPrefetch(ctx context.Context, args json.RawMessage) (toolResult, error) {
	var in struct {
		IDs           []string             `json:"ids"`
		Packs         []rules.PackSelector `json:"packs"`
		Source        string               `json:"source"`
		Name          string               `json:"name"`
		Target        string               `json:"target"`
		Limit         int                  `json:"limit"`
		Refresh       bool                 `json:"refresh"`
		Cache         string               `json:"cache"`
		Sources       string               `json:"sources"`
		ProviderCache string               `json:"provider_cache"`
	}
	if err := decodeToolInput(args, &in); err != nil {
		return toolResult{}, err
	}
	s.applyPackRulesDefaults(&in.Cache, &in.Sources, &in.ProviderCache)
	if len(in.IDs) > 0 {
		return toolResult{}, legacyPackIDsError(in.IDs, in.Cache)
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	result, err := rules.PrefetchPackRules(ctx, rules.PackRulesPrefetchOptions{
		SourcesDir:    in.Sources,
		CacheDir:      in.Cache,
		ProviderCache: in.ProviderCache,
		Packs:         in.Packs,
		Source:        in.Source,
		Name:          in.Name,
		Target:        in.Target,
		Limit:         in.Limit,
		Refresh:       in.Refresh,
	})
	if err != nil {
		return toolResult{}, err
	}
	return jsonToolResult(result)
}

func (s *Server) callPackRulesQuery(ctx context.Context, args json.RawMessage) (toolResult, error) {
	var in struct {
		Query         string `json:"query"`
		Source        string `json:"source"`
		Name          string `json:"name"`
		Target        string `json:"target"`
		Limit         int    `json:"limit"`
		Cache         string `json:"cache"`
		Sources       string `json:"sources"`
		ProviderCache string `json:"provider_cache"`
	}
	if err := decodeToolInput(args, &in); err != nil {
		return toolResult{}, err
	}
	s.applyPackRulesDefaults(&in.Cache, &in.Sources, &in.ProviderCache)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	result, err := rules.QueryPackRules(ctx, rules.PackRulesQueryOptions{
		SourcesDir:    in.Sources,
		CacheDir:      in.Cache,
		ProviderCache: in.ProviderCache,
		Query:         in.Query,
		Source:        in.Source,
		Name:          in.Name,
		Target:        in.Target,
		Limit:         in.Limit,
	})
	if err != nil {
		return toolResult{}, err
	}
	return jsonToolResult(result)
}

func legacyPackIDError(label, id, cacheDir string) error {
	id = strings.TrimSpace(id)
	if ref, ok := legacyPackIDSuggestion(id, cacheDir); ok {
		return fmt.Errorf("%s is no longer supported. Use %s. Composite renderer/provider names are not MCP pack selectors.", label, packRefExample(ref.Source, ref.Pack))
	}
	return fmt.Errorf("%s is no longer supported. Use source and pack fields from packs_list, for example %s. Composite renderer/provider names are not MCP pack selectors.", label, packRefExample("syncnext", "SyncnextUnbreak"))
}

func legacyPackIDsError(ids []string, cacheDir string) error {
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if ref, ok := legacyPackIDSuggestion(id, cacheDir); ok {
			return fmt.Errorf("pack ids are no longer supported. Use packs: [%s]. Composite renderer/provider names are not MCP pack selectors.", packRefExample(ref.Source, ref.Pack))
		}
		break
	}
	return fmt.Errorf("pack ids are no longer supported. Use packs[].source and packs[].pack from packs_list, for example packs: [%s]. Composite renderer/provider names are not MCP pack selectors.", packRefExample("syncnext", "SyncnextUnbreak"))
}

func legacyPackIDSuggestion(id, cacheDir string) (rules.PackRefArgs, bool) {
	if id == "" {
		return rules.PackRefArgs{}, false
	}
	catalog, err := rules.LoadPackCatalog(cacheDir)
	if err != nil {
		return rules.PackRefArgs{}, false
	}
	for _, detail := range catalog.Details {
		for _, provider := range detail.Providers {
			if provider.Name == id {
				return rules.PackRefArgs{Source: detail.Source, Pack: detail.Pack}, true
			}
		}
	}
	return rules.PackRefArgs{}, false
}

func packRefExample(source, pack string) string {
	data, err := json.Marshal(rules.PackRefArgs{Source: source, Pack: pack})
	if err != nil {
		return `{"source":"syncnext","pack":"SyncnextUnbreak"}`
	}
	return string(data)
}

func (s *Server) applyPackRulesDefaults(cache, sources, providerCache *string) {
	if s.state == nil {
		return
	}
	if *cache == "" {
		*cache = s.state.Paths.RulesCacheDir
	}
	if *sources == "" {
		*sources = s.state.Paths.RuleSourcesDir
	}
	if *providerCache == "" {
		runtimeRoot := s.state.Paths.RuntimeRoot
		if runtimeRoot == "" {
			runtimeRoot = ".runtime"
		}
		*providerCache = filepath.Join(runtimeRoot, "rules", "provider-cache")
	}
}

func (s *Server) callSubscriptionsStatus(args json.RawMessage) (toolResult, error) {
	var in struct {
		Config     string `json:"config"`
		Merged     string `json:"merged"`
		RuntimeDir string `json:"runtime_dir"`
	}
	if err := decodeToolInput(args, &in); err != nil {
		return toolResult{}, err
	}
	if s.state != nil {
		if in.Config == "" {
			in.Config = s.state.Paths.SubscriptionConfig
		}
		if in.Merged == "" {
			in.Merged = s.state.Paths.SubscriptionPath
		}
		if in.RuntimeDir == "" {
			in.RuntimeDir = s.state.Paths.SubscriptionRuntime
		}
	}
	result, err := subscriptions.Status(subscriptions.StatusOptions{
		ConfigPath: in.Config,
		MergedPath: in.Merged,
		RuntimeDir: in.RuntimeDir,
	})
	if err != nil {
		return toolResult{}, err
	}
	return jsonToolResult(result)
}

func (s *Server) callSubscriptionNodesList(args json.RawMessage) (toolResult, error) {
	var in struct {
		Subscription string `json:"subscription"`
		Limit        int    `json:"limit"`
	}
	if err := decodeToolInput(args, &in); err != nil {
		return toolResult{}, err
	}
	if s.state != nil && in.Subscription == "" {
		in.Subscription = s.state.Paths.SubscriptionPath
	}
	result, err := rules.ListSubscriptionNodes(rules.SubscriptionNodesListOptions{
		Subscription: in.Subscription,
		Limit:        in.Limit,
	})
	if err != nil {
		return toolResult{}, err
	}
	return jsonToolResult(result)
}

func (s *Server) callSubscriptionNodesSearch(args json.RawMessage) (toolResult, error) {
	var in struct {
		Subscription  string   `json:"subscription"`
		Query         string   `json:"query"`
		Patterns      []string `json:"patterns"`
		CaseSensitive bool     `json:"case_sensitive"`
		Limit         int      `json:"limit"`
	}
	if err := decodeToolInput(args, &in); err != nil {
		return toolResult{}, err
	}
	if s.state != nil && in.Subscription == "" {
		in.Subscription = s.state.Paths.SubscriptionPath
	}
	result, err := rules.SearchSubscriptionNodes(rules.SubscriptionNodesSearchOptions{
		Subscription:  in.Subscription,
		Query:         in.Query,
		Patterns:      in.Patterns,
		CaseSensitive: in.CaseSensitive,
		Limit:         in.Limit,
	})
	if err != nil {
		return toolResult{}, err
	}
	return jsonToolResult(result)
}

func (s *Server) callSubscriptionsConfigure(args json.RawMessage) (toolResult, error) {
	type subscriptionConfigureSourceInput struct {
		URL string `json:"url"`
	}
	var in struct {
		Config  string                             `json:"config"`
		Sources []subscriptionConfigureSourceInput `json:"sources"`
		Replace *bool                              `json:"replace"`
	}
	decoder := json.NewDecoder(bytes.NewReader(args))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil {
		return toolResult{}, err
	}
	if s.state != nil && in.Config == "" {
		in.Config = s.state.Paths.SubscriptionConfig
	}
	urls := make([]string, 0, len(in.Sources))
	for _, source := range in.Sources {
		urls = append(urls, source.URL)
	}
	result, err := subscriptions.Configure(subscriptions.ConfigureOptions{
		ConfigPath: in.Config,
		URLs:       urls,
		Replace:    in.Replace,
	})
	if err != nil {
		return toolResult{}, err
	}
	return jsonToolResult(result)
}

func (s *Server) callSubscriptionsRefresh(ctx context.Context, args json.RawMessage) (toolResult, error) {
	var in struct {
		Config               string   `json:"config"`
		IDs                  []string `json:"ids"`
		RuntimeDir           string   `json:"runtime_dir"`
		Merged               string   `json:"merged"`
		Force                bool     `json:"force"`
		UserAgent            string   `json:"user_agent"`
		LocalClashConfig     string   `json:"localclash_config"`
		Selection            string   `json:"selection"`
		RulesCache           string   `json:"rules_cache"`
		RuntimeProfileConfig string   `json:"runtime_profile"`
		Output               string   `json:"output"`
	}
	if err := decodeToolInput(args, &in); err != nil {
		return toolResult{}, err
	}
	if s.state != nil {
		if in.Config == "" {
			in.Config = s.state.Paths.SubscriptionConfig
		}
		if in.RuntimeDir == "" {
			in.RuntimeDir = s.state.Paths.SubscriptionRuntime
		}
		if in.Merged == "" {
			in.Merged = s.state.Paths.SubscriptionPath
		}
		if in.Selection == "" && s.state.Paths.PacksSelectionPath != "" {
			in.Selection = s.state.Paths.PacksSelectionPath
		}
		if in.RulesCache == "" {
			in.RulesCache = s.state.Paths.RulesCacheDir
		}
		if in.RuntimeProfileConfig == "" {
			in.RuntimeProfileConfig = s.state.Paths.RuntimeProfilePath
		}
		if in.Output == "" {
			in.Output = s.state.Paths.GeneratedConfig
		}
	}
	if in.Selection == "" {
		in.Selection = "localclash-packs.gob"
	}
	if in.LocalClashConfig == "" {
		in.LocalClashConfig = "localclash.json"
	}
	finish := startTaskStage(ctx, "load_subscription_nodes_before", map[string]any{"subscription": in.Merged})
	beforeNodes, _ := localconfig.LoadSubscriptionNodes(localconfig.SubscriptionNodeOptions{
		SubscriptionPath:    in.Merged,
		SubscriptionConfig:  in.Config,
		SubscriptionRuntime: in.RuntimeDir,
		OnStage:             localConfigTaskLogger(ctx, "load_subscription_nodes_before"),
	})
	finishTaskStage(finish, nil, map[string]any{"node_count": len(beforeNodes)})
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	result, err := subscriptions.Refresh(ctx, subscriptions.RefreshOptions{
		ConfigPath: in.Config,
		IDs:        in.IDs,
		RuntimeDir: in.RuntimeDir,
		MergedPath: in.Merged,
		Force:      in.Force,
		UserAgent:  in.UserAgent,
		OnStage:    subscriptionsTaskLogger(ctx),
	})
	if err != nil {
		return toolResult{}, err
	}
	finish = startTaskStage(ctx, "load_subscription_nodes_after", map[string]any{"subscription": in.Merged})
	afterNodes, buildStats := localconfig.BuildSubscriptionNodesFromArtifactsMeasured(subscriptionArtifactsForLocalConfig(result.Artifacts))
	finishTaskStage(finish, nil, mergeMCPFields(map[string]any{"node_count": len(afterNodes), "source": "refresh_memory"}, buildStats.Fields()))
	toolResultValue := subscriptionsRefreshToolResult{
		RefreshResult: result,
		NodeDiff:      buildNodeDiff(beforeNodes, afterNodes),
	}
	finish = startTaskStage(ctx, "evaluate_localclash_impact", map[string]any{"config": in.LocalClashConfig})
	impact := s.evaluateLocalClashAfterRefresh(ctx, in.LocalClashConfig, in.Selection, in.Merged, in.Config, in.RuntimeDir, in.RulesCache, in.RuntimeProfileConfig, in.Output, afterNodes, result.MergedDoc)
	finishTaskStage(finish, nil, map[string]any{"exists": impact.Exists, "state": impact.State, "valid": impact.Valid})
	if impact.Exists {
		toolResultValue.LocalClash = &impact
	}
	return jsonToolResult(toolResultValue)
}

func subscriptionArtifactsForLocalConfig(artifacts []subscriptions.RefreshArtifact) []localconfig.SubscriptionSourceArtifact {
	out := make([]localconfig.SubscriptionSourceArtifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		out = append(out, localconfig.SubscriptionSourceArtifact{
			SourceID: artifact.SourceID,
			Proxies:  artifact.Proxies,
		})
	}
	return out
}

type subscriptionsRefreshToolResult struct {
	subscriptions.RefreshResult
	NodeDiff   nodeDiff                 `json:"node_diff"`
	LocalClash *localClashRefreshImpact `json:"localclash_config,omitempty"`
}

type nodeDiff struct {
	BeforeCount  int      `json:"before_count"`
	AfterCount   int      `json:"after_count"`
	AddedCount   int      `json:"added_count"`
	RemovedCount int      `json:"removed_count"`
	KeptCount    int      `json:"kept_count"`
	Added        []string `json:"added,omitempty"`
	Removed      []string `json:"removed,omitempty"`
	Truncated    bool     `json:"truncated,omitempty"`
}

type localClashRefreshImpact struct {
	Exists              bool                         `json:"exists"`
	State               string                       `json:"state,omitempty"`
	ConfigPath          string                       `json:"config_path"`
	Valid               bool                         `json:"valid"`
	AppliedAuto         bool                         `json:"applied_auto"`
	RequiresAgentReplan bool                         `json:"requires_agent_replan"`
	Error               string                       `json:"error,omitempty"`
	MissingNodes        []string                     `json:"missing_nodes,omitempty"`
	GeneratedConfig     string                       `json:"generated_config,omitempty"`
	SelectionPath       string                       `json:"selection_path,omitempty"`
	ProxyGroups         []localClashProxyGroupImpact `json:"proxy_groups,omitempty"`
	NextActions         []string                     `json:"next_actions,omitempty"`
}

type localClashProxyGroupImpact struct {
	ID            string   `json:"id"`
	PreviousNodes []string `json:"previous_nodes,omitempty"`
	SelectedNodes []string `json:"selected_nodes,omitempty"`
	AddedNodes    []string `json:"added_nodes,omitempty"`
	RemovedNodes  []string `json:"removed_nodes,omitempty"`
}

func buildNodeDiff(before, after []localconfig.SubscriptionNode) nodeDiff {
	beforeSet := map[string]bool{}
	afterSet := map[string]bool{}
	for _, node := range before {
		beforeSet[node.Name] = true
	}
	for _, node := range after {
		afterSet[node.Name] = true
	}
	var added, removed []string
	kept := 0
	for name := range afterSet {
		if beforeSet[name] {
			kept++
		} else {
			added = append(added, name)
		}
	}
	for name := range beforeSet {
		if !afterSet[name] {
			removed = append(removed, name)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	diff := nodeDiff{
		BeforeCount:  len(beforeSet),
		AfterCount:   len(afterSet),
		AddedCount:   len(added),
		RemovedCount: len(removed),
		KeptCount:    kept,
	}
	const maxReturned = 100
	if len(added) > maxReturned || len(removed) > maxReturned {
		diff.Truncated = true
	}
	diff.Added = limitStrings(added, maxReturned)
	diff.Removed = limitStrings(removed, maxReturned)
	return diff
}

func (s *Server) evaluateLocalClashAfterRefresh(ctx context.Context, configPath, selectionPath, subscriptionPath, subscriptionConfig, subscriptionRuntime, rulesCache, presetPath, outputPath string, subscriptionNodes []localconfig.SubscriptionNode, subscriptionDoc map[string]any) localClashRefreshImpact {
	impact := localClashRefreshImpact{ConfigPath: configPath, GeneratedConfig: outputPath, SelectionPath: selectionPath}
	finish := startTaskStage(ctx, "evaluate_localclash_impact.load_config", map[string]any{"config": configPath})
	config, err := localconfig.Load(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			finishTaskStage(finish, nil, map[string]any{"exists": false})
			return impact
		}
		finishTaskStage(finish, err, nil)
		impact.Exists = true
		impact.RequiresAgentReplan = true
		impact.Error = err.Error()
		return impact
	}
	finishTaskStage(finish, nil, map[string]any{"exists": true})
	impact.Exists = true

	finish = startTaskStage(ctx, "evaluate_localclash_impact.resolve_after_refresh", map[string]any{"subscription_nodes": len(subscriptionNodes)})
	resolved, err := localconfig.Resolve(localconfig.ResolveOptions{
		Config:              config,
		SubscriptionPath:    subscriptionPath,
		SubscriptionConfig:  subscriptionConfig,
		SubscriptionRuntime: subscriptionRuntime,
		SubscriptionNodes:   subscriptionNodes,
		RulesCache:          rulesCache,
		OnStage:             localConfigTaskLogger(ctx, "evaluate_localclash_impact"),
	})
	if err != nil {
		finishTaskStage(finish, err, nil)
		var missingNodes *localconfig.MissingNodesError
		if errors.As(err, &missingNodes) {
			impact.State = "stale_exact_nodes"
			impact.Error = err.Error()
			impact.MissingNodes = append([]string{}, missingNodes.Nodes...)
			impact.NextActions = []string{"ask the user to choose replacement nodes or switch this group to a match selector", "call proxy_group_build", "call config_patch_create", "call config_patch_apply after review"}
			return impact
		}
		impact.State = "requires_agent_replan"
		impact.RequiresAgentReplan = true
		impact.Error = err.Error()
		impact.NextActions = []string{"read localclash.json", "search replacement subscription nodes", "call proxy_group_build", "call config_patch_create", "call config_patch_apply after review"}
		return impact
	}
	finishTaskStage(finish, nil, map[string]any{
		"proxy_groups":       len(resolved.ProxyGroups),
		"policy_groups":      len(resolved.PolicyGroups),
		"packs":              len(resolved.Packs),
		"enabled_rule_packs": len(resolved.RulePacks),
	})
	impact.State = "auto_applied"
	impact.Valid = true
	impact.ProxyGroups = proxyGroupImpacts(config, resolved.Config)

	finish = startTaskStage(ctx, "evaluate_localclash_impact.render_after_refresh", map[string]any{"output": outputPath, "subscription_source": renderSubscriptionSource(subscriptionDoc)})
	_, err = configrender.Render(configrender.Options{
		SourcePath:         subscriptionPath,
		Source:             subscriptionDoc,
		OutputPath:         outputPath,
		Selection:          &resolved.Selection,
		RulesCacheDir:      rulesCache,
		RuntimeProfilePath: presetPath,
		Force:              true,
		OnStage:            configRenderTaskLogger(ctx, "evaluate_localclash_impact.render_after_refresh"),
	})
	if err != nil {
		finishTaskStage(finish, err, nil)
		impact.RequiresAgentReplan = true
		impact.Error = err.Error()
		return impact
	}
	finishTaskStage(finish, nil, nil)

	finish = startTaskStage(ctx, "evaluate_localclash_impact.write_resolved_config", map[string]any{"config": configPath})
	if err := localconfig.Write(configPath, resolved.Config); err != nil {
		finishTaskStage(finish, err, nil)
		impact.RequiresAgentReplan = true
		impact.Error = err.Error()
		return impact
	}
	finishTaskStage(finish, nil, nil)

	finish = startTaskStage(ctx, "evaluate_localclash_impact.write_selection", map[string]any{"selection": selectionPath})
	if err := localconfig.WriteSelection(selectionPath, resolved.Selection); err != nil {
		finishTaskStage(finish, err, nil)
		impact.RequiresAgentReplan = true
		impact.Error = err.Error()
		return impact
	}
	finishTaskStage(finish, nil, nil)
	impact.AppliedAuto = true
	return impact
}

func renderSubscriptionSource(doc map[string]any) string {
	if doc != nil {
		return "provided"
	}
	return "disk"
}

func mergeMCPFields(base map[string]any, extra map[string]any) map[string]any {
	for key, value := range extra {
		base[key] = value
	}
	return base
}

func proxyGroupImpacts(before, after localconfig.Config) []localClashProxyGroupImpact {
	var ids []string
	for id := range after.ProxyGroups {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	impacts := make([]localClashProxyGroupImpact, 0, len(ids))
	for _, id := range ids {
		previous := before.ProxyGroups[id].SelectedNodes
		selected := after.ProxyGroups[id].SelectedNodes
		added, removed := stringSetDiff(previous, selected)
		impacts = append(impacts, localClashProxyGroupImpact{
			ID:            id,
			PreviousNodes: append([]string{}, previous...),
			SelectedNodes: append([]string{}, selected...),
			AddedNodes:    added,
			RemovedNodes:  removed,
		})
	}
	return impacts
}

func stringSetDiff(before, after []string) ([]string, []string) {
	beforeSet := map[string]bool{}
	afterSet := map[string]bool{}
	for _, value := range before {
		beforeSet[value] = true
	}
	for _, value := range after {
		afterSet[value] = true
	}
	var added, removed []string
	for value := range afterSet {
		if !beforeSet[value] {
			added = append(added, value)
		}
	}
	for value := range beforeSet {
		if !afterSet[value] {
			removed = append(removed, value)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	return added, removed
}

func limitStrings(values []string, limit int) []string {
	if len(values) > limit {
		values = values[:limit]
	}
	return append([]string{}, values...)
}

func (s *Server) callDoctor(ctx context.Context, args json.RawMessage) (toolResult, error) {
	var in struct {
		Core         string `json:"core"`
		CorePath     string `json:"core_path"`
		Subscription string `json:"subscription"`
		Config       string `json:"config"`
		ConfigPath   string `json:"config_path"`
		Dashboard    string `json:"dashboard"`
		DashboardDir string `json:"dashboard_dir"`
		WorkDir      string `json:"workdir"`
	}
	if err := decodeToolInput(args, &in); err != nil {
		return toolResult{}, err
	}
	opts := doctor.Options{
		CorePath:         firstNonEmpty(in.CorePath, in.Core),
		SubscriptionPath: in.Subscription,
		ConfigPath:       firstNonEmpty(in.ConfigPath, in.Config),
		DashboardDir:     firstNonEmpty(in.DashboardDir, in.Dashboard),
		WorkDir:          in.WorkDir,
	}
	if s.state != nil {
		if opts.CorePath == "" {
			opts.CorePath = s.state.Paths.CorePath
		}
		if opts.SubscriptionPath == "" {
			opts.SubscriptionPath = s.state.Paths.SubscriptionPath
		}
		if opts.ConfigPath == "" {
			opts.ConfigPath = s.state.Paths.GeneratedConfig
		}
		if opts.DashboardDir == "" {
			opts.DashboardDir = filepath.Join(s.state.Paths.MihomoRuntimeDir, "ui", "zashboard")
		}
		if opts.WorkDir == "" {
			opts.WorkDir = s.state.Paths.MihomoRuntimeDir
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	report, err := doctor.Run(ctx, opts)
	if err != nil {
		return toolResult{}, err
	}
	return jsonToolResult(report)
}

func (s *Server) callEnvironmentInspect(ctx context.Context, args json.RawMessage) (toolResult, error) {
	var in struct{}
	if err := decodeToolInput(args, &in); err != nil {
		return toolResult{}, err
	}
	opts := envinspect.Options{}
	if s.state != nil {
		opts.Paths = s.state.Paths
	}
	result, err := envinspect.Inspect(ctx, opts)
	if err != nil {
		return toolResult{}, err
	}
	return jsonToolResult(result)
}

func (s *Server) callRunRuntimeSync(ctx context.Context, args json.RawMessage) (toolResult, error) {
	var in struct {
		Config          string `json:"config"`
		RuntimeDir      string `json:"runtime_dir"`
		Core            string `json:"core"`
		Foreground      bool   `json:"foreground"`
		LogFile         string `json:"log_file"`
		ForceConfigTest bool   `json:"force_config_test"`
	}
	if err := decodeToolInput(args, &in); err != nil {
		return toolResult{}, err
	}
	if s.state != nil {
		if in.Config == "" {
			in.Config = s.state.Paths.GeneratedConfig
		}
		if in.Core == "" {
			in.Core = s.state.Paths.CorePath
		}
		if in.RuntimeDir == "" {
			in.RuntimeDir = s.state.Paths.MihomoRuntimeDir
		}
		if in.Core == "" {
			in.Core = s.state.Paths.CorePath
		}
		if in.Config == s.state.Paths.GeneratedConfig {
			if err := s.ensureRunnableConfig(in.Config); err != nil {
				return jsonToolResult(runtimeErrorResult("generated config is unavailable: " + err.Error()))
			}
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	result, err := corerun.Start(ctx, corerun.StartOptions{
		CorePath:            in.Core,
		ConfigPath:          in.Config,
		WorkDir:             in.RuntimeDir,
		LogPath:             in.LogFile,
		Foreground:          in.Foreground,
		SkipConfigTest:      false,
		ValidationCachePath: validationCachePath("", in.RuntimeDir),
		ForceConfigTest:     in.ForceConfigTest,
		OnStage:             startRuntimeStageLogger(ctx),
	})
	if err != nil {
		return jsonToolResult(runtimeErrorResult(err.Error()))
	}
	if s.state != nil {
		if profile, profileErr := runtimeprofile.StatusFor(s.state.Paths.RuntimeProfilePath); profileErr == nil && profile.Mode == runtimeprofile.ModeRouter {
			result.Warnings = append(result.Warnings, "Runtime profile is router; run_runtime only starts Mihomo and does not capture router traffic.")
			result.NextActions = append(result.NextActions, "call router_takeover_apply after user confirmation to install localClash-owned OpenWrt firewall, DNS, and TUN takeover rules")
		}
	}
	return jsonToolResult(result)
}

func (s *Server) callRestartRuntimeSync(ctx context.Context, args json.RawMessage) (toolResult, error) {
	var in struct {
		Config          string `json:"config"`
		RuntimeDir      string `json:"runtime_dir"`
		Core            string `json:"core"`
		Foreground      bool   `json:"foreground"`
		LogFile         string `json:"log_file"`
		TimeoutMS       int    `json:"timeout_ms"`
		Force           bool   `json:"force"`
		ForceConfigTest bool   `json:"force_config_test"`
	}
	if err := decodeToolInput(args, &in); err != nil {
		return toolResult{}, err
	}
	if in.Foreground {
		return jsonToolResult(runtimeErrorResult("foreground=true is not supported by MCP restart_runtime; use the CLI restart command"))
	}
	if s.state != nil {
		if in.Config == "" {
			in.Config = s.state.Paths.GeneratedConfig
		}
		if in.RuntimeDir == "" {
			in.RuntimeDir = s.state.Paths.MihomoRuntimeDir
		}
		if in.Core == "" {
			in.Core = s.state.Paths.CorePath
		}
		if in.Config == s.state.Paths.GeneratedConfig {
			if err := s.ensureRunnableConfig(in.Config); err != nil {
				return jsonToolResult(runtimeErrorResult("generated config is unavailable: " + err.Error()))
			}
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	result, err := corerun.Restart(ctx, corerun.RestartOptions{
		CorePath:            in.Core,
		ConfigPath:          in.Config,
		WorkDir:             in.RuntimeDir,
		LogPath:             in.LogFile,
		ValidationCachePath: validationCachePath("", in.RuntimeDir),
		ForceConfigTest:     in.ForceConfigTest,
		StopTimeout:         time.Duration(in.TimeoutMS) * time.Millisecond,
		ForceKill:           in.Force,
		OnStage:             restartRuntimeStageLogger(ctx),
	})
	if err != nil {
		return jsonToolResult(runtimeErrorResult(err.Error()))
	}
	if s.state != nil {
		if profile, profileErr := runtimeprofile.StatusFor(s.state.Paths.RuntimeProfilePath); profileErr == nil && profile.Mode == runtimeprofile.ModeRouter {
			result.Warnings = append(result.Warnings, "Runtime profile is router; restart_runtime only restarts Mihomo and does not capture router traffic.")
			result.NextActions = append(result.NextActions, "call router_takeover_status to verify existing takeover rules, or router_takeover_apply after user confirmation if takeover is not active")
		}
	}
	return jsonToolResult(result)
}

func restartRuntimeStageLogger(ctx context.Context) func(corerun.RestartStageEvent) {
	return func(event corerun.RestartStageEvent) {
		fields := map[string]any{
			"stage": event.Stage,
		}
		if event.DurationMS > 0 {
			fields["duration_ms"] = event.DurationMS
		}
		if event.PID > 0 {
			fields["pid"] = event.PID
		}
		if event.Error != "" {
			fields["error"] = event.Error
		}
		appendTaskStage(ctx, "stage_"+event.Event, event.Stage, fields)
	}
}

func startRuntimeStageLogger(ctx context.Context) func(corerun.StartStageEvent) {
	return func(event corerun.StartStageEvent) {
		fields := event.Fields
		if fields == nil {
			fields = map[string]any{}
		}
		if event.DurationMS > 0 {
			fields["duration_ms"] = event.DurationMS
		}
		if event.PID > 0 {
			fields["pid"] = event.PID
		}
		if event.Error != "" {
			fields["error"] = event.Error
		}
		appendTaskStage(ctx, "stage_"+event.Event, event.Stage, fields)
	}
}

func configRenderTaskLogger(ctx context.Context, parent string) func(configrender.StageEvent) {
	return func(event configrender.StageEvent) {
		stage := event.Stage
		if parent != "" {
			stage = parent + "." + stage
		}
		logGenericStage(ctx, stage, event.Event, event.DurationMS, event.Error, event.Fields)
	}
}

func configPlanTaskLogger(ctx context.Context) func(configplan.StageEvent) {
	return func(event configplan.StageEvent) {
		logGenericStage(ctx, event.Stage, event.Event, event.DurationMS, event.Error, event.Fields)
	}
}

func localConfigTaskLogger(ctx context.Context, parent string) func(localconfig.StageEvent) {
	return func(event localconfig.StageEvent) {
		stage := event.Stage
		if parent != "" {
			stage = parent + "." + stage
		}
		logGenericStage(ctx, stage, event.Event, event.DurationMS, event.Error, event.Fields)
	}
}

func subscriptionsTaskLogger(ctx context.Context) func(subscriptions.StageEvent) {
	return func(event subscriptions.StageEvent) {
		logGenericStage(ctx, event.Stage, event.Event, event.DurationMS, event.Error, event.Fields)
	}
}

func routerTakeoverTaskLogger(ctx context.Context) func(routertakeover.StageEvent) {
	return func(event routertakeover.StageEvent) {
		logGenericStage(ctx, event.Stage, event.Event, event.DurationMS, event.Error, event.Fields)
	}
}

func logGenericStage(ctx context.Context, stage, event string, durationMS int64, errText string, fields map[string]any) {
	out := map[string]any{}
	for key, value := range fields {
		out[key] = value
	}
	if durationMS > 0 {
		out["duration_ms"] = durationMS
	}
	if errText != "" {
		out["error"] = errText
	}
	appendTaskStage(ctx, "stage_"+event, stage, out)
}

func (s *Server) ensureRunnableConfig(configPath string) error {
	if fileExists(configPath) {
		return nil
	}
	if s.state == nil {
		return fmt.Errorf("missing %s", configPath)
	}
	if s.state.Config.Diagnostic != "" {
		return fmt.Errorf("%s; call config_render before run_runtime", s.state.Config.Diagnostic)
	}
	return fmt.Errorf("missing %s; call config_render before run_runtime", configPath)
}

func runtimeErrorResult(message string) map[string]any {
	return map[string]any{
		"started": false,
		"error":   message,
		"next_actions": []string{
			"call subscriptions_status",
			"call subscriptions_refresh if subscription.gob is unavailable or stale",
			"call run_runtime again after generated/mihomo.yaml can be rendered",
		},
		"warnings": []string{
			"Starting or restarting the proxy runtime may temporarily interrupt network connectivity.",
			"The Agent itself may depend on the current network/proxy path and could be disconnected after this operation.",
		},
	}
}

func (s *Server) applyConfigToolDefaults(in *configToolInput) {
	setDefault := func(value *string, fallback string) {
		if strings.TrimSpace(*value) == "" {
			*value = fallback
		}
	}
	if s.state != nil {
		setDefault(&in.Subscription, s.state.Paths.SubscriptionPath)
		setDefault(&in.RulesCache, s.state.Paths.RulesCacheDir)
		setDefault(&in.RuntimeProfile, s.state.Paths.RuntimeProfilePath)
		setDefault(&in.SubscriptionConfig, s.state.Paths.SubscriptionConfig)
		setDefault(&in.SubscriptionRuntime, s.state.Paths.SubscriptionRuntime)
		setDefault(&in.Output, s.state.Paths.GeneratedConfig)
		setDefault(&in.Core, s.state.Paths.CorePath)
		setDefault(&in.RuntimeDir, s.state.Paths.MihomoRuntimeDir)
		if s.state.Paths.PacksSelectionPath != "" {
			setDefault(&in.Selection, s.state.Paths.PacksSelectionPath)
		}
	}
	setDefault(&in.Config, "localclash.json")
	setDefault(&in.Subscription, "subscription.gob")
	setDefault(&in.RulesCache, filepath.Join(".runtime", "rules", "packs"))
	setDefault(&in.RuntimeProfile, runtimeprofile.DefaultPath)
	setDefault(&in.SubscriptionConfig, "localclash-subscriptions.json")
	setDefault(&in.SubscriptionRuntime, filepath.Join(".runtime", "subscriptions"))
	setDefault(&in.Selection, "localclash-packs.gob")
	setDefault(&in.Output, filepath.Join("generated", "mihomo.yaml"))
	setDefault(&in.RuntimeDir, filepath.Join(".runtime", "mihomo"))
	setDefault(&in.ValidationCache, validationCachePath(in.ValidationCache, in.RuntimeDir))
	setDefault(&in.PatchesDir, filepath.Join(".runtime", "patches"))
}

func missingRenderInputs(in configToolInput) []string {
	var missing []string
	if !fileExists(in.Subscription) {
		missing = append(missing, "subscription")
	}
	return missing
}

func inspectConfigFile(path string) configFileStatus {
	status := configFileStatus{Path: path}
	info, err := os.Stat(path)
	if err != nil {
		if !os.IsNotExist(err) {
			status.Error = err.Error()
		}
		return status
	}
	status.Present = true
	status.Size = info.Size()
	status.ModTime = info.ModTime().UTC().Format(time.RFC3339)
	return status
}

func isGeneratedStale(output string, sources []string) bool {
	outputInfo, err := os.Stat(output)
	if err != nil {
		return true
	}
	for _, source := range sources {
		info, err := os.Stat(source)
		if err != nil {
			continue
		}
		if info.ModTime().After(outputInfo.ModTime()) {
			return true
		}
	}
	return false
}

func listConfigPatches(root string, limit int) []configPatchSummary {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() > entries[j].Name() })
	var out []configPatchSummary
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		patchID := entry.Name()
		summaryPath := filepath.Join(root, patchID, "summary.json")
		item := configPatchSummary{PatchID: patchID, SummaryPath: summaryPath}
		if data, err := os.ReadFile(summaryPath); err == nil {
			var summary struct {
				Valid bool `json:"valid"`
			}
			if json.Unmarshal(data, &summary) == nil {
				item.Valid = &summary.Valid
			}
		}
		out = append(out, item)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func validationCachePath(path, runtimeDir string) string {
	if strings.TrimSpace(path) != "" {
		return path
	}
	return mihomotest.DefaultCachePath(runtimeDir)
}

func setDefault(value *string, fallback string) {
	if strings.TrimSpace(*value) == "" && strings.TrimSpace(fallback) != "" {
		*value = fallback
	}
}

func (s *Server) callConfigConfigure(args json.RawMessage) (toolResult, error) {
	var in struct {
		Config               string `json:"config"`
		RuntimeProfileConfig string `json:"runtime_profile_config"`
		RuntimeProfile       string `json:"runtime_profile"`
		Core                 string `json:"core"`
		PolicyTemplate       string `json:"policy_template"`
		PolicyTemplatesDir   string `json:"policy_templates_dir"`
		RulesCache           string `json:"rules_cache"`
		SubscriptionConfig   string `json:"subscription_config"`
		Subscription         string `json:"subscription"`
		SubscriptionRuntime  string `json:"subscription_runtime"`
	}
	if err := decodeToolInput(args, &in); err != nil {
		return toolResult{}, err
	}
	if s.state != nil {
		setDefault(&in.Config, "localclash.json")
		setDefault(&in.RuntimeProfileConfig, s.state.Paths.RuntimeProfilePath)
		setDefault(&in.RulesCache, s.state.Paths.RulesCacheDir)
		setDefault(&in.SubscriptionConfig, s.state.Paths.SubscriptionConfig)
		setDefault(&in.Subscription, s.state.Paths.SubscriptionPath)
		setDefault(&in.SubscriptionRuntime, s.state.Paths.SubscriptionRuntime)
	}
	setDefault(&in.Config, "localclash.json")
	setDefault(&in.RuntimeProfileConfig, runtimeprofile.DefaultPath)
	setDefault(&in.PolicyTemplatesDir, policytemplate.DefaultDir)
	setDefault(&in.RulesCache, filepath.Join(".runtime", "rules", "packs"))
	setDefault(&in.SubscriptionConfig, "localclash-subscriptions.json")
	setDefault(&in.Subscription, "subscription.gob")
	setDefault(&in.SubscriptionRuntime, filepath.Join(".runtime", "subscriptions"))

	if strings.TrimSpace(in.RuntimeProfile) == "" && strings.TrimSpace(in.Core) == "" && strings.TrimSpace(in.PolicyTemplate) == "" {
		return toolResult{}, fmt.Errorf("config_configure requires at least one of core, runtime_profile, or policy_template")
	}

	changed := []string{}
	var templateSummary *policytemplate.Summary
	configUpdated := false
	if strings.TrimSpace(in.PolicyTemplate) != "" {
		config, summary, err := policytemplate.Build(in.PolicyTemplatesDir, in.PolicyTemplate)
		if err != nil {
			return toolResult{}, err
		}
		if err := validatePolicyTemplateConfig(config, in.RulesCache); err != nil {
			return toolResult{}, err
		}
		if err := localconfig.Write(in.Config, config); err != nil {
			return toolResult{}, err
		}
		templateSummary = &summary
		configUpdated = true
		changed = append(changed, "policy_template")
	}

	var status runtimeprofile.Status
	var err error
	if strings.TrimSpace(in.RuntimeProfile) != "" || strings.TrimSpace(in.Core) != "" {
		status, err = runtimeprofile.Configure(in.RuntimeProfileConfig, in.RuntimeProfile, in.Core)
		if err != nil {
			return toolResult{}, err
		}
		if strings.TrimSpace(in.RuntimeProfile) != "" {
			changed = append(changed, "runtime_profile")
		}
		if strings.TrimSpace(in.Core) != "" {
			changed = append(changed, "core")
		}
	} else {
		status, err = runtimeprofile.StatusFor(in.RuntimeProfileConfig)
		if err != nil {
			return toolResult{}, err
		}
	}
	cacheWarnings := []string{}
	if s.state != nil {
		if strings.TrimSpace(in.RuntimeProfile) != "" || strings.TrimSpace(in.Core) != "" {
			corePath := normalizeMCPStateCorePath(s.state, status.CorePath)
			runtimeRoot := mcpStateRuntimeRoot(s.state)
			if runtimeRoot != "" {
				_, err := appinit.RefreshCoreVersionCache(context.Background(), runtimeRoot, corePath)
				if err != nil {
					cacheWarnings = append(cacheWarnings, "core version cache refresh failed: "+err.Error())
				}
			}
		}
		s.state.Paths.RuntimeProfilePath = in.RuntimeProfileConfig
		s.state.Paths.CorePath = status.CorePath
		s.state.Core.Path = status.CorePath
	}
	subStatus, _ := subscriptions.Status(subscriptions.StatusOptions{
		ConfigPath: in.SubscriptionConfig,
		MergedPath: in.Subscription,
		RuntimeDir: in.SubscriptionRuntime,
	})
	result := configConfigureResult{
		Changed:                   changed,
		ConfigPath:                in.Config,
		ConfigUpdated:             configUpdated,
		RuntimeProfile:            status,
		PolicyTemplate:            templateSummary,
		PolicyTemplatesDir:        in.PolicyTemplatesDir,
		SubscriptionConfigured:    subStatus.Configured,
		EffectiveSubscriptionPath: in.Subscription,
		EffectiveSubscription:     subStatus.Merged.Exists && subStatus.Merged.ProxiesCount > 0,
		NextActions:               configConfigureNextActions(status, subStatus.Merged.Exists && subStatus.Merged.ProxiesCount > 0),
	}
	templates, templateErr := policytemplate.List(in.PolicyTemplatesDir)
	if templateErr != nil {
		result.Warnings = append(result.Warnings, templateErr.Error())
	} else {
		result.AvailablePolicyTemplates = templates
	}
	result.Warnings = append(result.Warnings, cacheWarnings...)
	return jsonToolResult(result)
}

func normalizeMCPStateCorePath(state *appinit.RuntimeState, corePath string) string {
	corePath = strings.TrimSpace(corePath)
	if corePath == "" || filepath.IsAbs(corePath) || state == nil {
		return corePath
	}
	root := strings.TrimSpace(state.Paths.WorkspaceRoot)
	if root == "" {
		root = workspaceRootFromRuntimeProfilePath(state.Paths.RuntimeProfilePath)
	}
	if root == "" || root == "." {
		return corePath
	}
	return filepath.Join(root, corePath)
}

func mcpStateRuntimeRoot(state *appinit.RuntimeState) string {
	if state == nil {
		return ""
	}
	if root := strings.TrimSpace(state.Paths.RuntimeRoot); root != "" {
		return root
	}
	workspaceRoot := workspaceRootFromRuntimeProfilePath(state.Paths.RuntimeProfilePath)
	if workspaceRoot == "" || workspaceRoot == "." {
		return ""
	}
	return filepath.Join(workspaceRoot, ".runtime")
}

func workspaceRootFromRuntimeProfilePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	dir := filepath.Dir(path)
	if dir == "." {
		return ""
	}
	return dir
}

type configConfigureResult struct {
	Changed                   []string                 `json:"changed"`
	ConfigPath                string                   `json:"config"`
	ConfigUpdated             bool                     `json:"config_updated"`
	RuntimeProfile            runtimeprofile.Status    `json:"runtime_profile_status"`
	PolicyTemplate            *policytemplate.Summary  `json:"policy_template,omitempty"`
	AvailablePolicyTemplates  []policytemplate.Summary `json:"available_policy_templates"`
	PolicyTemplatesDir        string                   `json:"policy_templates_dir"`
	SubscriptionConfigured    bool                     `json:"subscription_configured"`
	EffectiveSubscriptionPath string                   `json:"effective_subscription_path"`
	EffectiveSubscription     bool                     `json:"effective_subscription"`
	Warnings                  []string                 `json:"warnings,omitempty"`
	NextActions               []string                 `json:"next_actions"`
}

func validatePolicyTemplateConfig(config localconfig.Config, rulesCache string) error {
	var packIndex *rules.PackIndex
	if len(config.Packs) > 0 {
		var err error
		packIndex, err = rules.LoadPackIndex(rules.PackIndexPath(rulesCache))
		if err != nil {
			return err
		}
	}
	for _, pack := range config.Packs {
		ref, err := packIndex.ResolvePackRef(pack.Source, pack.Pack)
		if err != nil {
			return fmt.Errorf("policy_template references unavailable pack %q: %w", rules.PackKey(pack.Source, pack.Pack), err)
		}
		if strings.TrimSpace(pack.Type) != "" && ref.Type != pack.Type {
			return fmt.Errorf("policy_template pack %q is type %q, want %q", rules.PackKey(pack.Source, pack.Pack), ref.Type, pack.Type)
		}
	}
	return nil
}

func configConfigureNextActions(status runtimeprofile.Status, effectiveSubscription bool) []string {
	if !effectiveSubscription {
		return []string{
			"call subscriptions_status",
			"call subscriptions_configure if no subscription source is configured",
			"call subscriptions_refresh to create subscription.gob",
			"call config_render after subscription.gob is available",
		}
	}
	actions := []string{
		"call config_render to rebuild generated/mihomo.yaml from durable localClash state",
		"call runtime_status to inspect whether Mihomo is already running",
		"call restart_runtime after user confirmation if Mihomo is already running and should load the updated generated config",
		"call run_runtime after user confirmation if Mihomo is not running",
	}
	if status.Mode == runtimeprofile.ModeRouter {
		actions = append(actions, "call router_takeover_apply after user confirmation to capture router traffic")
	}
	return actions
}

func (s *Server) callRuntimeProfileStatus(args json.RawMessage) (toolResult, error) {
	var in struct {
		Config string `json:"config"`
	}
	if err := decodeToolInput(args, &in); err != nil {
		return toolResult{}, err
	}
	if s.state != nil && in.Config == "" {
		in.Config = s.state.Paths.RuntimeProfilePath
	}
	status, err := runtimeprofile.StatusFor(in.Config)
	if err != nil {
		return toolResult{}, err
	}
	return jsonToolResult(status)
}

func (s *Server) callRuntimeStatus(args json.RawMessage) (toolResult, error) {
	var in struct {
		Config     string `json:"config"`
		RuntimeDir string `json:"runtime_dir"`
		Core       string `json:"core"`
		LogFile    string `json:"log_file"`
	}
	if err := decodeToolInput(args, &in); err != nil {
		return toolResult{}, err
	}
	if s.state != nil {
		if in.Config == "" {
			in.Config = s.state.Paths.GeneratedConfig
		}
		if in.RuntimeDir == "" {
			in.RuntimeDir = s.state.Paths.MihomoRuntimeDir
		}
		if in.Core == "" {
			in.Core = s.state.Paths.CorePath
		}
	}
	return jsonToolResult(corerun.Status(corerun.StatusOptions{
		CorePath:   in.Core,
		ConfigPath: in.Config,
		WorkDir:    in.RuntimeDir,
		LogPath:    in.LogFile,
	}))
}

type routerTakeoverInput struct {
	RuntimeProfile string `json:"runtime_profile"`
	Config         string `json:"config"`
	RuntimeDir     string `json:"runtime_dir"`
	LogFile        string `json:"log_file"`
	StateDir       string `json:"state_dir"`
	DNSPort        int    `json:"dns_port"`
	RedirPort      int    `json:"redir_port"`
	TunDevice      string `json:"tun_device"`
	DryRun         bool   `json:"dry_run"`
}

func (s *Server) routerTakeoverOptions(args json.RawMessage) (routertakeover.Options, error) {
	var in routerTakeoverInput
	if err := decodeToolInput(args, &in); err != nil {
		return routertakeover.Options{}, err
	}
	if s.state != nil {
		if in.RuntimeProfile == "" {
			in.RuntimeProfile = s.state.Paths.RuntimeProfilePath
		}
		if in.Config == "" {
			in.Config = s.state.Paths.GeneratedConfig
		}
		if in.RuntimeDir == "" {
			in.RuntimeDir = s.state.Paths.MihomoRuntimeDir
		}
	}
	return routertakeover.Options{
		RuntimeProfile: in.RuntimeProfile,
		ConfigPath:     in.Config,
		RuntimeDir:     in.RuntimeDir,
		LogPath:        in.LogFile,
		StateDir:       in.StateDir,
		DNSPort:        in.DNSPort,
		RedirPort:      in.RedirPort,
		TunDevice:      in.TunDevice,
		DryRun:         in.DryRun,
	}, nil
}

func (s *Server) callRouterTakeoverStatus(ctx context.Context, args json.RawMessage) (toolResult, error) {
	opts, err := s.routerTakeoverOptions(args)
	if err != nil {
		return toolResult{}, err
	}
	result, err := routertakeover.Status(ctx, opts)
	if err != nil {
		return toolResult{}, err
	}
	return jsonToolResult(result)
}

func (s *Server) callRouterTakeoverApplySync(ctx context.Context, args json.RawMessage) (toolResult, error) {
	opts, err := s.routerTakeoverOptions(args)
	if err != nil {
		return toolResult{}, err
	}
	opts.OnStage = routerTakeoverTaskLogger(ctx)
	result, err := routertakeover.Apply(ctx, opts)
	if err != nil {
		return toolResult{}, err
	}
	return jsonToolResult(result)
}

func (s *Server) callRouterTakeoverStopSync(ctx context.Context, args json.RawMessage) (toolResult, error) {
	opts, err := s.routerTakeoverOptions(args)
	if err != nil {
		return toolResult{}, err
	}
	opts.OnStage = routerTakeoverTaskLogger(ctx)
	result, err := routertakeover.Stop(ctx, opts)
	if err != nil {
		return toolResult{}, err
	}
	return jsonToolResult(result)
}

func (s *Server) callStopRuntimeSync(ctx context.Context, args json.RawMessage) (toolResult, error) {
	var in struct {
		RuntimeProfile string `json:"runtime_profile"`
		Config         string `json:"config"`
		Core           string `json:"core"`
		RuntimeDir     string `json:"runtime_dir"`
		LogFile        string `json:"log_file"`
		StateDir       string `json:"state_dir"`
		DNSPort        int    `json:"dns_port"`
		RedirPort      int    `json:"redir_port"`
		TunDevice      string `json:"tun_device"`
		TimeoutMS      int    `json:"timeout_ms"`
		Force          bool   `json:"force"`
	}
	if err := decodeToolInput(args, &in); err != nil {
		return toolResult{}, err
	}
	if s.state != nil {
		if in.RuntimeProfile == "" {
			in.RuntimeProfile = s.state.Paths.RuntimeProfilePath
		}
		if in.Config == "" {
			in.Config = s.state.Paths.GeneratedConfig
		}
		if in.RuntimeDir == "" {
			in.RuntimeDir = s.state.Paths.MihomoRuntimeDir
		}
	}
	if !in.Force && (s.state != nil || strings.TrimSpace(in.RuntimeProfile) != "") {
		finish := startTaskStage(ctx, "takeover_guard_status", map[string]any{"runtime_profile": in.RuntimeProfile})
		takeover, takeoverErr := routerTakeoverStatus(ctx, routertakeover.Options{
			RuntimeProfile: in.RuntimeProfile,
			ConfigPath:     in.Config,
			RuntimeDir:     in.RuntimeDir,
			LogPath:        in.LogFile,
			StateDir:       in.StateDir,
			DNSPort:        in.DNSPort,
			RedirPort:      in.RedirPort,
			TunDevice:      in.TunDevice,
		})
		if takeoverErr != nil {
			finishTaskStage(finish, takeoverErr, nil)
		} else {
			finishTaskStage(finish, nil, map[string]any{"effective": takeover.Effective})
		}
		if takeoverErr == nil && takeover.Effective {
			finish = startTaskStage(ctx, "runtime_status_for_refusal", map[string]any{"runtime_dir": in.RuntimeDir})
			status := corerun.Status(corerun.StatusOptions{
				CorePath:   in.Core,
				ConfigPath: in.Config,
				WorkDir:    in.RuntimeDir,
				LogPath:    in.LogFile,
			})
			finishTaskStage(finish, nil, map[string]any{"running": status.Running, "pid": status.PID})
			return jsonToolResult(corerun.StopResult{
				Refused:    true,
				WasRunning: status.Running,
				PID:        status.PID,
				RuntimeDir: status.RuntimeDir,
				PIDFile:    status.PIDFile,
				Error:      "router takeover is effective; stopping Mihomo would break the router traffic path",
				Warnings: []string{
					"router takeover is currently effective and depends on the Mihomo runtime.",
					"stop_runtime refused to stop Mihomo because router traffic may still be redirected to it.",
				},
				NextActions: []string{
					"call router_takeover_stop after user confirmation, then call stop_runtime again",
					"or call stop_runtime with force=true if the user explicitly accepts breaking the active router traffic path",
				},
			})
		}
	}
	finish := startTaskStage(ctx, "stop_runtime", map[string]any{"runtime_dir": in.RuntimeDir, "timeout_ms": in.TimeoutMS, "force": in.Force})
	result, err := corerun.Stop(corerun.StopOptions{
		CorePath:   in.Core,
		ConfigPath: in.Config,
		WorkDir:    in.RuntimeDir,
		Timeout:    time.Duration(in.TimeoutMS) * time.Millisecond,
		ForceKill:  in.Force,
	})
	if err != nil {
		finishTaskStage(finish, err, nil)
		return toolResult{}, err
	}
	finishTaskStage(finish, nil, map[string]any{"stopped": result.Stopped, "was_running": result.WasRunning, "pid": result.PID, "error": result.Error})
	return jsonToolResult(result)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func resultResponse(id json.RawMessage, result any) *rpcResponse {
	return &rpcResponse{JSONRPC: "2.0", ID: id, Result: result}
}

func errorResponse(id json.RawMessage, code int, message string) *rpcResponse {
	return &rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: message}}
}

func jsonToolResult(value any) (toolResult, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return toolResult{}, err
	}
	return toolResult{
		Content:           []toolContent{{Type: "text", Text: string(data)}},
		StructuredContent: value,
	}, nil
}
