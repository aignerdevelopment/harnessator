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

package harvest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"

	"harnessator.org/harnessator/internal/contenthash"
)

// Components embedded in HARNESS CONFIG files rather than in
// dedicated dirs/files - MCP server definitions (.mcp.json,
// .cursor/mcp.json, .agents/mcp_config.json, opencode.json "mcp",
// codex config.toml [mcp_servers]) and claude hook command scripts
// (.claude/settings{.local,}.json).
//
// Canonical shape: every MCP entry is normalized to the claude-code
// {"mcpServers":{name:{command,args,env}|{url,headers}}} member shape
// BEFORE digesting, so the same server defined in three harness config
// styles collapses to one component with three locations. Placement
// regenerates the per-target shapes from it.

const canonicalTarget = "claude"

// canonicalMCP returns the canonical claude-shape entry + digest, or an
// error if the entry is not a recognizable server definition.
func canonicalMCP(raw json.RawMessage) (canon json.RawMessage, digest string, err error) {
	var v map[string]any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, "", err
	}
	c := map[string]any{}
	switch {
	case v["url"] != nil:
		c["url"] = v["url"]
		if h, ok := v["headers"]; ok {
			c["headers"] = h
		}
	default:
		if arr, ok := v["command"].([]any); ok && len(arr) > 0 {
			// opencode local shape: command is an argv array
			c["command"] = arr[0]
			if len(arr) > 1 {
				c["args"] = arr[1:]
			}
			if e, ok := v["environment"]; ok {
				c["env"] = e
			}
			break
		}
		cmd, ok := v["command"].(string)
		if !ok {
			return nil, "", fmt.Errorf("neither url nor command")
		}
		c["command"] = cmd
		if a, ok := v["args"]; ok {
			c["args"] = a
		}
		if e, ok := v["env"]; ok {
			c["env"] = e
		}
	}
	b, err := json.Marshal(c) // Go sorts map keys -> canonical
	if err != nil {
		return nil, "", err
	}
	return b, contenthash.Bytes(b), nil
}

// opencodeShape derives the opencode config member from the canonical form.
func opencodeShape(canon json.RawMessage) json.RawMessage {
	var v map[string]any
	if json.Unmarshal(canon, &v) != nil {
		return canon
	}
	if u, ok := v["url"]; ok {
		out := map[string]any{"type": "remote", "url": u}
		if h, ok := v["headers"]; ok {
			out["headers"] = h
		}
		b, _ := json.Marshal(out)
		return b
	}
	cmd := []any{}
	if c, ok := v["command"].(string); ok {
		cmd = append(cmd, c)
	}
	if a, ok := v["args"].([]any); ok {
		cmd = append(cmd, a...)
	}
	out := map[string]any{"type": "local", "command": cmd}
	if e, ok := v["env"]; ok {
		out["environment"] = e
	}
	b, _ := json.Marshal(out)
	return b
}

// mcpServersMap parses a claude-shape {"mcpServers": {...}} file.
func mcpServersMap(path string) (map[string]json.RawMessage, error) {
	var f struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, err
	}
	return f.MCPServers, nil
}

func opencodeMCPMap(path string) (map[string]json.RawMessage, error) {
	var f struct {
		MCP map[string]json.RawMessage `json:"mcp"`
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, err
	}
	return f.MCP, nil
}

func codexMCPMap(path string) (map[string]json.RawMessage, error) {
	var f struct {
		MCPServers map[string]map[string]any `toml:"mcp_servers"`
	}
	if _, err := toml.DecodeFile(path, &f); err != nil {
		return nil, err
	}
	out := map[string]json.RawMessage{}
	for name, m := range f.MCPServers {
		b, err := json.Marshal(m)
		if err != nil {
			continue
		}
		out[name] = b
	}
	return out, nil
}

// compKey merges recognized and lifted candidates by content: same
// category+id+digest across files/roots => one component, many locations.
type compKey struct{ cat, id, digest string }

// liftConfig adds config-embedded components to the scan
// aggregation: MCP servers in harness config files and hook scripts
// referenced from claude settings. Sub records the exact fragment so
// harvest can place it canonically.
func liftConfig(abs string, seen map[compKey]*Component, order *[]compKey, root *Root) {
	add := func(c Component) {
		k := compKey{c.Category, c.ID, c.Digest}
		if prev, ok := seen[k]; ok {
			// identical content found in several config files: one
			// component, all locations; Sub stays the first extraction
			// point, which is byte-identical to the others.
			for _, l := range c.Locations {
				prev.Locations = appendUniqueStr(prev.Locations, l)
			}
			return
		}
		seen[k] = &c
		*order = append(*order, k)
	}
	skipped := func(path, why string) {
		root.Residual = append(root.Residual, Residual{Path: path, Why: why})
	}

	for _, cf := range configMCPFiles {
		full := filepath.Join(abs, filepath.FromSlash(cf.rel))
		if fi, err := os.Lstat(full); err != nil || !fi.Mode().IsRegular() {
			continue
		}
		var servers map[string]json.RawMessage
		var err error
		switch cf.flavor {
		case "claude":
			servers, err = mcpServersMap(full)
		case "opencode":
			servers, err = opencodeMCPMap(full)
		case "codex":
			servers, err = codexMCPMap(full)
		}
		if err != nil {
			root.Notes = append(root.Notes, fmt.Sprintf("%s: no MCP lift (%v)", cf.rel, err))
			skipped(cf.rel, "unparseable MCP config")
			continue
		}
		names := make([]string, 0, len(servers))
		for n := range servers {
			names = append(names, n)
		}
		sortStrings(names)
		for _, name := range names {
			canon, digest, cerr := canonicalMCP(servers[name])
			if cerr != nil {
				root.Notes = append(root.Notes, fmt.Sprintf("%s#%s: %v", cf.rel, name, cerr))
				skipped(cf.rel+"#"+name, cerr.Error())
				continue
			}
			rel := cf.rel + "#" + name
			add(Component{
				Category: "mcp", ID: name, Kind: "file", Digest: digest,
				Locations: []string{cf.rel}, Sub: full + "#" + name,
				Target: cf.target, ValidID: validID(name),
				Hits: sanitizeBytes(rel, string(canon)),
			})
		}
	}

	for _, sf := range []string{".claude/settings.json", ".claude/settings.local.json"} {
		full := filepath.Join(abs, filepath.FromSlash(sf))
		if fi, err := os.Lstat(full); err != nil || !fi.Mode().IsRegular() {
			continue
		}
		entries, notes := liftHooks(full, abs)
		root.Notes = append(root.Notes, notes...)
		for _, e := range entries {
			base := filepath.Base(e.Script)
			id := idFromBase(base)
			digest, derr := contenthash.File(e.Script)
			if derr != nil {
				continue
			}
			relScript, _ := filepath.Rel(abs, e.Script)
			add(Component{
				Category: "hook", ID: id, Kind: "file", Digest: digest,
				Locations: []string{filepath.ToSlash(relScript)},
				Sub:       full + "#hook:" + base,
				Target:    "claude", ValidID: validID(id),
				Hits: sanitize(abs, e.Script),
			})
		}
	}
}

func appendUniqueStr(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

// validID mirrors schema id rules without importing schema into this file's
// import block churn (same regex semantics).
func validID(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case i > 0 && (r == '.' || r == '_' || r == '-'):
		default:
			return false
		}
	}
	return true
}

// idFromBase derives a component id from a script filename.
func idFromBase(base string) string {
	name := strings.TrimSuffix(base, filepath.Ext(base))
	lower := strings.ToLower(name)
	var b strings.Builder
	prevDash := false
	for _, r := range lower {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteRune('-')
				prevDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "hook"
	}
	return out
}

// subFile/subBase decompose Sub ("<absfile>#<frag>").
func subFile(sub string) string {
	f, _, _ := strings.Cut(sub, "#")
	return f
}
func subFrag(sub string) string {
	_, f, _ := strings.Cut(sub, "#")
	return f
}
func subBase(sub string) string { // hook fragments: "hook:<base>"
	f := subFrag(sub)
	_, base, _ := strings.Cut(f, ":")
	if base == "" {
		return f
	}
	return base
}

// extractMCPEntry re-reads the canonical entry from a (possibly stale)
// donor config via its Sub.
func extractMCPEntry(sub string) (json.RawMessage, error) {
	file, name := filepath.ToSlash(subFile(sub)), subFrag(sub)
	var (
		servers map[string]json.RawMessage
		err     error
	)
	switch filepath.Base(file) {
	case "config.toml":
		servers, err = codexMCPMap(file)
	case "opencode.json":
		servers, err = opencodeMCPMap(file)
	default:
		servers, err = mcpServersMap(file)
	}
	if err != nil {
		return nil, err
	}
	raw, ok := servers[name]
	if !ok {
		return nil, fmt.Errorf("%s: server %q no longer present", file, name)
	}
	canon, _, err := canonicalMCP(raw)
	return canon, err
}

// canonicalizeShape builds the per-target config map for one canonical
// (claude-shape) MCP entry.
func canonicalizeShape(canon json.RawMessage) map[string]any {
	return map[string]any{
		"claude":      json.RawMessage(canon),
		"cursor":      json.RawMessage(canon),
		"antigravity": json.RawMessage(canon),
		"codex":       json.RawMessage(canon),
		"opencode":    opencodeShape(canon),
	}
}

// serversAccumulate merges a lifted MCP server's canonical entry into an
// in-progress servers.json document (idempotent: identical content no-ops).
func serversAccumulate(outDoc map[string]any, sub, id string) error {
	canon, err := extractMCPEntry(sub)
	if err != nil {
		return err
	}
	servers, _ := outDoc["servers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
		outDoc["servers"] = servers
	}
	if prev, ok := servers[id]; ok {
		pb, _ := json.Marshal(prev)
		if entryHasCanonical(pb, canon) {
			return nil // already present with identical content
		}
	}
	servers[id] = map[string]any{"config": canonicalizeShape(canon)}
	return nil
}

// entryHasCanonical reports whether a marshaled servers.json entry
// already carries the given canonical claude content (whitespace-tolerant).
func entryHasCanonical(doc []byte, canon json.RawMessage) bool {
	var v struct {
		Config struct {
			Claude json.RawMessage `json:"claude"`
		} `json:"config"`
	}
	if json.Unmarshal(doc, &v) != nil {
		return false
	}
	var a, b any
	if json.Unmarshal(v.Config.Claude, &a) != nil || json.Unmarshal(canon, &b) != nil {
		return false
	}
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(ab) == string(bb)
}

// existingMCPFragment compares a key's claude entry in the output's
// servers.json against the donor's canonical digest (--into idempotency
// for the shared servers.json file).
func existingMCPFragment(outDir, key, wantDigest string) bool {
	b, err := os.ReadFile(filepath.Join(outDir, "mcp", "servers.json"))
	if err != nil {
		return false
	}
	var f struct {
		Servers map[string]struct {
			Config map[string]json.RawMessage `json:"config"`
		} `json:"servers"`
	}
	if json.Unmarshal(b, &f) != nil {
		return false
	}
	e, ok := f.Servers[key]
	if !ok {
		return false
	}
	c, ok := e.Config[canonicalTarget]
	if !ok {
		return false
	}
	var v any
	if json.Unmarshal(c, &v) != nil {
		return false
	}
	can, _ := json.Marshal(v) // re-canonicalize (file is pretty-printed)
	return contenthash.Bytes(can) == wantDigest
}

// serversFile merge helpers --------------------------------------------------

func loadServers(outDir string) (map[string]any, error) {
	out := map[string]any{"servers": map[string]any{}}
	b, err := os.ReadFile(filepath.Join(outDir, "mcp", "servers.json"))
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	if json.Unmarshal(b, &out) != nil {
		return nil, fmt.Errorf("%s: unparseable servers.json", outDir)
	}
	if _, ok := out["servers"]; !ok {
		out["servers"] = map[string]any{}
	}
	return out, nil
}

func writeJSONFile(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// mergeHookTemplate appends lifted entries to the source's claude hooks
// template (the file the claude adapter substitutes at setup time).
func mergeHookTemplate(outDir string, refs []hookLiftRef) error {
	tmplPath := filepath.Join(outDir, "hooks", "claude", "settings.hooks.json")
	tmpl := map[string]any{"hooks": map[string][]any{}}
	if b, err := os.ReadFile(tmplPath); err == nil {
		_ = json.Unmarshal(b, &tmpl) // tolerate pre-existing shapes
	}
	hooks, _ := tmpl["hooks"].(map[string][]any)
	if hooks == nil {
		hooks = map[string][]any{}
	}
	existing, _ := json.Marshal(hooks)
	for _, ref := range refs {
		root := filepath.Dir(filepath.Dir(ref.settingsPath)) // <root>/.claude/settings.json
		entries, _ := liftHooks(ref.settingsPath, root)
		for _, e := range entries {
			if filepath.Base(e.Script) != ref.base {
				continue
			}
			rendered, _ := json.Marshal(e)
			if strings.Contains(string(existing), "hooks/shared/"+ref.base) &&
				strings.Contains(string(rendered), e.Command) {
				continue
			}
			group := map[string]any{
				"matcher": e.Matcher,
				"hooks": []map[string]string{{
					"type": "command",
					// the command references the SOURCE-relative script;
					// substitute the rewritten form
					"command": rewriteHookCommand(e, ref.base),
				}},
			}
			hooks[e.Event] = append(hooks[e.Event], group)
		}
	}
	tmpl["hooks"] = hooks
	return writeJSONFile(tmplPath, tmpl)
}

type hookLiftRef struct {
	settingsPath string
	base         string
}

// ---- hooks ----------------------------------------------------------------

// configMCPFiles are the per-root MCP config sources: rel path, parsing
// flavor and originating target.
var configMCPFiles = []struct{ rel, flavor, target string }{
	{".mcp.json", "claude", "claude"},
	{".cursor/mcp.json", "claude", "cursor"},
	{".agents/mcp_config.json", "claude", "antigravity"},
	{".opencode/opencode.json", "opencode", "opencode"},
	{".codex/config.toml", "codex", "codex"},
}

// hookEntry is one command hook lifted from claude settings.
type hookEntry struct {
	Event    string
	Matcher  string
	Command  string // full command string as written
	RawToken string // the script token exactly as in the command
	Script   string // resolved absolute script path
}

var scriptTokenRE = regexp.MustCompile(`["']?([^\s"']+\.(?:sh|py|rb|js|ts|mjs|cjs))["']?`)

// liftHooks parses {"hooks":{"<Event>":[{"matcher":...,"hooks":[{"command":...}]}]}}
// returning entries whose command references an existing script inside root.
func liftHooks(settingsPath, root string) ([]hookEntry, []string) {
	var notes []string
	var f struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	b, err := os.ReadFile(settingsPath)
	if err != nil {
		return nil, []string{fmt.Sprintf("settings %s: %v", settingsPath, err)}
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, []string{fmt.Sprintf("settings %s unparseable: %v", settingsPath, err)}
	}
	events := make([]string, 0, len(f.Hooks))
	for e := range f.Hooks {
		events = append(events, e)
	}
	sortStrings(events)
	var out []hookEntry
	for _, ev := range events {
		for _, grp := range f.Hooks[ev] {
			for _, h := range grp.Hooks {
				if h.Command == "" {
					continue
				}
				loc := scriptTokenRE.FindStringSubmatch(h.Command)
				if loc == nil {
					continue // no script token: inline shell - stays with the donor
				}
				tok := loc[1]
				abs, err := resolvePath(tok, root)
				if err != nil {
					notes = append(notes, fmt.Sprintf("%s %s: unresolvable script %q (skipped)", ev, grp.Matcher, tok))
					continue
				}
				if !inside(abs, root) {
					notes = append(notes, fmt.Sprintf("%s %s: script outside project %s (skipped)", ev, grp.Matcher, abs))
					continue
				}
				if _, err := os.Stat(abs); err != nil {
					notes = append(notes, fmt.Sprintf("%s %s: script missing %s (skipped)", ev, grp.Matcher, abs))
					continue
				}
				out = append(out, hookEntry{
					Event: ev, Matcher: grp.Matcher, Command: h.Command,
					RawToken: tok, Script: abs,
				})
			}
		}
	}
	return out, notes
}

func resolvePath(tok, root string) (string, error) {
	switch {
	case strings.HasPrefix(tok, "~"):
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		tok = filepath.Join(home, strings.TrimPrefix(tok, "~"))
	}
	p := tok
	if !filepath.IsAbs(p) {
		p = filepath.Join(root, p)
	}
	return filepath.Abs(p)
}

func inside(p, root string) bool {
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// rewriteHookCommand swaps the donor script token for the source-relative
// template path (the claude adapter substitutes {{HARNESSATOR_SOURCE}}).
func rewriteHookCommand(e hookEntry, base string) string {
	return strings.ReplaceAll(e.Command, e.RawToken,
		`"{{HARNESSATOR_SOURCE}}/hooks/shared/`+base+`"`)
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func sanitizeBytes(label, text string) []Hit {
	var hits []Hit
	for _, rule := range hitRules {
		if loc := rule.RE.FindString(text); loc != "" {
			hits = append(hits, Hit{Rule: rule.Name, File: label, Line: 1, Sample: mask(loc)})
		}
	}
	return hits
}
