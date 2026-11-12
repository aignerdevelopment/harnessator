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
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"harnessator.org/harnessator/internal/app"
)

//go:embed web
var webFS embed.FS

// serveCmd runs a localhost-only web GUI over the same core API the CLI
// uses. Loopback bind by construction - there is no
// non-local listener.
// newServeMux builds the handler tree (loopback use; split out for tests).
func newServeMux(projectFile string) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) {
		st, err := stateJSON(r.Context(), projectFile)
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, st)
	})
	mux.HandleFunc("POST /api/action", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Action string `json:"action"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad json"})
			return
		}
		out, err := runAction(r.Context(), projectFile, req.Action)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "output": out + "\nerror: " + err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "output": out})
	})
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		page, err := webFS.ReadFile("web/index.html")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(page)
	})
	return mux
}

func serveCmd(stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	port := fs.Int("port", 7070, "listen port (127.0.0.1 only)")
	projectFile := fs.String("config", "harnessator.toml", "project config file")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}

	mux := newServeMux(*projectFile)
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "harnessator web GUI: http://%s (loopback only; ctrl-c stops)\n", ln.Addr())
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	if err := srv.Serve(ln); err != nil && !strings.Contains(err.Error(), "use of closed") {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

// --- helpers ---------------------------------------------------------------

// stateJSON summarizes the live project: pins, plan, governance, dev, lock.
func stateJSON(ctx context.Context, projectFile string) (map[string]any, error) {
	a, err := app.Open(ctx, projectFile, app.ModeAuto)
	if err != nil {
		return nil, err
	}
	type choice struct {
		ID, Category, Source, Reason, Desc string
	}
	var enabled, skipped []choice
	for _, ch := range a.Plan.Enabled {
		enabled = append(enabled, choice{
			ID: ch.QualifiedID(), Category: ch.Category, Source: ch.Source,
			Reason: ch.Reason, Desc: ch.Component.Description,
		})
	}
	for _, s := range a.Plan.Skips {
		skipped = append(skipped, choice{
			ID: s.Choice.QualifiedID(), Category: s.Choice.Category, Source: s.Choice.Source,
			Reason: s.Reason, Desc: s.Choice.Component.Description,
		})
	}
	pins := map[string]any{}
	for name, p := range a.Pins {
		pins[name] = map[string]any{"git": p.Git, "range": p.Range,
			"resolved": p.Resolved, "sha": shortSHA(p.SHA)}
	}
	gov := map[string]any{}
	if a.GovProvider != nil {
		gov["provider"] = map[string]any{"name": a.GovProvider.Name,
			"models": a.GovProvider.Models, "default": a.GovProvider.DefaultModel}
	}
	if a.GovPolicy != nil {
		gov["policy"] = map[string]any{"allowed": a.GovPolicy.Allowed,
			"strict": a.GovPolicy.Strict, "subagent": a.GovPolicy.SubagentModel,
			"sources": a.GovPolicy.Sources}
	}
	st := map[string]any{
		"project":    a.Project,
		"targets":    a.Cfg.Targets,
		"templates":  a.Cfg.Templates,
		"pins":       pins,
		"enabled":    enabled,
		"skips":      skipped,
		"governance": gov,
		"dev":        a.Dev,
		"warnings":   a.Plan.Warnings,
		"fromLock":   a.FromLock,
	}
	if a.Lock != nil {
		st["lockComponents"] = len(a.Lock.Components)
	}
	return st, nil
}

// runAction executes lifecycle verbs and captures the human report.
func runAction(ctx context.Context, projectFile, action string) (string, error) {
	var buf strings.Builder
	switch action {
	case "setup", "update":
		a, err := app.Open(ctx, projectFile, app.ModeFetch)
		if err != nil {
			return buf.String(), err
		}
		if err := a.Setup(ctx, false, &buf); err != nil {
			return buf.String(), err
		}
		return buf.String(), nil
	case "verify":
		a, err := app.Open(ctx, projectFile, app.ModeLocked)
		if err != nil {
			return buf.String(), err
		}
		problems, err := a.VerifyDrift()
		if err != nil {
			return buf.String(), err
		}
		if len(problems) == 0 {
			buf.WriteString("lock state verified ok\n")
			return buf.String(), nil
		}
		for _, p := range problems {
			buf.WriteString("drift: " + p + "\n")
		}
		return buf.String(), fmt.Errorf("state diverges from lock")
	case "doctor":
		a, err := app.Open(ctx, projectFile, app.ModeAuto)
		if err != nil {
			return buf.String(), err
		}
		problems := a.DoctorProblems()
		if len(problems) == 0 {
			buf.WriteString("doctor: all good\n")
		}
		for _, p := range problems {
			buf.WriteString("- " + p + "\n")
		}
		return buf.String(), nil
	default:
		return "", fmt.Errorf("unknown action %q", action)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}
