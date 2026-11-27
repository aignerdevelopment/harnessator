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
	"fmt"
	"strings"

	"harnessator.org/harnessator/internal/policy"
	"harnessator.org/harnessator/internal/provider"
)

// codexDialect renders the managed [model_providers] stanza of
// .codex/config.toml. Codex speaks the OpenAI families (chat completions
// and the responses API); anthropic-compatible endpoints are not
// speakable here and yield ErrNoStyle (loud skip in the adapter).
type codexDialect struct{}

func (codexDialect) Target() string { return "codex" }

func (codexDialect) Styles() []string {
	return []string{provider.StyleOpenAICompat, provider.StyleOpenAIResponses}
}

// codexWire maps an API style to codex's wire_api field.
var codexWire = map[string]string{
	provider.StyleOpenAICompat:    "chat",
	provider.StyleOpenAIResponses: "responses",
}

func (d codexDialect) Govern(p *provider.Profile, _ *policy.Effective) (Result, error) {
	style, ep, ok := p.StylesSpoken(d.Styles())
	if !ok {
		return Result{}, ErrNoStyle
	}
	var b strings.Builder
	fmt.Fprintf(&b, "model_provider = %q\n", p.Name)
	if def := defaultReal(p); def != "" {
		fmt.Fprintf(&b, "model = %q\n", def)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "[model_providers.%s]\n", p.Name)
	fmt.Fprintf(&b, "name = %q\n", p.Name)
	fmt.Fprintf(&b, "base_url = %q\n", ep.BaseURL)
	if ep.AuthEnv != "" {
		fmt.Fprintf(&b, "env_key = %q\n", ep.AuthEnv)
	}
	fmt.Fprintf(&b, "wire_api = %q\n\n", codexWire[style])
	return Result{TOML: b.String()}, nil
}
