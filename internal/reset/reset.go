package reset

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"localclash/internal/corerun"
	"localclash/internal/workspace"
)

const ConfirmationPhrase = "reset localclash"
const FullConfirmationPhrase = "delete localclash workspace"

type Options struct {
	DryRun                   bool
	Yes                      bool
	Full                     bool
	Workspace                string
	WorkspaceSource          string
	RequireExplicitWorkspace bool
	In                       io.Reader
	Out                      io.Writer
}

type Target struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Exists bool   `json:"exists"`
}

type Result struct {
	Full            bool     `json:"full"`
	DryRun          bool     `json:"dry_run"`
	Workspace       string   `json:"workspace,omitempty"`
	WorkspaceSource string   `json:"workspace_source,omitempty"`
	Deleted         []Target `json:"deleted"`
	Skipped         []Target `json:"skipped"`
}

type resolvedWorkspace struct {
	Path     string
	Source   string
	Explicit bool
}

func Run(opts Options) (Result, error) {
	if opts.In == nil {
		opts.In = os.Stdin
	}
	if opts.Out == nil {
		opts.Out = os.Stdout
	}
	ws, err := resolveWorkspace(opts)
	if err != nil {
		return Result{}, err
	}
	if !opts.DryRun {
		if status := corerun.Status(corerun.StatusOptions{WorkDir: filepath.Join(ws.Path, ".runtime", "mihomo")}); status.Running {
			return Result{}, fmt.Errorf("mihomo runtime is running (pid %d); stop it before reset", status.PID)
		} else if status.ProcessAlive {
			return Result{}, fmt.Errorf("mihomo runtime is running or pid file points to a live process (pid %d); stop it before reset", status.PID)
		}
	}
	targets, err := buildPlanForWorkspace(opts, ws)
	if err != nil {
		return Result{}, err
	}
	result := Result{Full: opts.Full, DryRun: opts.DryRun, Workspace: ws.Path, WorkspaceSource: ws.Source}
	for _, target := range targets {
		if target.Exists {
			result.Deleted = append(result.Deleted, target)
		} else {
			result.Skipped = append(result.Skipped, target)
		}
	}
	printPlan(opts.Out, result)
	if opts.DryRun || len(result.Deleted) == 0 {
		return result, nil
	}
	if !opts.Yes {
		if err := confirm(opts.In, opts.Out, opts.Full); err != nil {
			return Result{}, err
		}
	}
	if err := removeTargets(result.Deleted, opts.Full); err != nil {
		return Result{}, err
	}
	fmt.Fprintln(opts.Out, "Reset complete.")
	return result, nil
}

func BuildPlanForOptions(opts Options) ([]Target, error) {
	ws, err := resolveWorkspace(opts)
	if err != nil {
		return nil, err
	}
	return buildPlanForWorkspace(opts, ws)
}

func BuildPlan() ([]Target, error) {
	return BuildPlanForOptions(Options{})
}

func buildPlanForWorkspace(opts Options, ws resolvedWorkspace) ([]Target, error) {
	if opts.Full {
		return buildFullPlanForWorkspace(ws)
	}
	paths := []Target{
		{Path: filepath.Join(ws.Path, ".runtime"), Kind: "directory"},
		{Path: filepath.Join(ws.Path, "generated"), Kind: "directory"},
		{Path: filepath.Join(ws.Path, "localclash.json"), Kind: "file"},
		{Path: filepath.Join(ws.Path, "localclash-packs.gob"), Kind: "file"},
		{Path: filepath.Join(ws.Path, "localclash-subscriptions.json"), Kind: "file"},
		{Path: filepath.Join(ws.Path, "localclash-runtime.json"), Kind: "file"},
		{Path: filepath.Join(ws.Path, "profiles"), Kind: "directory"},
	}
	subscriptions, err := filepath.Glob(filepath.Join(ws.Path, "subscription*.gob"))
	if err != nil {
		return nil, err
	}
	sort.Strings(subscriptions)
	for _, path := range subscriptions {
		paths = append(paths, Target{Path: path, Kind: "file"})
	}
	paths = dedupeTargets(paths)
	for i := range paths {
		exists, kind := inspect(paths[i].Path, paths[i].Kind)
		paths[i].Exists = exists
		paths[i].Kind = kind
	}
	return paths, nil
}

func BuildFullPlan() ([]Target, error) {
	return BuildPlanForOptions(Options{Full: true})
}

func buildFullPlanForWorkspace(ws resolvedWorkspace) ([]Target, error) {
	if err := validateFullResetWorkspace(ws); err != nil {
		return nil, err
	}
	exists, kind := inspect(ws.Path, "directory")
	return []Target{{Path: ws.Path, Kind: kind, Exists: exists}}, nil
}

func dedupeTargets(targets []Target) []Target {
	seen := map[string]bool{}
	out := make([]Target, 0, len(targets))
	for _, target := range targets {
		clean := filepath.Clean(strings.TrimSpace(target.Path))
		if clean == "." || clean == string(filepath.Separator) || strings.HasPrefix(clean, "..") || seen[clean] {
			continue
		}
		target.Path = clean
		seen[clean] = true
		out = append(out, target)
	}
	return out
}

func inspect(path, fallbackKind string) (bool, string) {
	info, err := os.Lstat(path)
	if err != nil {
		return false, fallbackKind
	}
	if info.IsDir() {
		return true, "directory"
	}
	return true, "file"
}

func resolveWorkspace(opts Options) (resolvedWorkspace, error) {
	if raw := strings.TrimSpace(opts.Workspace); raw != "" {
		source := strings.TrimSpace(opts.WorkspaceSource)
		if source == "" {
			source = "explicit"
		}
		path, err := canonicalWorkspacePath(raw)
		if err != nil {
			return resolvedWorkspace{}, err
		}
		ws := resolvedWorkspace{Path: path, Source: source, Explicit: isExplicitWorkspaceSource(source)}
		if (opts.Full || opts.RequireExplicitWorkspace) && !ws.Explicit {
			return resolvedWorkspace{}, fmt.Errorf("full reset requires an explicit workspace from --workspace or %s", workspace.EnvVar)
		}
		return ws, nil
	}
	if raw := strings.TrimSpace(os.Getenv(workspace.EnvVar)); raw != "" {
		path, err := canonicalWorkspacePath(raw)
		if err != nil {
			return resolvedWorkspace{}, err
		}
		return resolvedWorkspace{Path: path, Source: "env:" + workspace.EnvVar, Explicit: true}, nil
	}
	if opts.Full || opts.RequireExplicitWorkspace {
		return resolvedWorkspace{}, fmt.Errorf("full reset requires an explicit workspace from --workspace or %s", workspace.EnvVar)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return resolvedWorkspace{}, err
	}
	path, err := canonicalWorkspacePath(cwd)
	if err != nil {
		return resolvedWorkspace{}, err
	}
	return resolvedWorkspace{Path: path, Source: "cwd", Explicit: false}, nil
}

func canonicalWorkspacePath(raw string) (string, error) {
	path := filepath.Clean(strings.TrimSpace(raw))
	if path == "" || path == "." {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		path = cwd
	}
	if !filepath.IsAbs(path) {
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", err
		}
		path = abs
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	return filepath.Clean(path), nil
}

func isExplicitWorkspaceSource(source string) bool {
	source = strings.TrimSpace(source)
	return source == "explicit" || source == "flag:--workspace" || source == "env:"+workspace.EnvVar || strings.HasPrefix(source, "test")
}

func removeTargets(targets []Target, full bool) error {
	if full {
		if len(targets) != 1 {
			return fmt.Errorf("full reset expected exactly one workspace target, got %d", len(targets))
		}
		target := filepath.Clean(targets[0].Path)
		if err := validateFullResetWorkspace(resolvedWorkspace{Path: target, Source: "delete", Explicit: true}); err != nil {
			return err
		}
		parent := filepath.Dir(target)
		if err := os.Chdir(parent); err != nil {
			return fmt.Errorf("leave workspace before full reset: %w", err)
		}
		if err := os.RemoveAll(target); err != nil {
			return fmt.Errorf("remove %s: %w", target, err)
		}
		return nil
	}
	for _, target := range targets {
		if err := os.RemoveAll(target.Path); err != nil {
			return fmt.Errorf("remove %s: %w", target.Path, err)
		}
	}
	return nil
}

func validateFullResetWorkspace(ws resolvedWorkspace) error {
	clean := filepath.Clean(strings.TrimSpace(ws.Path))
	if clean == "" {
		return fmt.Errorf("full reset target is empty")
	}
	if !filepath.IsAbs(clean) {
		return fmt.Errorf("full reset target %q must be absolute", clean)
	}
	if clean == string(filepath.Separator) {
		return fmt.Errorf("full reset refuses to delete filesystem root")
	}
	parent := filepath.Dir(clean)
	if parent == clean || parent == string(filepath.Separator) {
		return fmt.Errorf("full reset refuses to delete top-level directory %q", clean)
	}
	if workspace.IsProtectedPath(clean) {
		return fmt.Errorf("full reset refuses to delete protected directory %q", clean)
	}
	info, err := os.Lstat(clean)
	if err != nil {
		return fmt.Errorf("full reset workspace %q is not accessible: %w", clean, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("full reset workspace %q is not a directory", clean)
	}
	if workspace.LooksLikeSourceCheckout(clean) {
		return fmt.Errorf("full reset refuses to delete source checkout %q", clean)
	}
	if !workspace.HasMarker(clean) {
		return fmt.Errorf("full reset refuses to delete workspace %q: missing %s marker", clean, workspace.MarkerName)
	}
	return nil
}

func printPlan(out io.Writer, result Result) {
	if result.DryRun && result.Full {
		fmt.Fprintln(out, "localClash full workspace reset dry run.")
	} else if result.DryRun {
		fmt.Fprintln(out, "localClash factory reset dry run.")
	} else if result.Full {
		fmt.Fprintln(out, "localClash full workspace reset.")
	} else {
		fmt.Fprintln(out, "localClash factory reset.")
	}
	fmt.Fprintln(out)
	if len(result.Deleted) == 0 {
		fmt.Fprintln(out, "Will delete: nothing")
	} else {
		fmt.Fprintln(out, "Will delete:")
		for _, target := range result.Deleted {
			fmt.Fprintf(out, "  - %s (%s)\n", target.Path, target.Kind)
		}
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Will keep:")
	if result.Full {
		fmt.Fprintln(out, "  - localClash core binary outside the workspace")
		fmt.Fprintln(out, "  - LuCI package files")
		fmt.Fprintln(out, "  - OpenWrt service wrapper files")
	} else {
		fmt.Fprintln(out, "  - bin/")
		fmt.Fprintln(out, "  - policy-templates/")
		fmt.Fprintln(out, "  - rule-sources/")
		fmt.Fprintln(out, "  - source code, docs, and scripts")
	}
}

func confirm(in io.Reader, out io.Writer, full bool) error {
	phrase := ConfirmationPhrase
	if full {
		phrase = FullConfirmationPhrase
	}
	fmt.Fprintf(out, "\nType %q to continue: ", phrase)
	scanner := bufio.NewScanner(in)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return err
		}
		return fmt.Errorf("reset cancelled")
	}
	if strings.TrimSpace(scanner.Text()) != phrase {
		return fmt.Errorf("reset cancelled")
	}
	return nil
}
