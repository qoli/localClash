package customsitesapply

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"localclash/internal/appinit"
	"localclash/internal/configrender"
	"localclash/internal/corerun"
	"localclash/internal/customsites"
	"localclash/internal/mihomoapi"
	"localclash/internal/mihomotest"
)

type MihomoRequestFunc func(context.Context, mihomoapi.RequestOptions) (mihomoapi.Response, error)

// TransactRuntime applies the product custom-site transaction against the
// runtime state shared by the CLI, LuCI adapter, and MCP server.
func TransactRuntime(ctx context.Context, state appinit.RuntimeState, input TransactionInput, progress func(stage, message string)) (TransactionResult, error) {
	paths := customsites.Paths{Proxy: state.Paths.CustomSitesProxy, Direct: state.Paths.CustomSitesDirect}
	return Transact(ctx, runtimeTransactionOptions(state, paths, input, progress))
}

func runtimeTransactionOptions(state appinit.RuntimeState, paths customsites.Paths, input TransactionInput, progress func(stage, message string)) TransactionOptions {
	attestationPath := mihomotest.DefaultAttestationPath(state.Paths.MihomoRuntimeDir)
	return TransactionOptions{
		Paths:           paths,
		GeneratedConfig: state.Paths.GeneratedConfig,
		AttestationPath: attestationPath,
		Input:           input,
		Hooks: TransactionHooks{
			Progress: progress,
			Render: func(ctx context.Context, candidatePaths customsites.Paths, output string) error {
				_, err := configrender.Render(configrender.Options{
					SourcePath:         state.Paths.SubscriptionPath,
					OutputPath:         output,
					PacksSelectionPath: state.Paths.PacksSelectionPath,
					RulesCacheDir:      state.Paths.RulesCacheDir,
					RuntimeProfilePath: state.Paths.RuntimeProfilePath,
					CustomSitesProxy:   candidatePaths.Proxy,
					CustomSitesDirect:  candidatePaths.Direct,
					Force:              true,
				})
				return err
			},
			Validate: func(ctx context.Context, configPath, candidateAttestation string) (ValidationStatus, error) {
				result, err := mihomotest.Test(ctx, mihomotest.TestOptions{
					ValidationOptions: mihomotest.ValidationOptions{
						CorePath:   state.Paths.CorePath,
						ConfigPath: configPath,
						WorkDir:    state.Paths.MihomoRuntimeDir,
						CachePath:  mihomotest.DefaultCachePath(state.Paths.MihomoRuntimeDir),
						Force:      true,
					},
					Record:             true,
					AttestationPath:    candidateAttestation,
					PromotedConfigPath: state.Paths.GeneratedConfig,
				})
				return ValidationStatus{ConfigSHA256: result.ConfigSHA256}, err
			},
			RuntimeStatus: func() (RuntimeStatus, error) {
				status := corerun.Status(corerun.StatusOptions{
					CorePath:   state.Paths.CorePath,
					ConfigPath: state.Paths.GeneratedConfig,
					WorkDir:    state.Paths.MihomoRuntimeDir,
				})
				return RuntimeStatus{Running: status.Running}, nil
			},
			Reload: func(ctx context.Context, configSHA256 string) (ReloadStatus, error) {
				validation, err := mihomotest.ValidateCached(ctx, mihomotest.ValidationOptions{
					CorePath:   state.Paths.CorePath,
					ConfigPath: state.Paths.GeneratedConfig,
					WorkDir:    state.Paths.MihomoRuntimeDir,
					CachePath:  mihomotest.DefaultCachePath(state.Paths.MihomoRuntimeDir),
					Force:      true,
				})
				if err != nil {
					return ReloadStatus{}, fmt.Errorf("validate promoted config before hot reload: %w", err)
				}
				if validation.ConfigSHA256 != configSHA256 {
					return ReloadStatus{}, fmt.Errorf("promoted config hash %s does not match transaction hash %s", validation.ConfigSHA256, configSHA256)
				}
				result, err := corerun.Restart(ctx, corerun.RestartOptions{
					CorePath:        state.Paths.CorePath,
					ConfigPath:      state.Paths.GeneratedConfig,
					WorkDir:         state.Paths.MihomoRuntimeDir,
					Strategy:        corerun.RestartStrategyHotReload,
					ConfigSHA256:    configSHA256,
					StopTimeout:     5 * time.Second,
					AttestationPath: attestationPath,
				})
				if err != nil {
					return ReloadStatus{}, err
				}
				if result.Error != "" {
					return ReloadStatus{}, errors.New(result.Error)
				}
				if !result.Reloaded {
					return ReloadStatus{}, errors.New("Mihomo hot reload did not report success")
				}
				client, err := mihomoapi.NewFromConfig(state.Paths.GeneratedConfig)
				if err != nil {
					return ReloadStatus{Reloaded: true}, err
				}
				pair, err := customsites.Load(paths)
				if err != nil {
					return ReloadStatus{Reloaded: true}, fmt.Errorf("load promoted custom site state for runtime read-back: %w", err)
				}
				attempts, err := WaitForRuntimeReadBack(ctx, pair, client.Request, 10*time.Second, 150*time.Millisecond)
				if err != nil {
					return ReloadStatus{Reloaded: true}, err
				}
				if progress != nil {
					progress("read_back", fmt.Sprintf("Runtime semantics converged after %d attempt(s).", attempts))
				}
				status := corerun.Status(corerun.StatusOptions{
					CorePath:   state.Paths.CorePath,
					ConfigPath: state.Paths.GeneratedConfig,
					WorkDir:    state.Paths.MihomoRuntimeDir,
				})
				if !status.Running {
					return ReloadStatus{Reloaded: true}, errors.New("runtime stopped before hot reload read-back completed")
				}
				return ReloadStatus{Reloaded: true, ReadBack: true}, nil
			},
		},
	}
}

func WaitForRuntimeReadBack(ctx context.Context, pair customsites.Pair, request MihomoRequestFunc, timeout, interval time.Duration) (int, error) {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	if interval <= 0 {
		interval = 150 * time.Millisecond
	}
	readBackCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var lastErr error
	attempt := 0
	for {
		attempt++
		rulesResponse, err := request(readBackCtx, mihomoapi.RequestOptions{Method: "GET", Path: "/rules", Timeout: 2 * time.Second, MaxBytes: 4 * 1024 * 1024})
		if err != nil {
			lastErr = fmt.Errorf("read back Mihomo rules after hot reload: %w", err)
		} else {
			proxiesResponse, proxiesErr := request(readBackCtx, mihomoapi.RequestOptions{Method: "GET", Path: "/proxies", Timeout: 2 * time.Second, MaxBytes: 2 * 1024 * 1024})
			if proxiesErr != nil {
				lastErr = fmt.Errorf("read back Mihomo proxies after hot reload: %w", proxiesErr)
			} else if verifyErr := VerifyRuntimeReadBack(pair, rulesResponse, proxiesResponse); verifyErr == nil {
				return attempt, nil
			} else {
				lastErr = verifyErr
			}
		}

		timer := time.NewTimer(interval)
		select {
		case <-readBackCtx.Done():
			timer.Stop()
			if lastErr == nil {
				lastErr = readBackCtx.Err()
			}
			return attempt, fmt.Errorf("custom site runtime read-back did not converge within %s: %w", timeout, lastErr)
		case <-timer.C:
		}
	}
}

func VerifyRuntimeReadBack(pair customsites.Pair, rulesResponse, proxiesResponse mihomoapi.Response) error {
	if rulesResponse.Truncated || proxiesResponse.Truncated {
		return errors.New("custom site runtime read-back response was truncated")
	}
	proxiesDoc, ok := proxiesResponse.JSON.(map[string]any)
	if !ok {
		return errors.New("Mihomo /proxies read-back is not a JSON object")
	}
	proxyMap, ok := proxiesDoc["proxies"].(map[string]any)
	if !ok {
		return errors.New("Mihomo /proxies read-back is missing proxies")
	}
	for _, name := range []string{customsites.ProxyPolicyGroup, customsites.DirectPolicyGroup} {
		_, exists := proxyMap[name]
		if pair.Initialized && !exists {
			return fmt.Errorf("Mihomo /proxies read-back is missing reserved policy group %q", name)
		}
		if !pair.Initialized && exists {
			return fmt.Errorf("Mihomo /proxies read-back unexpectedly retains reserved policy group %q", name)
		}
	}
	rulesDoc, ok := rulesResponse.JSON.(map[string]any)
	if !ok {
		return errors.New("Mihomo /rules read-back is not a JSON object")
	}
	rawRules, ok := rulesDoc["rules"].([]any)
	if !ok {
		return errors.New("Mihomo /rules read-back is missing rules")
	}
	actual := make([]map[string]any, 0)
	for _, raw := range rawRules {
		rule, ok := raw.(map[string]any)
		if !ok {
			return errors.New("Mihomo /rules read-back contains a non-object rule")
		}
		proxy, _ := rule["proxy"].(string)
		if proxy == customsites.ProxyPolicyGroup || proxy == customsites.DirectPolicyGroup {
			actual = append(actual, rule)
		}
	}
	expected := append([]customsites.Entry{}, pair.Proxy.Entries...)
	for index := range expected {
		expected[index].Route = customsites.RouteProxy
	}
	direct := append([]customsites.Entry{}, pair.Direct.Entries...)
	for index := range direct {
		direct[index].Route = customsites.RouteDirect
	}
	expected = append(expected, direct...)
	sort.SliceStable(expected, func(i, j int) bool { return expected[i].Sequence > expected[j].Sequence })
	if len(actual) != len(expected) {
		return fmt.Errorf("Mihomo /rules custom site count %d does not match durable count %d", len(actual), len(expected))
	}
	for index, entry := range expected {
		wantType := "DomainSuffix"
		if entry.Match == customsites.MatchWildcard {
			wantType = "DomainWildcard"
		}
		wantProxy := customsites.DirectPolicyGroup
		if entry.Route == customsites.RouteProxy {
			wantProxy = customsites.ProxyPolicyGroup
		}
		gotType, _ := actual[index]["type"].(string)
		gotPayload, _ := actual[index]["payload"].(string)
		gotProxy, _ := actual[index]["proxy"].(string)
		if gotType != wantType || gotPayload != entry.Pattern || gotProxy != wantProxy {
			return fmt.Errorf("Mihomo /rules custom site rule %d mismatch: got type=%q payload=%q proxy=%q, want type=%q payload=%q proxy=%q", index+1, gotType, gotPayload, gotProxy, wantType, entry.Pattern, wantProxy)
		}
	}
	return nil
}
