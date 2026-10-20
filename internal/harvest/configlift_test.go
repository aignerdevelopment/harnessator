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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"harnessator.org/harnessator/internal/config"
)

func TestScanConfigLifts(t *testing.T) {
	base := t.TempDir()
	// mcp in two styles, same server -> should merge to one component
	write(t, filepath.Join(base, ".mcp.json"),
		`{"mcpServers":{"jira":{"command":"npx","args":["-y","jira-mcp"],"env":{"TOKEN":"${JIRA_TOKEN}"}}}}`)
	write(t, filepath.Join(base, ".opencode/opencode.json"),
		`{"mcp":{"jira":{"type":"local","command":["npx","-y","jira-mcp"],"environment":{"TOKEN":"${JIRA_TOKEN}"}}}}`)
	// a secret-bearing server (inline token)
	write(t, filepath.Join(base, ".cursor/mcp.json"),
		`{"mcpServers":{"linear":{"command":"linear-mcp","env":{"TOKEN":"hunter2supersecretvalue9999"}}}}`)
	// hook script referenced from settings
	write(t, filepath.Join(base, ".claude/hooks/format.sh"), "#!/bin/sh\nnpx prettier --write \"$@\"\n")
	write(t, filepath.Join(base, ".claude/settings.json"),
		`{"hooks":{"PostToolUse":[{"matcher":"Edit|Write","hooks":[{"type":"command","command":"sh .claude/hooks/format.sh"}]}]}}`)
	// dangling hook reference -> note, not a component
	write(t, filepath.Join(base, ".codex/config.toml"),
		"[mcp_servers.gitlab]\nurl = \"https://gl.example/mcp\"\n")

	rep, err := Scan([]string{base})
	if err != nil {
		t.Fatal(err)
	}
	byCat := map[string]map[string]Component{}
	for _, r := range rep.Roots {
		for _, c := range r.Components {
			if byCat[c.Category] == nil {
				byCat[c.Category] = map[string]Component{}
			}
			byCat[c.Category][c.ID] = c
		}
	}
	jira, ok := byCat["mcp"]["jira"]
	if !ok {
		t.Fatalf("jira mcp not lifted: %v", byCat["mcp"])
	}
	if len(jira.Locations) != 2 {
		t.Errorf("same server in .mcp.json + opencode.json should merge locations, got %v", jira.Locations)
	}
	if _, ok := byCat["mcp"]["gitlab"]; !ok {
		t.Error("codex toml mcp not lifted")
	}
	lin := byCat["mcp"]["linear"]
	if lin.ValidID && len(lin.Hits) == 0 {
		t.Error("inline bearer token must produce a sanitize hit")
	}
	sec, hit := func() (bool, bool) {
		found := false
		for _, r := range rep.Roots {
			for _, c := range r.Components {
				if c.Category == "hook" && c.ID == "format" {
					found = true
				}
			}
		}
		return found, found
	}()
	_ = hit
	if !sec {
		t.Error("hook script not lifted (relative .claude/hooks path)")
	}
}

func TestHarvestConfigLiftsPlacement(t *testing.T) {
	base := t.TempDir()
	t.Chdir(base)
	donor := filepath.Join(base, "proj")
	write(t, filepath.Join(donor, ".mcp.json"),
		`{"mcpServers":{"jira":{"command":"npx","args":["-y","jira-mcp"],"env":{"TOKEN":"${JIRA_TOKEN}"}}}}`)
	write(t, filepath.Join(donor, ".claude/hooks/format.sh"), "#!/bin/sh\nnpx prettier --write \"$@\"\n")
	write(t, filepath.Join(donor, ".claude/settings.json"),
		`{"hooks":{"PostToolUse":[{"matcher":"Edit|Write","hooks":[{"type":"command","command":"sh .claude/hooks/format.sh"}]}]}}`)

	res, err := Harvest(Options{Paths: []string{donor}, Name: "pub"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 2 {
		t.Fatalf("want mcp+hook added, got %d %+v", res.Added, res.Planned)
	}
	out := filepath.Join(base, "pub")

	// servers.json: every target shape present, opencode converted
	sb, err := os.ReadFile(filepath.Join(out, "mcp", "servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	var servers struct {
		Servers map[string]struct {
			Config map[string]json.RawMessage `json:"config"`
		} `json:"servers"`
	}
	if json.Unmarshal(sb, &servers) != nil {
		t.Fatalf("servers.json unparseable: %s", sb)
	}
	jira, ok := servers.Servers["jira"]
	if !ok {
		t.Fatalf("jira missing: %s", sb)
	}
	for _, want := range []string{"claude", "opencode", "codex", "cursor", "antigravity"} {
		if _, has := jira.Config[want]; !has {
			t.Errorf("no %s shape: %s", want, sb)
		}
	}
	oc := jira.Config["opencode"]
	var ocShape struct {
		Type    string   `json:"type"`
		Command []string `json:"command"`
	}
	if json.Unmarshal(oc, &ocShape) != nil {
		t.Fatalf("opencode shape unparseable: %s", oc)
	}
	if ocShape.Type != "local" || len(ocShape.Command) != 3 ||
		ocShape.Command[0] != "npx" || ocShape.Command[2] != "jira-mcp" {
		t.Errorf("opencode conversion wrong: %+v", ocShape)
	}

	// hook: script copied + template rewritten to the source placeholder
	if _, err := os.Stat(filepath.Join(out, "hooks/shared/format.sh")); err != nil {
		t.Fatal(err)
	}
	tb, err := os.ReadFile(filepath.Join(out, "hooks/claude/settings.hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(tb), "{{HARNESSATOR_SOURCE}}/hooks/shared/format.sh") ||
		strings.Contains(string(tb), ".claude/hooks") {
		t.Errorf("template not rewritten: %s", tb)
	}

	// manifest references the shared files and validates
	m, err := config.LoadSourceManifest(filepath.Join(out, "harnessator.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := config.ValidateSource(m, out); err != nil {
		t.Fatalf("published source invalid: %v", err)
	}
	if k := m.Components["mcp"]["jira"].Key; k != "jira" {
		t.Errorf("mcp component needs key for the shared file, got %q", k)
	}

	// idempotent re-harvest: both skip-identical
	res2, err := Harvest(Options{Paths: []string{donor}, Into: out})
	if err != nil {
		t.Fatalf("re-harvest: %v", err)
	}
	if res2.Added != 0 || res2.Skipped != 2 {
		t.Errorf("re-harvest not idempotent: %+v", res2.Planned)
	}
}
