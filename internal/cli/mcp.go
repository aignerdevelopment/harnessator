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

package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"harnessator.org/harnessator/internal/app"
	"harnessator.org/harnessator/internal/mcp"
	"harnessator.org/harnessator/internal/provider"
	"harnessator.org/harnessator/internal/resolve"
)

// mcpCmd: harnessator mcp serve - read-only agent queries over stdio.
func mcpCmd(stdout, stderr io.Writer, args []string) int {
	if len(args) == 0 || args[0] != "serve" {
		fmt.Fprintln(stderr, "usage: harnessator mcp serve [--config <p>]")
		return 2
	}
	fs := flag.NewFlagSet("mcp serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	projectFile := fs.String("config", "harnessator.toml", "project config file")
	if err := parseFlags(fs, args[1:]); err != nil {
		return 2
	}
	a, err := app.Open(context.Background(), *projectFile, app.ModeAuto)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := mcp.Serve(os.Stdin, stdout, Version, &appToolset{a: a}); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

// appToolset implements the read-only MCP surface over the core app -
// the same internals list/search/why/info/policy use in the CLI.
type appToolset struct{ a *app.App }

func (t *appToolset) Search(text, category string) (string, error) {
	needle := strings.ToLower(strings.TrimSpace(text))
	if needle == "" {
		return "", fmt.Errorf("search text required")
	}
	enabled := map[string]bool{}
	for _, ch := range t.a.Plan.Enabled {
		enabled[ch.QualifiedID()] = true
	}
	var hits []string
	for _, name := range sortedManifestNames(t.a.Manifests) {
		m := t.a.Manifests[name]
		for _, cat := range resolve.CategoriesInOrder() {
			if category != "" && cat != category {
				continue
			}
			for _, id := range sortedCompIDs(m, cat) {
				c := m.Components[cat][id]
				q := name + ":" + cat + "/" + id
				blob := strings.ToLower(q + " " + c.Description + " " + strings.Join(c.Tags, " "))
				if strings.Contains(blob, needle) {
					state := "off"
					if enabled[q] {
						state = "on"
					}
					hits = append(hits, fmt.Sprintf("%-4s %s — %s", state, q, c.Description))
				}
			}
		}
	}
	if len(hits) == 0 {
		return "no matches", nil
	}
	return strings.Join(hits, "\n"), nil
}

func (t *appToolset) ListActive() (string, error) {
	var lines []string
	for _, ch := range t.a.Plan.Enabled {
		lines = append(lines, fmt.Sprintf("%-10s %s  [%s]", ch.Category, ch.QualifiedID(), ch.Reason))
	}
	if len(lines) == 0 {
		return "no components enabled", nil
	}
	return strings.Join(lines, "\n"), nil
}

func (t *appToolset) Why(ref string) (string, error) {
	ch, ok := t.find(ref)
	if ok {
		return fmt.Sprintf("%s ENABLED - %s", ch.QualifiedID(), ch.Reason), nil
	}
	for _, s := range t.a.Plan.Skips {
		if matchRef(ref, s.Choice) {
			return fmt.Sprintf("%s SKIPPED - %s", s.Choice.QualifiedID(), s.Reason), nil
		}
	}
	return "", fmt.Errorf("ref not found in plan: %s", ref)
}

// Content reads component content from the PINNED store checkout - the
// exact bytes the lock digests cover. Path traversal is refused.
func (t *appToolset) Content(ref, file string) (string, error) {
	ch, ok := t.find(ref)
	if !ok {
		return "", fmt.Errorf("component not enabled for this project: %s (mutations/reads follow the plan; see list_active)", ref)
	}
	pin := t.a.Pins[ch.Source]
	if pin == nil {
		return "", fmt.Errorf("source not pinned: %s", ch.Source)
	}
	root := filepath.Clean(filepath.Join(pin.ManifestRoot, ch.Component.Path))
	target := root
	if ch.Component.Key != "" && file == "" {
		return "", fmt.Errorf("shared-file component (key %q): pass file", ch.Component.Key)
	}
	if file != "" {
		clean := filepath.Clean(filepath.Join(root, file))
		if clean != root && !strings.HasPrefix(clean, root+string(os.PathSeparator)) {
			return "", fmt.Errorf("file escapes the component root")
		}
		target = clean
	}
	fi, err := os.Stat(target)
	if err != nil {
		return "", err
	}
	if fi.IsDir() {
		var names []string
		ents, _ := os.ReadDir(target)
		for _, e := range ents {
			names = append(names, e.Name())
		}
		sort.Strings(names)
		head := fmt.Sprintf("directory %s:\n%s", ch.Component.Path, strings.Join(names, "\n"))
		for _, cand := range []string{"SKILL.md", "README.md"} {
			if b, err := os.ReadFile(filepath.Join(target, cand)); err == nil {
				return head + "\n\n--- " + cand + " ---\n" + string(b), nil
			}
		}
		return head, nil
	}
	b, err := os.ReadFile(target)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (t *appToolset) PolicyStatus() (string, error) {
	var b strings.Builder
	p, e := t.a.GovProvider, t.a.GovPolicy
	if p == nil {
		b.WriteString("provider: none selected\n")
	} else {
		fmt.Fprintf(&b, "provider: %s (styles: %s)\ndefault_model: %s\ncatalog: %s\n",
			p.Name, strings.Join(sortedStyleKeys(p), ", "), p.DefaultModel, p.ModelList())
	}
	if e == nil {
		b.WriteString("policy: none enabled\n")
	} else {
		fmt.Fprintf(&b, "strict: %v\nallowed: %s\nsubagent_model: %s\nfrom: %s\n",
			e.Strict, strings.Join(e.Allowed, ", "), orDash(e.SubagentModel), strings.Join(e.Sources, ", "))
	}
	for _, w := range t.a.GovWarnings {
		fmt.Fprintf(&b, "warning: %s\n", w)
	}
	return b.String(), nil
}

func (t *appToolset) find(ref string) (resolve.Choice, bool) {
	for _, ch := range t.a.Plan.Enabled {
		if matchRef(ref, ch) {
			return ch, true
		}
	}
	return resolve.Choice{}, false
}

func matchRef(ref string, ch resolve.Choice) bool {
	return ref == ch.QualifiedID() || ref == ch.Category+"/"+ch.ID || ref == ch.ID
}

func sortedStyleKeys(p *provider.Profile) []string {
	var out []string
	for k := range p.Styles {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
