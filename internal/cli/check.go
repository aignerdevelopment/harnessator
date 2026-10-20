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
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"harnessator.org/harnessator/internal/app"
)

// checkCmd is the policy-enforcement CI helper:
// ONE command, OFFLINE (lock pins only, never fetches), exit code for
// pipelines, --json for machines. It answers: "is this checkout's agent
// configuration governed, intact and policy-valid?" without mutating
// anything. Also installs/removes the git-hook wrappers.
func checkCmd(stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	projectFile := fs.String("config", "harnessator.toml", "project config file")
	jsonOut := fs.Bool("json", false, "machine-readable output")
	quiet := fs.Bool("quiet", false, "silent unless something is wrong")
	install := fs.String("install-hook", "", "install git hook: pre-commit|pre-push")
	remove := fs.String("remove-hook", "", "remove a harnessator-installed git hook")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	if *install != "" || *remove != "" {
		return hookOp(stdout, stderr, *projectFile, *install, *remove)
	}

	problems, warnings, fail := runCheck(stderr, *projectFile)
	if *jsonOut {
		if problems == nil {
			problems = []string{}
		}
		out := map[string]any{"ok": fail == 0 && len(problems) == 0, "problems": problems}
		if warnings == nil {
			warnings = []string{}
		}
		out["warnings"] = warnings
		raw, _ := json.Marshal(out)
		fmt.Fprintln(stdout, string(raw))
		if len(problems) > 0 || fail > 0 {
			return 1
		}
		return 0
	}
	if !*quiet {
		for _, p := range problems {
			fmt.Fprintf(stderr, "check: FAIL %s\n", p)
		}
		for _, w := range warnings {
			fmt.Fprintf(stdout, "check: warn %s\n", w)
		}
	}
	if len(problems) > 0 || fail > 0 {
		return 1
	}
	if !*quiet {
		fmt.Fprintln(stdout, "check: ok (policy, lock digests, installed state)")
	}
	return 0
}

// runCheck collects enforcement problems; fail>0 counts hard failures
// (Open itself refusing, e.g. strict policy violations or missing lock).
func runCheck(stderr io.Writer, projectFile string) (problems, warnings []string, fail int) {
	// ModeLocked: pins come from harnessator.lock, no network. Strict
	// policy violations already fail Open (enforcement in the core).
	a, err := app.Open(context.Background(), projectFile, app.ModeLocked)
	if err != nil {
		return []string{strings.TrimSpace(err.Error())}, nil, 1
	}
	drift, err := a.VerifyDrift()
	if err != nil {
		return append(problems, "drift check failed: "+err.Error()), warnings, 1
	}
	problems = append(problems, drift...)
	warnings = append(warnings, a.GovWarnings...)
	return problems, warnings, 0
}

const hookMarker = ">>> harnessator hook (managed) >>>"
const hookEnd = "<<< harnessator hook <<<"

func hookOp(stdout, stderr io.Writer, projectFile, install, remove string) int {
	name := install
	action := "install"
	if remove != "" {
		name, action = remove, "remove"
	}
	switch name {
	case "pre-commit", "pre-push":
	default:
		fmt.Fprintf(stderr, "hook must be pre-commit or pre-push (got %q)\n", name)
		return 2
	}
	gitDir := filepath.Join(filepath.Dir(absOr(projectFile)), ".git", "hooks")
	if fi, err := os.Stat(gitDir); err != nil || !fi.IsDir() {
		fmt.Fprintf(stderr, "not a git repository (no .git/hooks): harnessator check hooks install beside the project's .git\n")
		return 1
	}
	path := filepath.Join(gitDir, name)
	if action == "remove" {
		raw, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			fmt.Fprintln(stdout, name+": no hook installed")
			return 0
		}
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if !strings.Contains(string(raw), hookMarker) {
			fmt.Fprintf(stderr, "%s: foreign hook (no harnessator marker) - refusing to delete\n", name)
			return 1
		}
		if err := os.Remove(path); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "%s: hook removed\n", name)
		return 0
	}
	script := "#!/bin/sh\n" + hookMarker + "\n# skip when this repo is not harnessator-managed\n[ -f harnessator.toml ] || exit 0\nharnessator check --quiet || exit 1\n" + hookEnd + "\n"
	if raw, err := os.ReadFile(path); err == nil {
		if !strings.Contains(string(raw), hookMarker) {
			fmt.Fprintf(stderr, "%s: foreign hook exists - refusing to clobber (remove it first)\n", name)
			return 1
		}
	} else if !os.IsNotExist(err) {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "%s: harnessator check hook installed (uninstall: harnessator check --remove-hook %s)\n", name, name)
	return 0
}

func absOr(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}
