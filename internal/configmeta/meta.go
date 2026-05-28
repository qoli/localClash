package configmeta

const Key = "x-localclash"

type Metadata struct {
	Version int             `yaml:"version" json:"version"`
	Base    BaseMetadata    `yaml:"base" json:"base"`
	Overlay OverlayMetadata `yaml:"overlay" json:"overlay"`
}

type BaseMetadata struct {
	Modifiable  bool   `yaml:"modifiable" json:"modifiable"`
	Description string `yaml:"description" json:"description"`
}

type OverlayMetadata struct {
	Modifiable    bool                  `yaml:"modifiable" json:"modifiable"`
	Packs         []OverlayPack         `yaml:"packs" json:"packs"`
	ProxyGroups   []OverlayProxyGroup   `yaml:"proxy_groups" json:"proxy_groups"`
	PolicyGroups  []OverlayPolicyGroup  `yaml:"policy_groups" json:"policy_groups"`
	RuleProviders []OverlayRuleProvider `yaml:"rule_providers" json:"rule_providers"`
	Rules         []OverlayRule         `yaml:"rules" json:"rules"`
	Insertion     string                `yaml:"insertion" json:"insertion"`
}

type OverlayPack struct {
	Source string `yaml:"source" json:"source"`
	Pack   string `yaml:"pack" json:"pack"`
	Type   string `yaml:"type" json:"type"`
	Target string `yaml:"target" json:"target"`
}

type OverlayProxyGroup struct {
	ID    string   `yaml:"id" json:"id"`
	Mode  string   `yaml:"mode" json:"mode"`
	Nodes []string `yaml:"nodes" json:"nodes"`
}

type OverlayPolicyGroup struct {
	ID    string   `yaml:"id" json:"id"`
	Mode  string   `yaml:"mode" json:"mode"`
	Exits []string `yaml:"exits" json:"exits"`
}

type OverlayRuleProvider struct {
	Name     string `yaml:"name" json:"name"`
	Behavior string `yaml:"behavior" json:"behavior"`
	Type     string `yaml:"type" json:"type"`
}

type OverlayRule struct {
	Type     string `yaml:"type" json:"type"`
	Provider string `yaml:"provider" json:"provider"`
	Value    string `yaml:"value,omitempty" json:"value,omitempty"`
	Target   string `yaml:"target" json:"target"`
}
