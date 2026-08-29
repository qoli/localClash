package localconfig

import (
	"fmt"
	"strings"
)

const (
	CustomProxySitesPolicyGroupName  = "自訂代理網站"
	CustomDirectSitesPolicyGroupName = "自訂直連網站"
)

// ValidateMutablePolicyGroupName rejects policy-group names owned by the
// custom-site renderer. AI and patch mutation paths must not replace them.
func ValidateMutablePolicyGroupName(name string) error {
	switch strings.TrimSpace(name) {
	case CustomProxySitesPolicyGroupName, CustomDirectSitesPolicyGroupName:
		return fmt.Errorf("reserved_policy_group_name: %s", strings.TrimSpace(name))
	default:
		return nil
	}
}
