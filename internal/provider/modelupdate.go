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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

// ModelUpdate is the inverse of import: instead of lifting live harness
// configs into governed profiles, it asks the GATEWAYS what they
// actually serve right now and writes the refreshed model lists back
// into the harnesses' own configs - removing unsupported entries and
// reporting every change.
//
// The three rules that make writing to foreign files acceptable:
//   - nothing is written without a timestamped backup next to the file;
//   - harnessator-managed artifacts are REFUSED (their truth lives in a
//     source repo - fix it there and run setup);
//   - keys are read from wherever the harness keeps them - {env:VAR}
//     and {file:PATH} references, inline values, opencode's auth
//     store, and the harness's OWN credential-helper command (claude's
//     apiKeyHelper, executed the same way the harness executes it on
//     every launch) - used for the probe request, and NEVER printed,
//     logged or copied. A reference that resolves to nothing is
//     reported as broken; it is never sent verbatim as a key.
// Harnesses whose config cannot carry a catalog (claude tiers, codex
// TOML comments, native-API surfaces) come back REPORT ONLY, saying so.

// ModelChange is one provider entry's outcome.
type ModelChange struct {
	Harness   string
	Provider  string
	Style     string
	BaseURL   string
	KeySource string // "env LITELLM_KEY" | "auth.json" | "inline (not shown)" | "none"
	Added     []string
	Removed   []string
	Kept      []string
	Applied   bool
	Drilled   bool // report-only by nature
	Backup    string
	Note      string
	Err       string
}

// ModelUpdate runs the whole sweep for the given harness names
// (already validated by the caller; "*" must be expanded before).
// MUOptions tunes the sweep. AllowRemovals is the deliberate
// opt-in past the mass-removal guard: when a gateway's id namespace
// (openrouter/deepseek/x) differs from a config's short names, the
// naive "-no longer served" list explodes - and applying THAT would
// gut a working catalog on a naming technicality, not a fact. The
// guard refuses by default and says exactly what it saw.
type MUOptions struct {
	Dry           bool
	AllowRemovals bool
}

func ModelUpdate(ctx context.Context, harnesses []string, home, cwd string, opts MUOptions) []ModelChange {
	var out []ModelChange
	for _, h := range harnesses {
		switch h {
		case "opencode":
			out = append(out, modelUpdateOpencode(ctx, home, cwd, opts)...)
		case "claude":
			out = append(out, modelUpdateClaude(ctx, home)...)
		case "codex":
			out = append(out, modelUpdateCodex(ctx, home)...)
		case "gemini":
			out = append(out, simpleSkip(h, "native Google API - no known style mapping to probe; report skipped", "")...)
		case "cline":
			out = append(out, simpleSkip(h, "provider state schema is moving; writing it is deliberately off in v1", "")...)
		default:
			out = append(out, simpleSkip(h, "nothing local to probe (server-side or unmapped)", "")...)
		}
	}
	return out
}

func simpleSkip(h, note, err string) []ModelChange {
	return []ModelChange{{Harness: h, Note: note, Drilled: true, Err: err}}
}

// --- opencode: the harness with a real, writable catalog -------------

func modelUpdateOpencode(ctx context.Context, home, cwd string, opts MUOptions) []ModelChange {
	var out []ModelChange
	for _, p := range []string{
		filepath.Join(home, ".config", "opencode", "opencode.json"),
		filepath.Join(home, ".config", "opencode", "opencode.jsonc"),
		filepath.Join(cwd, "opencode.json"),
		filepath.Join(cwd, "opencode.jsonc"),
	} {
		fi, err := os.Stat(p)
		if err != nil || fi.IsDir() {
			continue
		}
		if strings.Contains(filepath.ToSlash(p), "/.opencode/") {
			out = append(out, ModelChange{Harness: "opencode", Err: fmt.Sprintf("%s is a harnessator-managed artifact - its truth lives in the provider profile source; refresh there and run harnessator setup (or: harnessator tune)", p)})
			continue
		}
		out = append(out, opencodeOneFile(ctx, p, home, opts)...)
		break // first existing file per scope wins, like opencode itself
	}
	if len(out) == 0 {
		// the sweep must ACCOUNT for every harness it was asked about -
		// a silently missing line reads as "nothing to say", not "no
		// config to read"
		return []ModelChange{{Harness: "opencode",
			Note:    "no live config (~/.config/opencode/opencode.json{c} or ./opencode.json{c})",
			Drilled: true}}
	}
	return out
}

func opencodeOneFile(ctx context.Context, path, home string, opts MUOptions) []ModelChange {
	dry := opts.Dry
	var out []ModelChange
	raw, err := os.ReadFile(path)
	if err != nil {
		return append(out, ModelChange{Harness: "opencode", Err: err.Error()})
	}
	var doc map[string]any
	if err := json.Unmarshal(stripJSONC(raw), &doc); err != nil {
		return append(out, ModelChange{Harness: "opencode", Err: fmt.Sprintf("%s: unparseable (%v) - refusing to touch it", path, err)})
	}
	pmap := providersOf(doc)
	if len(pmap) == 0 {
		return append(out, ModelChange{Harness: "opencode", Note: "no providers configured in " + filepath.Base(path), Drilled: true})
	}
	auth := loadOpencodeAuth(home)
	ids := make([]string, 0, len(pmap))
	for id := range pmap {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	changed := false
	for _, id := range ids {
		entry, _ := pmap[id].(map[string]any)
		if entry == nil {
			out = append(out, ModelChange{Harness: "opencode", Provider: id, Err: "entry is not an object - refusing"})
			continue
		}
		ch := ModelChange{Harness: "opencode", Provider: id}
		ch.Style, ch.BaseURL = ocStyleBase(entry)
		var key string
		var broken bool
		key, ch.KeySource, broken = ocKey(entry, id, home, auth)
		if broken {
			ch.Err = fmt.Sprintf("%s: the apiKey reference is configured but resolves to nothing - fix the variable or the file; nothing probed, nothing written", ch.KeySource)
			out = append(out, ch)
			continue
		}
		if ch.Style == "" || ch.BaseURL == "" {
			ch.Note = "no known style/base_url - not probed"
			ch.Drilled = true
			out = append(out, ch)
			continue
		}
		reachable, perr := Probe(ctx, ch.Style, Endpoint{BaseURL: ch.BaseURL, AuthEnv: credLabel(ch.KeySource)}, key)
		if perr != nil {
			ch.Err = perr.Error()
			out = append(out, ch)
			continue
		}
		models, _ := entry["models"].(map[string]any)
		if models == nil {
			models = map[string]any{}
		}
		have := map[string]bool{}
		for m := range models {
			have[m] = true
		}
		fetched := map[string]bool{}
		for _, m := range reachable {
			fetched[m] = true
			if !have[m] {
				ch.Added = append(ch.Added, m)
			} else {
				ch.Kept = append(ch.Kept, m)
			}
		}
		for m := range have {
			if !fetched[m] {
				ch.Removed = append(ch.Removed, m)
			}
		}
		sort.Strings(ch.Added)
		sort.Strings(ch.Removed)
		sort.Strings(ch.Kept)
		if len(ch.Added) == 0 && len(ch.Removed) == 0 {
			ch.Note = "already matches the gateway"
			out = append(out, ch)
			continue
		}
		// mass-removal guard: deleting >25% (and >20) of a catalog on
		// the strength of one probe response is a namespace mismatch
		// until proven otherwise - refuse, explain, offer the opt-in.
		if len(ch.Removed) > 20 && len(ch.Removed)*4 > (len(ch.Removed)+len(ch.Kept)) && !opts.AllowRemovals {
			ch.Note = fmt.Sprintf("MASS REMOVAL REFUSED: %d of %d configured entries missing from the gateway response - likely an id-namespace mismatch (short names vs prefixed ids), not a real deprecation. See the exact ids: harnessator provider modelupdate %s --verbose --dry-run. Apply anyway (only for REAL deprecations): harnessator provider modelupdate %s --allow-removals",
				len(ch.Removed), len(ch.Removed)+len(ch.Kept), ch.Harness, ch.Harness)
			ch.Drilled = true
			out = append(out, ch)
			continue
		}
		if !dry {
			for _, m := range ch.Removed {
				delete(models, m)
			}
			for _, m := range ch.Added {
				models[m] = map[string]any{"name": humanish(m)}
			}
			entry["models"] = models
			updated, merr := json.MarshalIndent(doc, "", "  ")
			if merr != nil {
				ch.Err = "re-serialize: " + merr.Error()
				out = append(out, ch)
				continue
			}
			bak := path + ".bak-" + time.Now().UTC().Format("20060102T150405Z")
			if rerr := os.WriteFile(bak, raw, 0o644); rerr != nil {
				ch.Err = "backup failed: " + rerr.Error()
				out = append(out, ch)
				continue
			}
			if werr := os.WriteFile(path, append(updated, '\n'), 0o644); werr != nil {
				ch.Err = "write failed: " + werr.Error()
				out = append(out, ch)
				continue
			}
			ch.Applied = true
			ch.Backup = bak
			changed = true
		}
		out = append(out, ch)
	}
	_ = changed
	return out
}

func providersOf(doc map[string]any) map[string]any {
	if m, ok := doc["providers"].(map[string]any); ok && len(m) > 0 {
		return m
	}
	if m, ok := doc["provider"].(map[string]any); ok {
		return m
	}
	return nil
}

func ocStyleBase(entry map[string]any) (string, string) {
	pkg, _ := entry["package"].(string)
	if pkg == "" {
		npm, _ := entry["npm"].(string)
		pkg = npm
	}
	style, ok := styleFromNPM(strings.TrimPrefix(pkg, "aisdk:"))
	if !ok {
		return "", ""
	}
	settings, _ := entry["settings"].(map[string]any)
	if settings == nil {
		settings, _ = entry["options"].(map[string]any)
	}
	base := ""
	if settings != nil {
		base, _ = settings["baseURL"].(string)
	}
	return style, base
}

// ocKey resolves the probe credential WITHOUT ever returning something
// the caller may print: the second return is only a SOURCE LABEL. The
// apiKey field is evaluated the way opencode evaluates it - {env:VAR},
// {file:PATH}, file:/file:// references and inline values. broken
// marks a configured reference that resolves to nothing: the caller
// must say so, not fall back silently and not send the raw reference.
func ocKey(entry map[string]any, id, home string, auth map[string]string) (key, label string, broken bool) {
	settings, _ := entry["settings"].(map[string]any)
	if settings == nil {
		settings, _ = entry["options"].(map[string]any)
	}
	if settings != nil {
		if ak, _ := settings["apiKey"].(string); ak != "" {
			return resolveSecretRef(ak, home)
		}
	}
	if k, ok := auth[id]; ok && k != "" {
		return k, "opencode auth store", false
	}
	return "", "none", false
}

func loadOpencodeAuth(home string) map[string]string {
	out := map[string]string{}
	raw, err := os.ReadFile(filepath.Join(home, ".local", "share", "opencode", "auth.json"))
	if err != nil {
		return out
	}
	var doc map[string]map[string]any
	if json.Unmarshal(raw, &doc) != nil {
		return out
	}
	for id, v := range doc {
		if k, ok := v["key"].(string); ok {
			out[id] = k
		}
	}
	return out
}

// credLabel maps the key SOURCE label to the credential name a probe
// error can quote honestly (names only, never values).
func credLabel(src string) string {
	if strings.HasPrefix(src, "apiKeyHelper") {
		return "apiKeyHelper"
	}
	if s := strings.TrimPrefix(src, "env "); s != src {
		return s
	}
	if src == "none" {
		return ""
	}
	return src // "opencode auth store", "inline (not shown)"
}

func humanish(id string) string {
	parts := strings.FieldsFunc(id, func(r rune) bool { return r == '-' || r == '.' || r == '/' || r == '_' })
	for i, p := range parts {
		if p != "" {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, " ")
}

// --- claude: probe what the gateway serves; nothing to write ---------

func modelUpdateClaude(ctx context.Context, home string) []ModelChange {
	path := filepath.Join(home, ".claude", "settings.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return []ModelChange{{Harness: "claude", Note: "no live config", Drilled: true}}
	}
	var doc struct {
		Env          map[string]string `json:"env"`
		APIKeyHelper string            `json:"apiKeyHelper"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return []ModelChange{{Harness: "claude", Err: "unparseable settings.json"}}
	}
	// env VALUES in the config may themselves be references
	// ({env:VAR}, {file:PATH}) - expand them like the harness would.
	for k, v := range doc.Env {
		if rv, label, bk := resolveSecretRef(v, home); !bk && label != "inline (not shown)" {
			doc.Env[k] = rv
		}
	}
	base := doc.Env["ANTHROPIC_BASE_URL"]
	if base == "" {
		return []ModelChange{{Harness: "claude", Note: "no ANTHROPIC_BASE_URL - Anthropic-direct: catalog lives with the vendor, nothing to refetch", Drilled: true}}
	}
	key, src := "", "none"
	for _, n := range []string{"ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_API_KEY"} {
		if v := os.Getenv(n); v != "" {
			key, src = v, "env "+n
			break
		}
	}
	// the settings ENV BLOCK can carry the key too (expanded above)
	if key == "" {
		for _, n := range []string{"ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_API_KEY"} {
			if v := doc.Env[n]; v != "" {
				key, src = v, "settings env "+n
				break
			}
		}
	}
	// and finally the harness's OWN mechanism: apiKeyHelper is a
	// command claude runs on every launch to print the key. Running
	// it here crosses no new trust boundary - it is the user's own
	// config executing its own documented hook. The output is kept in
	// memory for the probe and never echoed, not even on failure.
	if key == "" && doc.APIKeyHelper != "" {
		k, herr := runCredentialHelper(ctx, doc.APIKeyHelper)
		if herr != nil {
			return []ModelChange{{Harness: "claude", Provider: "gateway", Style: StyleAnthropicCompat, BaseURL: base,
				KeySource: "apiKeyHelper", Drilled: true,
				Err: fmt.Sprintf("apiKeyHelper failed: %v (its output is never shown)", herr)}}
		}
		key, src = k, "apiKeyHelper (executed)"
	}
	ch := ModelChange{Harness: "claude", Provider: "gateway", Style: StyleAnthropicCompat, BaseURL: base, KeySource: src, Drilled: true}
	if key == "" {
		ch.Note = "no credentials found (no env key, nothing in the settings env block, no apiKeyHelper) - nothing probed, nothing written"
		return []ModelChange{ch}
	}
	reachable, perr := Probe(ctx, StyleAnthropicCompat, Endpoint{BaseURL: base, AuthEnv: orUnset(src)}, key)
	if perr != nil {
		ch.Err = perr.Error()
		ch.Note = "report only: claude selects by tier alias, not by catalog - nothing was written"
		return []ModelChange{ch}
	}
	// report the reachable set as the "new models" answer, keep Kept for
	// what settings already mentions (modelSettings keys are vendor ids)
	var doc2 struct {
		ModelSetting map[string]any `json:"modelSettings"`
	}
	_ = json.Unmarshal(raw, &doc2)
	fetched := map[string]bool{}
	for _, m := range reachable {
		fetched[m] = true
	}
	for m := range doc2.ModelSetting {
		if !fetched[m] {
			ch.Removed = append(ch.Removed, m) // stale per-model options, informational
		} else {
			ch.Kept = append(ch.Kept, m)
		}
	}
	ch.Added = []string{}
	sort.Strings(ch.Kept)
	sort.Strings(ch.Removed)
	ch.Note = fmt.Sprintf("report only (%d models reachable): claude has no model catalog in its config - tiers select at runtime; consider a profile via provider import + tune", len(reachable))
	return []ModelChange{ch}
}

// runCredentialHelper executes the harness's own credential command
// the way the harness does (a shell one-liner printing the key).
// Only stdout, trimmed, is returned; on failure the ERROR NAMES THE
// EXIT CODE, never any output - a helper whose stderr echoes part of
// the key cannot leak through this path.
func runCredentialHelper(ctx context.Context, command string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var c *exec.Cmd
	if runtime.GOOS == "windows" {
		c = exec.CommandContext(cctx, "cmd", "/C", command)
	} else {
		c = exec.CommandContext(cctx, "sh", "-c", command)
	}
	var stdout bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = nil // discarded on purpose: helper stderr must not reach any report
	if err := c.Run(); err != nil {
		code := -1
		if c.ProcessState != nil {
			code = c.ProcessState.ExitCode()
		}
		if cctx.Err() != nil {
			return "", fmt.Errorf("timed out after 15s")
		}
		return "", fmt.Errorf("exit code %d", code)
	}
	key := strings.TrimSpace(stdout.String())
	if key == "" {
		return "", fmt.Errorf("produced no output")
	}
	return key, nil
}

func orUnset(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

// --- codex: probe local gateway sections; TOML comments forbid writes --

func modelUpdateCodex(ctx context.Context, home string) []ModelChange {
	imps, _, skipped, failures := importCodex(home)
	if len(imps) == 0 {
		msg := "nothing to update"
		if len(skipped) > 0 {
			msg = skipped[0]
		}
		if len(failures) > 0 {
			return []ModelChange{{Harness: "codex", Err: failures[0]}}
		}
		return []ModelChange{{Harness: "codex", Note: msg, Drilled: true}}
	}
	var out []ModelChange
	for _, imp := range imps {
		for _, style := range []string{StyleOpenAICompat, StyleOpenAIResponses, StyleAnthropicCompat} {
			ep, ok := imp.Profile.Style(style)
			if !ok {
				continue
			}
			key := os.Getenv(imp.Profile.AuthEnv)
			src := "none"
			if imp.Profile.AuthEnv != "" {
				src = "env " + imp.Profile.AuthEnv
			}
			ch := ModelChange{Harness: "codex", Provider: imp.Profile.Name, Style: style, BaseURL: ep.BaseURL, KeySource: src, Drilled: true}
			reachable, err := Probe(ctx, style, ep, key)
			if err != nil {
				ch.Err = err.Error()
			} else {
				ch.Kept = reachable
			}
			ch.Note = "report only: codex config.toml carries hand comments a rewrite would eat; the ids above are what the gateway serves"
			out = append(out, ch)
			break
		}
	}
	return out
}

var _ = http.StatusOK // keep net/http import meaningful for future envelopes
