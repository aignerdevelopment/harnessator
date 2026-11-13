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
	"harnessator.org/harnessator/internal/policy"
	"harnessator.org/harnessator/internal/provider"
)

// opencodeDialect renders the opencode.json provider/model section.
// opencode consumes openai-compatible gateways via
// @ai-sdk/openai-compatible and anthropic-compatible ones via
// @ai-sdk/anthropic; the npm package is therefore chosen by the API
// style, not spelled out by the profile.
type opencodeDialect struct{}

func (opencodeDialect) Target() string { return "opencode" }

func (opencodeDialect) Styles() []string {
	return []string{provider.StyleOpenAICompat, provider.StyleAnthropicCompat}
}

var opencodeNPM = map[string]string{
	provider.StyleOpenAICompat:    "@ai-sdk/openai-compatible",
	provider.StyleAnthropicCompat: "@ai-sdk/anthropic",
}

func (d opencodeDialect) Govern(p *provider.Profile, eff *policy.Effective) (Result, error) {
	style, ep, ok := p.StylesSpoken(d.Styles())
	if !ok {
		return Result{}, ErrNoStyle
	}
	opts := map[string]any{"baseURL": ep.BaseURL}
	if ep.AuthEnv != "" {
		opts["apiKey"] = "{env:" + ep.AuthEnv + "}" // NAME only - value stays ambient
	}
	models := map[string]any{}
	for _, id := range allowedModels(p, eff) {
		models[id] = map[string]any{"name": id}
	}
	frag := map[string]any{
		"provider": map[string]any{
			p.Name: map[string]any{"npm": opencodeNPM[style], "options": opts, "models": models},
		},
	}
	if def := defaultReal(p); def != "" {
		frag["model"] = p.Name + "/" + def
	}
	return Result{JSON: frag}, nil
}
