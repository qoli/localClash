package policytemplate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"localclash/internal/localconfig"
)

const (
	DefaultDir                = "policy-templates"
	TemplateMinimal           = "minimal"
	TemplateLocalClashDefault = "localclash-default"
)

type File struct {
	ID          string             `yaml:"id" json:"id"`
	Name        string             `yaml:"name" json:"name"`
	Description string             `yaml:"description" json:"description"`
	Default     bool               `yaml:"default,omitempty" json:"default,omitempty"`
	Config      localconfig.Config `yaml:"config" json:"config"`
	Patches     []PatchRef         `yaml:"patches,omitempty" json:"patches,omitempty"`
	Path        string             `yaml:"-" json:"path,omitempty"`
}

type PatchRef struct {
	ID          string `yaml:"id" json:"id"`
	Path        string `yaml:"path" json:"path"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}

type PatchFile struct {
	ID          string             `yaml:"id" json:"id"`
	Description string             `yaml:"description,omitempty" json:"description,omitempty"`
	Config      localconfig.Config `yaml:"config" json:"config"`
	Path        string             `yaml:"-" json:"path,omitempty"`
}

type Summary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Default     bool   `json:"default,omitempty"`
	Path        string `json:"path,omitempty"`
}

func List(dir string) ([]Summary, error) {
	templates, err := loadAll(dir)
	if err != nil {
		return nil, err
	}
	out := make([]Summary, 0, len(templates))
	for _, template := range templates {
		out = append(out, summaryFor(template))
	}
	return out, nil
}

func Build(dir, id string) (localconfig.Config, Summary, error) {
	id = normalizeID(id)
	templates, err := loadAll(dir)
	if err != nil {
		return localconfig.Config{}, Summary{}, err
	}
	for _, template := range templates {
		if template.ID != id {
			continue
		}
		config, err := buildTemplateConfig(template)
		if err != nil {
			return localconfig.Config{}, Summary{}, err
		}
		if config.Version == 0 {
			config.Version = localconfig.ConfigSchemaVersion
		}
		if strings.TrimSpace(config.PolicyTemplate) == "" {
			config.PolicyTemplate = template.ID
		}
		return config, summaryFor(template), nil
	}
	return localconfig.Config{}, Summary{}, fmt.Errorf("unknown policy_template %q; supported templates: %s", id, strings.Join(idsFromTemplates(templates), ", "))
}

func buildTemplateConfig(template File) (localconfig.Config, error) {
	config := template.Config
	for _, patchRef := range template.Patches {
		patch, err := loadPatchFile(template, patchRef)
		if err != nil {
			return localconfig.Config{}, err
		}
		config = mergeConfig(config, patch.Config)
	}
	return config, nil
}

func loadPatchFile(template File, ref PatchRef) (PatchFile, error) {
	if strings.TrimSpace(ref.ID) == "" {
		return PatchFile{}, fmt.Errorf("%s: patch id is required", template.Path)
	}
	if strings.TrimSpace(ref.Path) == "" {
		return PatchFile{}, fmt.Errorf("%s: patch %q path is required", template.Path, ref.ID)
	}
	path := ref.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(filepath.Dir(template.Path), path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return PatchFile{}, err
	}
	var patch PatchFile
	if err := json.Unmarshal(data, &patch); err != nil {
		return PatchFile{}, fmt.Errorf("%s: %w", path, err)
	}
	patch.Path = path
	if strings.TrimSpace(patch.ID) == "" {
		return PatchFile{}, fmt.Errorf("%s: id is required", path)
	}
	if patch.ID != ref.ID {
		return PatchFile{}, fmt.Errorf("%s: patch id %q does not match manifest id %q", path, patch.ID, ref.ID)
	}
	if strings.TrimSpace(patch.Config.PolicyTemplate) != "" && patch.Config.PolicyTemplate != template.ID {
		return PatchFile{}, fmt.Errorf("%s: config.policy_template must be empty or match template id %q", path, template.ID)
	}
	return patch, nil
}

func mergeConfig(base localconfig.Config, patch localconfig.Config) localconfig.Config {
	if base.Version == 0 {
		base.Version = localconfig.ConfigSchemaVersion
	}
	if patch.Version > base.Version {
		base.Version = patch.Version
	}
	if strings.TrimSpace(base.PolicyTemplate) == "" {
		base.PolicyTemplate = patch.PolicyTemplate
	}
	if base.ProxyGroups == nil {
		base.ProxyGroups = map[string]localconfig.ProxyGroup{}
	}
	for id, group := range patch.ProxyGroups {
		base.ProxyGroups[id] = group
	}
	if base.PolicyGroups == nil {
		base.PolicyGroups = map[string]localconfig.PolicyGroup{}
	}
	for id, group := range patch.PolicyGroups {
		base.PolicyGroups[id] = group
	}
	base.CustomRules = mergeCustomRules(base.CustomRules, patch.CustomRules)
	base.RuleProviders = mergeRuleProviders(base.RuleProviders, patch.RuleProviders)
	base.Packs = mergePacks(base.Packs, patch.Packs)
	if strings.TrimSpace(patch.FallbackTarget) != "" {
		base.FallbackTarget = patch.FallbackTarget
	}
	if base.Version < localconfig.ConfigSchemaVersion {
		base.Version = localconfig.ConfigSchemaVersion
	}
	return base
}

func mergePacks(base []localconfig.Pack, patch []localconfig.Pack) []localconfig.Pack {
	merged := append([]localconfig.Pack{}, base...)
	index := map[string]int{}
	for i, item := range merged {
		index[packKey(item.Source, item.Pack)] = i
	}
	for _, item := range patch {
		key := packKey(item.Source, item.Pack)
		if i, ok := index[key]; ok {
			merged[i] = item
			continue
		}
		index[key] = len(merged)
		merged = append(merged, item)
	}
	return merged
}

func packKey(source, pack string) string {
	return strings.TrimSpace(source) + "/" + strings.TrimSpace(pack)
}

func mergeCustomRules(base []localconfig.CustomRule, patch []localconfig.CustomRule) []localconfig.CustomRule {
	merged := append([]localconfig.CustomRule{}, base...)
	index := map[string]int{}
	for i, item := range merged {
		index[strings.TrimSpace(item.ID)] = i
	}
	for _, item := range patch {
		id := strings.TrimSpace(item.ID)
		if i, ok := index[id]; ok {
			merged[i] = item
			continue
		}
		index[id] = len(merged)
		merged = append(merged, item)
	}
	return merged
}

func mergeRuleProviders(base []localconfig.ExternalRuleProvider, patch []localconfig.ExternalRuleProvider) []localconfig.ExternalRuleProvider {
	merged := append([]localconfig.ExternalRuleProvider{}, base...)
	index := map[string]int{}
	for i, item := range merged {
		index[strings.TrimSpace(item.ID)] = i
	}
	for _, item := range patch {
		id := strings.TrimSpace(item.ID)
		if i, ok := index[id]; ok {
			merged[i] = item
			continue
		}
		index[id] = len(merged)
		merged = append(merged, item)
	}
	return merged
}

func IDs(dir string) ([]string, error) {
	templates, err := loadAll(dir)
	if err != nil {
		return nil, err
	}
	return idsFromTemplates(templates), nil
}

func loadAll(dir string) ([]File, error) {
	dir = normalizeDir(dir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var templates []File
	for _, entry := range entries {
		if entry.IsDir() || shouldSkip(entry.Name()) {
			continue
		}
		template, err := loadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		templates = append(templates, template)
	}
	sort.Slice(templates, func(i, j int) bool { return templates[i].ID < templates[j].ID })
	if len(templates) == 0 {
		return nil, fmt.Errorf("no policy templates found in %q", dir)
	}
	return templates, nil
}

func loadFile(path string) (File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return File{}, err
	}
	var template File
	if err := json.Unmarshal(data, &template); err != nil {
		return File{}, fmt.Errorf("%s: %w", path, err)
	}
	template.Path = path
	if err := validate(template, path); err != nil {
		return File{}, err
	}
	return template, nil
}

func validate(template File, path string) error {
	if strings.TrimSpace(template.ID) == "" {
		return fmt.Errorf("%s: id is required", path)
	}
	if strings.TrimSpace(template.Name) == "" {
		return fmt.Errorf("%s: name is required", path)
	}
	hasConfig := template.Config.Version != 0 ||
		strings.TrimSpace(template.Config.PolicyTemplate) != "" ||
		len(template.Config.ProxyGroups) > 0 ||
		len(template.Config.PolicyGroups) > 0 ||
		len(template.Config.Packs) > 0 ||
		len(template.Config.CustomRules) > 0 ||
		len(template.Config.RuleProviders) > 0
	if template.Config.Version == 0 {
		template.Config.Version = localconfig.ConfigSchemaVersion
	}
	if strings.TrimSpace(template.Config.PolicyTemplate) != "" && template.Config.PolicyTemplate != template.ID {
		return fmt.Errorf("%s: config.policy_template must be empty or match id %q", path, template.ID)
	}
	if len(template.Patches) == 0 && !hasConfig {
		return fmt.Errorf("%s: config or patches is required", path)
	}
	return nil
}

func summaryFor(template File) Summary {
	return Summary{
		ID:          template.ID,
		Name:        template.Name,
		Description: template.Description,
		Default:     template.Default,
		Path:        filepath.ToSlash(template.Path),
	}
}

func idsFromTemplates(templates []File) []string {
	ids := make([]string, 0, len(templates))
	for _, template := range templates {
		ids = append(ids, template.ID)
	}
	sort.Strings(ids)
	return ids
}

func normalizeID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return TemplateMinimal
	}
	return id
}

func normalizeDir(dir string) string {
	if strings.TrimSpace(dir) == "" {
		return DefaultDir
	}
	return dir
}

func shouldSkip(name string) bool {
	if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "._") {
		return true
	}
	ext := strings.ToLower(filepath.Ext(name))
	return ext != ".json"
}
