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
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Programmatic decoding of agent-note files (AGENTS.md, CLAUDE.md, ...):
// the guided tail's first pass, done by heading structure instead of a
// model. The split is deliberately conservative - headings are layout,
// not intent, so the human (or a harness agent, prompted below) still
// reviews the result. One rule per top-level concern; a note without
// any decodable boundaries imports as ONE verbatim rule.

// NoteRule is one heading-decoded candidate component.
type NoteRule struct {
	ID          string
	Description string
	Body        string
	Section     string // heading text ("" = whole-file import)
	Source      string // donor note file (abs)
}

var headingRE = regexp.MustCompile(`^(#{1,6})[ \t]+(.+?)[ \t]*$`)

// SplitNoteFile decodes one note file into candidate rules.
func SplitNoteFile(path string) ([]NoteRule, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := strings.ReplaceAll(string(raw), "\r\n", "\n") // Windows donors
	lines := strings.Split(text, "\n")

	type hd struct {
		level int
		title string
		idx   int
	}
	var heads []hd
	inFence := false
	for i, ln := range lines {
		if strings.HasPrefix(strings.TrimSpace(ln), "```") || strings.HasPrefix(strings.TrimSpace(ln), "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if m := headingRE.FindStringSubmatch(ln); m != nil {
			heads = append(heads, hd{len(m[1]), strings.TrimSpace(m[2]), i})
		}
	}

	base := filepath.Base(path)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	if stem == "" { // dotfile donors (.cursorrules): the name IS the stem
		stem = strings.TrimPrefix(base, ".")
	}

	// split level: shallowest level with >=2 headings; otherwise one rule
	split := 0
	for lvl := 1; lvl <= 6 && split == 0; lvl++ {
		n := 0
		for _, h := range heads {
			if h.level == lvl {
				n++
			}
		}
		if n >= 2 {
			split = lvl
		}
	}
	if split == 0 {
		body := strings.TrimSpace(text)
		if body == "" {
			return nil, nil
		}
		return []NoteRule{{
			ID:          slugify(stem + "-md"),
			Description: "imported verbatim from " + filepath.Base(path) + " (no section structure found)",
			Body:        body + "\n",
			Source:      path,
		}}, nil
	}

	var out []NoteRule
	taken := map[string]bool{}
	first := true
	for i, h := range heads {
		if h.level != split {
			continue
		}
		end := len(lines)
		for j := i + 1; j < len(heads); j++ {
			if heads[j].level <= split {
				end = heads[j].idx
				break
			}
		}
		section := lines[h.idx:end]
		if first && h.idx > 0 {
			// keep the preamble (title, intro) attached to the first rule
			section = append(append([]string{}, lines[:h.idx]...), section...)
		}
		first = false
		body := strings.TrimSpace(strings.Join(section, "\n"))
		if body == "" {
			continue
		}
		id := slugify(h.title)
		for taken[id] {
			id = bumpSuffix(id)
		}
		taken[id] = true
		out = append(out, NoteRule{
			ID:          id,
			Description: firstProseLine(section),
			Body:        body + "\n",
			Section:     h.title,
			Source:      path,
		})
	}
	return out, nil
}

// firstProseLine: the first non-heading, non-empty line, lightly cleaned
// and length-capped - an honest description, never a fabrication.
func firstProseLine(lines []string) string {
	for _, ln := range lines {
		t := strings.TrimSpace(ln)
		if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "```") {
			continue
		}
		t = strings.TrimPrefix(t, "> ")
		t = strings.TrimLeft(t, "-* ")
		t = strings.ReplaceAll(t, "**", "")
		t = strings.ReplaceAll(t, "`", "")
		if len(t) > 110 {
			if i := strings.LastIndex(t[:110], " "); i > 40 {
				t = t[:i] + "..."
			} else {
				t = t[:107] + "..."
			}
		}
		return t
	}
	return ""
}

var slugDrop = regexp.MustCompile(`[^a-z0-9.]+`)

// slugify turns a heading into a valid, bounded component id.
func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = slugDrop.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-.")
	if s == "" {
		s = "rule"
	}
	if len(s) > 56 {
		if i := strings.LastIndex(s[:56], "-"); i > 20 {
			s = s[:i]
		} else {
			s = s[:56]
		}
		s = strings.Trim(s, "-.")
	}
	if c := s[0]; !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9') {
		s = "r-" + s
	}
	return s
}

// findNoteFiles: note files sitting at UNMANAGED scan roots (managed
// trees hold harnessator's own generated pointers - never import those).
func findNoteFiles(paths []string) []string {
	var out []string
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		if fi, serr := os.Stat(abs); serr != nil || !fi.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(abs, "harnessator.lock")); err == nil {
			continue // managed: its AGENTS.md is OUR pointer file
		}
		for _, note := range rootNoteFiles {
			if fi, err := os.Stat(filepath.Join(abs, note)); err == nil && !fi.IsDir() {
				out = append(out, filepath.Join(abs, note))
			}
		}
	}
	sort.Strings(out)
	return out
}

// GuidedTailPrompt is the concrete, paste-ready instruction for the
// model-assisted path: any harness agent in the donor repo can do what
// the heading split only guesses at - with judgment.
const GuidedTailPrompt = `You are doing a harnessator import. Read ./AGENTS.md (or CLAUDE.md)
and anything it references. Never edit the source file itself. Produce
one rule file per DISTINCT CONCERN under a fresh rules/ directory:
filenames rules/00-<short-slug>.md, each starting with '---' frontmatter
carrying a one-sentence 'description:'. Keep every rule's intent verbatim,
but: (1) strip secrets, tokens and personal paths (list what you removed),
(2) move project-local trivia into a final rules/90-local-notes.md,
(3) order files so identity/safety rules come first. When done run:
  harnessator harvest . --name my-tools --dry-run
then without --dry-run, and report both outputs plus your judgment calls.
Publish WITHOUT --with-notes: once rules/ files exist, that flag would
also heading-import the untouched fossil, duplicating your work.`

// bumpSuffix appends or increments a trailing -N (id collision relief).
func bumpSuffix(s string) string {
	i := len(s) - 1
	for i >= 0 && s[i] >= '0' && s[i] <= '9' {
		i--
	}
	if i >= 0 && s[i] == '-' && i < len(s)-1 {
		n := 0
		fmt.Sscanf(s[i+1:], "%d", &n)
		return fmt.Sprintf("%s-%d", s[:i+1], n+1)
	}
	return s + "-2"
}

// indentBlock prefixes every line, for pasting prompts into messages.
func indentBlock(s string) string {
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		lines[i] = "     " + ln
	}
	return strings.Join(lines, "\n")
}
