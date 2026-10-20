// Copyright 2026 Roland Aigner (https://www.aignerdevelopment.com)
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package dialect

import (
	"strings"

	"harnessator.org/harnessator/internal/policy"
	"harnessator.org/harnessator/internal/provider"
)

// claudeDialect renders the Claude Code settings env block. Claude Code
// speaks only the anthropic-compatible family; gateways expose it as a
// dedicated base URL (LiteLLM's /anthropic path), which is why profiles
// carry one endpoint per style.
type claudeDialect struct{}

func (claudeDialect) Target() string { return "claude" }

func (claudeDialect) Styles() []string { return []string{provider.StyleAnthropicCompat} }

// claudeTierVars is the tier-alias convention: catalog
// alias -> anthropic default-model env var. Walk order keeps the old
// semantics: aliases are visited sorted, later matches overwrite the same
// var (e.g. "sonnet" wins over "big").
var claudeTierVars = []struct {
	aliases []string
	key     string
}{
	{[]string{"opus"}, "ANTHROPIC_DEFAULT_OPUS_MODEL"},
	{[]string{"sonnet", "big"}, "ANTHROPIC_DEFAULT_SONNET_MODEL"},
	{[]string{"haiku", "fast", "small"}, "ANTHROPIC_SMALL_FAST_MODEL"},
}

func (d claudeDialect) Govern(p *provider.Profile, eff *policy.Effective) (Result, error) {
	_, ep, ok := p.StylesSpoken(d.Styles())
	if !ok {
		return Result{}, ErrNoStyle
	}
	env := map[string]any{"ANTHROPIC_BASE_URL": ep.BaseURL}
	if def := defaultReal(p); def != "" {
		env["ANTHROPIC_MODEL"] = def
	}
	// the sub-agent model pin that fixes the "sub-agents ignore
	// configured model" collision
	if eff != nil && eff.SubagentModel != "" {
		if r, ok := p.Resolve(eff.SubagentModel); ok {
			env["CLAUDE_CODE_SUBAGENT_MODEL"] = r
		}
	}
	allowed := map[string]bool{}
	for _, id := range allowedModels(p, eff) {
		allowed[id] = true
	}
	for _, alias := range p.SortedAliasList() {
		real := p.Models[alias]
		if !allowed[real] {
			continue
		}
		for _, tier := range claudeTierVars {
			for _, a := range tier.aliases {
				if strings.ToLower(alias) == a {
					env[tier.key] = real
				}
			}
		}
	}
	return Result{Env: env}, nil
}
