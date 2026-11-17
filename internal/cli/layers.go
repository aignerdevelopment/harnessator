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
	"strings"

	"harnessator.org/harnessator/internal/app"
	"harnessator.org/harnessator/internal/schema"
)

// layersCmd shows the scope-layer files the project actually resolves
// from, and what each contributes - debugging the merge should not
// require reading five files by hand.
func layersCmd(stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("layers", flag.ContinueOnError)
	fs.SetOutput(stderr)
	projectFile := fs.String("config", "harnessator.toml", "project config file")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	a, err := app.Open(context.Background(), *projectFile, app.ModeAuto)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintln(stdout, "scope layers (ascending precedence):")
	for _, layer := range schema.Layers {
		cfg, present := a.Layers.Cfgs[layer]
		path := a.Layers.Paths[layer]
		if !present {
			fmt.Fprintf(stdout, "  %-9s absent    %s\n", layer, path)
			continue
		}
		var parts []string
		if n := len(cfg.Sources); n > 0 {
			parts = append(parts, fmt.Sprintf("%d source(s)", n))
		}
		if n := len(cfg.Templates); n > 0 {
			parts = append(parts, fmt.Sprintf("%d template(s)", n))
		}
		inc, exc := 0, 0
		for _, s := range cfg.Select {
			inc += len(s.Include)
			exc += len(s.Exclude)
		}
		if inc > 0 {
			parts = append(parts, fmt.Sprintf("%d include(s)", inc))
		}
		if exc > 0 {
			parts = append(parts, fmt.Sprintf("%d exclude(s)", exc))
		}
		if cfg.Provider != nil {
			parts = append(parts, "provider")
		}
		if cfg.Policy != nil && len(cfg.Policy.Profiles) > 0 {
			parts = append(parts, "policy")
		}
		if cfg.Corporate != nil {
			parts = append(parts, "corporate")
		}
		extra := ""
		if len(parts) > 0 {
			extra = "  (" + strings.Join(parts, ", ") + ")"
		}
		fmt.Fprintf(stdout, "  %-9s present  %s%s\n", layer, path, extra)
	}
	prov := "none"
	if a.Cfg.Provider != nil {
		prov = a.Cfg.Provider.Profile
	}
	fmt.Fprintf(stdout, "\neffective: targets=[%s] templates=%d sources=%d provider=%s\n",
		strings.Join(a.Cfg.Targets, " "), len(a.Cfg.Templates), len(a.Cfg.Sources), prov)
	return 0
}
