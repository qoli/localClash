package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"localclash/internal/poolcontract"

	"gopkg.in/yaml.v3"
)

func chatGPTSmartGroups(config []byte, provider string) ([]string, error) {
	var loaded struct {
		Providers map[string]struct {
			Override struct {
				Expressions []string `yaml:"override-expr"`
			} `yaml:"override"`
		} `yaml:"proxy-providers"`
		Groups []struct {
			Name string   `yaml:"name"`
			Type string   `yaml:"type"`
			Use  []string `yaml:"use"`
		} `yaml:"proxy-groups"`
	}
	if err := yaml.Unmarshal(config, &loaded); err != nil {
		return nil, errors.New("decode loaded ChatGPT provider configuration")
	}
	if !slices.Contains(loaded.Providers[provider].Override.Expressions, poolcontract.ChatGPTSmartOverride) {
		return nil, nil
	}
	var groups []string
	for _, group := range loaded.Groups {
		if group.Type == "smart" && slices.Contains(group.Use, provider) {
			groups = append(groups, group.Name)
		}
	}
	return groups, nil
}

type chatGPTFixedSelection struct {
	group    string
	previous string
	next     string
}

func (live *activeRuntime) chatGPTFixedSelections(ctx context.Context, groups []string, renamed map[string]string) ([]chatGPTFixedSelection, error) {
	var changes []chatGPTFixedSelection
	for _, group := range groups {
		var status struct {
			Fixed string `json:"fixed"`
		}
		if err := live.readJSON(ctx, "/proxies/", group, &status); err != nil {
			return nil, fmt.Errorf("read ChatGPT selection in %q: %w", group, err)
		}
		if name := renamed[status.Fixed]; name != "" && name != status.Fixed {
			changes = append(changes, chatGPTFixedSelection{group: group, previous: status.Fixed, next: name})
		}
	}
	return changes, nil
}

func (live *activeRuntime) chatGPTProviderChanges(ctx context.Context, groups []string, previous, candidate []byte) ([]chatGPTFixedSelection, error) {
	before, err := poolcontract.ChatGPTProviderMembers(previous)
	if err != nil {
		return nil, err
	}
	after, err := poolcontract.ChatGPTProviderMembers(candidate)
	if err != nil {
		return nil, err
	}
	renamed := make(map[string]string)
	for original, name := range before {
		next := after[original]
		if next != "" && next != name {
			renamed[name] = next
		}
	}
	if len(renamed) == 0 {
		return nil, nil
	}
	return live.chatGPTFixedSelections(ctx, groups, renamed)
}

func (live *activeRuntime) restoreChatGPTFixedSelections(ctx context.Context, changes []chatGPTFixedSelection, rollback bool) error {
	for _, change := range changes {
		name := change.next
		if rollback {
			name = change.previous
		}
		body := struct {
			Name string `json:"name"`
		}{Name: name}
		if err := live.requestJSON(ctx, http.MethodPut, "/proxies/", change.group, body, nil); err != nil {
			return fmt.Errorf("restore ChatGPT selection in %q: %w", change.group, err)
		}
	}
	return nil
}
