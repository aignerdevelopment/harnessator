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

package provider

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// Import reads the LIVE, working configuration of installed harnesses
// and produces provider profiles - the governance equivalent of
// "import what already works, then govern it". harvest does it for
// components; this does it for providers.
//
// Hard rules, inherited from the product's core:
//   - env-var NAMES travel, secret VALUES never (an apiKeyHelper or a
//     literal key in a config file is reported, not copied);
//   - one profile per configured provider, because the harnesses allow
//     multiple (opencode's provider map, codex's [model_providers.*]);
//   - harnesses that cannot be mapped honestly say so in one line -
//     the skip speaks (the tune phase doctrine, applied to import).

// Imported is one provider profile discovered from a live config.
type Imported struct {
	Harness string   // which harness's config it came from
	ID      string   // component id to register under
	Origin  string   // the config file it was read from
	Profile *Profile // parsed, governance-shaped
	Notes   []string // findings worth a human line (kept secrets etc.)
}

// Result reports the full run, importable or not.
type Result struct {
	Imported []Imported
	Skipped  []string // "cursor: model control is server-side - nothing to import"
	Failures []string // unreadable/malformed configs (loud, never a silent drop)
}

// slugID keeps component ids to the schema's alphabet.
func slugID(s string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(s) {
		alnum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' || r == '/'
		if alnum {
			if r == '/' {
				r = '-'
			}
			b.WriteRune(r)
			lastDash = false
		} else if !lastDash && b.Len() > 0 {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-.")
	if out == "" {
		out = "imported"
	}
	return out
}

// Import dispatches over harnesses ("*" expands to the installed set).
// home is the user's home dir; cwd the working directory (project-local
// configs count as "working environment" too).
func Import(harnesses []string, home, cwd string) Result {
	var r Result
	if len(harnesses) == 1 && harnesses[0] == "*" {
		harnesses = []string{"opencode", "claude", "codex", "gemini", "cline", "copilot", "cursor", "antigravity", "pi"}
	}
	for _, h := range harnesses {
		imps, notes, skipped, failures := importOne(h, home, cwd)
		r.Imported = append(r.Imported, imps...)
		for _, n := range notes {
			r.Skipped = append(r.Skipped, fmt.Sprintf("%s: %s", h, n))
		}
		for _, s := range skipped {
			r.Skipped = append(r.Skipped, fmt.Sprintf("%s: %s", h, s))
		}
		r.Failures = append(r.Failures, failures...)
	}
	return r
}

func importOne(h, home, cwd string) (imps []Imported, notes, skipped, failures []string) {
	switch h {
	case "claude":
		return importClaude(home)
	case "opencode":
		return importOpencode(home, cwd)
	case "codex":
		return importCodex(home)
	case "copilot", "cursor":
		return nil, nil, []string{"model control is server-side - nothing local to import"}, nil
	case "gemini":
		p := filepath.Join(home, ".gemini", "settings.json")
		if _, err := os.Stat(p); err != nil {
			return nil, nil, []string{"no live config at ~/.gemini/settings.json"}, nil
		}
		return nil, nil, []string{"Google-native auth; no API-style mapping yet (v1) - see harnessator tune"}, nil
	case "cline":
		p := filepath.Join(home, ".cline", "data", "settings", "providers.json")
		if _, err := os.Stat(p); err != nil {
			return nil, nil, []string{"no live config at ~/.cline/data/settings/providers.json"}, nil
		}
		return nil, nil, []string{"provider state can embed secret VALUES - import from cline is deliberately off in v1 (wire an env-name profile: see the import source README)"}, nil
	case "antigravity", "pi":
		return nil, nil, []string{"config surface not mapped yet - contributions welcome via a source MR"}, nil
	default:
		return nil, nil, nil, []string{fmt.Sprintf("unknown harness %q (importable: opencode, claude, codex, gemini, cline, copilot, cursor, antigravity, pi, *)", h)}
	}
}

// --- claude: env block of settings.json -----------------------------

func importClaude(home string) (imps []Imported, notes, skipped, failures []string) {
	path := filepath.Join(home, ".claude", "settings.json")
	var doc struct {
		Env          map[string]string `json:"env"`
		APIKeyHelper string            `json:"apiKeyHelper"`
		Model        string            `json:"model"`
		ModelSetting map[string]any    `json:"modelSettings"`
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, []string{"no live config at ~/.claude/settings.json"}, nil
		}
		return nil, nil, nil, []string{fmt.Sprintf("%s: unreadable (%v)", path, err)}
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, nil, nil, []string{fmt.Sprintf("%s: malformed JSON (%v)", path, err)}
	}
	base := doc.Env["ANTHROPIC_BASE_URL"]
	if base == "" {
		notes = append(notes, "no ANTHROPIC_BASE_URL in the env block - claude runs on Anthropic-direct or its own OAuth: nothing gateway-shaped to import")
		return
	}
	p := &Profile{Name: "claude-gateway", Styles: map[string]Endpoint{}}
	p.Styles[StyleAnthropicCompat] = Endpoint{BaseURL: base}
	switch {
	case doc.Env["ANTHROPIC_AUTH_TOKEN"] != "" || hasKey(doc.Env, "ANTHROPIC_AUTH_TOKEN"):
		p.AuthEnv = "ANTHROPIC_AUTH_TOKEN"
	case doc.Env["ANTHROPIC_API_KEY"] != "" || hasKey(doc.Env, "ANTHROPIC_API_KEY"):
		p.AuthEnv = "ANTHROPIC_API_KEY"
	}
	if doc.APIKeyHelper != "" {
		notes = append(notes, "an apiKeyHelper is configured - it typically embeds a secret VALUE: NOT imported; move to an env var and set auth_env, this profile leaves it empty until you do")
	}
	if doc.Model != "" {
		p.DefaultModel = doc.Model
	}
	for id := range doc.ModelSetting {
		alias := slugID(id)
		if i := strings.LastIndex(alias, "-"); i > 0 && strings.Contains(id, "/") {
			alias = id[strings.LastIndex(id, "/")+1:]
		}
		p.Models = mergeModel(p.Models, alias, id)
	}
	if p.DefaultModel == "" && len(p.Models) > 0 {
		for _, v := range sortedVals(p.Models) {
			p.DefaultModel = v
			break
		}
	}
	notes = append(notes, fmt.Sprintf("endpoint %s read from %s", base, path))
	imps = append(imps, Imported{Harness: "claude", ID: "claude-gateway", Origin: path, Profile: p, Notes: notes})
	return imps, nil, skipped, failures
}

func hasKey(m map[string]string, k string) bool { _, ok := m[k]; return ok }

// --- opencode: provider map (user + project scope) -------------------

type ocEntry struct {
	NPM     string         `json:"npm"`
	Options map[string]any `json:"options"`
	Models  map[string]any `json:"models"`
	Name    string         `json:"name"`
}

// ocEntryAlt is the LIVE opencode schema observed in the field:
// {"providers": {"litellm": {"package": "aisdk:@ai-sdk/...",
// "settings": {...}, "models": {...}}}} - the documented singular
// "provider"/npm/options shape keeps working beside it.
type ocEntryAlt struct {
	Package  string         `json:"package"`
	Settings map[string]any `json:"settings"`
	Models   map[string]any `json:"models"`
	Name     string         `json:"name"`
}

func importOpencode(home, cwd string) (imps []Imported, notes, skipped, failures []string) {
	paths := []string{
		filepath.Join(home, ".config", "opencode", "opencode.json"),
		filepath.Join(home, ".config", "opencode", "opencode.jsonc"),
		filepath.Join(cwd, "opencode.json"),
		filepath.Join(cwd, "opencode.jsonc"),
	}
	var found []string
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			found = append(found, p)
		}
	}
	if len(found) == 0 {
		return nil, nil, []string{"no live config in ~/.config/opencode or ./opencode.json"}, nil
	}
	seenID := map[string]bool{}
	for _, path := range found {
		raw, err := os.ReadFile(path)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", path, err))
			continue
		}
		var doc struct {
			Provider  map[string]ocEntry    `json:"provider"`
			Providers map[string]ocEntryAlt `json:"providers"`
			Model     string                `json:"model"`
		}
		if err := json.Unmarshal(stripJSONC(raw), &doc); err != nil {
			failures = append(failures, fmt.Sprintf("%s: malformed JSON (%v)", path, err))
			continue
		}
		if len(doc.Provider) == 0 && len(doc.Providers) == 0 {
			notes = append(notes, fmt.Sprintf("%s configures no providers", filepath.Base(path)))
			continue
		}
		// normalize the live "providers" schema (package/settings/models)
		// into the documented one so a single pass handles both.
		if len(doc.Provider) == 0 {
			doc.Provider = map[string]ocEntry{}
			for id, alt := range doc.Providers {
				e := ocEntry{NPM: strings.TrimPrefix(alt.Package, "aisdk:"), Options: alt.Settings, Name: alt.Name}
				if len(alt.Models) > 0 {
					e.Models = map[string]any{}
					for mid := range alt.Models {
						e.Models[mid] = nil
					}
				}
				doc.Provider[id] = e
			}
		}
		ids := make([]string, 0, len(doc.Provider))
		for id := range doc.Provider {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			e := doc.Provider[id]
			style, ok := styleFromNPM(e.NPM)
			if !ok {
				notes = append(notes, fmt.Sprintf("provider %q (npm %q): no known API style - skipped, declare it manually if it matters", id, e.NPM))
				continue
			}
			p := &Profile{Name: id, Styles: map[string]Endpoint{}}
			base, _ := e.Options["baseURL"].(string)
			if base == "" {
				base, _ = e.Options["base_url"].(string)
			}
			p.Styles[style] = Endpoint{BaseURL: base}
			if ak, _ := e.Options["apiKey"].(string); ak != "" {
				if n, isEnv := envRefName(ak); isEnv {
					p.AuthEnv = n
				} else {
					notes = append(notes, fmt.Sprintf("provider %q: apiKey holds a literal VALUE - not imported; move it behind an env var", id))
				}
			}
			if doc.Model != "" && strings.HasPrefix(doc.Model, id+"/") {
				p.DefaultModel = strings.TrimPrefix(doc.Model, id+"/")
			}
			for mid := range e.Models {
				p.Models = mergeModel(p.Models, slugID(strings.TrimPrefix(mid, id+"/")), mid)
			}
			cid := "opencode-" + slugID(id)
			for seenID[cid] {
				cid += "-2"
			}
			seenID[cid] = true
			imps = append(imps, Imported{Harness: "opencode", ID: cid, Origin: path, Profile: p,
				Notes: []string{fmt.Sprintf("%s via %s", style, base)}})
		}
	}
	return imps, notes, skipped, failures
}

// styleFromNPM maps opencode's ai-sdk package names to API styles.
func styleFromNPM(npm string) (string, bool) {
	switch {
	case strings.Contains(npm, "anthropic"):
		return StyleAnthropicCompat, true
	case strings.Contains(npm, "responses"):
		return StyleOpenAIResponses, true
	case strings.Contains(npm, "openai"):
		return StyleOpenAICompat, true
	default:
		return "", false
	}
}

// envRefName parses "{env:VAR}" (opencode's secret-reference syntax) -
// the ONLY shape whose value may cross into a profile (as a NAME).
func envRefName(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "{env:") && strings.HasSuffix(s, "}") {
		return strings.TrimSuffix(strings.TrimPrefix(s, "{env:"), "}"), true
	}
	return "", false
}

// stripJSONC removes // line comments from jsonc WITHOUT touching
// strings - a naive pass would chop the "//" inside "https://…".
// Whole comment lines vanish; trailing comments (after ") { , etc.)
// are cut at the first // outside a quoted context.
func stripJSONC(raw []byte) []byte {
	var out []byte
	inStr, esc := false, false
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if inStr {
			out = append(out, c)
			if esc {
				esc = false
			} else if c == '\\' {
				esc = true
			} else if c == '"' {
				inStr = false
			}
			continue
		}
		if c == '"' {
			inStr = true
			out = append(out, c)
			continue
		}
		if c == '/' && i+1 < len(raw) && raw[i+1] == '/' {
			for i < len(raw) && raw[i] != '\n' {
				i++
			}
			if i < len(raw) {
				out = append(out, '\n') // keep line structure
			}
			continue
		}
		if c == '/' && i+1 < len(raw) && raw[i+1] == '*' { // block comment
			i += 2
			for i+1 < len(raw) && !(raw[i] == '*' && raw[i+1] == '/') {
				if raw[i] == '\n' {
					out = append(out, '\n')
				}
				i++
			}
			i++
			continue
		}
		out = append(out, c)
	}
	return dropTrailingCommas(out)
}

// dropTrailingCommas makes the stdlib parser accept the JSONC habit of
// `… "x": 1, }` - a comma whose next non-whitespace byte is } or ] (and
// which sits OUTSIDE a string, hence the stateful scan) is removed.
func dropTrailingCommas(raw []byte) []byte {
	var out []byte
	inStr, esc := false, false
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if inStr {
			out = append(out, c)
			if esc {
				esc = false
			} else if c == '\\' {
				esc = true
			} else if c == '"' {
				inStr = false
			}
			continue
		}
		if c == '"' {
			inStr = true
			out = append(out, c)
			continue
		}
		if c == ',' {
			j := i + 1
			for j < len(raw) && (raw[j] == ' ' || raw[j] == '\t' || raw[j] == '\n' || raw[j] == '\r') {
				j++
			}
			if j < len(raw) && (raw[j] == '}' || raw[j] == ']') {
				continue // drop it
			}
		}
		out = append(out, c)
	}
	return out
}

// --- codex: [model_providers.<id>] of config.toml --------------------

func importCodex(home string) (imps []Imported, notes, skipped, failures []string) {
	path := filepath.Join(home, ".codex", "config.toml")
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, []string{"no live config at ~/.codex/config.toml"}, nil
		}
		return nil, nil, nil, []string{fmt.Sprintf("%s: unreadable (%v)", path, err)}
	}
	var doc struct {
		Model          string `toml:"model"`
		ModelProvider  string `toml:"model_provider"`
		ModelProviders map[string]struct {
			Name    string `toml:"name"`
			BaseURL string `toml:"base_url"`
			EnvKey  string `toml:"env_key"`
			WireAPI string `toml:"wire_api"`
		} `toml:"model_providers"`
	}
	if err := toml.Unmarshal(raw, &doc); err != nil {
		return nil, nil, nil, []string{fmt.Sprintf("%s: malformed TOML (%v)", path, err)}
	}
	if len(doc.ModelProviders) == 0 {
		notes = append(notes, "no [model_providers.*] sections - codex runs on its built-in ChatGPT/API auth: nothing gateway-shaped to import")
		return
	}
	ids := make([]string, 0, len(doc.ModelProviders))
	for id := range doc.ModelProviders {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		mp := doc.ModelProviders[id]
		style := StyleOpenAICompat
		switch strings.ToLower(mp.WireAPI) {
		case "responses":
			style = StyleOpenAIResponses
		case "chat", "":
			style = StyleOpenAICompat
		default:
			notes = append(notes, fmt.Sprintf("provider %q: wire_api %q unknown - imported as openai-compatible, verify before trusting", id, mp.WireAPI))
		}
		p := &Profile{Name: id, AuthEnv: mp.EnvKey, Styles: map[string]Endpoint{}}
		p.Styles[style] = Endpoint{BaseURL: mp.BaseURL}
		if doc.ModelProvider == id && doc.Model != "" {
			p.DefaultModel = doc.Model
		}
		imps = append(imps, Imported{Harness: "codex", ID: "codex-" + slugID(id), Origin: path, Profile: p,
			Notes: []string{fmt.Sprintf("%s via %s (auth env: %s)", style, mp.BaseURL, orNone(mp.EnvKey))}})
	}
	return
}

func orNone(s string) string {
	if s == "" {
		return "unset - wire an env name"
	}
	return s
}

// --- shared helpers ---------------------------------------------------

func mergeModel(m map[string]string, alias, id string) map[string]string {
	if m == nil {
		m = map[string]string{}
	}
	if _, taken := m[alias]; taken {
		alias = alias + "-" + slugID(id)[min(4, len(slugID(id))):]
	}
	m[alias] = id
	return m
}

func sortedVals(m map[string]string) []string {
	var out []string
	for _, v := range m {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// RenderProfile prints a profile in the corpus file format (the one
// agentictools' litellm-demo.toml demonstrates). Names and URLs only -
// values never reached the Profile structs to begin with.
func RenderProfile(p *Profile) string {
	var b strings.Builder
	b.WriteString("# Provider profile imported by \"harnessator provider import\".\n")
	b.WriteString("# Env-var NAMES only - never secret values (rule 07).\n\n")
	fmt.Fprintf(&b, "name = %q\n", p.Name)
	if p.DefaultModel != "" {
		fmt.Fprintf(&b, "default_model = %q\n", p.DefaultModel)
	}
	if p.AuthEnv != "" {
		fmt.Fprintf(&b, "auth_env = %q\n", p.AuthEnv)
	}
	var styles []string
	for s := range p.Styles {
		styles = append(styles, s)
	}
	sort.Strings(styles)
	for _, s := range styles {
		ep := p.Styles[s]
		fmt.Fprintf(&b, "\n[styles.%s]\nbase_url = %q\n", s, ep.BaseURL)
		if ep.AuthEnv != "" {
			fmt.Fprintf(&b, "auth_env = %q\n", ep.AuthEnv)
		}
	}
	if len(p.Models) > 0 {
		b.WriteString("\n[models]\n")
		var aliases []string
		for a := range p.Models {
			aliases = append(aliases, a)
		}
		sort.Strings(aliases)
		for _, a := range aliases {
			fmt.Fprintf(&b, "%s = %q\n", a, p.Models[a])
		}
	}
	return b.String()
}
