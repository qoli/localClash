package customsitesapply

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"localclash/internal/customsites"
	"localclash/internal/mihomotest"
)

const (
	OperationAdd    = "add"
	OperationDelete = "delete"
)

var transactionMu sync.Mutex

type TransactionInput struct {
	Version   int    `json:"version"`
	Operation string `json:"operation"`
	Pattern   string `json:"pattern,omitempty"`
	Route     string `json:"route,omitempty"`
	ID        string `json:"id,omitempty"`
}

type ValidationStatus struct {
	ConfigSHA256 string `json:"config_sha256"`
}

type RuntimeStatus struct {
	Running bool `json:"running"`
}

type ReloadStatus struct {
	Reloaded bool `json:"reloaded"`
	ReadBack bool `json:"read_back"`
}

type TransactionHooks struct {
	Render        func(context.Context, customsites.Paths, string) error
	Validate      func(context.Context, string, string) (ValidationStatus, error)
	RuntimeStatus func() (RuntimeStatus, error)
	Reload        func(context.Context, string) (ReloadStatus, error)
}

type TransactionOptions struct {
	Paths           customsites.Paths
	GeneratedConfig string
	AttestationPath string
	Input           TransactionInput
	Now             time.Time
	Hooks           TransactionHooks
}

type ApplyStatus struct {
	Validated        bool   `json:"validated"`
	Promoted         bool   `json:"promoted"`
	RuntimeRunning   bool   `json:"runtime_running"`
	Reloaded         bool   `json:"reloaded"`
	ReadBack         bool   `json:"read_back"`
	Effective        bool   `json:"effective"`
	PendingNextStart bool   `json:"pending_next_start"`
	RolledBack       bool   `json:"rolled_back"`
	ConfigSHA256     string `json:"config_sha256,omitempty"`
	GeneratedConfig  string `json:"generated_config"`
	Attestation      string `json:"attestation"`
}

type TransactionResult struct {
	Operation string               `json:"operation"`
	Entry     customsites.Entry    `json:"entry"`
	Snapshot  customsites.Snapshot `json:"snapshot"`
	Apply     ApplyStatus          `json:"apply"`
}

func Transact(ctx context.Context, opts TransactionOptions) (TransactionResult, error) {
	transactionMu.Lock()
	defer transactionMu.Unlock()

	if err := validateTransactionOptions(opts); err != nil {
		return TransactionResult{}, err
	}
	pair, err := customsites.Load(opts.Paths)
	if err != nil {
		return TransactionResult{}, err
	}
	candidate, entry, err := applyInput(pair, opts.Input, opts.Now)
	if err != nil {
		return TransactionResult{}, err
	}
	currentSnapshot, err := customsites.SnapshotChecked(pair)
	if err != nil {
		return TransactionResult{}, err
	}
	result := TransactionResult{
		Operation: opts.Input.Operation,
		Entry:     entry,
		Snapshot:  currentSnapshot,
		Apply: ApplyStatus{
			GeneratedConfig: opts.GeneratedConfig,
			Attestation:     opts.AttestationPath,
		},
	}

	tempParent := filepath.Dir(filepath.Dir(opts.Paths.Proxy))
	tempRoot, err := os.MkdirTemp(tempParent, ".custom-sites-transaction-")
	if err != nil {
		return result, fmt.Errorf("create custom site candidate directory: %w", err)
	}
	defer os.RemoveAll(tempRoot)
	candidatePaths := customsites.Paths{
		Proxy:  filepath.Join(tempRoot, customsites.ProxyFilename),
		Direct: filepath.Join(tempRoot, customsites.DirectFilename),
	}
	if err := writeCandidateDocuments(candidatePaths, candidate); err != nil {
		return result, err
	}
	candidateConfig := filepath.Join(tempRoot, "config.yaml")
	candidateAttestation := filepath.Join(tempRoot, "config-test-attestation.json")
	if err := opts.Hooks.Render(ctx, candidatePaths, candidateConfig); err != nil {
		return result, fmt.Errorf("render custom site candidate config: %w", err)
	}
	validation, err := opts.Hooks.Validate(ctx, candidateConfig, candidateAttestation)
	if err != nil {
		return result, fmt.Errorf("validate custom site candidate config: %w", err)
	}
	if strings.TrimSpace(validation.ConfigSHA256) == "" {
		return result, errors.New("validate custom site candidate config: config_sha256 is required")
	}
	if _, err := mihomotest.VerifyConfigHash(candidateConfig, validation.ConfigSHA256); err != nil {
		return result, fmt.Errorf("validate custom site candidate config hash: %w", err)
	}
	if err := preparePromotedAttestation(candidateAttestation, opts.GeneratedConfig, validation.ConfigSHA256); err != nil {
		return result, err
	}
	result.Apply.Validated = true
	result.Apply.ConfigSHA256 = validation.ConfigSHA256

	runtimeStatus, err := opts.Hooks.RuntimeStatus()
	if err != nil {
		return result, fmt.Errorf("inspect runtime before custom site promotion: %w", err)
	}
	result.Apply.RuntimeRunning = runtimeStatus.Running
	targets := []promotionTarget{
		{candidate: candidatePaths.Proxy, final: opts.Paths.Proxy},
		{candidate: candidatePaths.Direct, final: opts.Paths.Direct},
		{candidate: candidateConfig, final: opts.GeneratedConfig},
		{candidate: candidateAttestation, final: opts.AttestationPath},
	}
	backups, err := backupPromotionTargets(targets)
	if err != nil {
		return result, fmt.Errorf("backup custom site promotion targets: %w", err)
	}
	if err := promoteTargets(targets); err != nil {
		rollbackErr := restorePromotionTargets(backups)
		result.Apply.RolledBack = rollbackErr == nil
		if rollbackErr != nil {
			return result, fmt.Errorf("promote custom site transaction: %v; rollback files: %w", err, rollbackErr)
		}
		return result, fmt.Errorf("promote custom site transaction: %w", err)
	}
	result.Apply.Promoted = true
	result.Snapshot, err = customsites.SnapshotChecked(candidate)
	if err != nil {
		rollbackErr := restorePromotionTargets(backups)
		result.Apply.Promoted = rollbackErr != nil
		result.Apply.RolledBack = rollbackErr == nil
		return result, fmt.Errorf("read promoted custom site snapshot: %v; rollback files: %v", err, rollbackErr)
	}
	if !runtimeStatus.Running {
		result.Apply.PendingNextStart = true
		return result, nil
	}

	reload, reloadErr := opts.Hooks.Reload(ctx, validation.ConfigSHA256)
	if reloadErr == nil && (!reload.Reloaded || !reload.ReadBack) {
		reloadErr = fmt.Errorf("hot reload did not prove loaded state: reloaded=%t read_back=%t", reload.Reloaded, reload.ReadBack)
	}
	if reloadErr == nil {
		result.Apply.Reloaded = true
		result.Apply.ReadBack = true
		result.Apply.Effective = true
		return result, nil
	}

	fileRollbackErr := restorePromotionTargets(backups)
	var runtimeRollbackErr error
	if fileRollbackErr == nil {
		runtimeRollbackErr = rollbackRuntime(ctx, opts.Hooks, backups, opts.GeneratedConfig)
	} else {
		runtimeRollbackErr = errors.New("runtime rollback skipped because prior config files were not safely restored")
	}
	result.Apply.Promoted = fileRollbackErr != nil
	result.Apply.RolledBack = fileRollbackErr == nil && runtimeRollbackErr == nil
	if fileRollbackErr == nil {
		if restored, loadErr := customsites.Load(opts.Paths); loadErr == nil {
			result.Snapshot, loadErr = customsites.SnapshotChecked(restored)
			if loadErr != nil {
				fileRollbackErr = fmt.Errorf("read restored custom site snapshot: %w", loadErr)
			}
		} else {
			fileRollbackErr = fmt.Errorf("read restored custom site snapshot: %w", loadErr)
		}
	}
	if fileRollbackErr != nil || runtimeRollbackErr != nil {
		return result, fmt.Errorf("hot reload custom site transaction: %v; rollback files: %v; rollback runtime: %v", reloadErr, fileRollbackErr, runtimeRollbackErr)
	}
	return result, fmt.Errorf("hot reload custom site transaction: %w; prior files and runtime restored", reloadErr)
}

func validateTransactionOptions(opts TransactionOptions) error {
	if opts.Input.Version != customsites.SchemaVersion {
		return fmt.Errorf("version must be %d", customsites.SchemaVersion)
	}
	if strings.TrimSpace(opts.Paths.Proxy) == "" || strings.TrimSpace(opts.Paths.Direct) == "" {
		return errors.New("custom site proxy and direct paths are required")
	}
	if strings.TrimSpace(opts.GeneratedConfig) == "" {
		return errors.New("generated config path is required")
	}
	if strings.TrimSpace(opts.AttestationPath) == "" {
		return errors.New("config-test attestation path is required")
	}
	if opts.Hooks.Render == nil || opts.Hooks.Validate == nil || opts.Hooks.RuntimeStatus == nil || opts.Hooks.Reload == nil {
		return errors.New("custom site transaction hooks are required")
	}
	switch strings.TrimSpace(opts.Input.Operation) {
	case OperationAdd:
		if strings.TrimSpace(opts.Input.ID) != "" {
			return errors.New("add operation must not include id")
		}
	case OperationDelete:
		if strings.TrimSpace(opts.Input.Pattern) != "" || strings.TrimSpace(opts.Input.Route) != "" {
			return errors.New("delete operation must not include pattern or route")
		}
	default:
		return fmt.Errorf("operation must be %q or %q", OperationAdd, OperationDelete)
	}
	return nil
}

func applyInput(pair customsites.Pair, input TransactionInput, now time.Time) (customsites.Pair, customsites.Entry, error) {
	switch strings.TrimSpace(input.Operation) {
	case OperationAdd:
		return customsites.Add(pair, input.Route, input.Pattern, now)
	case OperationDelete:
		return customsites.Delete(pair, input.ID)
	default:
		return customsites.Pair{}, customsites.Entry{}, fmt.Errorf("unsupported custom site operation %q", input.Operation)
	}
}

func writeCandidateDocuments(paths customsites.Paths, pair customsites.Pair) error {
	for _, item := range []struct {
		path     string
		document customsites.Document
	}{
		{path: paths.Proxy, document: pair.Proxy},
		{path: paths.Direct, document: pair.Direct},
	} {
		data, err := customsites.MarshalDocument(item.document)
		if err != nil {
			return err
		}
		if err := os.WriteFile(item.path, data, 0o644); err != nil {
			return fmt.Errorf("write custom site candidate %q: %w", item.path, err)
		}
	}
	return nil
}

func preparePromotedAttestation(path, promotedConfig, expectedSHA string) error {
	attestation, err := mihomotest.ReadAttestation(path)
	if err != nil {
		return fmt.Errorf("read custom site candidate attestation: %w", err)
	}
	if attestation.ConfigSHA256 != expectedSHA {
		return fmt.Errorf("custom site candidate attestation hash %s does not match validation hash %s", attestation.ConfigSHA256, expectedSHA)
	}
	attestation.Config = promotedConfig
	attestation.PromotedConfig = promotedConfig
	if err := mihomotest.WriteAttestation(path, attestation); err != nil {
		return fmt.Errorf("prepare promoted custom site attestation: %w", err)
	}
	return nil
}

type promotionTarget struct {
	candidate string
	final     string
}

type promotionBackup struct {
	path   string
	exists bool
	data   []byte
	mode   os.FileMode
}

func backupPromotionTargets(targets []promotionTarget) ([]promotionBackup, error) {
	backups := make([]promotionBackup, 0, len(targets))
	for _, target := range targets {
		info, err := os.Stat(target.final)
		if errors.Is(err, os.ErrNotExist) {
			backups = append(backups, promotionBackup{path: target.final})
			continue
		}
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("promotion target %q is not a regular file", target.final)
		}
		data, err := os.ReadFile(target.final)
		if err != nil {
			return nil, err
		}
		backups = append(backups, promotionBackup{path: target.final, exists: true, data: data, mode: info.Mode().Perm()})
	}
	return backups, nil
}

func promoteTargets(targets []promotionTarget) error {
	for _, target := range targets {
		if err := os.MkdirAll(filepath.Dir(target.final), 0o755); err != nil {
			return err
		}
		if err := os.Rename(target.candidate, target.final); err != nil {
			return err
		}
	}
	return nil
}

func restorePromotionTargets(backups []promotionBackup) error {
	var restoreErrors []error
	for _, backup := range backups {
		if !backup.exists {
			if err := os.Remove(backup.path); err != nil && !errors.Is(err, os.ErrNotExist) {
				restoreErrors = append(restoreErrors, err)
			}
			continue
		}
		if err := atomicWriteFile(backup.path, backup.data, backup.mode); err != nil {
			restoreErrors = append(restoreErrors, err)
		}
	}
	return errors.Join(restoreErrors...)
}

func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".custom-sites-restore-")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(mode); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}

func rollbackRuntime(ctx context.Context, hooks TransactionHooks, backups []promotionBackup, generatedConfig string) error {
	var priorConfig *promotionBackup
	for index := range backups {
		if backups[index].path == generatedConfig {
			priorConfig = &backups[index]
			break
		}
	}
	if priorConfig == nil || !priorConfig.exists {
		return errors.New("prior generated config is unavailable for runtime rollback")
	}
	sha, err := mihomotest.ConfigSHA256(generatedConfig)
	if err != nil {
		return err
	}
	reload, err := hooks.Reload(ctx, sha)
	if err != nil {
		return err
	}
	if !reload.Reloaded || !reload.ReadBack {
		return fmt.Errorf("prior runtime reload did not prove loaded state: reloaded=%t read_back=%t", reload.Reloaded, reload.ReadBack)
	}
	return nil
}
