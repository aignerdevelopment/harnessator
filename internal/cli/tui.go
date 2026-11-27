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
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	"harnessator.org/harnessator/internal/app"
)

// tuiCmd is the first cut of the terminal UI: a keyboard menu over the
// SAME core API as the CLI and web GUI. It is deliberately stdlib-only
// (no full-screen redraw, no new dependency); the bubbletea-style polish
// is a deliberate later refinement, not a silent scope expansion.
//
// Commands: s setup | u update | v verify | d doctor | l list |
// e <ref> enable | x <ref> disable | w <ref> why | q quit.
func tuiCmd(stdin io.Reader, stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("tui", flag.ContinueOnError)
	fs.SetOutput(stderr)
	projectFile := fs.String("config", "harnessator.toml", "project config file")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	cfg := *projectFile
	in := bufio.NewScanner(stdin)

	printStatus(stdout, stderr, cfg)
	for {
		fmt.Fprint(stdout, "\n[s]etup [u]pdate [v]erify [d]octor [l]ist "+
			"[e]nable <ref> [x]disable <ref> [w]hy <ref> [q]uit > ")
		if !in.Scan() {
			return 0 // EOF
		}
		fields := strings.Fields(in.Text())
		if len(fields) == 0 {
			continue
		}
		switch cmd := strings.ToLower(fields[0]); cmd {
		case "q", "quit":
			fmt.Fprintln(stdout, "bye")
			return 0
		case "s":
			runAndShow(stdout, stderr, cfg, "setup")
		case "u":
			runAndShow(stdout, stderr, cfg, "update")
		case "v":
			runAndShow(stdout, stderr, cfg, "verify")
		case "d":
			runAndShow(stdout, stderr, cfg, "doctor")
		case "l":
			listCmd(stdout, stderr, []string{"--config", cfg})
		case "e", "x":
			if len(fields) < 2 {
				fmt.Fprintln(stderr, "need a reference")
				continue
			}
			target := enableCmd
			if cmd == "x" {
				target = disableCmd
			}
			if code := target(stdout, stderr, []string{"--config", cfg, fields[1]}); code != 0 {
				fmt.Fprintln(stderr, "(rejected)")
			}
		case "w":
			if len(fields) < 2 {
				fmt.Fprintln(stderr, "need a reference")
				continue
			}
			whyCmd(stdout, stderr, []string{"--config", cfg, fields[1]})
		default:
			fmt.Fprintln(stderr, "unknown command")
		}
	}
}

func runAndShow(stdout, stderr io.Writer, cfg, action string) {
	out, err := runAction(context.Background(), cfg, action)
	fmt.Fprint(stdout, out)
	if err != nil {
		fmt.Fprintln(stderr, err)
	}
}

// printStatus renders the compact project header.
func printStatus(stdout, stderr io.Writer, cfg string) {
	a, err := app.Open(context.Background(), cfg, app.ModeAuto)
	if err != nil {
		fmt.Fprintf(stderr, "harnessator tui - %v\n", err)
		return
	}
	fmt.Fprintf(stdout, "harnessator tui - %s\n", a.Project)
	fmt.Fprintf(stdout, "targets: %s\n", strings.Join(a.Cfg.Targets, ", "))
	var names []string
	for n := range a.Pins {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		p := a.Pins[n]
		dev := ""
		if b, ok := a.Dev[n]; ok {
			dev = "  DEV(" + b + ")"
		}
		fmt.Fprintf(stdout, "source: %-16s %s -> %s (%s)%s\n", n, p.Range, p.Resolved, shortSHA(p.SHA), dev)
	}
	byCat := a.Plan.ByCategory()
	var parts []string
	for _, c := range catOrder {
		if n := len(byCat[c]); n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, c))
		}
	}
	fmt.Fprintf(stdout, "enabled: %s | skips: %d\n", strings.Join(parts, ", "), len(a.Plan.Skips))
	for _, w := range append(a.GovWarnings, a.Plan.Warnings...) {
		fmt.Fprintf(stdout, "warning: %s\n", w)
	}
	if a.GovProvider != nil {
		fmt.Fprintf(stdout, "provider: %s (policy strict=%v)\n", a.GovProvider.Name,
			a.GovPolicy != nil && a.GovPolicy.Strict)
	}
}

var catOrder = []string{"rule", "skill", "prompt", "workflow", "agent", "hook", "tool", "mcp", "provider", "policy"}
