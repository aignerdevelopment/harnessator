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
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestDocs is the machine-enforced doc-sync gate: every command verb
// and every long flag of this CLI must be documented in USAGE.md AND in
// the man pages, or CI fails.
// It reads the actual source (AST), so it cannot be forgotten the way a
// human checklist can.

func TestDocs(t *testing.T) {
	verbs := commandVerbs(t)
	flags := declaredFlags(t)

	usage := readFile(t, "../../USAGE.md")
	man1 := readFile(t, "../../man/man1/harnessator.1")
	man5 := readFile(t, "../../man/man5/harnessator.toml.5")
	man := man1 + man5

	for _, v := range verbs {
		if !strings.Contains(usage, "harnessator "+v) && !strings.Contains(usage, "harnessator `"+v) &&
			!strings.Contains(usage, "`harnessator "+v) {
			t.Errorf("docs drift: command %q not documented in USAGE.md", v)
		}
		re := regexp.MustCompile(`\b` + regexp.QuoteMeta(v) + `\b`)
		if !re.MatchString(man1) {
			t.Errorf("docs drift: command %q not documented in man/man1/harnessator.1", v)
		}
	}

	for _, f := range flags {
		if !strings.Contains(usage, "--"+f) {
			t.Errorf("docs drift: flag --%s not documented in USAGE.md", f)
		}
		if !strings.Contains(man, "Fl -"+f) {
			t.Errorf("docs drift: flag --%s not documented in the man pages", f)
		}
	}

	t.Logf("docs drift gate: %d commands, %d flags covered", len(verbs), len(flags))
}

// commandVerbs extracts the top-level command labels from RunIO's
// args[0] switch (the single source of truth for what exists).
func commandVerbs(t *testing.T) []string {
	t.Helper()
	src := readFile(t, "cli.go")
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "cli.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	ast.Inspect(file, func(n ast.Node) bool {
		ssw, ok2 := n.(*ast.SwitchStmt)
		if !ok2 {
			return true
		}
		if sel, isIdx := ssw.Tag.(*ast.Ident); !isIdx || sel.Name != "args" {
			// tag is args[0] - IndexExpr
			idx, isIndex := ssw.Tag.(*ast.IndexExpr)
			if !isIndex {
				return true
			}
			if id, isIdent := idx.X.(*ast.Ident); !isIdent || id.Name != "args" {
				return true
			}
		}
		for _, cl := range ssw.Body.List {
			cc, ok := cl.(*ast.CaseClause)
			if !ok {
				continue
			}
			for _, e := range cc.List {
				lit, ok := e.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				s, err := strconv.Unquote(lit.Value)
				if err == nil && regexp.MustCompile(`^[a-z][a-z-]*$`).MatchString(s) && s != "help" {
					out = append(out, s)
				}
			}
		}
		return true
	})
	sort.Strings(out)
	if len(out) < 15 {
		t.Fatalf("command discovery suspiciously small: %v", out)
	}
	return out
}

// declaredFlags finds every flag name registered via fs.String/Int/Bool...
// across the whole cli package.
func declaredFlags(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	set := map[string]bool{}
	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, e.Name(), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			recv, ok := sel.X.(*ast.Ident)
			if !ok || recv.Name != "fs" {
				return true
			}
			switch sel.Sel.Name {
			case "String", "Bool", "Int", "StringVar", "BoolVar", "IntVar":
			default:
				return true
			}
			if len(call.Args) == 0 {
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			if s, err := strconv.Unquote(lit.Value); err == nil {
				set[s] = true
			}
			return true
		})
	}
	var out []string
	for f := range set {
		out = append(out, f)
	}
	sort.Strings(out)
	if len(out) < 15 {
		t.Fatalf("flag discovery suspiciously small: %v", out)
	}
	return out
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
