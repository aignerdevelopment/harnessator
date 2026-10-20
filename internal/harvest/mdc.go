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

import "strings"

// liftMDC is the exact inverse of the cursor adapter's generated rules -
// write and read are round-trip partners: it
// extracts description/globs/alwaysApply from the YAML-ish frontmatter
// (plus the harnessator marker line, if present) and returns the plain
// rule body plus hint.cursor data for the source manifest.
type mdcLift struct {
	Description string
	Globs       []string
	AlwaysApply *bool
	Body        string
}

func liftMDC(raw string) mdcLift {
	var l mdcLift
	lines := strings.Split(raw, "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	if len(lines) > 0 && strings.HasPrefix(lines[0], "# managed by harnessator") {
		lines = lines[1:]
	}
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		l.Body = raw
		return l
	}
	fm := []string{}
	rest := lines[1:]
	closed := false
	for i, ln := range rest {
		if strings.TrimSpace(ln) == "---" {
			fm = rest[:i]
			l.Body = strings.Join(rest[i+1:], "\n")
			closed = true
			break
		}
	}
	if !closed {
		l.Body = raw // malformed frontmatter: keep as-is, no hints
		return l
	}
	for _, ln := range fm {
		k, v, ok := strings.Cut(ln, ":")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch strings.TrimSpace(k) {
		case "description":
			l.Description = strings.Trim(v, "\"'")
		case "alwaysApply":
			if v == "true" || v == "false" {
				b := v == "true"
				l.AlwaysApply = &b
			}
		case "globs":
			l.Globs = parseInlineList(v)
		}
	}
	// drop leading blank lines from the body for tidiness
	l.Body = strings.TrimPrefix(l.Body, "\n")
	return l
}

func parseInlineList(v string) []string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "[")
	v = strings.TrimSuffix(v, "]")
	if strings.TrimSpace(v) == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(v, ",") {
		p = strings.Trim(strings.TrimSpace(p), "\"'")
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
