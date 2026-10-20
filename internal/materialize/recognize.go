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

package materialize

import "sort"

// Recognize is the READ-side counterpart of Specs: how already-present
// component content is spotted in a project tree. The knowledge lives in the same package -
// and per target right next to its writer - so a layout rename cannot
// update one direction without the other (asserted by
// TestRecognizeSymmetry: every managed dir is recognized).

// EntryKind tells how a component is laid out under a recognized dir.
type EntryKind int

const (
	// EntryDir: each qualifying sub-directory IS a component (skills:
	// directory containing SKILL.md).
	EntryDir EntryKind = iota
	// EntryFile: each file whose name ends in Suffix is a component.
	EntryFile
)

// Recognition is one layout claim by one harness adapter.
type Recognition struct {
	Dir      string // project-relative dir scanned, e.g. ".claude/skills"
	Suffix   string // for EntryFile, e.g. ".md", ".mdc", ".ts"
	Kind     EntryKind
	Category string // schema category (singular)
	Target   string // adapter providing this knowledge (provenance)
	NeedsMD  bool   // EntryDir only: dir must contain SKILL.md
}

// recognitionsFor returns the layout claims of one adapter. Shared
// locations (AgentSkills .agents/skills) repeat per target on purpose -
// Recognitions() dedupes.
func recognitionsFor(target string) []Recognition {
	switch target {
	case "opencode":
		return []Recognition{
			{Dir: ".opencode/skill", Kind: EntryDir, Category: "skill", Target: target, NeedsMD: true},
			{Dir: ".opencode/command", Suffix: ".md", Kind: EntryFile, Category: "prompt", Target: target},
			{Dir: ".opencode/agent", Suffix: ".md", Kind: EntryFile, Category: "agent", Target: target},
			{Dir: ".opencode/plugin", Suffix: ".ts", Kind: EntryFile, Category: "hook", Target: target},
			{Dir: ".opencode/tool", Suffix: ".ts", Kind: EntryFile, Category: "tool", Target: target},
		}
	case "claude":
		return []Recognition{
			{Dir: ".claude/skills", Kind: EntryDir, Category: "skill", Target: target, NeedsMD: true},
			{Dir: ".claude/commands", Suffix: ".md", Kind: EntryFile, Category: "prompt", Target: target},
			{Dir: ".claude/agents", Suffix: ".md", Kind: EntryFile, Category: "agent", Target: target},
		}
	case "codex":
		return []Recognition{
			{Dir: ".codex/prompts", Suffix: ".md", Kind: EntryFile, Category: "prompt", Target: target},
			{Dir: ".agents/skills", Kind: EntryDir, Category: "skill", Target: target, NeedsMD: true},
		}
	case "antigravity":
		return []Recognition{
			{Dir: ".agents/skills", Kind: EntryDir, Category: "skill", Target: target, NeedsMD: true},
			{Dir: ".agents/workflows", Suffix: ".md", Kind: EntryFile, Category: "workflow", Target: target},
		}
	case "cursor":
		return []Recognition{
			{Dir: ".agents/skills", Kind: EntryDir, Category: "skill", Target: target, NeedsMD: true},
			{Dir: ".cursor/commands", Suffix: ".md", Kind: EntryFile, Category: "prompt", Target: target},
			// generated .mdc rules: readable back (rule + hint.cursor);
			// recognition already spots them as rule candidates.
			{Dir: ".cursor/rules", Suffix: ".mdc", Kind: EntryFile, Category: "rule", Target: target},
		}
	case "pi":
		return []Recognition{
			{Dir: ".agents/skills", Kind: EntryDir, Category: "skill", Target: target, NeedsMD: true},
			// pi prompt templates live inside pi-packages or ~/.pi; the
			// generated .harnessator/pi farm is NOT authored content and
			// is never recognized.
		}
	}
	return nil
}

// Recognitions returns the deduplicated layout claims across all MVP
// adapters, sorted deterministically, plus the synthetic AgentSkills-root
// claim: scanning a loose pile (dirs of SKILL.md directly under the scan
// path) or a single skill directory (SKILL.md AT the root).
func Recognitions() []Recognition {
	out := []Recognition{
		{Dir: ".", Kind: EntryDir, Category: "skill", Target: "agentskills", NeedsMD: true},
	}
	seen := map[string]bool{}
	for _, target := range []string{"opencode", "claude", "codex", "antigravity", "cursor", "pi"} {
		for _, r := range recognitionsFor(target) {
			k := r.Dir + "\x00" + r.Suffix + "\x00" + r.Category
			if seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir < out[j].Dir
		}
		return out[i].Category < out[j].Category
	})
	return out
}
